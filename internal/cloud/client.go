// Package cloud provides WebSocket connection to the cloud (go-reachy)
package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/teslashibe/go-eva/internal/protocol"
)

// Config holds cloud client configuration
type Config struct {
	URL              string        // WebSocket URL (e.g., "ws://cloud.example.com/ws/robot")
	ReconnectBackoff time.Duration // Initial reconnect delay
	MaxBackoff       time.Duration // Maximum reconnect delay
	PingInterval     time.Duration // Ping interval for keepalive
	WriteTimeout     time.Duration // Write timeout
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		URL:              "ws://localhost:8080/ws/robot",
		ReconnectBackoff: 1 * time.Second,
		MaxBackoff:       30 * time.Second,
		PingInterval:     10 * time.Second,
		WriteTimeout:     5 * time.Second,
	}
}

// Client manages WebSocket connection to go-reachy cloud
type Client struct {
	cfg    Config
	logger *slog.Logger

	mu        sync.Mutex
	conn      *websocket.Conn
	connected bool
	cancel    context.CancelFunc

	// Callbacks for incoming messages
	onMotorCommand   func(protocol.MotorCommand)
	onEmotionCommand func(protocol.EmotionCommand)
	onSpeakData      func(protocol.SpeakData)
	onConfigUpdate   func(protocol.ConfigUpdate)

	// Stats
	messagesSent     atomic.Uint64
	messagesReceived atomic.Uint64
	reconnects       atomic.Uint64
}

// NewClient creates a new cloud client
func NewClient(cfg Config, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}

	return &Client{
		cfg:    cfg,
		logger: logger,
	}
}

// OnMotorCommand sets the callback for motor commands
func (c *Client) OnMotorCommand(callback func(protocol.MotorCommand)) {
	c.mu.Lock()
	c.onMotorCommand = callback
	c.mu.Unlock()
}

// OnEmotionCommand sets the callback for emotion commands
func (c *Client) OnEmotionCommand(callback func(protocol.EmotionCommand)) {
	c.mu.Lock()
	c.onEmotionCommand = callback
	c.mu.Unlock()
}

// OnSpeakData sets the callback for TTS audio
func (c *Client) OnSpeakData(callback func(protocol.SpeakData)) {
	c.mu.Lock()
	c.onSpeakData = callback
	c.mu.Unlock()
}

// OnConfigUpdate sets the callback for config updates
func (c *Client) OnConfigUpdate(callback func(protocol.ConfigUpdate)) {
	c.mu.Lock()
	c.onConfigUpdate = callback
	c.mu.Unlock()
}

// Connect establishes WebSocket connection to cloud
func (c *Client) Connect(ctx context.Context) error {
	ctx, c.cancel = context.WithCancel(ctx)

	go c.connectionLoop(ctx)
	return nil
}

// connectionLoop manages connection with auto-reconnect
func (c *Client) connectionLoop(ctx context.Context) {
	backoff := c.cfg.ReconnectBackoff

	for {
		select {
		case <-ctx.Done():
			c.closeConnection()
			return
		default:
		}

		err := c.connect(ctx)
		if err != nil {
			c.logger.Warn("cloud connection failed",
				"error", err,
				"retry_in", backoff,
			)

			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return
			}

			// Exponential backoff
			backoff *= 2
			if backoff > c.cfg.MaxBackoff {
				backoff = c.cfg.MaxBackoff
			}
			c.reconnects.Add(1)
			continue
		}

		// Reset backoff on successful connection
		backoff = c.cfg.ReconnectBackoff

		// Read messages until error
		c.readLoop(ctx)
	}
}

// connect establishes the WebSocket connection
func (c *Client) connect(ctx context.Context) error {
	c.logger.Info("connecting to cloud", "url", c.cfg.URL)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, c.cfg.URL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.mu.Unlock()

	c.logger.Info("connected to cloud")

	// Start ping goroutine
	go c.pingLoop(ctx)

	return nil
}

// pingLoop sends periodic pings
func (c *Client) pingLoop(ctx context.Context) {
	ticker := time.NewTicker(c.cfg.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.mu.Lock()
			if c.conn == nil {
				c.mu.Unlock()
				return
			}
			conn := c.conn
			c.mu.Unlock()

			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				c.logger.Debug("ping failed", "error", err)
				return
			}
		}
	}
}

// readLoop reads messages from cloud
func (c *Client) readLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()

		if conn == nil {
			return
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			c.logger.Warn("read error", "error", err)
			c.closeConnection()
			return
		}

		c.messagesReceived.Add(1)
		c.handleMessage(data)
	}
}

// handleMessage processes incoming messages
func (c *Client) handleMessage(data []byte) {
	msg, err := protocol.ParseMessage(data)
	if err != nil {
		c.logger.Warn("parse message error", "error", err)
		return
	}

	c.mu.Lock()
	motorCb := c.onMotorCommand
	emotionCb := c.onEmotionCommand
	speakCb := c.onSpeakData
	configCb := c.onConfigUpdate
	c.mu.Unlock()

	switch msg.Type {
	case protocol.TypeMotor:
		if motorCb != nil {
			cmd, err := msg.GetMotorCommand()
			if err == nil {
				motorCb(*cmd)
			}
		}

	case protocol.TypeEmotion:
		if emotionCb != nil {
			cmd, err := msg.GetEmotionCommand()
			if err == nil {
				emotionCb(*cmd)
			}
		}

	case protocol.TypeSpeak:
		if speakCb != nil {
			data, err := msg.GetSpeakData()
			if err == nil {
				speakCb(*data)
			}
		}

	case protocol.TypeConfig:
		if configCb != nil {
			cfg, err := msg.GetConfigUpdate()
			if err == nil {
				configCb(*cfg)
			}
		}

	case protocol.TypePing:
		// Respond with pong
		pong := &protocol.Message{Type: protocol.TypePong, Timestamp: time.Now().UnixMilli()}
		c.SendMessage(pong)
	}
}

// SendMessage sends a message to cloud
func (c *Client) SendMessage(msg *protocol.Message) error {
	c.mu.Lock()
	conn := c.conn
	connected := c.connected
	c.mu.Unlock()

	if !connected || conn == nil {
		return fmt.Errorf("not connected")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	conn.SetWriteDeadline(time.Now().Add(c.cfg.WriteTimeout))
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		c.logger.Warn("send error", "error", err)
		c.closeConnection()
		return fmt.Errorf("write: %w", err)
	}

	c.messagesSent.Add(1)
	return nil
}

// SendFrame sends a video frame to cloud
func (c *Client) SendFrame(width, height int, jpegData []byte, frameID uint64) error {
	msg, err := protocol.NewFrameMessage(width, height, jpegData, frameID)
	if err != nil {
		return err
	}
	return c.SendMessage(msg)
}

// SendDOA sends DOA data to cloud
func (c *Client) SendDOA(angle, smoothedAngle float64, speaking, speakingLatched bool, confidence float64) error {
	msg, err := protocol.NewDOAMessage(angle, smoothedAngle, speaking, speakingLatched, confidence)
	if err != nil {
		return err
	}
	return c.SendMessage(msg)
}

// closeConnection closes the WebSocket connection
func (c *Client) closeConnection() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connected = false
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

// Close shuts down the client
func (c *Client) Close() error {
	if c.cancel != nil {
		c.cancel()
	}
	c.closeConnection()
	return nil
}

// IsConnected returns connection status
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// Stats returns client statistics
type Stats struct {
	Connected        bool   `json:"connected"`
	MessagesSent     uint64 `json:"messages_sent"`
	MessagesReceived uint64 `json:"messages_received"`
	Reconnects       uint64 `json:"reconnects"`
}

// GetStats returns client statistics
func (c *Client) GetStats() Stats {
	c.mu.Lock()
	connected := c.connected
	c.mu.Unlock()

	return Stats{
		Connected:        connected,
		MessagesSent:     c.messagesSent.Load(),
		MessagesReceived: c.messagesReceived.Load(),
		Reconnects:       c.reconnects.Load(),
	}
}

