// go-eva: Shadow daemon for Reachy Mini with cloud connectivity
// Provides DOA, camera proxy, and motor control bridging to go-reachy cloud
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/teslashibe/go-eva/internal/camera"
	"github.com/teslashibe/go-eva/internal/cloud"
	"github.com/teslashibe/go-eva/internal/config"
	"github.com/teslashibe/go-eva/internal/doa"
	"github.com/teslashibe/go-eva/internal/pollen"
	"github.com/teslashibe/go-eva/internal/protocol"
	"github.com/teslashibe/go-eva/internal/server"
	"github.com/teslashibe/go-eva/internal/xvf3800"
)

var (
	version     = "2.0.0"
	configPath  = flag.String("config", "/etc/go-eva/config.yaml", "config file path")
	showVersion = flag.Bool("version", false, "print version and exit")
	debug       = flag.Bool("debug", false, "enable debug logging")
	useMock     = flag.Bool("mock", false, "use mock DOA source (for testing)")
	cloudURL    = flag.String("cloud", "", "cloud WebSocket URL (overrides config)")
	pollenURL   = flag.String("pollen", "", "Pollen daemon URL (overrides config)")
)

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Printf("go-eva %s\n", version)
		os.Exit(0)
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load config from %s: %v\n", *configPath, err)
		cfg = config.Default()
	}

	// Override from flags
	if *debug {
		cfg.Logging.Level = "debug"
	}
	if *cloudURL != "" {
		cfg.Cloud.URL = *cloudURL
		cfg.Cloud.Enabled = true
	}
	if *pollenURL != "" {
		cfg.Pollen.BaseURL = *pollenURL
	}

	// Setup logging
	logger := setupLogger(cfg.Logging)

	logger.Info("starting go-eva",
		"version", version,
		"config", *configPath,
		"port", cfg.Server.Port,
		"cloud_enabled", cfg.Cloud.Enabled,
	)

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	// Create root context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize DOA source
	var source doa.Source
	if *useMock {
		logger.Info("using mock DOA source")
		source = xvf3800.NewMockSourceWithWave()
	} else {
		logger.Info("initializing DOA source")
		source = xvf3800.NewSourceWithFallback(logger)
	}
	defer source.Close()

	logger.Info("DOA source ready",
		"type", source.Name(),
		"healthy", source.Healthy(),
	)

	// Create tracker configuration from config
	trackerCfg := doa.TrackerConfig{
		PollInterval:     time.Duration(1000/cfg.Audio.PollHz) * time.Millisecond,
		SpeakingLatchDur: time.Duration(cfg.Audio.SpeakingLatchMs) * time.Millisecond,
		EMAAlpha:         cfg.Audio.EMAAlpha,
		HistorySize:      cfg.Audio.HistorySize,
		Confidence: doa.ConfidenceConfig{
			Base:           cfg.Audio.Confidence.Base,
			SpeakingBonus:  cfg.Audio.Confidence.SpeakingBonus,
			StabilityBonus: cfg.Audio.Confidence.StabilityBonus,
		},
	}

	// Create tracker
	tracker := doa.NewTracker(source, trackerCfg, logger)

	// Start tracker in background
	go func() {
		if err := tracker.Run(ctx); err != nil && err != context.Canceled {
			logger.Error("tracker error", "error", err)
		}
	}()

	// Initialize Pollen client
	pollenClient := pollen.NewClient(pollen.Config{
		BaseURL:     cfg.Pollen.BaseURL,
		Timeout:     cfg.Pollen.Timeout,
		RateLimitHz: cfg.Pollen.RateLimitHz,
	}, logger)

	// Initialize cloud client if enabled
	var cloudClient *cloud.Client
	var cameraClient *camera.Client

	if cfg.Cloud.Enabled {
		logger.Info("cloud mode enabled", "url", cfg.Cloud.URL)

		// Create cloud client
		cloudClient = cloud.NewClient(cloud.Config{
			URL:              cfg.Cloud.URL,
			ReconnectBackoff: cfg.Cloud.ReconnectBackoff,
			MaxBackoff:       cfg.Cloud.MaxBackoff,
			PingInterval:     cfg.Cloud.PingInterval,
			WriteTimeout:     5 * time.Second,
		}, logger)

		// Set up motor command callback
		cloudClient.OnMotorCommand(func(cmd protocol.MotorCommand) {
			logger.Debug("received motor command",
				"yaw", cmd.Head.Yaw,
				"pitch", cmd.Head.Pitch,
				"roll", cmd.Head.Roll,
			)

			head := pollen.HeadTarget{
				X:     cmd.Head.X,
				Y:     cmd.Head.Y,
				Z:     cmd.Head.Z,
				Yaw:   cmd.Head.Yaw,
				Pitch: cmd.Head.Pitch,
				Roll:  cmd.Head.Roll,
			}

			if err := pollenClient.SetTarget(ctx, head, cmd.Antennas, cmd.BodyYaw); err != nil {
				logger.Warn("motor command failed", "error", err)
			}
		})

		// Set up emotion command callback
		cloudClient.OnEmotionCommand(func(cmd protocol.EmotionCommand) {
			logger.Info("playing emotion", "name", cmd.Name)
			if err := pollenClient.PlayEmotion(ctx, cmd.Name, cmd.Duration); err != nil {
				logger.Warn("emotion command failed", "error", err)
			}
		})

		// Connect to cloud
		if err := cloudClient.Connect(ctx); err != nil {
			logger.Error("cloud connection failed", "error", err)
		}

		// Forward DOA updates to cloud (with enhanced 3D positioning data)
		go func() {
			ticker := time.NewTicker(50 * time.Millisecond) // 20 Hz DOA updates
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if cloudClient.IsConnected() {
						reading := tracker.GetLatest()
						cloudClient.SendEnhancedDOA(
							reading.Angle,
							reading.SmoothedAngle,
							reading.Speaking,
							reading.SpeakingLatched,
							reading.Confidence,
							reading.EstX,
							reading.EstY,
							reading.TotalEnergy,
							reading.SpeechEnergy,
						)
					}
				}
			}
		}()

		// Initialize camera client if enabled
		if cfg.Camera.Enabled {
			logger.Info("camera capture enabled",
				"framerate", cfg.Camera.Framerate,
				"resolution", fmt.Sprintf("%dx%d", cfg.Camera.Width, cfg.Camera.Height),
			)

			cameraClient = camera.NewClient(camera.Config{
				PollenURL: cfg.Pollen.BaseURL,
				Framerate: cfg.Camera.Framerate,
				Width:     cfg.Camera.Width,
				Height:    cfg.Camera.Height,
				Quality:   cfg.Camera.Quality,
				Timeout:   2 * time.Second,
			}, logger)

			// Forward frames to cloud
			cameraClient.OnFrame(func(frame camera.Frame) {
				if cloudClient.IsConnected() {
					if err := cloudClient.SendFrame(frame.Width, frame.Height, frame.Data, frame.FrameID); err != nil {
						logger.Debug("frame send failed", "error", err)
					}
				}
			})

			if err := cameraClient.Start(ctx); err != nil {
				logger.Error("camera start failed", "error", err)
			}
		}
	}

	// Create server
	srv := server.New(cfg.Server, tracker, logger, version)

	// Start WebSocket hub in background
	go srv.WSHub().Run(ctx)

	// Start server in background
	go func() {
		if err := srv.Start(); err != nil {
			logger.Error("server error", "error", err)
			cancel()
		}
	}()

	// Print startup info
	printStartupBanner(cfg, version, cloudClient)

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit

	logger.Info("received shutdown signal", "signal", sig.String())

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(
		context.Background(),
		cfg.Server.GracefulTimeout,
	)
	defer shutdownCancel()

	// Stop in order: camera -> cloud -> server -> tracker -> source
	if cameraClient != nil {
		logger.Info("stopping camera client...")
		cameraClient.Stop()
	}

	if cloudClient != nil {
		logger.Info("disconnecting from cloud...")
		cloudClient.Close()
	}

	logger.Info("shutting down server...")
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("server shutdown error", "error", err)
	}

	logger.Info("stopping tracker...")
	tracker.Stop()

	logger.Info("go-eva stopped")
}

func setupLogger(cfg config.LoggingConfig) *slog.Logger {
	var handler slog.Handler

	level := slog.LevelInfo
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	opts := &slog.HandlerOptions{Level: level}

	if cfg.Format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}

func printStartupBanner(cfg *config.Config, version string, cloudClient *cloud.Client) {
	fmt.Println()
	fmt.Println("🤖 go-eva v" + version)
	fmt.Println("   Shadow daemon for Reachy Mini")
	fmt.Println()
	fmt.Printf("🚀 Running at http://0.0.0.0:%d\n", cfg.Server.Port)
	fmt.Println()
	fmt.Println("   Local Endpoints:")
	fmt.Println("   GET  /health              - Health check")
	fmt.Println("   GET  /api/audio/doa       - Current DOA reading")
	fmt.Println("   WS   /api/audio/doa/stream - Real-time DOA stream")
	fmt.Println("   GET  /api/stats           - Tracker statistics")
	fmt.Println("   GET  /metrics             - Prometheus metrics")

	if cfg.Cloud.Enabled {
		fmt.Println()
		fmt.Println("   ☁️  Cloud Mode:")
		fmt.Printf("      URL: %s\n", cfg.Cloud.URL)
		if cloudClient != nil && cloudClient.IsConnected() {
			fmt.Println("      Status: ✅ Connected")
		} else {
			fmt.Println("      Status: 🔄 Connecting...")
		}
	}

	if cfg.Camera.Enabled {
		fmt.Println()
		fmt.Printf("   📷 Camera: %dx%d @ %d FPS\n", cfg.Camera.Width, cfg.Camera.Height, cfg.Camera.Framerate)
	}

	fmt.Println()
	fmt.Println("   Press Ctrl+C to stop")
	fmt.Println()
}
