// Package pollen provides HTTP client for Pollen robot daemon
package pollen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Config holds Pollen client configuration
type Config struct {
	BaseURL     string        // Base URL for Pollen API (e.g., "http://localhost:8000")
	Timeout     time.Duration // HTTP request timeout
	RateLimitHz int           // Max commands per second (0 = unlimited)
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		BaseURL:     "http://localhost:8000",
		Timeout:     2 * time.Second,
		RateLimitHz: 30, // 30 Hz max
	}
}

// HeadTarget represents the target head pose
type HeadTarget struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Z     float64 `json:"z"`
	Roll  float64 `json:"roll"`
	Pitch float64 `json:"pitch"`
	Yaw   float64 `json:"yaw"`
}

// FullBodyTarget is the format expected by Pollen API
type FullBodyTarget struct {
	TargetHeadPose HeadTarget `json:"target_head_pose"`
	TargetAntennas [2]float64 `json:"target_antennas"`
	TargetBodyYaw  float64    `json:"target_body_yaw"`
}

// EmotionRequest is the format for playing emotions
type EmotionRequest struct {
	Name     string  `json:"name"`
	Duration float64 `json:"duration,omitempty"`
}

// Client is the HTTP client for Pollen robot daemon
type Client struct {
	cfg        Config
	logger     *slog.Logger
	httpClient *http.Client

	// Rate limiting
	mu            sync.Mutex
	lastCommandAt time.Time
	minInterval   time.Duration

	// Stats
	commandsSent  atomic.Uint64
	commandErrors atomic.Uint64
	emotionsSent  atomic.Uint64
	emotionErrors atomic.Uint64
}

// NewClient creates a new Pollen client
func NewClient(cfg Config, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}

	var minInterval time.Duration
	if cfg.RateLimitHz > 0 {
		minInterval = time.Second / time.Duration(cfg.RateLimitHz)
	}

	return &Client{
		cfg:    cfg,
		logger: logger,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		minInterval: minInterval,
	}
}

// SetTarget sends a movement command to the robot
func (c *Client) SetTarget(ctx context.Context, head HeadTarget, antennas [2]float64, bodyYaw float64) error {
	// Rate limiting
	if c.minInterval > 0 {
		c.mu.Lock()
		elapsed := time.Since(c.lastCommandAt)
		if elapsed < c.minInterval {
			c.mu.Unlock()
			return nil // Skip this command to maintain rate limit
		}
		c.lastCommandAt = time.Now()
		c.mu.Unlock()
	}

	target := FullBodyTarget{
		TargetHeadPose: head,
		TargetAntennas: antennas,
		TargetBodyYaw:  bodyYaw,
	}

	data, err := json.Marshal(target)
	if err != nil {
		return fmt.Errorf("marshal target: %w", err)
	}

	url := c.cfg.BaseURL + "/api/move/set_target"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.commandErrors.Add(1)
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.commandErrors.Add(1)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	c.commandsSent.Add(1)
	return nil
}

// PlayEmotion triggers an emotion animation
func (c *Client) PlayEmotion(ctx context.Context, name string, duration float64) error {
	emotion := EmotionRequest{
		Name:     name,
		Duration: duration,
	}

	data, err := json.Marshal(emotion)
	if err != nil {
		return fmt.Errorf("marshal emotion: %w", err)
	}

	url := c.cfg.BaseURL + "/api/emotion/play"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.emotionErrors.Add(1)
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		c.emotionErrors.Add(1)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	c.emotionsSent.Add(1)
	c.logger.Debug("emotion played", "name", name)
	return nil
}

// GetStatus fetches the current robot status
func (c *Client) GetStatus(ctx context.Context) (map[string]interface{}, error) {
	url := c.cfg.BaseURL + "/api/daemon/status"
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
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var status map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return status, nil
}

// StartDaemon starts the robot daemon if not running
func (c *Client) StartDaemon(ctx context.Context) error {
	url := c.cfg.BaseURL + "/api/daemon/start"
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	c.logger.Info("daemon started")
	return nil
}

// Stats contains client statistics
type Stats struct {
	CommandsSent  uint64 `json:"commands_sent"`
	CommandErrors uint64 `json:"command_errors"`
	EmotionsSent  uint64 `json:"emotions_sent"`
	EmotionErrors uint64 `json:"emotion_errors"`
}

// GetStats returns client statistics
func (c *Client) GetStats() Stats {
	return Stats{
		CommandsSent:  c.commandsSent.Load(),
		CommandErrors: c.commandErrors.Load(),
		EmotionsSent:  c.emotionsSent.Load(),
		EmotionErrors: c.emotionErrors.Load(),
	}
}

// IsHealthy checks if Pollen daemon is reachable
func (c *Client) IsHealthy(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	_, err := c.GetStatus(ctx)
	return err == nil
}

