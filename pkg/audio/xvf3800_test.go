package audio

import (
	"math"
	"testing"
)

func TestToEvaAngle(t *testing.T) {
	tests := []struct {
		name     string
		xvfAngle float64
		expected float64
	}{
		{
			name:     "XVF left (0) -> Eva left (+π/2)",
			xvfAngle: 0,
			expected: math.Pi / 2,
		},
		{
			name:     "XVF front (π/2) -> Eva front (0)",
			xvfAngle: math.Pi / 2,
			expected: 0,
		},
		{
			name:     "XVF right (π) -> Eva right (-π/2)",
			xvfAngle: math.Pi,
			expected: -math.Pi / 2,
		},
		{
			name:     "XVF front-left (π/4) -> Eva front-left (+π/4)",
			xvfAngle: math.Pi / 4,
			expected: math.Pi / 4,
		},
		{
			name:     "XVF front-right (3π/4) -> Eva front-right (-π/4)",
			xvfAngle: 3 * math.Pi / 4,
			expected: -math.Pi / 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ToEvaAngle(tt.xvfAngle)
			if !floatEquals(result, tt.expected, 0.001) {
				t.Errorf("ToEvaAngle(%v) = %v, want %v", tt.xvfAngle, result, tt.expected)
			}
		})
	}
}

func floatEquals(a, b, epsilon float64) bool {
	return math.Abs(a-b) < epsilon
}

