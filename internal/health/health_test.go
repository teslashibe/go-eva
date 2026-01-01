package health

import (
	"testing"
)

func TestChecker_Basic(t *testing.T) {
	checker := NewChecker("1.0.0")

	status := checker.GetStatus()

	if status.Status != "ok" {
		t.Errorf("expected status 'ok', got %s", status.Status)
	}

	if status.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %s", status.Version)
	}

	if status.UptimeSeconds < 0 {
		t.Error("expected non-negative uptime")
	}
}

func TestChecker_SetComponent(t *testing.T) {
	checker := NewChecker("1.0.0")

	checker.SetComponent("doa_source", true, "connected")

	status := checker.GetStatus()

	if len(status.Components) != 1 {
		t.Errorf("expected 1 component, got %d", len(status.Components))
	}

	doa, ok := status.Components["doa_source"]
	if !ok {
		t.Fatal("expected doa_source component")
	}

	if !doa.Healthy {
		t.Error("expected doa_source to be healthy")
	}

	if doa.Message != "connected" {
		t.Errorf("expected message 'connected', got %s", doa.Message)
	}
}

func TestChecker_Degraded(t *testing.T) {
	checker := NewChecker("1.0.0")

	checker.SetComponent("doa_source", true, "ok")
	checker.SetComponent("usb", false, "disconnected")

	status := checker.GetStatus()

	if status.Status != "degraded" {
		t.Errorf("expected status 'degraded', got %s", status.Status)
	}

	if checker.IsHealthy() {
		t.Error("expected IsHealthy() to return false")
	}
}

func TestChecker_Recovery(t *testing.T) {
	checker := NewChecker("1.0.0")

	// Start unhealthy
	checker.SetComponent("doa_source", false, "error")

	if checker.IsHealthy() {
		t.Error("expected unhealthy")
	}

	// Recover
	checker.SetComponent("doa_source", true, "recovered")

	if !checker.IsHealthy() {
		t.Error("expected healthy after recovery")
	}

	status := checker.GetStatus()
	if status.Status != "ok" {
		t.Errorf("expected status 'ok', got %s", status.Status)
	}
}

func TestChecker_MultipleComponents(t *testing.T) {
	checker := NewChecker("1.0.0")

	checker.SetComponent("doa_source", true, "")
	checker.SetComponent("tracker", true, "")
	checker.SetComponent("server", true, "")

	status := checker.GetStatus()

	if len(status.Components) != 3 {
		t.Errorf("expected 3 components, got %d", len(status.Components))
	}

	if status.Status != "ok" {
		t.Errorf("expected status 'ok', got %s", status.Status)
	}
}

