package xvf3800

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/teslashibe/go-eva/internal/doa"
)

// MockSource is a mock DOA source for testing
type MockSource struct {
	mu           sync.Mutex
	angle        float64
	speaking     bool
	healthy      bool
	simulateWave bool
	startTime    time.Time
}

// NewMockSource creates a new mock DOA source
func NewMockSource() *MockSource {
	return &MockSource{
		angle:        math.Pi / 2, // Front in XVF coords
		healthy:      true,
		simulateWave: false,
		startTime:    time.Now(),
	}
}

// NewMockSourceWithWave creates a mock that simulates a moving source
func NewMockSourceWithWave() *MockSource {
	return &MockSource{
		healthy:      true,
		simulateWave: true,
		startTime:    time.Now(),
	}
}

// GetDOA returns the current direction of arrival
func (m *MockSource) GetDOA(ctx context.Context) (doa.Reading, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	rawAngle := m.angle

	if m.simulateWave {
		// Simulate a source moving left-right
		elapsed := time.Since(m.startTime).Seconds()
		rawAngle = math.Pi/2 + math.Sin(elapsed)*math.Pi/4 // ±45° from front
	}

	return doa.Reading{
		Angle:     doa.ToEvaAngle(rawAngle),
		RawAngle:  rawAngle,
		Speaking:  m.speaking,
		Timestamp: time.Now(),
		LatencyMs: 1, // Simulate minimal latency
	}, nil
}

// Close releases resources
func (m *MockSource) Close() error {
	return nil
}

// Healthy returns true if the source is operational
func (m *MockSource) Healthy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.healthy
}

// Name returns the source type name
func (m *MockSource) Name() string {
	return "mock"
}

// SetAngle sets the mock angle (in XVF coordinates)
func (m *MockSource) SetAngle(angle float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.angle = angle
}

// SetSpeaking sets the mock speaking state
func (m *MockSource) SetSpeaking(speaking bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.speaking = speaking
}

// SetHealthy sets the mock health state
func (m *MockSource) SetHealthy(healthy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healthy = healthy
}

