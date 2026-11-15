package telemetry

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	CPUStatusAvailable  = "available"
	CPUStatusWarmingUp  = "warming_up"
	CPUStatusReset      = "counter_reset"
	defaultProcRoot     = "/proc"
	cacheBytesPerKiB    = uint64(1024)
	linuxMemInfoUnitKiB = "kB"
)

// Snapshot is one coherent observation of the local Linux system.
//
// CPU usage is a delta between two aggregate counter samples. It is nil and
// has status warming_up for the first sample, or counter_reset when the
// kernel counters move backwards. Memory values are bytes, not rounded MiB.
type Snapshot struct {
	CPU                 CPUStats    `json:"cpu"`
	Memory              MemoryStats `json:"memory"`
	Processes           int         `json:"processes"`
	SystemUptimeSeconds float64     `json:"system_uptime_seconds"`
	CollectedAt         time.Time   `json:"collected_at"`
}

// CPUStats describes current aggregate CPU utilization. UsagePercent is a
// percentage from 0 through 100 and is unavailable until two valid samples
// have been observed.
type CPUStats struct {
	UsagePercent *float64 `json:"usage_percent"`
	Status       string   `json:"status"`
}

// MemoryStats distinguishes total, available, used, and cache memory.
type MemoryStats struct {
	TotalBytes     uint64 `json:"total_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
	UsedBytes      uint64 `json:"used_bytes"`
	CacheBytes     uint64 `json:"cache_bytes"`
}

// Collector reads Linux process statistics from an injectable procfs root.
// The default root is /proc; injecting a root keeps the parsers testable
// without depending on the host running the tests.
type Collector struct {
	procRoot string
	now      func() time.Time

	mu       sync.Mutex
	previous *cpuCounters
}

// NewCollector creates a collector rooted at procRoot. An empty root selects
// /proc.
func NewCollector(procRoot string) *Collector {
	if strings.TrimSpace(procRoot) == "" {
		procRoot = defaultProcRoot
	}

	return &Collector{
		procRoot: filepath.Clean(procRoot),
		now:      time.Now,
	}
}

// Collect reads one telemetry snapshot. It returns an error instead of
// fabricated values when a required procfs input cannot be read or parsed.
func (c *Collector) Collect() (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	collectedAt := c.now().UTC()

	currentCPU, err := readCPUTotal(filepath.Join(c.procRoot, "stat"))
	if err != nil {
		return Snapshot{}, fmt.Errorf("read CPU counters: %w", err)
	}
	cpu, err := c.cpuUtilization(currentCPU)
	if err != nil {
		return Snapshot{}, fmt.Errorf("calculate CPU utilization: %w", err)
	}

	memory, err := readMemory(filepath.Join(c.procRoot, "meminfo"))
	if err != nil {
		return Snapshot{}, fmt.Errorf("read memory statistics: %w", err)
	}

	processes, err := countProcesses(c.procRoot)
	if err != nil {
		return Snapshot{}, fmt.Errorf("count processes: %w", err)
	}

	uptime, err := readSystemUptime(filepath.Join(c.procRoot, "uptime"))
	if err != nil {
		return Snapshot{}, fmt.Errorf("read system uptime: %w", err)
	}

	return Snapshot{
		CPU:                 cpu,
		Memory:              memory,
		Processes:           processes,
		SystemUptimeSeconds: uptime,
		CollectedAt:         collectedAt,
	}, nil
}

type cpuCounters struct {
	user      uint64
	nice      uint64
	system    uint64
	idle      uint64
	ioWait    uint64
	irq       uint64
	softIRQ   uint64
	steal     uint64
	guest     uint64
	guestNice uint64
}

func (c cpuCounters) total() uint64 {
	// Linux already accounts guest time in user time and guest_nice in nice
	// time. Including those fields again would double-count guest work.
	return c.user + c.nice + c.system + c.idle + c.ioWait + c.irq + c.softIRQ + c.steal
}

func (c cpuCounters) busy() uint64 {
	return c.total() - c.idle - c.ioWait
}

func (c *Collector) cpuUtilization(current cpuCounters) (CPUStats, error) {
	if c.previous == nil {
		copy := current
		c.previous = &copy
		return CPUStats{Status: CPUStatusWarmingUp}, nil
	}

	previous := *c.previous
	copy := current
	c.previous = &copy

	if countersReset(previous, current) {
		return CPUStats{Status: CPUStatusReset}, nil
	}

	totalDelta := current.total() - previous.total()
	if totalDelta == 0 {
		return CPUStats{Status: CPUStatusReset}, nil
	}

	busyDelta := current.busy() - previous.busy()
	usage := float64(busyDelta) / float64(totalDelta) * 100
	if usage < 0 {
		usage = 0
	}
	if usage > 100 {
		usage = 100
	}

	return CPUStats{
		UsagePercent: &usage,
		Status:       CPUStatusAvailable,
	}, nil
}

func countersReset(previous, current cpuCounters) bool {
	if current.total() < previous.total() {
		return true
	}

	return current.user < previous.user ||
		current.nice < previous.nice ||
		current.system < previous.system ||
		current.idle < previous.idle ||
		current.ioWait < previous.ioWait ||
		current.irq < previous.irq ||
		current.softIRQ < previous.softIRQ ||
		current.steal < previous.steal
}

func readCPUTotal(path string) (cpuCounters, error) {
	file, err := os.Open(path)
	if err != nil {
		return cpuCounters{}, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || fields[0] != "cpu" {
			continue
		}
		if len(fields) < 9 {
			return cpuCounters{}, fmt.Errorf("aggregate CPU line has %d fields, want at least 9", len(fields))
		}

		values := make([]uint64, 0, len(fields)-1)
		for _, field := range fields[1:] {
			value, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return cpuCounters{}, fmt.Errorf("parse CPU counter %q: %w", field, err)
			}
			values = append(values, value)
		}

		counters := cpuCounters{
			user:    values[0],
			nice:    values[1],
			system:  values[2],
			idle:    values[3],
			ioWait:  values[4],
			irq:     values[5],
			softIRQ: values[6],
			steal:   values[7],
		}
		if len(values) > 8 {
			counters.guest = values[8]
		}
		if len(values) > 9 {
			counters.guestNice = values[9]
		}
		return counters, nil
	}
	if err := scanner.Err(); err != nil {
		return cpuCounters{}, err
	}

	return cpuCounters{}, fmt.Errorf("aggregate CPU line not found")
}

func readMemory(path string) (MemoryStats, error) {
	file, err := os.Open(path)
	if err != nil {
		return MemoryStats{}, err
	}
	defer file.Close()

	var total, available, cached, buffers, reclaimable uint64
	var haveTotal, haveAvailable, haveCached bool

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		name, rawValue, found := strings.Cut(scanner.Text(), ":")
		if !found {
			continue
		}

		var destination *uint64
		switch name {
		case "MemTotal":
			destination = &total
			haveTotal = true
		case "MemAvailable":
			destination = &available
			haveAvailable = true
		case "Cached":
			destination = &cached
			haveCached = true
		case "Buffers":
			destination = &buffers
		case "SReclaimable":
			destination = &reclaimable
		default:
			continue
		}

		value, err := parseMemoryBytes(rawValue)
		if err != nil {
			return MemoryStats{}, fmt.Errorf("parse %s: %w", name, err)
		}
		*destination = value
	}
	if err := scanner.Err(); err != nil {
		return MemoryStats{}, err
	}

	if !haveTotal {
		return MemoryStats{}, fmt.Errorf("MemTotal not found")
	}
	if !haveAvailable {
		return MemoryStats{}, fmt.Errorf("MemAvailable not found")
	}
	if !haveCached {
		return MemoryStats{}, fmt.Errorf("Cached not found")
	}
	if available > total {
		return MemoryStats{}, fmt.Errorf("MemAvailable exceeds MemTotal")
	}

	cache, err := checkedAdd(cached, buffers)
	if err != nil {
		return MemoryStats{}, fmt.Errorf("calculate cache memory: %w", err)
	}
	cache, err = checkedAdd(cache, reclaimable)
	if err != nil {
		return MemoryStats{}, fmt.Errorf("calculate cache memory: %w", err)
	}

	return MemoryStats{
		TotalBytes:     total,
		AvailableBytes: available,
		UsedBytes:      total - available,
		CacheBytes:     cache,
	}, nil
}

func parseMemoryBytes(raw string) (uint64, error) {
	fields := strings.Fields(raw)
	if len(fields) != 2 {
		return 0, fmt.Errorf("unexpected value %q", raw)
	}

	value, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse quantity %q: %w", fields[0], err)
	}

	switch fields[1] {
	case linuxMemInfoUnitKiB:
		if value > math.MaxUint64/cacheBytesPerKiB {
			return 0, fmt.Errorf("quantity overflows bytes")
		}
		return value * cacheBytesPerKiB, nil
	case "B":
		return value, nil
	default:
		return 0, fmt.Errorf("unsupported unit %q", fields[1])
	}
}

func checkedAdd(left, right uint64) (uint64, error) {
	if right > math.MaxUint64-left {
		return 0, fmt.Errorf("uint64 overflow")
	}
	return left + right, nil
}

func countProcesses(procRoot string) (int, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return 0, err
	}
	return countProcessEntries(entries)
}

func countProcessEntries(entries []fs.DirEntry) (int, error) {
	count := 0
	for _, entry := range entries {
		pid, err := strconv.ParseUint(entry.Name(), 10, 64)
		if err != nil || pid == 0 {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
				continue
			}
			return 0, fmt.Errorf("inspect process %s: %w", entry.Name(), err)
		}
		if !info.IsDir() {
			continue
		}
		count++
	}
	return count, nil
}

func readSystemUptime(path string) (float64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return 0, fmt.Errorf("uptime value not found")
	}
	uptime, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("parse uptime %q: %w", fields[0], err)
	}
	if math.IsNaN(uptime) || math.IsInf(uptime, 0) || uptime < 0 {
		return 0, fmt.Errorf("invalid uptime %q", fields[0])
	}
	return uptime, nil
}
