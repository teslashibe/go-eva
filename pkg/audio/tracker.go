package audio

import (
	"fmt"
	"sync"
	"time"
)

// DOAResult represents a processed DOA reading
type DOAResult struct {
	Angle      float64   `json:"angle"`       // Radians in Eva coordinates (0=front, +π/2=left)
	Speaking   bool      `json:"speaking"`    // Voice activity detected
	Confidence float64   `json:"confidence"`  // 0-1 confidence score
	Timestamp  time.Time `json:"timestamp"`   // When this reading was taken
	RawAngle   float64   `json:"raw_angle"`   // Original XVF3800 angle
}

// Tracker smooths DOA readings over time
type Tracker struct {
	source DOASource

	mu         sync.RWMutex
	latest     DOAResult
	history    []DOAResult
	historyMax int

	// Exponential moving average parameters
	alpha float64 // EMA smoothing factor (0-1, higher = more responsive)

	// Control
	running bool
	stop    chan struct{}
	pollHz  int
}

// NewTracker creates a new DOA tracker
func NewTracker(source DOASource) *Tracker {
	return &Tracker{
		source:     source,
		history:    make([]DOAResult, 0, 100),
		historyMax: 100,
		alpha:      0.3, // Moderate smoothing
		pollHz:     10,  // 10 Hz polling
		stop:       make(chan struct{}),
	}
}

// Run starts the tracker polling loop
func (t *Tracker) Run() {
	if t.source == nil {
		if Debug {
			fmt.Println("🎤 Tracker: No DOA source, running in mock mode")
		}
		return
	}

	t.mu.Lock()
	t.running = true
	t.mu.Unlock()

	ticker := time.NewTicker(time.Second / time.Duration(t.pollHz))
	defer ticker.Stop()

	if Debug {
		fmt.Printf("🎤 Tracker: Started polling at %d Hz\n", t.pollHz)
	}

	for {
		select {
		case <-t.stop:
			if Debug {
				fmt.Println("🎤 Tracker: Stopped")
			}
			return
		case <-ticker.C:
			t.poll()
		}
	}
}

// poll reads DOA and updates the smoothed value
func (t *Tracker) poll() {
	rawAngle, speaking, err := t.source.GetDOA()
	if err != nil {
		if Debug {
			fmt.Printf("🎤 Tracker: DOA read error: %v\n", err)
		}
		return
	}

	// Convert to Eva coordinates
	evaAngle := ToEvaAngle(rawAngle)

	t.mu.Lock()
	defer t.mu.Unlock()

	// Apply EMA smoothing
	if len(t.history) > 0 {
		prevAngle := t.latest.Angle
		evaAngle = t.alpha*evaAngle + (1-t.alpha)*prevAngle
	}

	// Calculate confidence based on:
	// 1. Speaking status
	// 2. Angle stability (low variance = high confidence)
	confidence := 0.5
	if speaking {
		confidence = 0.9
	}

	// Check angle stability
	if len(t.history) >= 5 {
		var variance float64
		for i := len(t.history) - 5; i < len(t.history); i++ {
			diff := t.history[i].Angle - evaAngle
			variance += diff * diff
		}
		variance /= 5

		// Low variance = high confidence
		if variance < 0.01 {
			confidence += 0.1
		} else if variance > 0.1 {
			confidence -= 0.2
		}
	}

	// Clamp confidence
	if confidence > 1.0 {
		confidence = 1.0
	}
	if confidence < 0.0 {
		confidence = 0.0
	}

	result := DOAResult{
		Angle:      evaAngle,
		Speaking:   speaking,
		Confidence: confidence,
		Timestamp:  time.Now(),
		RawAngle:   rawAngle,
	}

	t.latest = result
	t.history = append(t.history, result)

	// Trim history
	if len(t.history) > t.historyMax {
		t.history = t.history[1:]
	}

	if Debug && speaking {
		fmt.Printf("🎤 DOA: %.2f rad (raw: %.2f), confidence: %.2f, speaking: %v\n",
			evaAngle, rawAngle, confidence, speaking)
	}
}

// GetLatest returns the most recent DOA reading
func (t *Tracker) GetLatest() DOAResult {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.latest
}

// GetTarget returns the current target angle if confidence is high enough
func (t *Tracker) GetTarget() (angle float64, confidence float64, ok bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.latest.Confidence < 0.3 {
		return 0, 0, false
	}

	return t.latest.Angle, t.latest.Confidence, true
}

// Stop stops the tracker
func (t *Tracker) Stop() {
	t.mu.Lock()
	running := t.running
	t.running = false
	t.mu.Unlock()

	if running {
		close(t.stop)
	}
}

// SetAlpha sets the EMA smoothing factor
func (t *Tracker) SetAlpha(alpha float64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if alpha < 0 {
		alpha = 0
	}
	if alpha > 1 {
		alpha = 1
	}
	t.alpha = alpha
}

