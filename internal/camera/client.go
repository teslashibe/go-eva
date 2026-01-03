// Package camera provides WebRTC video capture from Reachy Mini
package camera

import (
	"context"
	"log/slog"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

// Config holds camera client configuration
type Config struct {
	PollenURL string        // Base URL for Pollen API (e.g., "http://localhost:8000")
	Framerate int           // Target frames per second (for rate limiting callbacks)
	Width     int           // Desired width (informational only)
	Height    int           // Desired height (informational only)
	Quality   int           // JPEG quality (1-100)
	Timeout   time.Duration // Connection timeout
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		PollenURL: "http://localhost:8000",
		Framerate: 10, // 10 FPS for bandwidth
		Width:     640,
		Height:    480,
		Quality:   80,
		Timeout:   15 * time.Second,
	}
}

// Frame represents a captured video frame
type Frame struct {
	Data      []byte    // JPEG encoded
	Width     int       // Actual width
	Height    int       // Actual height
	Timestamp time.Time // Capture time
	FrameID   uint64    // Sequential frame ID
}

// Client captures frames via WebRTC from Pollen
type Client struct {
	cfg    Config
	logger *slog.Logger

	webrtc    *WebRTCClient
	robotIP   string

	mu        sync.RWMutex
	running   bool
	cancel    context.CancelFunc
	lastFrame *Frame

	// Callbacks
	onFrame func(Frame)

	// Stats
	framesCaptured atomic.Uint64
	frameErrors    atomic.Uint64
}

// NewClient creates a new camera client
func NewClient(cfg Config, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}

	// Extract robot IP from Pollen URL
	robotIP := "localhost"
	if u, err := url.Parse(cfg.PollenURL); err == nil {
		robotIP = u.Hostname()
	}

	return &Client{
		cfg:     cfg,
		logger:  logger,
		robotIP: robotIP,
	}
}

// OnFrame sets the callback for new frames
func (c *Client) OnFrame(callback func(Frame)) {
	c.mu.Lock()
	c.onFrame = callback
	c.mu.Unlock()
}

// Start begins capturing frames via WebRTC
func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}

	ctx, c.cancel = context.WithCancel(ctx)
	c.mu.Unlock()

	c.logger.Info("camera client starting (WebRTC)",
		"robot_ip", c.robotIP,
		"framerate", c.cfg.Framerate,
	)

	// Create WebRTC client
	c.webrtc = NewWebRTCClient(c.robotIP, c.logger)

	// Set up frame callback
	c.webrtc.OnFrame(func(frame Frame) {
		c.framesCaptured.Add(1)

		c.mu.Lock()
		c.lastFrame = &frame
		callback := c.onFrame
		c.mu.Unlock()

		if callback != nil {
			callback(frame)
		}
	})

	// Connect in background (retries on failure)
	go c.connectLoop(ctx)

	c.mu.Lock()
	c.running = true
	c.mu.Unlock()

	return nil
}

// connectLoop attempts to connect and reconnects on failure
func (c *Client) connectLoop(ctx context.Context) {
	backoff := time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := c.webrtc.Connect()
		if err != nil {
			c.frameErrors.Add(1)
			c.logger.Warn("WebRTC connection failed", "error", err, "retry_in", backoff)

			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return
			}

			// Exponential backoff
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			continue
		}

		// Reset backoff on success
		backoff = time.Second
		c.logger.Info("WebRTC connected successfully")

		// Wait until closed or context cancelled
		for c.webrtc.IsConnected() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				// Check connection periodically
			}
		}

		c.logger.Warn("WebRTC connection lost, reconnecting...")
	}
}

// Stop stops capturing frames
func (c *Client) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return
	}

	c.running = false
	if c.cancel != nil {
		c.cancel()
	}
	if c.webrtc != nil {
		c.webrtc.Close()
	}
	c.logger.Info("camera client stopped")
}

// GetLastFrame returns the most recently captured frame
func (c *Client) GetLastFrame() *Frame {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastFrame
}

// Stats returns capture statistics
func (c *Client) Stats() CameraStats {
	c.mu.RLock()
	running := c.running
	c.mu.RUnlock()

	connected := false
	if c.webrtc != nil {
		connected = c.webrtc.IsConnected()
	}

	return CameraStats{
		FramesCaptured: c.framesCaptured.Load(),
		FrameErrors:    c.frameErrors.Load(),
		Running:        running,
		Connected:      connected,
	}
}

// CameraStats contains camera statistics
type CameraStats struct {
	FramesCaptured uint64 `json:"frames_captured"`
	FrameErrors    uint64 `json:"frame_errors"`
	Running        bool   `json:"running"`
	Connected      bool   `json:"connected"`
}
