package xvf3800

import (
	"log/slog"

	"github.com/teslashibe/go-eva/internal/doa"
)

// NewSource creates the best available DOA source
// Priority: USB (pure Go, fast) > Mock (testing only)
func NewSource(logger *slog.Logger) (doa.Source, error) {
	// Try USB first - pure Go, fast, production-ready
	usb, err := NewUSBSource(logger)
	if err == nil {
		return usb, nil
	}

	logger.Warn("USB source unavailable",
		"error", err,
		"hint", "ensure libusb is installed and device is connected",
	)

	// No fallback to Python - we want pure Go
	// Return error so caller can decide (use mock for testing)
	return nil, err
}

// NewSourceWithFallback creates a DOA source with mock fallback
// Use this for development/testing when hardware is unavailable
func NewSourceWithFallback(logger *slog.Logger) doa.Source {
	source, err := NewSource(logger)
	if err == nil {
		return source
	}

	logger.Warn("using mock DOA source - no hardware available")
	return NewMockSource()
}

