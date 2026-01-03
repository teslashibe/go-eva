package doa

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// TrackerConfig configures the DOA tracker
type TrackerConfig struct {
	PollInterval     time.Duration
	SpeakingLatchDur time.Duration
	EMAAlpha         float64
	HistorySize      int

	Confidence ConfidenceConfig
}

// ConfidenceConfig configures confidence scoring
type ConfidenceConfig struct {
	Base           float64
	SpeakingBonus  float64
	StabilityBonus float64
}

// DefaultTrackerConfig returns sensible defaults
func DefaultTrackerConfig() TrackerConfig {
	return TrackerConfig{
		PollInterval:     50 * time.Millisecond, // 20Hz
		SpeakingLatchDur: 500 * time.Millisecond,
		EMAAlpha:         0.3,
		HistorySize:      100,
		Confidence: ConfidenceConfig{
			Base:           0.3,
			SpeakingBonus:  0.4,
			StabilityBonus: 0.2,
		},
	}
}

// Result represents a processed, smoothed DOA reading
type Result struct {
	Reading

	SmoothedAngle   float64 `json:"smoothed_angle"`
	Confidence      float64 `json:"confidence"`
	SpeakingLatched bool    `json:"speaking_latched"`

	// Estimated position (from energy-based distance + angle)
	EstX float64 `json:"est_x"` // Forward distance (meters)
	EstY float64 `json:"est_y"` // Lateral position (meters, + = left)
}

// Tracker smooths and processes DOA readings
type Tracker struct {
	source Source
	cfg    TrackerConfig
	logger *slog.Logger

	mu      sync.RWMutex
	latest  Result
	history []Result

	// Speaking latch state
	speakingLatchedAt time.Time

	// Metrics
	pollCount      int64
	pollErrorCount int64
	totalLatencyMs int64

	// Lifecycle
	cancel context.CancelFunc
	done   chan struct{}

	// Subscribers for real-time updates
	subsMu sync.RWMutex
	subs   map[chan Result]struct{}
}

// NewTracker creates a new DOA tracker
func NewTracker(source Source, cfg TrackerConfig, logger *slog.Logger) *Tracker {
	if logger == nil {
		logger = slog.Default()
	}

	return &Tracker{
		source:  source,
		cfg:     cfg,
		logger:  logger,
		history: make([]Result, 0, cfg.HistorySize),
		done:    make(chan struct{}),
		subs:    make(map[chan Result]struct{}),
	}
}

// Run starts the polling loop (blocking, use goroutine)
func (t *Tracker) Run(ctx context.Context) error {
	ctx, t.cancel = context.WithCancel(ctx)
	defer close(t.done)

	ticker := time.NewTicker(t.cfg.PollInterval)
	defer ticker.Stop()

	t.logger.Info("tracker started",
		"poll_interval", t.cfg.PollInterval,
		"ema_alpha", t.cfg.EMAAlpha,
		"speaking_latch", t.cfg.SpeakingLatchDur,
		"source", t.source.Name(),
	)

	for {
		select {
		case <-ctx.Done():
			t.logger.Info("tracker stopped",
				"polls", t.pollCount,
				"errors", t.pollErrorCount,
			)
			return ctx.Err()
		case <-ticker.C:
			if err := t.poll(ctx); err != nil {
				t.logger.Warn("poll failed", "error", err)
			}
		}
	}
}

func (t *Tracker) poll(ctx context.Context) error {
	start := time.Now()

	reading, err := t.source.GetDOA(ctx)
	if err != nil {
		t.mu.Lock()
		t.pollErrorCount++
		t.mu.Unlock()
		return err
	}

	latencyMs := time.Since(start).Milliseconds()
	reading.LatencyMs = latencyMs

	t.mu.Lock()
	defer t.mu.Unlock()

	t.pollCount++
	t.totalLatencyMs += latencyMs

	// Latch speaking flag
	speakingLatched := t.updateSpeakingLatch(reading.Speaking)

	// Smooth angle with EMA
	smoothedAngle := reading.Angle
	if len(t.history) > 0 {
		prev := t.latest.SmoothedAngle
		smoothedAngle = t.cfg.EMAAlpha*reading.Angle + (1-t.cfg.EMAAlpha)*prev
	}

	// Calculate confidence
	confidence := t.calculateConfidence(speakingLatched, smoothedAngle)

	// Calculate estimated position from energy-based distance
	estX := reading.EstimatedX()
	estY := reading.EstimatedY()

	result := Result{
		Reading:         reading,
		SmoothedAngle:   smoothedAngle,
		Confidence:      confidence,
		SpeakingLatched: speakingLatched,
		EstX:            estX,
		EstY:            estY,
	}

	t.latest = result
	t.appendHistory(result)

	// Notify subscribers (non-blocking)
	t.notifySubscribers(result)

	if speakingLatched && t.pollCount%10 == 0 {
		t.logger.Debug("doa poll",
			"angle", smoothedAngle,
			"speaking", speakingLatched,
			"confidence", confidence,
			"latency_ms", latencyMs,
			"total_energy", reading.TotalEnergy,
			"est_x", estX,
			"est_y", estY,
		)
	}

	return nil
}

func (t *Tracker) updateSpeakingLatch(rawSpeaking bool) bool {
	now := time.Now()

	if rawSpeaking {
		t.speakingLatchedAt = now
		return true
	}

	// Keep latched for duration after last detection
	if time.Since(t.speakingLatchedAt) < t.cfg.SpeakingLatchDur {
		return true
	}

	return false
}

func (t *Tracker) calculateConfidence(speaking bool, angle float64) float64 {
	conf := t.cfg.Confidence.Base

	if speaking {
		conf += t.cfg.Confidence.SpeakingBonus
	}

	// Check angle stability over last 5 readings
	if len(t.history) >= 5 {
		var variance float64
		for i := len(t.history) - 5; i < len(t.history); i++ {
			diff := t.history[i].SmoothedAngle - angle
			variance += diff * diff
		}
		variance /= 5

		if variance < 0.01 {
			conf += t.cfg.Confidence.StabilityBonus
		}
	}

	return Clamp(conf, 0, 1)
}

func (t *Tracker) appendHistory(result Result) {
	t.history = append(t.history, result)

	// Trim history
	if len(t.history) > t.cfg.HistorySize {
		// Shift instead of slice to avoid memory leak
		copy(t.history, t.history[1:])
		t.history = t.history[:t.cfg.HistorySize]
	}
}

func (t *Tracker) notifySubscribers(result Result) {
	t.subsMu.RLock()
	defer t.subsMu.RUnlock()

	for ch := range t.subs {
		select {
		case ch <- result:
		default:
			// Drop if subscriber is slow
		}
	}
}

// Subscribe returns a channel that receives DOA updates
func (t *Tracker) Subscribe() chan Result {
	ch := make(chan Result, 10) // Buffer to avoid blocking

	t.subsMu.Lock()
	t.subs[ch] = struct{}{}
	t.subsMu.Unlock()

	return ch
}

// Unsubscribe removes a subscriber
func (t *Tracker) Unsubscribe(ch chan Result) {
	t.subsMu.Lock()
	if _, exists := t.subs[ch]; exists {
		delete(t.subs, ch)
		close(ch)
	}
	t.subsMu.Unlock()
}

// GetLatest returns the most recent DOA result
func (t *Tracker) GetLatest() Result {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.latest
}

// GetTarget returns the current target angle if confidence is high enough
func (t *Tracker) GetTarget() (angle float64, confidence float64, ok bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.latest.Confidence < t.cfg.Confidence.Base {
		return 0, 0, false
	}

	return t.latest.SmoothedAngle, t.latest.Confidence, true
}

// Stats returns tracker statistics
func (t *Tracker) Stats() TrackerStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	avgLatency := float64(0)
	if t.pollCount > 0 {
		avgLatency = float64(t.totalLatencyMs) / float64(t.pollCount)
	}

	return TrackerStats{
		PollCount:         t.pollCount,
		ErrorCount:        t.pollErrorCount,
		AvgLatencyMs:      avgLatency,
		HistorySize:       len(t.history),
		SubscriberCount:   len(t.subs),
		SourceHealthy:     t.source.Healthy(),
		SpeakingLatched:   t.latest.SpeakingLatched,
		CurrentAngle:      t.latest.SmoothedAngle,
		CurrentConfidence: t.latest.Confidence,
	}
}

// TrackerStats contains tracker statistics
type TrackerStats struct {
	PollCount         int64   `json:"poll_count"`
	ErrorCount        int64   `json:"error_count"`
	AvgLatencyMs      float64 `json:"avg_latency_ms"`
	HistorySize       int     `json:"history_size"`
	SubscriberCount   int     `json:"subscriber_count"`
	SourceHealthy     bool    `json:"source_healthy"`
	SpeakingLatched   bool    `json:"speaking_latched"`
	CurrentAngle      float64 `json:"current_angle"`
	CurrentConfidence float64 `json:"current_confidence"`
}

// Stop stops the tracker gracefully
func (t *Tracker) Stop() {
	if t.cancel != nil {
		t.cancel()
		<-t.done
	}

	// Close all subscriber channels
	t.subsMu.Lock()
	for ch := range t.subs {
		close(ch)
		delete(t.subs, ch)
	}
	t.subsMu.Unlock()
}
