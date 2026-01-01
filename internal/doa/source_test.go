package doa

import (
	"math"
	"testing"
)

func TestToEvaAngle(t *testing.T) {
	tests := []struct {
		name     string
		xvfAngle float64
		want     float64
	}{
		{
			name:     "front (π/2 XVF = 0 Eva)",
			xvfAngle: math.Pi / 2,
			want:     0,
		},
		{
			name:     "left (0 XVF = +π/2 Eva)",
			xvfAngle: 0,
			want:     math.Pi / 2,
		},
		{
			name:     "right (π XVF = -π/2 Eva)",
			xvfAngle: math.Pi,
			want:     -math.Pi / 2,
		},
		{
			name:     "45° left",
			xvfAngle: math.Pi / 4,
			want:     math.Pi / 4,
		},
		{
			name:     "45° right",
			xvfAngle: 3 * math.Pi / 4,
			want:     -math.Pi / 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToEvaAngle(tt.xvfAngle)
			if math.Abs(got-tt.want) > 0.001 {
				t.Errorf("ToEvaAngle(%f) = %f, want %f", tt.xvfAngle, got, tt.want)
			}
		})
	}
}

func TestFromEvaAngle(t *testing.T) {
	tests := []struct {
		name     string
		evaAngle float64
		want     float64
	}{
		{
			name:     "front (0 Eva = π/2 XVF)",
			evaAngle: 0,
			want:     math.Pi / 2,
		},
		{
			name:     "left (+π/2 Eva = 0 XVF)",
			evaAngle: math.Pi / 2,
			want:     0,
		},
		{
			name:     "right (-π/2 Eva = π XVF)",
			evaAngle: -math.Pi / 2,
			want:     math.Pi,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FromEvaAngle(tt.evaAngle)
			if math.Abs(got-tt.want) > 0.001 {
				t.Errorf("FromEvaAngle(%f) = %f, want %f", tt.evaAngle, got, tt.want)
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	// Converting to Eva and back should give the same angle
	angles := []float64{0, math.Pi / 4, math.Pi / 2, 3 * math.Pi / 4, math.Pi}

	for _, angle := range angles {
		eva := ToEvaAngle(angle)
		back := FromEvaAngle(eva)

		if math.Abs(back-angle) > 0.001 {
			t.Errorf("round trip failed: %f -> %f -> %f", angle, eva, back)
		}
	}
}

func TestNormalizeAngle(t *testing.T) {
	tests := []struct {
		name  string
		angle float64
		want  float64
	}{
		{
			name:  "already normalized",
			angle: 0.5,
			want:  0.5,
		},
		{
			name:  "greater than π",
			angle: 4.0,
			want:  4.0 - 2*math.Pi,
		},
		{
			name:  "less than -π",
			angle: -4.0,
			want:  -4.0 + 2*math.Pi,
		},
		{
			name:  "exactly π",
			angle: math.Pi,
			want:  math.Pi,
		},
		{
			name:  "exactly -π",
			angle: -math.Pi,
			want:  -math.Pi,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeAngle(tt.angle)
			if math.Abs(got-tt.want) > 0.001 {
				t.Errorf("NormalizeAngle(%f) = %f, want %f", tt.angle, got, tt.want)
			}
		})
	}
}

func TestClamp(t *testing.T) {
	tests := []struct {
		name       string
		value      float64
		min        float64
		max        float64
		want       float64
	}{
		{
			name:  "within range",
			value: 0.5,
			min:   0,
			max:   1,
			want:  0.5,
		},
		{
			name:  "below min",
			value: -0.5,
			min:   0,
			max:   1,
			want:  0,
		},
		{
			name:  "above max",
			value: 1.5,
			min:   0,
			max:   1,
			want:  1,
		},
		{
			name:  "at min",
			value: 0,
			min:   0,
			max:   1,
			want:  0,
		},
		{
			name:  "at max",
			value: 1,
			min:   0,
			max:   1,
			want:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Clamp(tt.value, tt.min, tt.max)
			if got != tt.want {
				t.Errorf("Clamp(%f, %f, %f) = %f, want %f", tt.value, tt.min, tt.max, got, tt.want)
			}
		})
	}
}

