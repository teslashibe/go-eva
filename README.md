# go-eva 🤖

Shadow daemon for Reachy Mini that provides DOA (Direction of Arrival) audio spatial awareness.

## Overview

go-eva runs on the Reachy Mini's Raspberry Pi 4 **alongside** Pollen's Python daemon. It provides real-time audio spatial awareness that Pollen doesn't expose.

```
┌─────────────────────────────────────────────────────────┐
│                    Raspberry Pi 4                       │
│  ┌──────────────────┐      ┌──────────────────────┐    │
│  │  Pollen Daemon   │      │     go-eva           │    │
│  │  :8000           │      │     :9000            │    │
│  │  Motors, Camera  │      │  DOA, Audio APIs     │    │
│  └──────────────────┘      └──────────────────────┘    │
│           │                         │                   │
│           │         USB             │                   │
│           ▼                         ▼                   │
│  ┌──────────────────────────────────────────────┐      │
│  │          XVF3800 DSP (4-mic array)           │      │
│  └──────────────────────────────────────────────┘      │
└─────────────────────────────────────────────────────────┘
```

## Features

- **Pure Go USB** - Direct gousb access to XVF3800 (~8μs latency)
- **DOA Tracking** - EMA smoothing, speaking latch, confidence scoring
- **WebSocket Streaming** - Real-time DOA at 20Hz
- **Health Monitoring** - Prometheus-ready metrics
- **Auto-recovery** - USB reconnection with exponential backoff

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Health check with component status |
| `/api/audio/doa` | GET | Current DOA reading |
| `/api/audio/doa/stream` | WebSocket | Real-time DOA stream |
| `/api/stats` | GET | Tracker statistics |
| `/metrics` | GET | Prometheus metrics |

## Quick Start

```bash
# Build and deploy to robot
make deploy ROBOT_IP=192.168.68.77

# Check status
make status

# View logs
make logs
```

## Architecture

```
go-eva/
├── cmd/go-eva/
│   └── main.go              # Entry point, graceful shutdown
├── internal/
│   ├── config/              # Viper configuration
│   ├── doa/                 # DOA tracking, smoothing
│   │   ├── source.go        # Source interface
│   │   └── tracker.go       # EMA, speaking latch
│   ├── health/              # Health checker
│   ├── server/              # Fiber HTTP/WebSocket
│   └── xvf3800/             # USB driver (pure Go)
│       ├── usb.go           # gousb implementation
│       ├── mock.go          # Testing mock
│       └── source.go        # Factory
├── configs/
│   └── config.yaml          # Default configuration
├── scripts/
│   └── go-eva.service       # Systemd service
└── Makefile                 # Build automation
```

## Configuration

Configuration via YAML file or environment variables:

```yaml
server:
  port: 9000

audio:
  poll_hz: 20              # DOA polling frequency
  ema_alpha: 0.3           # Smoothing factor (0-1)
  speaking_latch_ms: 500   # Hold speaking state

logging:
  level: info              # debug, info, warn, error
  format: json             # json or text
```

Environment overrides: `GOEVA_SERVER_PORT=9000`

## Hardware

The XVF3800 is an XMOS DSP chip that processes the 4-microphone array. go-eva reads DOA via USB control transfers:

| Parameter | Description |
|-----------|-------------|
| `DOA_VALUE_RADIANS` | Angle (radians) + speech detection |
| VID/PID | `0x38FB` / `0x1001` |

## Development

```bash
# Run tests
make test

# Build locally (uses mock source)
make build
./go-eva -mock

# Build for ARM64
make build-arm64
```

## Related

- [go-reachy](https://github.com/teslashibe/go-reachy) - Main Eva application
- [Pollen Robotics](https://www.pollen-robotics.com/) - Reachy Mini manufacturer
