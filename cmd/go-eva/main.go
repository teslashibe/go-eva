// go-eva: Shadow daemon for Reachy Mini
// Provides DOA (Direction of Arrival) and enhanced APIs
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

	"github.com/teslashibe/go-eva/internal/config"
	"github.com/teslashibe/go-eva/internal/doa"
	"github.com/teslashibe/go-eva/internal/server"
	"github.com/teslashibe/go-eva/internal/xvf3800"
)

var (
	version     = "1.1.0"
	configPath  = flag.String("config", "/etc/go-eva/config.yaml", "config file path")
	showVersion = flag.Bool("version", false, "print version and exit")
	debug       = flag.Bool("debug", false, "enable debug logging")
	useMock     = flag.Bool("mock", false, "use mock DOA source (for testing)")
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

	// Override log level if debug flag is set
	if *debug {
		cfg.Logging.Level = "debug"
	}

	// Setup logging
	logger := setupLogger(cfg.Logging)

	logger.Info("starting go-eva",
		"version", version,
		"config", *configPath,
		"port", cfg.Server.Port,
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
		// Use pure Go USB source - no Python dependency
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
	printStartupBanner(cfg, version)

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

	// Stop in order: server -> tracker -> source
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

func printStartupBanner(cfg *config.Config, version string) {
	fmt.Println()
	fmt.Println("🤖 go-eva v" + version)
	fmt.Println("   Shadow daemon for Reachy Mini")
	fmt.Println()
	fmt.Printf("🚀 Running at http://0.0.0.0:%d\n", cfg.Server.Port)
	fmt.Println()
	fmt.Println("   Endpoints:")
	fmt.Println("   GET  /health              - Health check")
	fmt.Println("   GET  /api/audio/doa       - Current DOA reading")
	fmt.Println("   WS   /api/audio/doa/stream - Real-time DOA stream")
	fmt.Println("   GET  /api/stats           - Tracker statistics")
	fmt.Println("   GET  /metrics             - Prometheus metrics")
	fmt.Println()
	fmt.Println("   Press Ctrl+C to stop")
	fmt.Println()
}
