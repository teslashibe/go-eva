package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/teslashibe/go-eva/internal/protocol"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.ReconnectBackoff <= 0 {
		t.Error("ReconnectBackoff should be positive")
	}
	if cfg.MaxBackoff <= 0 {
		t.Error("MaxBackoff should be positive")
	}
	if cfg.PingInterval <= 0 {
		t.Error("PingInterval should be positive")
	}
}

func TestNewClient(t *testing.T) {
	cfg := DefaultConfig()
	client := NewClient(cfg, nil)

	if client == nil {
		t.Fatal("NewClient returned nil")
	}

	if client.IsConnected() {
		t.Error("Client should not be connected initially")
	}
}

func TestSendFrameNotConnected(t *testing.T) {
	cfg := DefaultConfig()
	client := NewClient(cfg, nil)

	err := client.SendFrame(640, 480, []byte("test"), 1)
	if err == nil {
		t.Error("SendFrame should return error when not connected")
	}
}

func TestSendDOANotConnected(t *testing.T) {
	cfg := DefaultConfig()
	client := NewClient(cfg, nil)

	err := client.SendDOA(0.5, 0.48, true, true, 0.9)
	if err == nil {
		t.Error("SendDOA should return error when not connected")
	}
}

func TestGetStats(t *testing.T) {
	cfg := DefaultConfig()
	client := NewClient(cfg, nil)

	stats := client.GetStats()

	if stats.Connected {
		t.Error("Stats.Connected should be false initially")
	}
	if stats.MessagesSent != 0 {
		t.Error("Stats.MessagesSent should be 0 initially")
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func TestConnectAndSend(t *testing.T) {
	// Track messages received by server
	var messagesReceived atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("Upgrade error: %v", err)
			return
		}
		defer conn.Close()

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			messagesReceived.Add(1)

			// Parse and validate message
			var parsed protocol.Message
			if err := json.Unmarshal(msg, &parsed); err != nil {
				t.Logf("Parse error: %v", err)
			}
		}
	}))
	defer server.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	cfg := DefaultConfig()
	cfg.URL = wsURL
	cfg.ReconnectBackoff = 100 * time.Millisecond

	client := NewClient(cfg, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	// Wait for connection
	time.Sleep(200 * time.Millisecond)

	if !client.IsConnected() {
		t.Error("Client should be connected")
	}

	// Send a frame
	err = client.SendFrame(640, 480, []byte("test jpeg data"), 1)
	if err != nil {
		t.Errorf("SendFrame() error = %v", err)
	}

	// Send DOA
	err = client.SendDOA(0.5, 0.48, true, true, 0.9)
	if err != nil {
		t.Errorf("SendDOA() error = %v", err)
	}

	// Wait for messages to be received
	time.Sleep(100 * time.Millisecond)

	if messagesReceived.Load() < 2 {
		t.Errorf("Server should have received at least 2 messages, got %d", messagesReceived.Load())
	}

	stats := client.GetStats()
	if stats.MessagesSent < 2 {
		t.Errorf("MessagesSent should be at least 2, got %d", stats.MessagesSent)
	}

	client.Close()

	if client.IsConnected() {
		t.Error("Client should not be connected after Close()")
	}
}

func TestReceiveMotorCommand(t *testing.T) {
	var motorReceived atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Send a motor command
		motorCmd := protocol.MotorCommand{
			Head:     protocol.HeadTarget{X: 0.1, Y: 0.2, Z: 0.3},
			Antennas: [2]float64{0.5, 0.5},
			BodyYaw:  0.1,
		}
		msg, _ := protocol.NewMessage(protocol.TypeMotor, motorCmd)
		data, _ := json.Marshal(msg)
		conn.WriteMessage(websocket.TextMessage, data)

		// Keep connection alive
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	cfg := DefaultConfig()
	cfg.URL = wsURL

	client := NewClient(cfg, nil)
	client.OnMotorCommand(func(cmd protocol.MotorCommand) {
		if cmd.Head.X == 0.1 && cmd.Antennas[0] == 0.5 {
			motorReceived.Store(true)
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client.Connect(ctx)

	// Wait for message to be received
	time.Sleep(300 * time.Millisecond)

	if !motorReceived.Load() {
		t.Error("Motor command callback should have been called")
	}

	client.Close()
}

func TestReconnect(t *testing.T) {
	// Start server that closes connections
	var connectionCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		connectionCount.Add(1)

		// Close after brief delay
		time.Sleep(50 * time.Millisecond)
		conn.Close()
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	cfg := DefaultConfig()
	cfg.URL = wsURL
	cfg.ReconnectBackoff = 50 * time.Millisecond
	cfg.MaxBackoff = 100 * time.Millisecond

	client := NewClient(cfg, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	client.Connect(ctx)

	// Wait for multiple reconnection attempts
	time.Sleep(400 * time.Millisecond)

	// Multiple connections = reconnection happening
	if connectionCount.Load() < 2 {
		t.Errorf("Should have reconnected at least once, got %d connections", connectionCount.Load())
	}

	client.Close()
}

func TestCallbacksNotSet(t *testing.T) {
	// Server sends commands but client has no callbacks
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Send emotion command (no callback set)
		emotionCmd := protocol.EmotionCommand{Name: "happy"}
		msg, _ := protocol.NewMessage(protocol.TypeEmotion, emotionCmd)
		data, _ := json.Marshal(msg)
		conn.WriteMessage(websocket.TextMessage, data)

		// Keep alive
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	cfg := DefaultConfig()
	cfg.URL = wsURL

	client := NewClient(cfg, nil)
	// No callbacks set

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client.Connect(ctx)
	time.Sleep(200 * time.Millisecond)

	// Should not panic when receiving messages with no callbacks
	stats := client.GetStats()
	if stats.MessagesReceived < 1 {
		t.Error("Should have received at least 1 message")
	}

	client.Close()
}

