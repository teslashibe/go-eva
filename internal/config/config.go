// Package config provides configuration management for go-eva
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config is the root configuration structure
type Config struct {
	Server  ServerConfig  `mapstructure:"server"`
	Audio   AudioConfig   `mapstructure:"audio"`
	Cloud   CloudConfig   `mapstructure:"cloud"`
	Pollen  PollenConfig  `mapstructure:"pollen"`
	Camera  CameraConfig  `mapstructure:"camera"`
	Logging LoggingConfig `mapstructure:"logging"`
}

// CloudConfig configures connection to go-reachy cloud
type CloudConfig struct {
	Enabled          bool          `mapstructure:"enabled"`
	URL              string        `mapstructure:"url"`
	ReconnectBackoff time.Duration `mapstructure:"reconnect_backoff"`
	MaxBackoff       time.Duration `mapstructure:"max_backoff"`
	PingInterval     time.Duration `mapstructure:"ping_interval"`
}

// PollenConfig configures connection to Pollen daemon
type PollenConfig struct {
	BaseURL     string        `mapstructure:"base_url"`
	Timeout     time.Duration `mapstructure:"timeout"`
	RateLimitHz int           `mapstructure:"rate_limit_hz"`
}

// CameraConfig configures camera capture
type CameraConfig struct {
	Enabled   bool `mapstructure:"enabled"`
	Framerate int  `mapstructure:"framerate"`
	Width     int  `mapstructure:"width"`
	Height    int  `mapstructure:"height"`
	Quality   int  `mapstructure:"quality"`
}

// ServerConfig configures the HTTP server
type ServerConfig struct {
	Port            int           `mapstructure:"port"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	GracefulTimeout time.Duration `mapstructure:"graceful_timeout"`
}

// AudioConfig configures DOA tracking
type AudioConfig struct {
	PollHz            int           `mapstructure:"poll_hz"`
	SpeakingLatchMs   int           `mapstructure:"speaking_latch_ms"`
	EMAAlpha          float64       `mapstructure:"ema_alpha"`
	HistorySize       int           `mapstructure:"history_size"`
	USBReconnectDelay time.Duration `mapstructure:"usb_reconnect_delay"`

	Confidence ConfidenceConfig `mapstructure:"confidence"`
}

// ConfidenceConfig configures confidence scoring
type ConfidenceConfig struct {
	Base           float64 `mapstructure:"base"`
	SpeakingBonus  float64 `mapstructure:"speaking_bonus"`
	StabilityBonus float64 `mapstructure:"stability_bonus"`
}

// LoggingConfig configures logging
type LoggingConfig struct {
	Level  string `mapstructure:"level"`  // debug, info, warn, error
	Format string `mapstructure:"format"` // json, text
}

// Default returns the default configuration
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Port:            9000,
			ReadTimeout:     10 * time.Second,
			WriteTimeout:    10 * time.Second,
			GracefulTimeout: 5 * time.Second,
		},
		Audio: AudioConfig{
			PollHz:            20,
			SpeakingLatchMs:   500,
			EMAAlpha:          0.3,
			HistorySize:       100,
			USBReconnectDelay: 1 * time.Second,
			Confidence: ConfidenceConfig{
				Base:           0.3,
				SpeakingBonus:  0.4,
				StabilityBonus: 0.2,
			},
		},
		Cloud: CloudConfig{
			Enabled:          true, // Enabled by default
			URL:              "ws://localhost:8888/ws/robot",
			ReconnectBackoff: 1 * time.Second,
			MaxBackoff:       30 * time.Second,
			PingInterval:     10 * time.Second,
		},
		Pollen: PollenConfig{
			BaseURL:     "http://localhost:8000",
			Timeout:     2 * time.Second,
			RateLimitHz: 30,
		},
		Camera: CameraConfig{
			Enabled:   true, // Enabled by default
			Framerate: 10,
			Width:     640,
			Height:    480,
			Quality:   80,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
		},
	}
}

// Load loads configuration from file and environment
func Load(path string) (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Config file
	if path != "" {
		v.SetConfigFile(path)
		v.SetConfigType("yaml")

		if err := v.ReadInConfig(); err != nil {
			// Config file not found is okay, use defaults
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
				// Only warn, don't fail - we have defaults
				fmt.Printf("Warning: config file not found at %s, using defaults\n", path)
			}
		}
	}

	// Environment variable overrides
	v.SetEnvPrefix("GOEVA")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	// Server defaults
	v.SetDefault("server.port", 9000)
	v.SetDefault("server.read_timeout", "10s")
	v.SetDefault("server.write_timeout", "10s")
	v.SetDefault("server.graceful_timeout", "5s")

	// Audio defaults
	v.SetDefault("audio.poll_hz", 20)
	v.SetDefault("audio.speaking_latch_ms", 500)
	v.SetDefault("audio.ema_alpha", 0.3)
	v.SetDefault("audio.history_size", 100)
	v.SetDefault("audio.usb_reconnect_delay", "1s")

	// Confidence defaults
	v.SetDefault("audio.confidence.base", 0.3)
	v.SetDefault("audio.confidence.speaking_bonus", 0.4)
	v.SetDefault("audio.confidence.stability_bonus", 0.2)

	// Cloud defaults
	v.SetDefault("cloud.enabled", true)
	v.SetDefault("cloud.url", "ws://localhost:8888/ws/robot")
	v.SetDefault("cloud.reconnect_backoff", "1s")
	v.SetDefault("cloud.max_backoff", "30s")
	v.SetDefault("cloud.ping_interval", "10s")

	// Pollen defaults
	v.SetDefault("pollen.base_url", "http://localhost:8000")
	v.SetDefault("pollen.timeout", "2s")
	v.SetDefault("pollen.rate_limit_hz", 30)

	// Camera defaults
	v.SetDefault("camera.enabled", true)
	v.SetDefault("camera.framerate", 10)
	v.SetDefault("camera.width", 640)
	v.SetDefault("camera.height", 480)
	v.SetDefault("camera.quality", 80)

	// Logging defaults
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}

	if c.Audio.PollHz < 1 || c.Audio.PollHz > 100 {
		return fmt.Errorf("poll_hz must be between 1 and 100, got %d", c.Audio.PollHz)
	}

	if c.Audio.EMAAlpha < 0 || c.Audio.EMAAlpha > 1 {
		return fmt.Errorf("ema_alpha must be between 0 and 1, got %f", c.Audio.EMAAlpha)
	}

	if c.Cloud.Enabled && c.Cloud.URL == "" {
		return fmt.Errorf("cloud.url is required when cloud is enabled")
	}

	if c.Camera.Enabled && (c.Camera.Framerate < 1 || c.Camera.Framerate > 60) {
		return fmt.Errorf("camera.framerate must be between 1 and 60, got %d", c.Camera.Framerate)
	}

	return nil
}
