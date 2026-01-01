package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/teslashibe/go-eva/internal/config"
	"github.com/teslashibe/go-eva/internal/doa"
	"github.com/teslashibe/go-eva/internal/xvf3800"
)

func setupTestServer(t *testing.T) (*Server, *doa.Tracker) {
	t.Helper()

	cfg := config.ServerConfig{
		Port:            9000,
		ReadTimeout:     10 * time.Second,
		WriteTimeout:    10 * time.Second,
		GracefulTimeout: 5 * time.Second,
	}

	source := xvf3800.NewMockSource()
	source.SetSpeaking(true)

	trackerCfg := doa.DefaultTrackerConfig()
	trackerCfg.PollInterval = 10 * time.Millisecond

	logger := slog.Default()
	tracker := doa.NewTracker(source, trackerCfg, logger)

	server := New(cfg, tracker, logger, "test")

	return server, tracker
}

func TestServer_Health(t *testing.T) {
	server, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/health", nil)
	resp, err := server.app.Test(req, -1)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if result["version"] != "test" {
		t.Errorf("expected version 'test', got %v", result["version"])
	}

	if _, ok := result["uptime_seconds"]; !ok {
		t.Error("expected uptime_seconds in response")
	}
}

func TestServer_DOA(t *testing.T) {
	server, tracker := setupTestServer(t)

	// Run tracker briefly to get a reading
	go func() {
		tracker.Run(t.Context())
	}()
	time.Sleep(50 * time.Millisecond)

	req := httptest.NewRequest("GET", "/api/audio/doa", nil)
	resp, err := server.app.Test(req, -1)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	var result doa.Result
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	// Should have a timestamp
	if result.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}

	tracker.Stop()
}

func TestServer_Stats(t *testing.T) {
	server, tracker := setupTestServer(t)

	// Run tracker briefly
	go func() {
		tracker.Run(t.Context())
	}()
	time.Sleep(50 * time.Millisecond)

	req := httptest.NewRequest("GET", "/api/stats", nil)
	resp, err := server.app.Test(req, -1)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	var stats doa.TrackerStats
	if err := json.Unmarshal(body, &stats); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if stats.PollCount == 0 {
		t.Error("expected non-zero poll count")
	}

	tracker.Stop()
}

func TestServer_Metrics(t *testing.T) {
	server, tracker := setupTestServer(t)

	// Run tracker briefly
	go func() {
		tracker.Run(t.Context())
	}()
	time.Sleep(50 * time.Millisecond)

	req := httptest.NewRequest("GET", "/metrics", nil)
	resp, err := server.app.Test(req, -1)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	bodyStr := string(body)

	// Check for expected metrics
	expectedMetrics := []string{
		"go_eva_doa_angle_radians",
		"go_eva_speaking",
		"go_eva_doa_confidence",
		"go_eva_poll_count",
		"go_eva_source_healthy",
	}

	for _, metric := range expectedMetrics {
		if !contains(bodyStr, metric) {
			t.Errorf("expected metric %s in response", metric)
		}
	}

	tracker.Stop()
}

func TestServer_Config(t *testing.T) {
	server, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/config", nil)
	resp, err := server.app.Test(req, -1)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	serverCfg := result["server"].(map[string]interface{})
	if serverCfg["port"].(float64) != 9000 {
		t.Errorf("expected port 9000, got %v", serverCfg["port"])
	}
}

func TestServer_DOAStream_UpgradeRequired(t *testing.T) {
	server, _ := setupTestServer(t)

	// Non-WebSocket request should get 426
	req := httptest.NewRequest("GET", "/api/audio/doa/stream", nil)
	resp, err := server.app.Test(req, -1)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 426 {
		t.Errorf("expected status 426, got %d", resp.StatusCode)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

