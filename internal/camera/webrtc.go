// Package camera provides WebRTC video streaming from Reachy Mini
package camera

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
)

// WebRTCClient connects to Reachy's WebRTC video stream via GStreamer signalling
type WebRTCClient struct {
	robotIP       string
	signallingURL string
	logger        *slog.Logger

	ws      *websocket.Conn
	pc      *webrtc.PeerConnection
	wsMutex sync.Mutex

	myPeerID   string
	producerID string
	sessionID  string

	// Latest decoded frame
	latestFrame []byte
	frameMutex  sync.RWMutex
	frameReady  chan struct{}
	frameID     uint64

	// Rate limiting for decoding
	lastDecode  time.Time
	minInterval time.Duration
	decodeMutex sync.Mutex

	// Callbacks
	onFrame func(Frame)

	connected bool
	closed    bool
}

// NewWebRTCClient creates a new WebRTC video client
func NewWebRTCClient(robotIP string, logger *slog.Logger) *WebRTCClient {
	if logger == nil {
		logger = slog.Default()
	}

	return &WebRTCClient{
		robotIP:       robotIP,
		signallingURL: fmt.Sprintf("ws://%s:8443", robotIP),
		logger:        logger,
		frameReady:    make(chan struct{}, 1),
		minInterval:   100 * time.Millisecond, // 10 FPS max decode rate
		lastDecode:    time.Now(),
	}
}

// OnFrame sets the callback for new frames
func (c *WebRTCClient) OnFrame(callback func(Frame)) {
	c.frameMutex.Lock()
	c.onFrame = callback
	c.frameMutex.Unlock()
}

// Connect establishes the WebRTC connection
func (c *WebRTCClient) Connect() error {
	c.logger.Info("connecting to WebRTC signalling", "url", c.signallingURL)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	var err error
	c.ws, _, err = dialer.Dial(c.signallingURL, nil)
	if err != nil {
		return fmt.Errorf("signalling connect failed: %w", err)
	}

	// Wait for welcome message
	if err := c.waitForWelcome(); err != nil {
		return fmt.Errorf("welcome failed: %w", err)
	}
	c.logger.Debug("got peer ID", "peer_id", c.myPeerID[:8])

	// Get producer list
	if err := c.findProducer(); err != nil {
		return fmt.Errorf("find producer failed: %w", err)
	}
	c.logger.Debug("found producer", "producer_id", c.producerID[:8])

	// Create peer connection
	if err := c.createPeerConnection(); err != nil {
		return fmt.Errorf("peer connection failed: %w", err)
	}

	// Start session
	if err := c.startSession(); err != nil {
		return fmt.Errorf("start session failed: %w", err)
	}

	// Start signalling handler
	go c.handleSignalling()

	// Wait for connection
	c.logger.Info("waiting for video track...")
	select {
	case <-c.frameReady:
		c.logger.Info("WebRTC video connected")
	case <-time.After(15 * time.Second):
		return fmt.Errorf("timeout waiting for video")
	}

	c.connected = true
	return nil
}

func (c *WebRTCClient) waitForWelcome() error {
	c.ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, msg, err := c.ws.ReadMessage()
	c.ws.SetReadDeadline(time.Time{})

	if err != nil {
		return err
	}

	var welcome struct {
		Type   string `json:"type"`
		PeerID string `json:"peerId"`
	}
	if err := json.Unmarshal(msg, &welcome); err != nil {
		return err
	}
	if welcome.Type != "welcome" {
		return fmt.Errorf("expected welcome, got %s", welcome.Type)
	}
	c.myPeerID = welcome.PeerID
	return nil
}

func (c *WebRTCClient) findProducer() error {
	c.wsMutex.Lock()
	err := c.ws.WriteJSON(map[string]string{"type": "list"})
	c.wsMutex.Unlock()
	if err != nil {
		return err
	}

	c.ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, msg, err := c.ws.ReadMessage()
	c.ws.SetReadDeadline(time.Time{})
	if err != nil {
		return err
	}

	var listResp struct {
		Type      string `json:"type"`
		Producers []struct {
			ID   string            `json:"id"`
			Meta map[string]string `json:"meta"`
		} `json:"producers"`
	}
	if err := json.Unmarshal(msg, &listResp); err != nil {
		return err
	}

	for _, p := range listResp.Producers {
		if name, ok := p.Meta["name"]; ok && name == "reachymini" {
			c.producerID = p.ID
			return nil
		}
	}
	return fmt.Errorf("reachymini producer not found in %d producers", len(listResp.Producers))
}

func (c *WebRTCClient) createPeerConnection() error {
	config := webrtc.Configuration{}

	var err error
	c.pc, err = webrtc.NewPeerConnection(config)
	if err != nil {
		return err
	}

	// We want to receive video
	if _, err = c.pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		return err
	}

	// Handle incoming video tracks
	c.pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		c.logger.Debug("got track", "kind", track.Kind().String(), "codec", track.Codec().MimeType)
		if track.Kind() == webrtc.RTPCodecTypeVideo {
			go c.handleVideoTrack(track)
		}
	})

	// Handle ICE candidates
	c.pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate != nil {
			c.sendICECandidate(candidate)
		}
	})

	c.pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		c.logger.Debug("connection state changed", "state", state.String())
	})

	return nil
}

func (c *WebRTCClient) startSession() error {
	c.wsMutex.Lock()
	err := c.ws.WriteJSON(map[string]string{
		"type":   "startSession",
		"peerId": c.producerID,
	})
	c.wsMutex.Unlock()
	return err
}

func (c *WebRTCClient) handleSignalling() {
	for !c.closed {
		_, msg, err := c.ws.ReadMessage()
		if err != nil {
			if !c.closed {
				c.logger.Warn("signalling error", "error", err)
			}
			return
		}

		var baseMsg struct {
			Type      string `json:"type"`
			SessionID string `json:"sessionId"`
		}
		json.Unmarshal(msg, &baseMsg)

		switch baseMsg.Type {
		case "sessionStarted":
			c.sessionID = baseMsg.SessionID

		case "peer":
			c.handlePeerMessage(msg)

		case "endSession":
			return
		}
	}
}

func (c *WebRTCClient) handlePeerMessage(msg []byte) {
	var peerMsg map[string]interface{}
	json.Unmarshal(msg, &peerMsg)

	// Check for SDP
	if sdpData, ok := peerMsg["sdp"]; ok {
		sdpMap := sdpData.(map[string]interface{})
		sdpType := sdpMap["type"].(string)
		sdpStr := sdpMap["sdp"].(string)

		if sdpType == "offer" {
			offer := webrtc.SessionDescription{
				Type: webrtc.SDPTypeOffer,
				SDP:  sdpStr,
			}

			if err := c.pc.SetRemoteDescription(offer); err != nil {
				c.logger.Warn("SetRemoteDescription error", "error", err)
				return
			}

			answer, err := c.pc.CreateAnswer(nil)
			if err != nil {
				c.logger.Warn("CreateAnswer error", "error", err)
				return
			}

			if err := c.pc.SetLocalDescription(answer); err != nil {
				c.logger.Warn("SetLocalDescription error", "error", err)
				return
			}

			c.sendSDP(answer)
		}
	}

	// Check for ICE
	if iceData, ok := peerMsg["ice"]; ok {
		iceMap := iceData.(map[string]interface{})
		candidate := iceMap["candidate"].(string)

		var sdpMid string
		if mid, ok := iceMap["sdpMid"]; ok && mid != nil {
			sdpMid = mid.(string)
		}

		var sdpMLineIndex uint16
		if idx, ok := iceMap["sdpMLineIndex"]; ok && idx != nil {
			sdpMLineIndex = uint16(idx.(float64))
		}

		c.pc.AddICECandidate(webrtc.ICECandidateInit{
			Candidate:     candidate,
			SDPMid:        &sdpMid,
			SDPMLineIndex: &sdpMLineIndex,
		})
	}
}

func (c *WebRTCClient) sendSDP(sdp webrtc.SessionDescription) {
	msg := map[string]interface{}{
		"type":      "peer",
		"sessionId": c.sessionID,
		"sdp": map[string]string{
			"type": sdp.Type.String(),
			"sdp":  sdp.SDP,
		},
	}
	c.wsMutex.Lock()
	c.ws.WriteJSON(msg)
	c.wsMutex.Unlock()
}

func (c *WebRTCClient) sendICECandidate(candidate *webrtc.ICECandidate) {
	if c.sessionID == "" {
		return
	}

	init := candidate.ToJSON()
	msg := map[string]interface{}{
		"type":      "peer",
		"sessionId": c.sessionID,
		"ice": map[string]interface{}{
			"candidate":     init.Candidate,
			"sdpMid":        init.SDPMid,
			"sdpMLineIndex": init.SDPMLineIndex,
		},
	}
	c.wsMutex.Lock()
	c.ws.WriteJSON(msg)
	c.wsMutex.Unlock()
}

func (c *WebRTCClient) handleVideoTrack(track *webrtc.TrackRemote) {
	// Signal that we got video
	select {
	case c.frameReady <- struct{}{}:
	default:
	}

	// H264 depacketizer
	var h264Buffer bytes.Buffer
	var frameBuffer bytes.Buffer
	hasKeyframe := false
	var keyframeBuffer bytes.Buffer
	frameCount := 0

	for !c.closed {
		rtpPacket, _, err := track.ReadRTP()
		if err != nil {
			return
		}

		payload := rtpPacket.Payload
		if len(payload) < 2 {
			continue
		}

		// Parse H264 NAL unit header
		nalType := payload[0] & 0x1F

		switch {
		case nalType >= 1 && nalType <= 23:
			// Single NAL unit
			h264Buffer.Write([]byte{0x00, 0x00, 0x00, 0x01})
			h264Buffer.Write(payload)
			if nalType == 5 || nalType == 7 || nalType == 8 {
				hasKeyframe = true
			}

		case nalType == 28: // FU-A (Fragmentation Unit)
			fuHeader := payload[1]
			startBit := (fuHeader & 0x80) != 0
			endBit := (fuHeader & 0x40) != 0
			fragNalType := fuHeader & 0x1F

			if startBit {
				h264Buffer.Write([]byte{0x00, 0x00, 0x00, 0x01})
				h264Buffer.WriteByte((payload[0] & 0xE0) | fragNalType)
				if fragNalType == 5 {
					hasKeyframe = true
				}
			}
			h264Buffer.Write(payload[2:])

			if endBit {
				frameBuffer.Write(h264Buffer.Bytes())
				h264Buffer.Reset()
				if hasKeyframe {
					keyframeBuffer.Reset()
					keyframeBuffer.Write(frameBuffer.Bytes())
				}
			}

		case nalType == 24: // STAP-A
			offset := 1
			for offset < len(payload)-2 {
				nalSize := int(payload[offset])<<8 | int(payload[offset+1])
				offset += 2
				if offset+nalSize > len(payload) {
					break
				}
				h264Buffer.Write([]byte{0x00, 0x00, 0x00, 0x01})
				h264Buffer.Write(payload[offset : offset+nalSize])
				if nalSize > 0 {
					aggNalType := payload[offset] & 0x1F
					if aggNalType == 5 || aggNalType == 7 || aggNalType == 8 {
						hasKeyframe = true
					}
				}
				offset += nalSize
			}
		}

		// Decode when we have a keyframe and rate limit allows
		if hasKeyframe && keyframeBuffer.Len() > 1000 {
			c.decodeMutex.Lock()
			if time.Since(c.lastDecode) >= c.minInterval {
				c.lastDecode = time.Now()
				c.decodeMutex.Unlock()

				jpegData := c.decodeH264ToJPEG(keyframeBuffer.Bytes())
				if len(jpegData) > 1000 {
					c.frameID++
					frame := Frame{
						Data:      jpegData,
						Width:     640, // Will be updated from actual decode
						Height:    480,
						Timestamp: time.Now(),
						FrameID:   c.frameID,
					}

					c.frameMutex.Lock()
					c.latestFrame = jpegData
					callback := c.onFrame
					c.frameMutex.Unlock()

					if callback != nil {
						callback(frame)
					}

					frameCount++
					if frameCount%100 == 1 {
						c.logger.Debug("decoded frame", "count", frameCount, "size", len(jpegData))
					}
				}

				frameBuffer.Reset()
				if frameCount%30 == 0 {
					hasKeyframe = false
				}
			} else {
				c.decodeMutex.Unlock()
			}
		}
	}
}

func (c *WebRTCClient) decodeH264ToJPEG(h264Data []byte) []byte {
	if len(h264Data) < 100 {
		return nil
	}

	// Use ffmpeg pipe-based decoding
	cmd := exec.Command("ffmpeg",
		"-f", "h264",
		"-i", "pipe:0",
		"-vframes", "1",
		"-f", "image2pipe",
		"-vcodec", "mjpeg",
		"-q:v", "3",
		"pipe:1",
	)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil
	}

	if err := cmd.Start(); err != nil {
		return nil
	}

	go func() {
		stdin.Write(h264Data)
		stdin.Close()
	}()

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		cmd.Process.Kill()
		return nil
	}

	jpegData := stdout.Bytes()
	if len(jpegData) > 1000 && !c.isGrayFrame(jpegData) {
		return jpegData
	}

	return nil
}

func (c *WebRTCClient) isGrayFrame(jpegData []byte) bool {
	if len(jpegData) < 1000 {
		return true
	}

	img, err := jpeg.Decode(bytes.NewReader(jpegData))
	if err != nil {
		return true
	}

	bounds := img.Bounds()
	if bounds.Dx() < 100 || bounds.Dy() < 100 {
		return true
	}

	var rSum, gSum, bSum, samples int
	for y := bounds.Min.Y; y < bounds.Max.Y; y += bounds.Dy() / 10 {
		for x := bounds.Min.X; x < bounds.Max.X; x += bounds.Dx() / 10 {
			r, g, b, _ := img.At(x, y).RGBA()
			rSum += int(r >> 8)
			gSum += int(g >> 8)
			bSum += int(b >> 8)
			samples++
		}
	}

	if samples == 0 {
		return true
	}

	avgR := rSum / samples
	avgG := gSum / samples
	avgB := bSum / samples

	// Gray frames have low brightness
	if avgR < 30 && avgG < 30 && avgB < 30 {
		return true
	}

	return false
}

// GetFrame returns the latest video frame as JPEG bytes
func (c *WebRTCClient) GetFrame() ([]byte, error) {
	c.frameMutex.RLock()
	defer c.frameMutex.RUnlock()

	if c.latestFrame == nil {
		return nil, fmt.Errorf("no frame available")
	}

	frame := make([]byte, len(c.latestFrame))
	copy(frame, c.latestFrame)
	return frame, nil
}

// IsConnected returns true if WebRTC is connected
func (c *WebRTCClient) IsConnected() bool {
	return c.connected && !c.closed
}

// Close closes the WebRTC connection
func (c *WebRTCClient) Close() {
	c.closed = true
	if c.pc != nil {
		c.pc.Close()
	}
	if c.ws != nil {
		c.ws.Close()
	}
	c.logger.Info("WebRTC client closed")
}

