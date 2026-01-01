package xvf3800

import (
	"testing"
	"time"
)

func TestDefaultUSBSourceConfig(t *testing.T) {
	cfg := DefaultUSBSourceConfig()

	if cfg.MaxConsecutiveErrors != 5 {
		t.Errorf("expected MaxConsecutiveErrors 5, got %d", cfg.MaxConsecutiveErrors)
	}

	if cfg.InitialBackoff != 100*time.Millisecond {
		t.Errorf("expected InitialBackoff 100ms, got %v", cfg.InitialBackoff)
	}

	if cfg.MaxBackoff != 5*time.Second {
		t.Errorf("expected MaxBackoff 5s, got %v", cfg.MaxBackoff)
	}
}

func TestUSBStats(t *testing.T) {
	stats := USBStats{
		Healthy:           true,
		ConsecutiveErrors: 0,
		DeviceConnected:   true,
	}

	if !stats.Healthy {
		t.Error("expected healthy")
	}

	if stats.ConsecutiveErrors != 0 {
		t.Error("expected 0 errors")
	}

	if !stats.DeviceConnected {
		t.Error("expected device connected")
	}
}

// Note: Full integration tests require actual XVF3800 hardware
// These tests verify the interface and configuration only

func TestUSBSourceConstants(t *testing.T) {
	// Verify USB IDs match the XVF3800
	if VendorID != 0x38FB {
		t.Errorf("expected VendorID 0x38FB, got 0x%04X", VendorID)
	}

	if ProductID != 0x1001 {
		t.Errorf("expected ProductID 0x1001, got 0x%04X", ProductID)
	}
}

