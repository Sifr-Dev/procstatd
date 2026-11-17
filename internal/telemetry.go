// Package internal reads real telemetry from the Linux /proc filesystem.
// No synthetic data is ever served: on any read error the sampler reports it.
package internal

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Telemetry is a stateful sampler. CPU percentage is computed from the delta
// between successive /proc/stat samples, not cumulative boot averages.
type Telemetry struct {
	mu        sync.Mutex
	startTime time.Time
	prevBusy  float64
	prevTotal float64
	filled    bool
}

func NewTelemetry() *Telemetry {
	return &Telemetry{startTime: time.Now()}
}

type Stats struct {
	CPU           float64 `json:"cpu"`            // percent, 0-100, successive-delta
	MemoryTotal   int     `json:"mem_total_mb"`   // MemTotal
	MemoryAvailable int   `json:"mem_available_mb"` // MemAvailable
	MemoryUsed    int     `json:"mem_used_mb"`    // Total - Available
	MemoryCache   int     `json:"mem_cache_mb"`   // Cached
	Processes     int     `json:"procs"`          // live PIDs in /proc
	UptimeSeconds float64 `json:"uptime_seconds"` // since daemon start
}

// Collect gathers one sample. An error means the sample is invalid and must
// not be served (callers should return 503).
func (t *Telemetry) Collect() (Stats, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	cpu, err := t.readCPU()
	if err != nil {
		return Stats{}, fmt.Errorf("cpu: %w", err)
	}
	mem, err := readMemory()
	if err != nil {
		return Stats{}, fmt.Errorf("meminfo: %w", err)
	}
	procs, err := readProcessCount()
	if err != nil {
		return Stats{}, fmt.Errorf("procs: %w", err)
	}

	return Stats{
		CPU:             cpu,
		MemoryTotal:     mem.Total,
		MemoryAvailable: mem.Available,
		MemoryUsed:      mem.Used,
		MemoryCache:     mem.Cache,
		Processes:       procs,
		UptimeSeconds:   time.Since(t.startTime).Seconds(),
	}, nil
}

type memSample struct {
	Total     int
	Available int
	Used      int
	Cache     int
}

func readMemory() (memSample, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return memSample{}, err
	}
	defer f.Close()

	vals := map[string]int{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		kb, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		switch key {
		case "MemTotal", "MemAvailable", "Cached", "MemFree", "Buffers":
			vals[key] = kb / 1024 // kB -> MB
		}
	}
	if err := sc.Err(); err != nil {
		return memSample{}, err
	}
	total, ok := vals["MemTotal"]
	if !ok {
		return memSample{}, fmt.Errorf("MemTotal not found")
	}
	// MemAvailable exists on modern kernels; fall back to MemFree+Buffers
	avail, ok := vals["MemAvailable"]
	if !ok {
		avail = vals["MemFree"] + vals["Buffers"]
	}
	cache := vals["Cached"]
	return memSample{
		Total:     total,
		Available: avail,
		Used:      total - avail,
		Cache:     cache,
	}, nil
}

func (t *Telemetry) readCPU() (float64, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return 0, fmt.Errorf("no data")
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 8 || fields[0] != "cpu" {
		return 0, fmt.Errorf("unexpected format")
	}
	nums := make([]float64, 0, len(fields)-1)
	for _, s := range fields[1:] {
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("bad field %q", s)
		}
		nums = append(nums, n)
	}
	var idle float64
	if len(nums) >= 5 {
		idle = nums[3] + nums[4] // idle + iowait
	}
	var total float64
	for _, n := range nums {
		total += n
	}
	busy := total - idle

	if !t.filled {
		t.prevBusy, t.prevTotal = busy, total
		t.filled = true
		return 0, nil // first sample: no delta yet
	}
	dTotal := total - t.prevTotal
	dBusy := busy - t.prevBusy
	t.prevBusy, t.prevTotal = busy, total
	if dTotal <= 0 {
		return 0, nil
	}
	return dBusy / dTotal * 100, nil
}

func readProcessCount() (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(e.Name()); err == nil {
			count++ // live PID; disappeared entries just never appear
		}
	}
	return count, nil
}
