// Package protocol defines the WebSocket message types for robot-cloud communication.
// This is a copy of the protocol from go-reachy for robot-side use.
package protocol

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// MessageType identifies the type of WebSocket message
type MessageType string

const (
	// Robot → Cloud messages
	TypeFrame MessageType = "frame" // Video frame
	TypeDOA   MessageType = "doa"   // Direction of arrival
	TypeMic   MessageType = "mic"   // Microphone audio
	TypeState MessageType = "state" // Robot state

	// Cloud → Robot messages
	TypeMotor   MessageType = "motor"   // Motor command
	TypeSpeak   MessageType = "speak"   // TTS audio playback
	TypeEmotion MessageType = "emotion" // Play emotion animation
	TypeConfig  MessageType = "config"  // Configuration update

	// Bidirectional
	TypePing MessageType = "ping"
	TypePong MessageType = "pong"
)

// Message is the base wrapper for all WebSocket messages
type Message struct {
	Type      MessageType     `json:"type"`
	Timestamp int64           `json:"ts,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// NewMessage creates a new message with the current timestamp
func NewMessage(msgType MessageType, data interface{}) (*Message, error) {
	var rawData json.RawMessage
	if data != nil {
		var err error
		rawData, err = json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal message data: %w", err)
		}
	}

	return &Message{
		Type:      msgType,
		Timestamp: time.Now().UnixMilli(),
		Data:      rawData,
	}, nil
}

// ParseData unmarshals the message data into the provided struct
func (m *Message) ParseData(v interface{}) error {
	if m.Data == nil {
		return nil
	}
	return json.Unmarshal(m.Data, v)
}

// Bytes returns the JSON-encoded message
func (m *Message) Bytes() ([]byte, error) {
	return json.Marshal(m)
}

// ParseMessage parses a JSON message from bytes
func ParseMessage(data []byte) (*Message, error) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("failed to parse message: %w", err)
	}
	return &msg, nil
}

// FrameData contains a video frame
type FrameData struct {
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Format  string `json:"format"`
	Data    string `json:"data"`
	FrameID uint64 `json:"frame_id,omitempty"`
}

// NewFrameMessage creates a frame message from raw JPEG data
func NewFrameMessage(width, height int, jpegData []byte, frameID uint64) (*Message, error) {
	return NewMessage(TypeFrame, FrameData{
		Width:   width,
		Height:  height,
		Format:  "jpeg",
		Data:    base64.StdEncoding.EncodeToString(jpegData),
		FrameID: frameID,
	})
}

// DOAData contains direction of arrival information
type DOAData struct {
	Angle           float64 `json:"angle"`
	SmoothedAngle   float64 `json:"smoothed_angle"`
	Speaking        bool    `json:"speaking"`
	SpeakingLatched bool    `json:"speaking_latched"`
	Confidence      float64 `json:"confidence"`
}

// NewDOAMessage creates a DOA message
func NewDOAMessage(angle, smoothedAngle float64, speaking, speakingLatched bool, confidence float64) (*Message, error) {
	return NewMessage(TypeDOA, DOAData{
		Angle:           angle,
		SmoothedAngle:   smoothedAngle,
		Speaking:        speaking,
		SpeakingLatched: speakingLatched,
		Confidence:      confidence,
	})
}

// MotorCommand contains motor movement instructions
type MotorCommand struct {
	Head     HeadTarget `json:"head"`
	Antennas [2]float64 `json:"antennas"`
	BodyYaw  float64    `json:"body_yaw"`
}

// HeadTarget specifies head position
type HeadTarget struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Z     float64 `json:"z"`
	Roll  float64 `json:"roll"`
	Pitch float64 `json:"pitch"`
	Yaw   float64 `json:"yaw"`
}

// GetMotorCommand extracts motor command from a message
func (m *Message) GetMotorCommand() (*MotorCommand, error) {
	var data MotorCommand
	if err := m.ParseData(&data); err != nil {
		return nil, err
	}
	return &data, nil
}

// EmotionCommand triggers an emotion animation
type EmotionCommand struct {
	Name     string  `json:"name"`
	Duration float64 `json:"duration,omitempty"`
}

// GetEmotionCommand extracts emotion command from a message
func (m *Message) GetEmotionCommand() (*EmotionCommand, error) {
	var data EmotionCommand
	if err := m.ParseData(&data); err != nil {
		return nil, err
	}
	return &data, nil
}

// SpeakData contains TTS audio to play
type SpeakData struct {
	Format     string `json:"format"`
	SampleRate int    `json:"sample_rate"`
	Channels   int    `json:"channels"`
	Data       string `json:"data"`
}

// GetSpeakData extracts speak data from a message
func (m *Message) GetSpeakData() (*SpeakData, error) {
	var data SpeakData
	if err := m.ParseData(&data); err != nil {
		return nil, err
	}
	return &data, nil
}

// DecodeSpeakData decodes the base64 audio data
func (s *SpeakData) DecodeSpeakData() ([]byte, error) {
	return base64.StdEncoding.DecodeString(s.Data)
}

// ConfigUpdate contains configuration changes
type ConfigUpdate struct {
	Camera *CameraConfig `json:"camera,omitempty"`
}

// CameraConfig contains camera settings
type CameraConfig struct {
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	Framerate int    `json:"framerate,omitempty"`
	Quality   int    `json:"quality,omitempty"`
	Preset    string `json:"preset,omitempty"`
}

// GetConfigUpdate extracts config update from a message
func (m *Message) GetConfigUpdate() (*ConfigUpdate, error) {
	var data ConfigUpdate
	if err := m.ParseData(&data); err != nil {
		return nil, err
	}
	return &data, nil
}
