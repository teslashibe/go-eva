// go-eva: Shadow daemon for Reachy Mini
// Provides DOA (Direction of Arrival) and enhanced APIs
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/teslashibe/go-eva/pkg/api"
	"github.com/teslashibe/go-eva/pkg/audio"
)

var (
	version = "0.1.0"
	port    = flag.Int("port", 9000, "HTTP server port")
	debug   = flag.Bool("debug", false, "Enable debug logging")
)

func main() {
	flag.Parse()

	fmt.Println("🤖 go-eva v" + version)
	fmt.Println("   Shadow daemon for Reachy Mini")
	fmt.Println()

	// Set debug mode
	audio.Debug = *debug
	api.Debug = *debug

	// Initialize DOA source (uses Python wrapper for USB access)
	fmt.Print("🎤 Initializing DOA source... ")
	var doaSource audio.DOASource

	// Try Python-based DOA first (more reliable on ARM)
	pythonDOA := audio.NewPythonDOA()
	doa, speaking, err := pythonDOA.GetDOA()
	if err != nil {
		fmt.Printf("⚠️  %v (running without DOA)\n", err)
		doaSource = nil
	} else {
		fmt.Println("✅")
		fmt.Printf("   DOA: %.2f rad, Speaking: %v\n", doa, speaking)
		doaSource = pythonDOA
	}

	// Create DOA tracker
	tracker := audio.NewTracker(doaSource)

	// Start tracker polling
	go tracker.Run()
	defer tracker.Stop()

	// Start HTTP server
	fmt.Printf("🌐 Starting server on :%d... ", *port)
	server := api.NewServer(*port, tracker)

	// Handle shutdown gracefully
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		fmt.Println("\n👋 Shutting down...")
		server.Shutdown()
		os.Exit(0)
	}()

	fmt.Println("✅")
	fmt.Println()
	fmt.Printf("🚀 go-eva running at http://0.0.0.0:%d\n", *port)
	fmt.Println("   GET /health        - Health check")
	fmt.Println("   GET /api/audio/doa - DOA angle + speaking")
	fmt.Println("   WS  /api/audio/doa/stream - Real-time DOA")
	fmt.Println()
	fmt.Println("   Press Ctrl+C to stop")

	if err := server.Start(); err != nil {
		fmt.Printf("❌ Server error: %v\n", err)
		os.Exit(1)
	}
}

