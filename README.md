# Linux System Telemetry Daemon

A lightweight system monitoring daemon written in Go that collects real-time system telemetry.

## Tech Stack
- Go
- Linux /proc filesystem
- REST API

## Features
- CPU load monitoring (parses /proc/stat)
- Memory usage tracking (parses /proc/meminfo)
- Process count monitoring (scans /proc/[pid])
- HTTP API for telemetry data
- Graceful fallback to simulated data on non-Linux systems

## Run
```bash
go run main.go
```

## API
- GET /api/sysmon - Get telemetry stats
- GET /health - Health check
