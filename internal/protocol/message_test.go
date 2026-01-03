package protocol

import (
	"encoding/json"
	"testing"
)

func TestNewMessage(t *testing.T) {
	msg, err := NewMessage(TypeFrame, FrameData{Width: 640, Height: 480})
	if err != nil {
		t.Fatalf("NewMessage() error = %v", err)
	}

	if msg.Type != TypeFrame {
		t.Errorf("Type = %v, want %v", msg.Type, TypeFrame)
	}

	if msg.Timestamp == 0 {
		t.Error("Timestamp should be set")
	}
}

func TestMessageRoundTrip(t *testing.T) {
	original := MotorCommand{
		Head:     HeadTarget{X: 0.1, Y: 0.2, Z: 0.3, Yaw: 0.5},
		Antennas: [2]float64{0.3, 0.7},
		BodyYaw:  0.1,
	}

	msg, err := NewMessage(TypeMotor, original)
	if err != nil {
		t.Fatalf("NewMessage() error = %v", err)
	}

	bytes, err := msg.Bytes()
	if err != nil {
		t.Fatalf("Bytes() error = %v", err)
	}

	parsed, err := ParseMessage(bytes)
	if err != nil {
		t.Fatalf("ParseMessage() error = %v", err)
	}

	if parsed.Type != TypeMotor {
		t.Errorf("Type = %v, want %v", parsed.Type, TypeMotor)
	}

	cmd, err := parsed.GetMotorCommand()
	if err != nil {
		t.Fatalf("GetMotorCommand() error = %v", err)
	}

	if cmd.Head.X != 0.1 {
		t.Errorf("Head.X = %v, want 0.1", cmd.Head.X)
	}
}

func TestNewFrameMessage(t *testing.T) {
	jpegData := []byte{0xFF, 0xD8, 0xFF, 0xE0}

	msg, err := NewFrameMessage(1920, 1080, jpegData, 42)
	if err != nil {
		t.Fatalf("NewFrameMessage() error = %v", err)
	}

	if msg.Type != TypeFrame {
		t.Errorf("Type = %v, want %v", msg.Type, TypeFrame)
	}

	var frameData FrameData
	if err := msg.ParseData(&frameData); err != nil {
		t.Fatalf("ParseData() error = %v", err)
	}

	if frameData.Width != 1920 {
		t.Errorf("Width = %v, want 1920", frameData.Width)
	}

	if frameData.FrameID != 42 {
		t.Errorf("FrameID = %v, want 42", frameData.FrameID)
	}
}

func TestNewDOAMessage(t *testing.T) {
	msg, err := NewDOAMessage(0.5, 0.48, true, true, 0.95)
	if err != nil {
		t.Fatalf("NewDOAMessage() error = %v", err)
	}

	if msg.Type != TypeDOA {
		t.Errorf("Type = %v, want %v", msg.Type, TypeDOA)
	}
}

func TestParseInvalidMessage(t *testing.T) {
	_, err := ParseMessage([]byte("not json"))
	if err == nil {
		t.Error("ParseMessage should fail for invalid JSON")
	}
}

func TestMessageJSONFormat(t *testing.T) {
	msg, _ := NewMessage(TypePing, nil)
	bytes, _ := msg.Bytes()

	var parsed map[string]interface{}
	if err := json.Unmarshal(bytes, &parsed); err != nil {
		t.Fatalf("JSON unmarshal failed: %v", err)
	}

	if parsed["type"] != "ping" {
		t.Errorf("type = %v, want ping", parsed["type"])
	}
}

