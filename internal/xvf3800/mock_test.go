package xvf3800

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/teslashibe/go-eva/internal/doa"
)

func TestMockSource_Basic(t *testing.T) {
	source := NewMockSource()

	reading, err := source.GetDOA(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Default is front (π/2 XVF = 0 Eva)
	if math.Abs(reading.Angle) > 0.01 {
		t.Errorf("expected angle ~0, got %f", reading.Angle)
	}

	if !source.Healthy() {
		t.Error("expected mock to be healthy")
	}

	if source.Name() != "mock" {
		t.Errorf("expected name 'mock', got %s", source.Name())
	}
}

func TestMockSource_SetAngle(t *testing.T) {
	source := NewMockSource()

	// Set to 45° left in XVF coords (π/4)
	source.SetAngle(math.Pi / 4)

	reading, err := source.GetDOA(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should convert to Eva coords: π/2 - π/4 = π/4
	expected := doa.ToEvaAngle(math.Pi / 4)
	if math.Abs(reading.Angle-expected) > 0.01 {
		t.Errorf("expected angle %f, got %f", expected, reading.Angle)
	}
}

func TestMockSource_SetSpeaking(t *testing.T) {
	source := NewMockSource()

	// Initially not speaking
	reading1, _ := source.GetDOA(context.Background())
	if reading1.Speaking {
		t.Error("expected not speaking initially")
	}

	// Set speaking
	source.SetSpeaking(true)

	reading2, _ := source.GetDOA(context.Background())
	if !reading2.Speaking {
		t.Error("expected speaking after SetSpeaking(true)")
	}
}

func TestMockSource_SetHealthy(t *testing.T) {
	source := NewMockSource()

	if !source.Healthy() {
		t.Error("expected healthy initially")
	}

	source.SetHealthy(false)

	if source.Healthy() {
		t.Error("expected unhealthy after SetHealthy(false)")
	}
}

func TestMockSourceWithWave(t *testing.T) {
	source := NewMockSourceWithWave()

	// Get initial reading
	reading1, err := source.GetDOA(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait a bit and get another reading
	time.Sleep(100 * time.Millisecond)

	reading2, err := source.GetDOA(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Angles should be different due to wave simulation
	// (unless we got unlucky with timing)
	t.Logf("wave readings: %f -> %f", reading1.Angle, reading2.Angle)

	if source.Name() != "mock" {
		t.Errorf("expected name 'mock', got %s", source.Name())
	}
}

func TestMockSource_Close(t *testing.T) {
	source := NewMockSource()

	err := source.Close()
	if err != nil {
		t.Errorf("unexpected error on close: %v", err)
	}
}

// Verify MockSource implements doa.Source interface
var _ doa.Source = (*MockSource)(nil)

