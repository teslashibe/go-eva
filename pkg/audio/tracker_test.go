package audio

import (
	"math"
	"testing"
	"time"
)

// mockDOASource provides fake DOA readings for testing
type mockDOASource struct {
	angle    float64
	speaking bool
	err      error
}

func (m *mockDOASource) GetDOA() (float64, bool, error) {
	return m.angle, m.speaking, m.err
}

func TestTracker_NewTracker(t *testing.T) {
	mock := &mockDOASource{angle: math.Pi / 2, speaking: false}
	tracker := NewTracker(mock)

	if tracker == nil {
		t.Fatal("NewTracker returned nil")
	}

	if tracker.alpha != 0.3 {
		t.Errorf("Default alpha = %v, want 0.3", tracker.alpha)
	}

	if tracker.pollHz != 10 {
		t.Errorf("Default pollHz = %v, want 10", tracker.pollHz)
	}
}

func TestTracker_GetLatest_Empty(t *testing.T) {
	mock := &mockDOASource{}
	tracker := NewTracker(mock)

	result := tracker.GetLatest()

	// Should return zero value when no readings yet
	if result.Angle != 0 {
		t.Errorf("Empty tracker Angle = %v, want 0", result.Angle)
	}
}

func TestTracker_Poll(t *testing.T) {
	mock := &mockDOASource{
		angle:    math.Pi / 2, // XVF front
		speaking: true,
	}
	tracker := NewTracker(mock)

	// Manually call poll
	tracker.poll()

	result := tracker.GetLatest()

	// XVF front (π/2) -> Eva front (0)
	if !floatEquals(result.Angle, 0, 0.01) {
		t.Errorf("After poll, Angle = %v, want ~0 (front)", result.Angle)
	}

	if !result.Speaking {
		t.Error("Expected Speaking = true")
	}

	// Speaking should increase confidence
	if result.Confidence < 0.8 {
		t.Errorf("Confidence = %v, want >= 0.8 when speaking", result.Confidence)
	}
}

func TestTracker_EMASmoothing(t *testing.T) {
	mock := &mockDOASource{angle: 0, speaking: false}
	tracker := NewTracker(mock)
	tracker.alpha = 0.5 // 50% new, 50% old

	// First reading: XVF left (0) -> Eva left (+π/2)
	tracker.poll()
	first := tracker.GetLatest()

	// Second reading: XVF right (π) -> Eva right (-π/2)
	mock.angle = math.Pi
	tracker.poll()
	second := tracker.GetLatest()

	// With alpha=0.5:
	// new = 0.5 * (-π/2) + 0.5 * (π/2) = 0
	expected := 0.0

	if !floatEquals(second.Angle, expected, 0.01) {
		t.Errorf("After EMA, Angle = %v, want ~%v", second.Angle, expected)
	}

	// First angle should have been π/2
	if !floatEquals(first.Angle, math.Pi/2, 0.01) {
		t.Errorf("First Angle = %v, want ~π/2", first.Angle)
	}
}

func TestTracker_GetTarget(t *testing.T) {
	mock := &mockDOASource{angle: math.Pi / 2, speaking: true}
	tracker := NewTracker(mock)

	// No readings yet - should return not ok
	_, _, ok := tracker.GetTarget()
	if ok {
		t.Error("GetTarget should return ok=false with no readings")
	}

	// Add some readings to build confidence
	for i := 0; i < 10; i++ {
		tracker.poll()
	}

	angle, confidence, ok := tracker.GetTarget()

	if !ok {
		t.Error("GetTarget should return ok=true after readings")
	}

	if confidence < 0.3 {
		t.Errorf("Confidence = %v, want >= 0.3", confidence)
	}

	if !floatEquals(angle, 0, 0.1) {
		t.Errorf("Angle = %v, want ~0 (front)", angle)
	}
}

func TestTracker_SetAlpha(t *testing.T) {
	mock := &mockDOASource{}
	tracker := NewTracker(mock)

	// Test clamping
	tracker.SetAlpha(2.0)
	if tracker.alpha != 1.0 {
		t.Errorf("Alpha should clamp to 1.0, got %v", tracker.alpha)
	}

	tracker.SetAlpha(-1.0)
	if tracker.alpha != 0.0 {
		t.Errorf("Alpha should clamp to 0.0, got %v", tracker.alpha)
	}

	tracker.SetAlpha(0.7)
	if tracker.alpha != 0.7 {
		t.Errorf("Alpha = %v, want 0.7", tracker.alpha)
	}
}

func TestTracker_Stop(t *testing.T) {
	mock := &mockDOASource{}
	tracker := NewTracker(mock)

	// Start and stop should not panic
	go tracker.Run()
	time.Sleep(50 * time.Millisecond)
	tracker.Stop()

	// Double stop should not panic
	tracker.Stop()
}

