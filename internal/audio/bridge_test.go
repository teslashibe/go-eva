package audio

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.SampleRate <= 0 {
		t.Error("SampleRate should be positive")
	}
	if cfg.Channels <= 0 {
		t.Error("Channels should be positive")
	}
	if cfg.ChunkDuration <= 0 {
		t.Error("ChunkDuration should be positive")
	}
	if cfg.PlaybackCmd == "" {
		t.Error("PlaybackCmd should not be empty")
	}
	if cfg.CaptureCmd == "" {
		t.Error("CaptureCmd should not be empty")
	}
}

func TestNewBridge(t *testing.T) {
	cfg := DefaultConfig()
	bridge := NewBridge(cfg, nil)

	if bridge == nil {
		t.Fatal("NewBridge returned nil")
	}

	stats := bridge.GetStats()
	if stats.Capturing {
		t.Error("Should not be capturing initially")
	}
	if stats.ChunksCaptured != 0 {
		t.Error("ChunksCaptured should be 0 initially")
	}
}

func TestOnAudioChunkCallback(t *testing.T) {
	cfg := DefaultConfig()
	bridge := NewBridge(cfg, nil)

	var callCount atomic.Int32
	bridge.OnAudioChunk(func(chunk AudioChunk) {
		callCount.Add(1)
	})

	// Verify callback was set (we can't easily test actual capture without hardware)
	if callCount.Load() != 0 {
		t.Error("Callback should not have been called yet")
	}
}

func TestStartStopCapture(t *testing.T) {
	cfg := DefaultConfig()
	// Use a command that doesn't exist to make capture fail quickly
	cfg.CaptureCmd = "nonexistent_command_12345"
	
	bridge := NewBridge(cfg, nil)

	ctx, cancel := context.WithCancel(context.Background())

	err := bridge.StartCapture(ctx)
	if err != nil {
		t.Fatalf("StartCapture() error = %v", err)
	}

	stats := bridge.GetStats()
	if !stats.Capturing {
		t.Error("Should be capturing after StartCapture")
	}

	// Wait a bit for capture attempts
	time.Sleep(50 * time.Millisecond)

	cancel()
	bridge.StopCapture()

	stats = bridge.GetStats()
	if stats.Capturing {
		t.Error("Should not be capturing after StopCapture")
	}
}

func TestDoubleStartCapture(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CaptureCmd = "nonexistent_command_12345"
	
	bridge := NewBridge(cfg, nil)

	ctx := context.Background()

	// Start twice - should be idempotent
	bridge.StartCapture(ctx)
	err := bridge.StartCapture(ctx)
	if err != nil {
		t.Errorf("Second StartCapture() should not error: %v", err)
	}

	bridge.StopCapture()
}

func TestDoubleStopCapture(t *testing.T) {
	cfg := DefaultConfig()
	bridge := NewBridge(cfg, nil)

	// Stop when not capturing - should be safe
	bridge.StopCapture()
	bridge.StopCapture()
	// No panic = pass
}

func TestClose(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CaptureCmd = "nonexistent_command_12345"
	
	bridge := NewBridge(cfg, nil)

	bridge.StartCapture(context.Background())
	
	err := bridge.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	stats := bridge.GetStats()
	if stats.Capturing {
		t.Error("Should not be capturing after Close")
	}
}

func TestGetStats(t *testing.T) {
	cfg := DefaultConfig()
	bridge := NewBridge(cfg, nil)

	stats := bridge.GetStats()

	// All should be zero initially
	if stats.ChunksCaptured != 0 {
		t.Error("ChunksCaptured should be 0")
	}
	if stats.ChunksPlayed != 0 {
		t.Error("ChunksPlayed should be 0")
	}
	if stats.CaptureErrors != 0 {
		t.Error("CaptureErrors should be 0")
	}
	if stats.PlaybackErrors != 0 {
		t.Error("PlaybackErrors should be 0")
	}
}

func TestIsAvailable(t *testing.T) {
	cfg := DefaultConfig()
	
	// Test with non-existent commands
	cfg.PlaybackCmd = "nonexistent_command_12345"
	bridge := NewBridge(cfg, nil)
	
	if bridge.IsAvailable() {
		t.Error("IsAvailable should return false for non-existent commands")
	}
}

func TestPlayAudioInvalidBase64(t *testing.T) {
	cfg := DefaultConfig()
	bridge := NewBridge(cfg, nil)

	err := bridge.PlayAudio(context.Background(), []byte("not valid base64!!!"), "base64", 16000)
	if err == nil {
		t.Error("PlayAudio should return error for invalid base64")
	}

	stats := bridge.GetStats()
	if stats.PlaybackErrors != 1 {
		t.Errorf("PlaybackErrors = %d, want 1", stats.PlaybackErrors)
	}
}

func TestPlayAudioAsyncNoBlock(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PlaybackCmd = "nonexistent_command_12345"
	
	bridge := NewBridge(cfg, nil)

	// This should not block
	done := make(chan bool)
	go func() {
		bridge.PlayAudioAsync([]byte("test"), "raw", 16000)
		done <- true
	}()

	select {
	case <-done:
		// Good - returned immediately
	case <-time.After(100 * time.Millisecond):
		t.Error("PlayAudioAsync should return immediately")
	}
}

func TestAudioChunkStruct(t *testing.T) {
	chunk := AudioChunk{
		Data:       []byte{0x00, 0x01, 0x02},
		SampleRate: 16000,
		Channels:   1,
		Timestamp:  time.Now(),
	}

	if len(chunk.Data) != 3 {
		t.Errorf("Data length = %d, want 3", len(chunk.Data))
	}
	if chunk.SampleRate != 16000 {
		t.Errorf("SampleRate = %d, want 16000", chunk.SampleRate)
	}
}


