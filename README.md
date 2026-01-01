# go-eva 🤖

Shadow daemon for Reachy Mini that adds DOA (Direction of Arrival) and enhanced APIs.

## Overview

go-eva runs on the Reachy Mini's Raspberry Pi 4 **alongside** Pollen's Python daemon. It provides features that Pollen doesn't expose, starting with audio spatial awareness (DOA).

```
┌─────────────────────────────────────────────────────────┐
│                    Raspberry Pi 4                       │
│  ┌──────────────────┐      ┌──────────────────────┐    │
│  │  Pollen Daemon   │      │     go-eva           │    │
│  │  :8000           │◄────►│     :9000            │    │
│  │  Motors, Camera  │ poll │  DOA, Enhanced APIs  │    │
│  └──────────────────┘      └──────────────────────┘    │
└─────────────────────────────────────────────────────────┘
```

## Features

### V1 (Current)
- **DOA API** - Direction of Arrival from 4-mic XVF3800 array
- **WebSocket streaming** - Real-time DOA at 10Hz
- **Health check** - Service monitoring

### V2 (Planned)
- 100Hz state streaming
- Enhanced state proxy
- Raw audio access

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Health check |
| `/api/audio/doa` | GET | `{"angle": 1.45, "speaking": true}` |
| `/api/audio/doa/stream` | WebSocket | Real-time DOA stream |

## Build

```bash
# Native (for testing)
go build -o go-eva ./cmd/go-eva

# Cross-compile for Reachy Mini (ARM64)
make build-arm64

# Deploy to robot
make deploy ROBOT_IP=192.168.68.77
```

## Install on Robot

```bash
# Copy binary
scp go-eva pollen@192.168.68.77:/home/pollen/

# Install systemd service
ssh pollen@192.168.68.77 'sudo cp go-eva.service /etc/systemd/system/'
ssh pollen@192.168.68.77 'sudo systemctl enable go-eva && sudo systemctl start go-eva'
```

## Architecture

```
go-eva/
├── cmd/go-eva/main.go     # Entry point
├── pkg/
│   ├── audio/
│   │   ├── xvf3800.go     # USB interface to XVF3800 chip
│   │   └── tracker.go     # DOA smoothing + confidence
│   ├── api/
│   │   ├── server.go      # Fiber HTTP server
│   │   └── handlers.go    # Request handlers
│   └── proxy/
│       └── pollen.go      # Poll Pollen's daemon
└── scripts/
    └── install.sh         # systemd setup
```

## Hardware

The Reachy Mini has an XMOS XVF3800 DSP chip that processes the 4-microphone array internally. go-eva reads DOA via USB control transfers:

| Parameter | Description |
|-----------|-------------|
| `DOA_VALUE_RADIANS` | Angle (radians) + speech detection |
| `AEC_SPENERGY_VALUES` | Speech energy per channel |
| `AEC_MIC_ARRAY_GEO` | Mic array geometry |

## Related

- [go-reachy](https://github.com/teslashibe/go-reachy) - Main Eva application
- [Pollen Robotics](https://www.pollen-robotics.com/) - Reachy Mini manufacturer

