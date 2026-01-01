// Package audio provides DOA (Direction of Arrival) functionality
package audio

import "math"

// Debug enables verbose logging
var Debug bool

// DOASource provides raw DOA readings
type DOASource interface {
	GetDOA() (angle float64, speaking bool, err error)
}

// ToEvaAngle converts XVF3800 angle to Eva's coordinate system
// XVF3800: 0 = left, π/2 = front, π = right
// Eva:     0 = front, +π/2 = left, -π/2 = right
func ToEvaAngle(xvfAngle float64) float64 {
	return (math.Pi / 2) - xvfAngle
}

