// Package doa provides Direction of Arrival functionality
package doa

import (
	"context"
	"math"
	"time"
)

// Reading represents a single DOA measurement from hardware
type Reading struct {
	Angle        float64   `json:"angle"`         // Radians in Eva coordinates (0=front, +left, -right)
	RawAngle     float64   `json:"raw_angle"`     // Original sensor angle
	Speaking     bool      `json:"speaking"`      // Voice activity detected
	Timestamp    time.Time `json:"timestamp"`     // When this reading was taken
	LatencyMs    int64     `json:"latency_ms"`    // Processing latency

	// Enhanced data from XVF3800 AEC module
	SpeechEnergy [4]float64 `json:"speech_energy"`  // Speech energy per mic (4 mics)
	MicAzimuths  [4]float64 `json:"mic_azimuths"`   // Per-mic azimuth readings (radians)
	TotalEnergy  float64    `json:"total_energy"`   // Sum of speech energy across all mics
}

// EstimatedDistance returns a rough distance estimate based on speech energy.
// Higher energy = closer. Returns 0 if no speech detected.
// Calibrated using real-world measurements with XVF3800.
func (r *Reading) EstimatedDistance() float64 {
	if r.TotalEnergy <= 0 || !r.Speaking {
		return 0
	}
	// Calibrated reference energy at 1 meter (from calibration 2026-01-03)
	// Multi-distance calibration: 0.5m, 1m, 2m, 3m with constant voice volume
	// Using median k value for robustness against outliers
	const referenceEnergy = 6267144.0

	// Inverse square law: distance = sqrt(refEnergy / measuredEnergy)
	distance := math.Sqrt(referenceEnergy / r.TotalEnergy)

	// Clamp to reasonable range (0.3m - 5m)
	if distance < 0.3 {
		distance = 0.3
	}
	if distance > 5.0 {
		distance = 5.0
	}
	return distance
}

// EstimatedY returns the lateral (left/right) position estimate in meters.
// Positive = left, negative = right (Eva coordinates)
func (r *Reading) EstimatedY() float64 {
	dist := r.EstimatedDistance()
	if dist <= 0 {
		return 0
	}
	return dist * math.Sin(r.Angle)
}

// EstimatedX returns the forward distance estimate in meters.
func (r *Reading) EstimatedX() float64 {
	dist := r.EstimatedDistance()
	if dist <= 0 {
		return 0
	}
	return dist * math.Cos(r.Angle)
}

// Source provides raw DOA readings from hardware
type Source interface {
	// GetDOA returns the current direction of arrival
	GetDOA(ctx context.Context) (Reading, error)

	// Close releases hardware resources
	Close() error

	// Healthy returns true if the source is operational
	Healthy() bool

	// Name returns the source type name
	Name() string
}

// ToEvaAngle converts XVF3800 angle to Eva's coordinate system
// XVF3800: 0 = left, π/2 = front, π = right
// Eva:     0 = front, +π/2 = left, -π/2 = right
func ToEvaAngle(xvfAngle float64) float64 {
	return (math.Pi / 2) - xvfAngle
}

// FromEvaAngle converts Eva's angle back to XVF3800 coordinates
func FromEvaAngle(evaAngle float64) float64 {
	return (math.Pi / 2) - evaAngle
}

// NormalizeAngle normalizes an angle to [-π, π]
func NormalizeAngle(angle float64) float64 {
	for angle > math.Pi {
		angle -= 2 * math.Pi
	}
	for angle < -math.Pi {
		angle += 2 * math.Pi
	}
	return angle
}

// Clamp clamps a value to [min, max]
func Clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

