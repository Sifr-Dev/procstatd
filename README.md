<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset=".github/banner-dark.svg">
    <img alt="sysmon_daemon — built from zero" src=".github/banner-light.svg" width="100%">
  </picture>
</p>

# sysmon_daemon

Linux `/proc` telemetry over HTTP, in Go. Real data or 503 — never synthetic.

## what it serves

```console
$ curl -s localhost:8080/api/sysmon
{"cpu":6.27,"mem_total_mb":15796,"mem_available_mb":6047,"mem_used_mb":9749,"mem_cache_mb":5259,"procs":522,"uptime_seconds":19.12}
$ curl -s localhost:8080/health
{"status":"ok"}
```

## honesty notes (vs the old README)

- **CPU** is successive-delta `/proc/stat` (`busy = total - (idle + iowait)`),
  not cumulative-since-boot and never random. First sample after startup is
  `0` by design (no delta yet).
- **Memory** distinguishes `mem_total_mb`, `mem_available_mb`
  (MemAvailable, fallback MemFree+Buffers), `mem_used_mb` (total-available)
  and `mem_cache_mb` (Cached).
- **/proc/[pid]** scan counts live PIDs; disappeared PIDs are simply absent,
  permission errors do not poison counting.
- on any read failure the API returns **503** with the error; there is no
  simulated-data fallback anywhere. the old README claimed /proc parsing
  that did not exist (the handler returned `rand`); that is fixed here.

## build & verify

```sh
go build ./...     # exit 0
go vet ./...       # exit 0
go test -race ./...  # ok — meminfo real, procs>0, cpu delta in [0,100]
PORT=8090 ./sysmon_daemon_bin   # then curl /api/sysmon
```

verified on this machine (2026-11-17): `go vet` clean, `go test -race ./...`
PASS, live daemon returns successive-delta CPU (6.04% → 6.27% across two
samples) and real MemAvailable.

## run

`PORT=8080 go run .` (or build and run the binary).

## api

- `GET /api/sysmon` — telemetry JSON (see above)
- `GET /health` — `{"status":"ok"}`

## license

`MIT` — see `LICENSE`. built from zero.
