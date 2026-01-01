package server

import (
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
)

// LoggingMiddleware logs HTTP requests
func LoggingMiddleware(logger *slog.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()

		// Process request
		err := c.Next()

		// Skip logging for high-frequency endpoints
		path := c.Path()
		if path == "/metrics" || path == "/health" {
			return err
		}

		logger.Info("http request",
			"method", c.Method(),
			"path", path,
			"status", c.Response().StatusCode(),
			"latency_ms", time.Since(start).Milliseconds(),
			"ip", c.IP(),
		)

		return err
	}
}

