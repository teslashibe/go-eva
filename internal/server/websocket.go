package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"

	"github.com/teslashibe/go-eva/internal/doa"
)

// WSHub manages WebSocket connections and broadcasts DOA updates
type WSHub struct {
	tracker *doa.Tracker
	logger  *slog.Logger

	mu      sync.RWMutex
	clients map[*websocket.Conn]struct{}

	cancel context.CancelFunc
	done   chan struct{}
}

// NewWSHub creates a new WebSocket hub
func NewWSHub(tracker *doa.Tracker, logger *slog.Logger) *WSHub {
	return &WSHub{
		tracker: tracker,
		logger:  logger,
		clients: make(map[*websocket.Conn]struct{}),
		done:    make(chan struct{}),
	}
}

// Message represents a WebSocket message
type Message struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// Run starts the broadcast loop
func (h *WSHub) Run(ctx context.Context) {
	ctx, h.cancel = context.WithCancel(ctx)
	defer close(h.done)

	ticker := time.NewTicker(100 * time.Millisecond) // 10Hz
	defer ticker.Stop()

	var lastSpeaking bool

	h.logger.Info("websocket hub started")

	for {
		select {
		case <-ctx.Done():
			h.logger.Info("websocket hub stopped")
			return
		case <-ticker.C:
			if h.tracker == nil {
				continue
			}

			result := h.tracker.GetLatest()

			// Broadcast to all clients
			h.broadcast(Message{
				Type: "doa",
				Data: result,
			})

			// Immediate VAD change notification
			if result.SpeakingLatched != lastSpeaking {
				h.broadcast(Message{
					Type: "vad",
					Data: map[string]interface{}{
						"speaking": result.SpeakingLatched,
						"angle":    result.SmoothedAngle,
					},
				})
				lastSpeaking = result.SpeakingLatched

				h.logger.Debug("vad state change",
					"speaking", result.SpeakingLatched,
					"angle", result.SmoothedAngle,
				)
			}
		}
	}
}

func (h *WSHub) broadcast(msg Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		h.logger.Warn("websocket marshal error", "error", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for conn := range h.clients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			// Will be cleaned up when connection closes
			h.logger.Debug("websocket write error", "error", err)
		}
	}
}

// UpgradeHandler returns the WebSocket upgrade handler
func (h *WSHub) UpgradeHandler() fiber.Handler {
	// Middleware to check if request is a WebSocket upgrade
	return func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			c.Locals("allowed", true)
			return websocket.New(h.handleConnection)(c)
		}

		return c.Status(fiber.StatusUpgradeRequired).JSON(fiber.Map{
			"error":   "WebSocket upgrade required",
			"message": "Connect via WebSocket to receive DOA stream",
		})
	}
}

func (h *WSHub) handleConnection(c *websocket.Conn) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	clientCount := len(h.clients)
	h.mu.Unlock()

	h.logger.Info("websocket client connected",
		"remote_addr", c.RemoteAddr().String(),
		"clients", clientCount,
	)

	defer func() {
		h.mu.Lock()
		delete(h.clients, c)
		clientCount := len(h.clients)
		h.mu.Unlock()

		h.logger.Info("websocket client disconnected",
			"remote_addr", c.RemoteAddr().String(),
			"clients", clientCount,
		)
	}()

	// Keep connection alive, read for close or commands
	for {
		_, msg, err := c.ReadMessage()
		if err != nil {
			// Connection closed
			break
		}

		// Handle incoming commands (e.g., config changes)
		h.handleCommand(c, msg)
	}
}

func (h *WSHub) handleCommand(c *websocket.Conn, msg []byte) {
	var cmd struct {
		Type string `json:"type"`
	}

	if err := json.Unmarshal(msg, &cmd); err != nil {
		return
	}

	switch cmd.Type {
	case "ping":
		c.WriteJSON(Message{Type: "pong", Data: time.Now().Unix()})
	case "get_stats":
		if h.tracker != nil {
			c.WriteJSON(Message{Type: "stats", Data: h.tracker.Stats()})
		}
	}
}

// ClientCount returns the number of connected WebSocket clients
func (h *WSHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Close shuts down the WebSocket hub
func (h *WSHub) Close() {
	if h.cancel != nil {
		h.cancel()
		<-h.done
	}

	// Close all client connections
	h.mu.Lock()
	for conn := range h.clients {
		conn.Close()
	}
	h.clients = make(map[*websocket.Conn]struct{})
	h.mu.Unlock()
}

