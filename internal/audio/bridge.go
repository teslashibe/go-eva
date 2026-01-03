// Package audio provides audio capture and playback for robot-cloud communication
package audio

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// Config holds audio bridge configuration
type Config struct {
	SampleRate    int           // Sample rate in Hz (default: 16000)
	Channels      int           // Number of channels (default: 1 for mono)
	ChunkDuration time.Duration // Duration of each audio chunk (default: 100ms)
	PlaybackCmd   string        // Command for audio playback (default: "aplay")
	CaptureCmd    string        // Command for audio capture (default: "arecord")
}

// DefaultConfig returns sensible defaults for Raspberry Pi
func DefaultConfig() Config {
	return Config{
		SampleRate:    16000,
		Channels:      1,
		ChunkDuration: 100 * time.Millisecond,
		PlaybackCmd:   "aplay",
		CaptureCmd:    "arecord",
	}
}

// AudioChunk represents a chunk of audio data
type AudioChunk struct {
	Data       []byte    // PCM16 audio data
	SampleRate int       // Sample rate
	Channels   int       // Channel count
	Timestamp  time.Time // Capture timestamp
}

// Bridge handles bidirectional audio streaming
type Bridge struct {
	cfg    Config
	logger *slog.Logger

	mu           sync.Mutex
	capturing    bool
	captureCmd   *exec.Cmd
	cancelFunc   context.CancelFunc

	// Callbacks
	onAudioChunk func(AudioChunk)

	// Stats
	chunksCaptured atomic.Uint64
	chunksPlayed   atomic.Uint64
	captureErrors  atomic.Uint64
	playbackErrors atomic.Uint64
}

// NewBridge creates a new audio bridge
func NewBridge(cfg Config, logger *slog.Logger) *Bridge {
	if logger == nil {
		logger = slog.Default()
	}

	return &Bridge{
		cfg:    cfg,
		logger: logger,
	}
}

// OnAudioChunk sets the callback for captured audio
func (b *Bridge) OnAudioChunk(callback func(AudioChunk)) {
	b.mu.Lock()
	b.onAudioChunk = callback
	b.mu.Unlock()
}

// StartCapture begins capturing audio from the microphone
func (b *Bridge) StartCapture(ctx context.Context) error {
	b.mu.Lock()
	if b.capturing {
		b.mu.Unlock()
		return nil
	}
	b.capturing = true

	ctx, b.cancelFunc = context.WithCancel(ctx)
	b.mu.Unlock()

	b.logger.Info("starting audio capture",
		"sample_rate", b.cfg.SampleRate,
		"channels", b.cfg.Channels,
	)

	go b.captureLoop(ctx)
	return nil
}

// StopCapture stops audio capture
func (b *Bridge) StopCapture() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.capturing {
		return
	}

	b.capturing = false
	if b.cancelFunc != nil {
		b.cancelFunc()
	}
	if b.captureCmd != nil && b.captureCmd.Process != nil {
		b.captureCmd.Process.Kill()
	}
	b.logger.Info("audio capture stopped")
}

// captureLoop runs the audio capture loop
func (b *Bridge) captureLoop(ctx context.Context) {
	chunkSize := b.cfg.SampleRate * b.cfg.Channels * 2 * int(b.cfg.ChunkDuration.Milliseconds()) / 1000

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		chunk, err := b.captureChunk(ctx, chunkSize)
		if err != nil {
			b.captureErrors.Add(1)
			b.logger.Debug("capture error", "error", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		b.chunksCaptured.Add(1)

		b.mu.Lock()
		callback := b.onAudioChunk
		b.mu.Unlock()

		if callback != nil {
			callback(*chunk)
		}
	}
}

// captureChunk captures a single audio chunk
func (b *Bridge) captureChunk(ctx context.Context, size int) (*AudioChunk, error) {
	// Use arecord to capture audio
	// arecord -f S16_LE -r 16000 -c 1 -d 0.1 -t raw -q
	duration := float64(b.cfg.ChunkDuration.Milliseconds()) / 1000.0

	cmd := exec.CommandContext(ctx, b.cfg.CaptureCmd,
		"-f", "S16_LE",
		"-r", fmt.Sprintf("%d", b.cfg.SampleRate),
		"-c", fmt.Sprintf("%d", b.cfg.Channels),
		"-d", fmt.Sprintf("%.3f", duration),
		"-t", "raw",
		"-q",
	)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("capture command failed: %w", err)
	}

	return &AudioChunk{
		Data:       stdout.Bytes(),
		SampleRate: b.cfg.SampleRate,
		Channels:   b.cfg.Channels,
		Timestamp:  time.Now(),
	}, nil
}

// PlayAudio plays audio data through the speaker
func (b *Bridge) PlayAudio(ctx context.Context, data []byte, format string, sampleRate int) error {
	// Decode base64 if needed
	audioData := data
	if format == "base64" {
		var err error
		audioData, err = base64.StdEncoding.DecodeString(string(data))
		if err != nil {
			b.playbackErrors.Add(1)
			return fmt.Errorf("decode base64: %w", err)
		}
	}

	// Use aplay to play audio
	// aplay -f S16_LE -r <rate> -c 1 -t raw -q
	cmd := exec.CommandContext(ctx, b.cfg.PlaybackCmd,
		"-f", "S16_LE",
		"-r", fmt.Sprintf("%d", sampleRate),
		"-c", "1",
		"-t", "raw",
		"-q",
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		b.playbackErrors.Add(1)
		return fmt.Errorf("stdin pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		b.playbackErrors.Add(1)
		return fmt.Errorf("start playback: %w", err)
	}

	go func() {
		io.Copy(stdin, bytes.NewReader(audioData))
		stdin.Close()
	}()

	if err := cmd.Wait(); err != nil {
		b.playbackErrors.Add(1)
		return fmt.Errorf("playback wait: %w", err)
	}

	b.chunksPlayed.Add(1)
	return nil
}

// PlayAudioAsync plays audio in the background
func (b *Bridge) PlayAudioAsync(data []byte, format string, sampleRate int) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := b.PlayAudio(ctx, data, format, sampleRate); err != nil {
			b.logger.Warn("async playback error", "error", err)
		}
	}()
}

// Stats contains audio bridge statistics
type Stats struct {
	ChunksCaptured uint64 `json:"chunks_captured"`
	ChunksPlayed   uint64 `json:"chunks_played"`
	CaptureErrors  uint64 `json:"capture_errors"`
	PlaybackErrors uint64 `json:"playback_errors"`
	Capturing      bool   `json:"capturing"`
}

// GetStats returns bridge statistics
func (b *Bridge) GetStats() Stats {
	b.mu.Lock()
	capturing := b.capturing
	b.mu.Unlock()

	return Stats{
		ChunksCaptured: b.chunksCaptured.Load(),
		ChunksPlayed:   b.chunksPlayed.Load(),
		CaptureErrors:  b.captureErrors.Load(),
		PlaybackErrors: b.playbackErrors.Load(),
		Capturing:      capturing,
	}
}

// Close stops all audio operations
func (b *Bridge) Close() error {
	b.StopCapture()
	return nil
}

// IsAvailable checks if audio commands are available
func (b *Bridge) IsAvailable() bool {
	_, err := exec.LookPath(b.cfg.PlaybackCmd)
	if err != nil {
		return false
	}
	_, err = exec.LookPath(b.cfg.CaptureCmd)
	return err == nil
}

