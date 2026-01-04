package pollen

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.BaseURL == "" {
		t.Error("BaseURL should not be empty")
	}
	if cfg.Timeout <= 0 {
		t.Error("Timeout should be positive")
	}
	if cfg.RateLimitHz <= 0 {
		t.Error("RateLimitHz should be positive")
	}
}

func TestNewClient(t *testing.T) {
	cfg := DefaultConfig()
	client := NewClient(cfg, nil)

	if client == nil {
		t.Fatal("NewClient returned nil")
	}

	stats := client.GetStats()
	if stats.CommandsSent != 0 {
		t.Error("CommandsSent should be 0 initially")
	}
}

func TestSetTarget(t *testing.T) {
	var receivedTarget FullBodyTarget
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/move/set_target" && r.Method == "POST" {
			requestCount.Add(1)
			json.NewDecoder(r.Body).Decode(&receivedTarget)
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.BaseURL = server.URL
	cfg.RateLimitHz = 0 // No rate limit for test

	client := NewClient(cfg, nil)

	head := HeadTarget{X: 0.1, Y: 0.2, Z: 0.3, Yaw: 0.5}
	antennas := [2]float64{0.3, 0.7}

	err := client.SetTarget(context.Background(), head, antennas, 0.1)
	if err != nil {
		t.Fatalf("SetTarget() error = %v", err)
	}

	if requestCount.Load() != 1 {
		t.Errorf("Expected 1 request, got %d", requestCount.Load())
	}

	if receivedTarget.TargetHeadPose.X != 0.1 {
		t.Errorf("Head.X = %v, want 0.1", receivedTarget.TargetHeadPose.X)
	}

	if receivedTarget.TargetAntennas[0] != 0.3 {
		t.Errorf("Antennas[0] = %v, want 0.3", receivedTarget.TargetAntennas[0])
	}

	stats := client.GetStats()
	if stats.CommandsSent != 1 {
		t.Errorf("CommandsSent = %d, want 1", stats.CommandsSent)
	}
}

func TestSetTargetRateLimit(t *testing.T) {
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.BaseURL = server.URL
	cfg.RateLimitHz = 10 // 10 Hz = 100ms between commands

	client := NewClient(cfg, nil)

	head := HeadTarget{}
	antennas := [2]float64{0, 0}

	// Send 5 commands rapidly
	for i := 0; i < 5; i++ {
		client.SetTarget(context.Background(), head, antennas, 0)
	}

	// Only 1 should have gone through due to rate limiting
	if requestCount.Load() != 1 {
		t.Errorf("Expected 1 request due to rate limiting, got %d", requestCount.Load())
	}
}

func TestSetTargetError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.BaseURL = server.URL
	cfg.RateLimitHz = 0

	client := NewClient(cfg, nil)

	err := client.SetTarget(context.Background(), HeadTarget{}, [2]float64{}, 0)
	if err == nil {
		t.Error("SetTarget should return error for 500 response")
	}

	stats := client.GetStats()
	if stats.CommandErrors != 1 {
		t.Errorf("CommandErrors = %d, want 1", stats.CommandErrors)
	}
}

func TestPlayEmotion(t *testing.T) {
	var receivedEmotion EmotionRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/emotion/play" && r.Method == "POST" {
			json.NewDecoder(r.Body).Decode(&receivedEmotion)
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.BaseURL = server.URL

	client := NewClient(cfg, nil)

	err := client.PlayEmotion(context.Background(), "happy", 2.5)
	if err != nil {
		t.Fatalf("PlayEmotion() error = %v", err)
	}

	if receivedEmotion.Name != "happy" {
		t.Errorf("Name = %v, want happy", receivedEmotion.Name)
	}

	if receivedEmotion.Duration != 2.5 {
		t.Errorf("Duration = %v, want 2.5", receivedEmotion.Duration)
	}

	stats := client.GetStats()
	if stats.EmotionsSent != 1 {
		t.Errorf("EmotionsSent = %d, want 1", stats.EmotionsSent)
	}
}

func TestGetStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/daemon/status" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"state":   "running",
				"version": "1.0.0",
			})
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.BaseURL = server.URL

	client := NewClient(cfg, nil)

	status, err := client.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus() error = %v", err)
	}

	if status["state"] != "running" {
		t.Errorf("state = %v, want running", status["state"])
	}
}

func TestStartDaemon(t *testing.T) {
	var called bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/daemon/start" && r.Method == "POST" {
			called = true
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.BaseURL = server.URL

	client := NewClient(cfg, nil)

	err := client.StartDaemon(context.Background())
	if err != nil {
		t.Fatalf("StartDaemon() error = %v", err)
	}

	if !called {
		t.Error("StartDaemon endpoint should have been called")
	}
}

func TestIsHealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/daemon/status" {
			json.NewEncoder(w).Encode(map[string]interface{}{"state": "running"})
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.BaseURL = server.URL

	client := NewClient(cfg, nil)

	if !client.IsHealthy(context.Background()) {
		t.Error("IsHealthy should return true when daemon is reachable")
	}
}

func TestIsHealthyFalse(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BaseURL = "http://localhost:12345" // Non-existent

	client := NewClient(cfg, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if client.IsHealthy(ctx) {
		t.Error("IsHealthy should return false when daemon is unreachable")
	}
}

