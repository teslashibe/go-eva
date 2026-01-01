package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.Server.Port != 9000 {
		t.Errorf("expected port 9000, got %d", cfg.Server.Port)
	}

	if cfg.Audio.PollHz != 20 {
		t.Errorf("expected poll_hz 20, got %d", cfg.Audio.PollHz)
	}

	if cfg.Audio.SpeakingLatchMs != 500 {
		t.Errorf("expected speaking_latch_ms 500, got %d", cfg.Audio.SpeakingLatchMs)
	}

	if cfg.Audio.EMAAlpha != 0.3 {
		t.Errorf("expected ema_alpha 0.3, got %f", cfg.Audio.EMAAlpha)
	}

	if cfg.Logging.Level != "info" {
		t.Errorf("expected level info, got %s", cfg.Logging.Level)
	}
}

func TestLoad_NoFile(t *testing.T) {
	// Load with non-existent file should use defaults
	cfg, err := Load("/nonexistent/path.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Server.Port != 9000 {
		t.Errorf("expected default port 9000, got %d", cfg.Server.Port)
	}
}

func TestLoad_WithFile(t *testing.T) {
	// Create temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
server:
  port: 8080
audio:
  poll_hz: 30
  speaking_latch_ms: 750
  ema_alpha: 0.5
logging:
  level: debug
  format: text
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Server.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Server.Port)
	}

	if cfg.Audio.PollHz != 30 {
		t.Errorf("expected poll_hz 30, got %d", cfg.Audio.PollHz)
	}

	if cfg.Audio.SpeakingLatchMs != 750 {
		t.Errorf("expected speaking_latch_ms 750, got %d", cfg.Audio.SpeakingLatchMs)
	}

	if cfg.Audio.EMAAlpha != 0.5 {
		t.Errorf("expected ema_alpha 0.5, got %f", cfg.Audio.EMAAlpha)
	}

	if cfg.Logging.Level != "debug" {
		t.Errorf("expected level debug, got %s", cfg.Logging.Level)
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	// Set environment variable
	os.Setenv("GOEVA_SERVER_PORT", "7777")
	defer os.Unsetenv("GOEVA_SERVER_PORT")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Server.Port != 7777 {
		t.Errorf("expected port 7777 from env, got %d", cfg.Server.Port)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{
			name:    "valid default config",
			modify:  func(c *Config) {},
			wantErr: false,
		},
		{
			name: "invalid port too low",
			modify: func(c *Config) {
				c.Server.Port = 0
			},
			wantErr: true,
		},
		{
			name: "invalid port too high",
			modify: func(c *Config) {
				c.Server.Port = 70000
			},
			wantErr: true,
		},
		{
			name: "invalid poll_hz too low",
			modify: func(c *Config) {
				c.Audio.PollHz = 0
			},
			wantErr: true,
		},
		{
			name: "invalid ema_alpha too high",
			modify: func(c *Config) {
				c.Audio.EMAAlpha = 1.5
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tt.modify(cfg)

			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestServerConfig_Timeouts(t *testing.T) {
	cfg := Default()

	if cfg.Server.ReadTimeout != 10*time.Second {
		t.Errorf("expected read_timeout 10s, got %v", cfg.Server.ReadTimeout)
	}

	if cfg.Server.WriteTimeout != 10*time.Second {
		t.Errorf("expected write_timeout 10s, got %v", cfg.Server.WriteTimeout)
	}

	if cfg.Server.GracefulTimeout != 5*time.Second {
		t.Errorf("expected graceful_timeout 5s, got %v", cfg.Server.GracefulTimeout)
	}
}

