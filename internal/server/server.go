// Package server provides the HTTP server for go-eva
package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"github.com/teslashibe/go-eva/internal/config"
	"github.com/teslashibe/go-eva/internal/doa"
)

// Server is the HTTP server for go-eva
type Server struct {
	app       *fiber.App
	cfg       config.ServerConfig
	tracker   *doa.Tracker
	logger    *slog.Logger
	wsHub     *WSHub
	startTime time.Time
	version   string
}

// New creates a new HTTP server
func New(cfg config.ServerConfig, tracker *doa.Tracker, logger *slog.Logger, version string) *Server {
	if logger == nil {
		logger = slog.Default()
	}

	app := fiber.New(fiber.Config{
		AppName:               "go-eva",
		DisableStartupMessage: true,
		ReadTimeout:           cfg.ReadTimeout,
		WriteTimeout:          cfg.WriteTimeout,
	})

	// Middleware
	app.Use(recover.New())
	app.Use(cors.New())
	app.Use(LoggingMiddleware(logger))

	s := &Server{
		app:       app,
		cfg:       cfg,
		tracker:   tracker,
		logger:    logger,
		wsHub:     NewWSHub(tracker, logger),
		startTime: time.Now(),
		version:   version,
	}

	// Register routes
	s.registerRoutes()

	return s
}

// registerRoutes sets up all API routes
func (s *Server) registerRoutes() {
	// Health check
	s.app.Get("/health", s.healthHandler)

	// Metrics endpoint
	s.app.Get("/metrics", s.metricsHandler)

	// Audio API
	api := s.app.Group("/api")

	audio := api.Group("/audio")
	audio.Get("/doa", s.doaHandler)
	audio.Get("/doa/stream", s.wsHub.UpgradeHandler())

	// Config endpoint
	api.Get("/config", s.configHandler)

	// Stats endpoint
	api.Get("/stats", s.statsHandler)
}

// healthHandler returns service health
func (s *Server) healthHandler(c *fiber.Ctx) error {
	uptime := time.Since(s.startTime)

	sourceHealthy := false
	sourceName := "unknown"
	if s.tracker != nil {
		stats := s.tracker.Stats()
		sourceHealthy = stats.SourceHealthy
	}

	status := "ok"
	if !sourceHealthy {
		status = "degraded"
	}

	return c.JSON(fiber.Map{
		"status":         status,
		"version":        s.version,
		"uptime_seconds": int64(uptime.Seconds()),
		"doa_source":     sourceName,
		"source_healthy": sourceHealthy,
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

// configHandler returns current configuration
func (s *Server) configHandler(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"server": fiber.Map{
			"port":             s.cfg.Port,
			"read_timeout_ms":  s.cfg.ReadTimeout.Milliseconds(),
			"write_timeout_ms": s.cfg.WriteTimeout.Milliseconds(),
		},
	})
}

// statsHandler returns tracker statistics
func (s *Server) statsHandler(c *fiber.Ctx) error {
	if s.tracker == nil {
		return c.Status(503).JSON(fiber.Map{
			"error": "tracker not available",
		})
	}

	return c.JSON(s.tracker.Stats())
}

// metricsHandler returns Prometheus-format metrics
func (s *Server) metricsHandler(c *fiber.Ctx) error {
	if s.tracker == nil {
		return c.Status(503).SendString("# no tracker available\n")
	}

	stats := s.tracker.Stats()

	metrics := fmt.Sprintf(`# HELP go_eva_doa_angle_radians Current DOA angle in radians
# TYPE go_eva_doa_angle_radians gauge
go_eva_doa_angle_radians %f

# HELP go_eva_speaking Speaking state (1=speaking, 0=silent)
# TYPE go_eva_speaking gauge
go_eva_speaking %d

# HELP go_eva_doa_confidence DOA confidence score
# TYPE go_eva_doa_confidence gauge
go_eva_doa_confidence %f

# HELP go_eva_poll_count Total DOA polls
# TYPE go_eva_poll_count counter
go_eva_poll_count %d

# HELP go_eva_poll_errors Total DOA poll errors
# TYPE go_eva_poll_errors counter
go_eva_poll_errors %d

# HELP go_eva_avg_latency_ms Average poll latency in milliseconds
# TYPE go_eva_avg_latency_ms gauge
go_eva_avg_latency_ms %f

# HELP go_eva_source_healthy DOA source health (1=healthy, 0=unhealthy)
# TYPE go_eva_source_healthy gauge
go_eva_source_healthy %d

# HELP go_eva_uptime_seconds Server uptime in seconds
# TYPE go_eva_uptime_seconds gauge
go_eva_uptime_seconds %d

# HELP go_eva_websocket_clients Current WebSocket client count
# TYPE go_eva_websocket_clients gauge
go_eva_websocket_clients %d
`,
		stats.CurrentAngle,
		boolToInt(stats.SpeakingLatched),
		stats.CurrentConfidence,
		stats.PollCount,
		stats.ErrorCount,
		stats.AvgLatencyMs,
		boolToInt(stats.SourceHealthy),
		int64(time.Since(s.startTime).Seconds()),
		s.wsHub.ClientCount(),
	)

	c.Set("Content-Type", "text/plain; charset=utf-8")
	return c.SendString(metrics)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Start starts the HTTP server
func (s *Server) Start() error {
	s.logger.Info("starting HTTP server",
		"port", s.cfg.Port,
	)

	return s.app.Listen(fmt.Sprintf(":%d", s.cfg.Port))
}

// WSHub returns the WebSocket hub for external control
func (s *Server) WSHub() *WSHub {
	return s.wsHub
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down HTTP server")

	// Close WebSocket hub
	s.wsHub.Close()

	// Shutdown Fiber with timeout from context
	done := make(chan error, 1)
	go func() {
		done <- s.app.Shutdown()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

