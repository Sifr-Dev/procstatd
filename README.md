<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset=".github/banner-dark.svg">
    <img alt="sysmon_daemon — built from zero" src=".github/banner-light.svg" width="100%">
  </picture>
</p>

# Linux System Telemetry Daemon

A foreground Go service that reads Linux `procfs` telemetry and exposes it through a small HTTP API. The service does not generate, estimate, or randomize telemetry: when a required input is missing or malformed, the telemetry endpoint returns an explicit `503` response.

## What it measures

- **CPU utilization:** the busy-time delta between two successive aggregate `/proc/stat` samples. The first sample has status `warming_up`; a counter reset has status `counter_reset` and does not produce a made-up percentage.
- **Memory:** total, available, used, and cache bytes from `/proc/meminfo`. Used memory is `MemTotal - MemAvailable`. Cache bytes are `Buffers + Cached + SReclaimable`.
- **Processes:** the number of numeric process directories visible under the configured procfs root. A process that disappears or cannot be inspected is skipped without invalidating the whole scan.
- **System uptime:** seconds from `/proc/uptime`, not daemon-process uptime.
- **Collection time:** an RFC 3339 UTC timestamp generated with the snapshot.

Aggregate CPU counters include user, nice, system, idle, I/O wait, IRQ, soft IRQ, and steal time. Linux already includes guest time in user time and guest-nice time in nice time, so guest fields are parsed for format validation but are not added again.

## Run

Requirements: Linux and Go 1.26.3 or newer.

```sh
go run .
```

The default listener is `:8080`:

```text
GET http://localhost:8080/api/sysmon
GET http://localhost:8080/health
```

### Configuration

| variable | default | purpose |
|---|---|---|
| `SYSMON_ADDR` | `:8080` | HTTP listen address |
| `SYSMON_PROC_ROOT` | `/proc` | procfs root; injectable for controlled test environments |
| `SYSMON_ALLOWED_ORIGINS` | empty | comma-separated exact browser-origin allowlist |

The default empty origin allowlist grants no cross-origin browser access. Allowed origins must be exact `http://` or `https://` origins without credentials, paths, queries, fragments, or wildcards. For example:

```sh
SYSMON_ALLOWED_ORIGINS=https://portfolio.example go run .
```

A request from an origin outside the allowlist is rejected with `403`. Same-origin requests without an `Origin` header remain available.

## Telemetry response

After at least two CPU samples, a successful response has this shape (the values below are an example snapshot):

```json
{
  "cpu": {
    "usage_percent": 15,
    "status": "available"
  },
  "memory": {
    "total_bytes": 16564948992,
    "available_bytes": 7939432448,
    "used_bytes": 8625516544,
    "cache_bytes": 8523812864
  },
  "processes": 332,
  "system_uptime_seconds": 68993.66,
  "collected_at": "2026-11-15T16:47:47.518314167Z"
}
```

The first successful sample returns `usage_percent: null` and CPU status `warming_up`. A counter reset returns `usage_percent: null` and status `counter_reset`; the next increasing sample can recover without restarting the service.

When required procfs data is unavailable, the endpoint returns a stable error instead of partial or fabricated values:

```json
{
  "error": {
    "code": "telemetry_unavailable",
    "message": "system telemetry is temporarily unavailable"
  }
}
```

## HTTP contract

- `GET /api/sysmon` returns the current measured snapshot.
- `GET /health` returns `{"status":"ok"}` for process liveness.
- Unsupported methods return `405` with an `Allow: GET` header.
- Telemetry responses set `Cache-Control: no-store`.
- The server uses header, read, write, and idle timeouts and shuts down gracefully on `SIGINT` or `SIGTERM`.

## Architecture

```text
main.go
├── configures the procfs root, listener, and exact origin allowlist
├── wires one telemetry collector into one HTTP handler
└── runs a timeout-bounded server with graceful shutdown

internal/telemetry
├── parses aggregate CPU counters and computes successive deltas
├── parses memory semantics and system uptime
└── counts process directories while skipping per-process races

internal/api
├── restricts methods and emits stable JSON
├── enforces the exact CORS allowlist
└── returns an explicit unavailable response on collection failure
```

## Verify

```sh
test -z "$(gofmt -l .)"
go mod tidy
git diff --exit-code -- go.mod go.sum
go vet ./...
go test -race -count=1 -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
go build ./...
```

The fixture tests cover valid counter deltas, the first sample, counter resets, malformed CPU and memory input, process disappearance/permission errors, exact CORS origins, method restrictions, and collection failures. GitHub Actions runs the same formatting, vet, race, coverage, and build gates on Ubuntu.

## Operational limits

- This is a foreground service, not a daemonizer, service manager, persistence layer, remote collector, or TLS terminator.
- Host and process telemetry can be sensitive. Bind it to a trusted interface, restrict network access, and configure an explicit origin allowlist before exposing it beyond localhost.
- Procfs fields and permissions vary by kernel and sandbox. The service reports unavailable data instead of silently changing its measurement contract.
- Memory cache is reported using the documented Linux `Buffers + Cached + SReclaimable` definition; it is not a replacement for a full system-memory model.
