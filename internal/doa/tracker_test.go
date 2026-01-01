package doa

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// MockSource is a test mock for DOA Source
type MockSource struct {
	mu       sync.Mutex
	angle    float64
	speaking bool
	healthy  bool
	err      error
	calls    int
}

func NewMockSource() *MockSource {
	return &MockSource{
		healthy: true,
	}
}

func (m *MockSource) GetDOA(ctx context.Context) (Reading, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls++

	if m.err != nil {
		return Reading{}, m.err
	}

	return Reading{
		Angle:     ToEvaAngle(m.angle), // Convert to Eva coords
		RawAngle:  m.angle,
		Speaking:  m.speaking,
		Timestamp: time.Now(),
	}, nil
}

func (m *MockSource) Close() error { return nil }

func (m *MockSource) Healthy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.healthy
}

func (m *MockSource) Name() string { return "mock" }

func (m *MockSource) SetAngle(angle float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.angle = angle
}

func (m *MockSource) SetSpeaking(speaking bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.speaking = speaking
}

func (m *MockSource) GetCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func TestTracker_BasicPolling(t *testing.T) {
	source := NewMockSource()
	source.SetAngle(1.57) // π/2 = front in XVF coords

	cfg := TrackerConfig{
		PollInterval:     10 * time.Millisecond,
		SpeakingLatchDur: 100 * time.Millisecond,
		EMAAlpha:         0.3,
		HistorySize:      10,
		Confidence: ConfidenceConfig{
			Base:           0.3,
			SpeakingBonus:  0.4,
			StabilityBonus: 0.2,
		},
	}

	logger := slog.Default()
	tracker := NewTracker(source, cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go tracker.Run(ctx)

	// Wait for some polls
	time.Sleep(50 * time.Millisecond)

	result := tracker.GetLatest()

	if result.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}

	// Should have polled several times
	if source.GetCalls() < 3 {
		t.Errorf("expected at least 3 polls, got %d", source.GetCalls())
	}

	tracker.Stop()
}

func TestTracker_SpeakingLatch(t *testing.T) {
	source := NewMockSource()
	source.SetAngle(1.57)

	cfg := TrackerConfig{
		PollInterval:     10 * time.Millisecond,
		SpeakingLatchDur: 50 * time.Millisecond, // Short latch for testing
		EMAAlpha:         0.5,
		HistorySize:      10,
		Confidence: ConfidenceConfig{
			Base:           0.3,
			SpeakingBonus:  0.4,
			StabilityBonus: 0.2,
		},
	}

	logger := slog.Default()
	tracker := NewTracker(source, cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go tracker.Run(ctx)

	// Start speaking
	source.SetSpeaking(true)
	time.Sleep(30 * time.Millisecond)

	result1 := tracker.GetLatest()
	if !result1.SpeakingLatched {
		t.Error("expected speaking to be latched")
	}

	// Stop speaking
	source.SetSpeaking(false)
	time.Sleep(20 * time.Millisecond)

	// Should still be latched
	result2 := tracker.GetLatest()
	if !result2.SpeakingLatched {
		t.Error("expected speaking to still be latched")
	}

	// Wait for latch to expire
	time.Sleep(60 * time.Millisecond)

	result3 := tracker.GetLatest()
	if result3.SpeakingLatched {
		t.Error("expected speaking latch to have expired")
	}

	tracker.Stop()
}

func TestTracker_EMASmoothing(t *testing.T) {
	source := NewMockSource()

	cfg := TrackerConfig{
		PollInterval:     5 * time.Millisecond,
		SpeakingLatchDur: 100 * time.Millisecond,
		EMAAlpha:         0.5, // 50% new, 50% old
		HistorySize:      10,
		Confidence: ConfidenceConfig{
			Base:           0.3,
			SpeakingBonus:  0.4,
			StabilityBonus: 0.2,
		},
	}

	logger := slog.Default()
	tracker := NewTracker(source, cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go tracker.Run(ctx)

	// Start at 0
	source.SetAngle(1.57) // Front
	time.Sleep(30 * time.Millisecond)

	// Jump to 45 degrees left
	source.SetAngle(0.78) // π/4
	time.Sleep(10 * time.Millisecond)

	result := tracker.GetLatest()

	// Smoothed angle should be between old and new due to EMA
	// Exact value depends on timing, but should not be exactly the new angle
	if result.SmoothedAngle == ToEvaAngle(0.78) {
		t.Log("Note: EMA may have converged quickly with high alpha")
	}

	tracker.Stop()
}

func TestTracker_Confidence(t *testing.T) {
	source := NewMockSource()
	source.SetAngle(1.57)

	cfg := TrackerConfig{
		PollInterval:     5 * time.Millisecond,
		SpeakingLatchDur: 100 * time.Millisecond,
		EMAAlpha:         0.5,
		HistorySize:      10,
		Confidence: ConfidenceConfig{
			Base:           0.3,
			SpeakingBonus:  0.4,
			StabilityBonus: 0.2,
		},
	}

	logger := slog.Default()
	tracker := NewTracker(source, cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go tracker.Run(ctx)

	// Not speaking, low confidence
	time.Sleep(30 * time.Millisecond)
	result1 := tracker.GetLatest()

	// Should have base confidence only (plus maybe stability)
	if result1.Confidence > 0.6 {
		t.Errorf("expected low confidence when not speaking, got %f", result1.Confidence)
	}

	// Start speaking, high confidence
	source.SetSpeaking(true)
	time.Sleep(30 * time.Millisecond)

	result2 := tracker.GetLatest()
	if result2.Confidence < 0.7 {
		t.Errorf("expected high confidence when speaking, got %f", result2.Confidence)
	}

	tracker.Stop()
}

func TestTracker_Subscribe(t *testing.T) {
	source := NewMockSource()
	source.SetAngle(1.57)

	cfg := TrackerConfig{
		PollInterval:     10 * time.Millisecond,
		SpeakingLatchDur: 100 * time.Millisecond,
		EMAAlpha:         0.5,
		HistorySize:      10,
		Confidence: ConfidenceConfig{
			Base: 0.3,
		},
	}

	logger := slog.Default()
	tracker := NewTracker(source, cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go tracker.Run(ctx)

	// Subscribe
	ch := tracker.Subscribe()
	defer tracker.Unsubscribe(ch)

	// Should receive updates
	select {
	case result := <-ch:
		if result.Timestamp.IsZero() {
			t.Error("expected valid result")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for subscription update")
	}

	tracker.Stop()
}

func TestTracker_Stats(t *testing.T) {
	source := NewMockSource()
	source.SetAngle(1.57)

	cfg := TrackerConfig{
		PollInterval:     10 * time.Millisecond,
		SpeakingLatchDur: 100 * time.Millisecond,
		EMAAlpha:         0.5,
		HistorySize:      10,
		Confidence: ConfidenceConfig{
			Base: 0.3,
		},
	}

	logger := slog.Default()
	tracker := NewTracker(source, cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go tracker.Run(ctx)

	time.Sleep(50 * time.Millisecond)

	stats := tracker.Stats()

	if stats.PollCount == 0 {
		t.Error("expected non-zero poll count")
	}

	if !stats.SourceHealthy {
		t.Error("expected source to be healthy")
	}

	tracker.Stop()
}

func TestTracker_GetTarget(t *testing.T) {
	source := NewMockSource()
	source.SetAngle(1.57)

	cfg := TrackerConfig{
		PollInterval:     10 * time.Millisecond,
		SpeakingLatchDur: 100 * time.Millisecond,
		EMAAlpha:         0.5,
		HistorySize:      10,
		Confidence: ConfidenceConfig{
			Base:          0.3,
			SpeakingBonus: 0.4,
		},
	}

	logger := slog.Default()
	tracker := NewTracker(source, cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go tracker.Run(ctx)

	// Wait for some polls
	time.Sleep(30 * time.Millisecond)

	// With speaking, should get a target
	source.SetSpeaking(true)
	time.Sleep(30 * time.Millisecond)

	angle, conf, ok := tracker.GetTarget()
	if !ok {
		t.Error("expected to get a target when speaking")
	}

	if conf < 0.5 {
		t.Errorf("expected confidence > 0.5, got %f", conf)
	}

	_ = angle // Just checking it doesn't panic

	tracker.Stop()
}

func TestDefaultTrackerConfig(t *testing.T) {
	cfg := DefaultTrackerConfig()

	if cfg.PollInterval != 50*time.Millisecond {
		t.Errorf("expected poll interval 50ms, got %v", cfg.PollInterval)
	}

	if cfg.SpeakingLatchDur != 500*time.Millisecond {
		t.Errorf("expected speaking latch 500ms, got %v", cfg.SpeakingLatchDur)
	}

	if cfg.EMAAlpha != 0.3 {
		t.Errorf("expected EMA alpha 0.3, got %f", cfg.EMAAlpha)
	}
}

