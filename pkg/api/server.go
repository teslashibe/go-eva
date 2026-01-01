// Package api provides the HTTP server for go-eva
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gorilla/websocket"

	"github.com/teslashibe/go-eva/pkg/audio"
)

// Debug enables verbose logging
var Debug bool

// Server is the HTTP server for go-eva
type Server struct {
	app     *fiber.App
	port    int
	tracker *audio.Tracker
}

// NewServer creates a new HTTP server
func NewServer(port int, tracker *audio.Tracker) *Server {
	app := fiber.New(fiber.Config{
		AppName:               "go-eva",
		DisableStartupMessage: true,
	})

	// Middleware
	app.Use(cors.New())
	if Debug {
		app.Use(logger.New())
	}

	s := &Server{
		app:     app,
		port:    port,
		tracker: tracker,
	}

	// Register routes
	s.registerRoutes()

	return s
}

// registerRoutes sets up all API routes
func (s *Server) registerRoutes() {
	// Health check
	s.app.Get("/health", s.healthHandler)

	// Audio API
	api := s.app.Group("/api")

	audio := api.Group("/audio")
	audio.Get("/doa", s.doaHandler)
	audio.Get("/doa/stream", s.doaStreamHandler)
}

// healthHandler returns service health
func (s *Server) healthHandler(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"status":  "ok",
		"service": "go-eva",
		"time":    time.Now().Format(time.RFC3339),
	})
}

// doaHandler returns the current DOA reading
func (s *Server) doaHandler(c *fiber.Ctx) error {
	if s.tracker == nil {
		return c.Status(503).JSON(fiber.Map{
			"error": "DOA tracker not available",
		})
	}

	result := s.tracker.GetLatest()

	return c.JSON(result)
}

// WebSocket upgrader
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins
	},
}

// doaStreamHandler streams DOA readings via WebSocket
func (s *Server) doaStreamHandler(c *fiber.Ctx) error {
	// Upgrade to WebSocket using Fiber's built-in support
	return c.Status(fiber.StatusUpgradeRequired).JSON(fiber.Map{
		"error":   "WebSocket upgrade required",
		"message": "Connect via WebSocket to receive DOA stream",
	})
}

// Start starts the HTTP server
func (s *Server) Start() error {
	return s.app.Listen(fmt.Sprintf(":%d", s.port))
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown() error {
	return s.app.Shutdown()
}

// WebSocketHandler handles WebSocket DOA streaming
// This is called from a goroutine for each WebSocket connection
func (s *Server) WebSocketHandler(conn *websocket.Conn) {
	defer conn.Close()

	ticker := time.NewTicker(100 * time.Millisecond) // 10 Hz
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if s.tracker == nil {
				continue
			}

			result := s.tracker.GetLatest()

			data, err := json.Marshal(result)
			if err != nil {
				if Debug {
					fmt.Printf("🌐 WebSocket marshal error: %v\n", err)
				}
				continue
			}

			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				if Debug {
					fmt.Printf("🌐 WebSocket write error: %v\n", err)
				}
				return
			}
		}
	}
}

