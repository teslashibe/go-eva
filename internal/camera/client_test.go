package camera

import (
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Framerate <= 0 {
		t.Error("Framerate should be positive")
	}
	if cfg.Quality <= 0 || cfg.Quality > 100 {
		t.Error("Quality should be 1-100")
	}
	if cfg.Timeout <= 0 {
		t.Error("Timeout should be positive")
	}
}

func TestNewClient(t *testing.T) {
	cfg := DefaultConfig()
	client := NewClient(cfg, nil)

	if client == nil {
		t.Fatal("NewClient returned nil")
	}

	stats := client.Stats()
	if stats.Running {
		t.Error("Client should not be running initially")
	}
}

func TestCaptureFrame(t *testing.T) {
	// Create a test JPEG image
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}

	// Create test server that returns JPEG
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/video/snapshot" {
			w.Header().Set("Content-Type", "image/jpeg")
			jpeg.Encode(w, img, &jpeg.Options{Quality: 80})
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.PollenURL = server.URL
	cfg.Quality = 80

	client := NewClient(cfg, nil)

	frame, err := client.captureFrame(context.Background())
	if err != nil {
		t.Fatalf("captureFrame() error = %v", err)
	}

	if frame == nil {
		t.Fatal("captureFrame() returned nil frame")
	}

	if frame.Width != 100 {
		t.Errorf("Width = %d, want 100", frame.Width)
	}

	if frame.Height != 100 {
		t.Errorf("Height = %d, want 100", frame.Height)
	}

	if len(frame.Data) == 0 {
		t.Error("Frame data should not be empty")
	}

	if frame.FrameID != 1 {
		t.Errorf("FrameID = %d, want 1", frame.FrameID)
	}
}

func TestStartStop(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PollenURL = "http://localhost:12345" // Non-existent server
	cfg.Framerate = 100                       // Fast for testing

	client := NewClient(cfg, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := client.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	stats := client.Stats()
	if !stats.Running {
		t.Error("Client should be running after Start()")
	}

	// Wait a bit for capture attempts
	time.Sleep(50 * time.Millisecond)

	client.Stop()

	stats = client.Stats()
	if stats.Running {
		t.Error("Client should not be running after Stop()")
	}
}

func TestOnFrameCallback(t *testing.T) {
	// Create test image
	img := image.NewRGBA(image.Rect(0, 0, 50, 50))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		jpeg.Encode(w, img, &jpeg.Options{Quality: 80})
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.PollenURL = server.URL
	cfg.Framerate = 100

	client := NewClient(cfg, nil)

	frameReceived := make(chan Frame, 1)
	client.OnFrame(func(f Frame) {
		select {
		case frameReceived <- f:
		default:
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	client.Start(ctx)

	select {
	case frame := <-frameReceived:
		if frame.Width != 50 {
			t.Errorf("Width = %d, want 50", frame.Width)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Timeout waiting for frame callback")
	}

	cancel()
	client.Stop()
}

func TestCaptureFrameError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.PollenURL = server.URL

	client := NewClient(cfg, nil)

	_, err := client.captureFrame(context.Background())
	if err == nil {
		t.Error("captureFrame() should return error for 500 response")
	}
}

func TestReencodeJPEG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))

	cfg := DefaultConfig()
	client := NewClient(cfg, nil)

	data, err := client.reencodeJPEG(img, 50)
	if err != nil {
		t.Fatalf("reencodeJPEG() error = %v", err)
	}

	if len(data) == 0 {
		t.Error("Reencoded data should not be empty")
	}
}

func TestGetLastFrame(t *testing.T) {
	cfg := DefaultConfig()
	client := NewClient(cfg, nil)

	// Initially nil
	if client.GetLastFrame() != nil {
		t.Error("GetLastFrame() should return nil initially")
	}
}

