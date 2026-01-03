// Package camera provides camera access via Pollen's HTTP API
package camera

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Config holds camera client configuration
type Config struct {
	PollenURL string        // Base URL for Pollen API (e.g., "http://localhost:8000")
	Framerate int           // Target frames per second
	Width     int           // Desired width (0 = native)
	Height    int           // Desired height (0 = native)
	Quality   int           // JPEG quality (1-100)
	Timeout   time.Duration // HTTP request timeout
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		PollenURL: "http://localhost:8000",
		Framerate: 10, // 10 FPS for bandwidth
		Width:     640,
		Height:    480,
		Quality:   80,
		Timeout:   2 * time.Second,
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

// Client captures frames from Pollen's camera API
type Client struct {
	cfg        Config
	logger     *slog.Logger
	httpClient *http.Client

	mu        sync.RWMutex
	running   bool
	cancel    context.CancelFunc
	frameID   atomic.Uint64
	lastFrame *Frame

	// Callbacks
	onFrame func(Frame)

	// Stats
	framesCaptures atomic.Uint64
	frameErrors    atomic.Uint64
}

// NewClient creates a new camera client
func NewClient(cfg Config, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}

	return &Client{
		cfg:    cfg,
		logger: logger,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// OnFrame sets the callback for new frames
func (c *Client) OnFrame(callback func(Frame)) {
	c.mu.Lock()
	c.onFrame = callback
	c.mu.Unlock()
}

// Start begins capturing frames
func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = true

	ctx, c.cancel = context.WithCancel(ctx)
	c.mu.Unlock()

	c.logger.Info("camera client starting",
		"pollen_url", c.cfg.PollenURL,
		"framerate", c.cfg.Framerate,
		"resolution", fmt.Sprintf("%dx%d", c.cfg.Width, c.cfg.Height),
	)

	go c.captureLoop(ctx)
	return nil
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
	c.logger.Info("camera client stopped")
}

// captureLoop continuously fetches frames
func (c *Client) captureLoop(ctx context.Context) {
	interval := time.Duration(1000/c.cfg.Framerate) * time.Millisecond
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			frame, err := c.captureFrame(ctx)
			if err != nil {
				c.frameErrors.Add(1)
				c.logger.Debug("frame capture error", "error", err)
				continue
			}

			c.framesCaptures.Add(1)

			c.mu.Lock()
			c.lastFrame = frame
			callback := c.onFrame
			c.mu.Unlock()

			if callback != nil {
				callback(*frame)
			}
		}
	}
}

// captureFrame fetches a single frame from Pollen
func (c *Client) captureFrame(ctx context.Context) (*Frame, error) {
	// Try MJPEG snapshot endpoint first
	url := fmt.Sprintf("%s/api/video/snapshot", c.cfg.PollenURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	// Decode to get dimensions
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		// Try returning raw data if it's already JPEG
		return &Frame{
			Data:      data,
			Width:     c.cfg.Width,
			Height:    c.cfg.Height,
			Timestamp: time.Now(),
			FrameID:   c.frameID.Add(1),
		}, nil
	}

	bounds := img.Bounds()

	// Re-encode if quality adjustment needed
	if c.cfg.Quality > 0 && c.cfg.Quality < 100 {
		data, err = c.reencodeJPEG(img, c.cfg.Quality)
		if err != nil {
			return nil, fmt.Errorf("reencode: %w", err)
		}
	}

	return &Frame{
		Data:      data,
		Width:     bounds.Dx(),
		Height:    bounds.Dy(),
		Timestamp: time.Now(),
		FrameID:   c.frameID.Add(1),
	}, nil
}

// reencodeJPEG re-encodes an image with the specified quality
func (c *Client) reencodeJPEG(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality})
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GetLastFrame returns the most recently captured frame
func (c *Client) GetLastFrame() *Frame {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastFrame
}

// Stats returns capture statistics
func (c *Client) Stats() CameraStats {
	return CameraStats{
		FramesCaptured: c.framesCaptures.Load(),
		FrameErrors:    c.frameErrors.Load(),
		Running:        c.running,
	}
}

// CameraStats contains camera statistics
type CameraStats struct {
	FramesCaptured uint64 `json:"frames_captured"`
	FrameErrors    uint64 `json:"frame_errors"`
	Running        bool   `json:"running"`
}

