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
	Logging LoggingConfig `mapstructure:"logging"`
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

	return nil
}
