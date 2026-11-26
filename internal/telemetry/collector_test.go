package telemetry

import (
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCollectUsesCounterDeltaAndMemorySemantics(t *testing.T) {
	root := t.TempDir()
	writeProcFixtures(t, root, "cpu 100 10 50 800 20 5 10 5 2 1\n", 1000, 250, 200, 100, 50)
	if err := os.MkdirAll(filepath.Join(root, "101"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "202"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "not-a-pid"), 0o755); err != nil {
		t.Fatal(err)
	}

	collector := NewCollector(root)
	collectedAt := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	collector.now = func() time.Time { return collectedAt }

	first, err := collector.Collect()
	if err != nil {
		t.Fatalf("first Collect() error = %v", err)
	}
	if first.CPU.Status != CPUStatusWarmingUp {
		t.Fatalf("first CPU status = %q, want %q", first.CPU.Status, CPUStatusWarmingUp)
	}
	if first.CPU.UsagePercent != nil {
		t.Fatalf("first CPU usage = %v, want nil", *first.CPU.UsagePercent)
	}
	if first.Processes != 2 {
		t.Fatalf("process count = %d, want 2", first.Processes)
	}
	if !first.CollectedAt.Equal(collectedAt) {
		t.Fatalf("collected at = %v, want %v", first.CollectedAt, collectedAt)
	}

	writeCPUFixture(t, root, "cpu 150 10 70 900 20 5 10 5 2 1\n")
	second, err := collector.Collect()
	if err != nil {
		t.Fatalf("second Collect() error = %v", err)
	}
	if second.CPU.Status != CPUStatusAvailable {
		t.Fatalf("second CPU status = %q, want %q", second.CPU.Status, CPUStatusAvailable)
	}
	if second.CPU.UsagePercent == nil {
		t.Fatal("second CPU usage is nil")
	}
	wantUsage := 70.0 / 170.0 * 100
	if math.Abs(*second.CPU.UsagePercent-wantUsage) > 0.000001 {
		t.Fatalf("CPU usage = %f, want %f", *second.CPU.UsagePercent, wantUsage)
	}

	wantMemory := MemoryStats{
		TotalBytes:     1000 * 1024,
		AvailableBytes: 250 * 1024,
		UsedBytes:      750 * 1024,
		CacheBytes:     350 * 1024,
	}
	if second.Memory != wantMemory {
		t.Fatalf("memory = %+v, want %+v", second.Memory, wantMemory)
	}
	if second.SystemUptimeSeconds != 123.5 {
		t.Fatalf("system uptime = %f, want 123.5", second.SystemUptimeSeconds)
	}
}

func TestCollectReportsCounterResetWithoutFabricatingUsage(t *testing.T) {
	root := t.TempDir()
	writeProcFixtures(t, root, "cpu 100 10 50 800 20 5 10 5\n", 1000, 250, 200, 100, 50)
	collector := NewCollector(root)

	if _, err := collector.Collect(); err != nil {
		t.Fatalf("first Collect() error = %v", err)
	}

	writeCPUFixture(t, root, "cpu 10 1 5 80 2 0 1 0\n")
	reset, err := collector.Collect()
	if err != nil {
		t.Fatalf("reset Collect() error = %v", err)
	}
	if reset.CPU.Status != CPUStatusReset {
		t.Fatalf("CPU status = %q, want %q", reset.CPU.Status, CPUStatusReset)
	}
	if reset.CPU.UsagePercent != nil {
		t.Fatalf("CPU usage = %v, want nil after reset", *reset.CPU.UsagePercent)
	}

	writeCPUFixture(t, root, "cpu 20 2 10 160 4 1 2 1\n")
	recovered, err := collector.Collect()
	if err != nil {
		t.Fatalf("recovered Collect() error = %v", err)
	}
	if recovered.CPU.Status != CPUStatusAvailable {
		t.Fatalf("recovered CPU status = %q, want %q", recovered.CPU.Status, CPUStatusAvailable)
	}
}

func TestCollectReportsBusyCounterDecreaseAsReset(t *testing.T) {
	root := t.TempDir()
	writeProcFixtures(t, root, "cpu 100 0 100 100 0 0 0 0\n", 1000, 250, 200, 100, 50)
	collector := NewCollector(root)
	if _, err := collector.Collect(); err != nil {
		t.Fatalf("first Collect() error = %v", err)
	}

	// Total time increases, but user time moves backwards. The resulting busy
	// delta cannot be inferred as a valid utilization measurement.
	writeCPUFixture(t, root, "cpu 50 0 100 300 0 0 0 0\n")
	second, err := collector.Collect()
	if err != nil {
		t.Fatalf("second Collect() error = %v", err)
	}
	if second.CPU.Status != CPUStatusReset {
		t.Fatalf("CPU status = %q, want %q", second.CPU.Status, CPUStatusReset)
	}
	if second.CPU.UsagePercent != nil {
		t.Fatalf("CPU usage = %v, want nil for a backwards busy delta", *second.CPU.UsagePercent)
	}
}

func TestCollectReportsIdenticalCountersAsUnchanged(t *testing.T) {
	root := t.TempDir()
	writeProcFixtures(t, root, "cpu 100 10 50 800 20 5 10 5\n", 1000, 250, 200, 100, 50)
	collector := NewCollector(root)

	if _, err := collector.Collect(); err != nil {
		t.Fatalf("first Collect() error = %v", err)
	}

	// A cached or idle procfs read returns byte-identical counters. That is
	// normal and must not be reported as a component regression.
	for sample := 1; sample <= 2; sample++ {
		unchanged, err := collector.Collect()
		if err != nil {
			t.Fatalf("unchanged Collect() #%d error = %v", sample, err)
		}
		if unchanged.CPU.Status != CPUStatusUnchanged {
			t.Fatalf("unchanged sample #%d status = %q, want %q", sample, unchanged.CPU.Status, CPUStatusUnchanged)
		}
		if unchanged.CPU.UsagePercent != nil {
			t.Fatalf("unchanged sample #%d usage = %v, want nil", sample, unchanged.CPU.UsagePercent)
		}
	}

	// Identical counters must not poison the next advancing sample.
	writeCPUFixture(t, root, "cpu 150 10 70 900 20 5 10 5\n")
	advanced, err := collector.Collect()
	if err != nil {
		t.Fatalf("advancing Collect() error = %v", err)
	}
	if advanced.CPU.Status != CPUStatusAvailable {
		t.Fatalf("advancing status = %q, want %q", advanced.CPU.Status, CPUStatusAvailable)
	}
	if advanced.CPU.UsagePercent == nil {
		t.Fatal("advancing usage is nil")
	}
	wantUsage := 70.0 / 170.0 * 100
	if math.Abs(*advanced.CPU.UsagePercent-wantUsage) > 0.000001 {
		t.Fatalf("advancing usage = %f, want %f", *advanced.CPU.UsagePercent, wantUsage)
	}
}

func TestCollectReportsAdvancingCountersAsAvailable(t *testing.T) {
	root := t.TempDir()
	writeProcFixtures(t, root, "cpu 200 10 100 400 10 5 5 5\n", 1000, 250, 200, 100, 50)
	collector := NewCollector(root)
	if _, err := collector.Collect(); err != nil {
		t.Fatalf("first Collect() error = %v", err)
	}

	writeCPUFixture(t, root, "cpu 250 10 150 500 10 5 5 5\n")
	second, err := collector.Collect()
	if err != nil {
		t.Fatalf("second Collect() error = %v", err)
	}
	if second.CPU.Status != CPUStatusAvailable {
		t.Fatalf("CPU status = %q, want %q", second.CPU.Status, CPUStatusAvailable)
	}
	if second.CPU.UsagePercent == nil {
		t.Fatal("CPU usage is nil for an advancing counter sample")
	}
	// total delta 200, busy delta 100 (idle gained exactly the remaining 100).
	wantUsage := 100.0 / 200.0 * 100
	if math.Abs(*second.CPU.UsagePercent-wantUsage) > 0.000001 {
		t.Fatalf("CPU usage = %f, want %f", *second.CPU.UsagePercent, wantUsage)
	}
}

func TestCollectReportsPartialCounterResetAsReset(t *testing.T) {
	root := t.TempDir()
	writeProcFixtures(t, root, "cpu 100 10 50 800 20 5 10 5\n", 1000, 250, 200, 100, 50)
	collector := NewCollector(root)
	if _, err := collector.Collect(); err != nil {
		t.Fatalf("first Collect() error = %v", err)
	}

	// Total time grows and busy time grows, but idle time moves backwards.
	// A single regressing component is enough to invalidate the delta.
	writeCPUFixture(t, root, "cpu 1100 10 50 700 20 5 10 5\n")
	partial, err := collector.Collect()
	if err != nil {
		t.Fatalf("partial reset Collect() error = %v", err)
	}
	if partial.CPU.Status != CPUStatusReset {
		t.Fatalf("CPU status = %q, want %q", partial.CPU.Status, CPUStatusReset)
	}
	if partial.CPU.UsagePercent != nil {
		t.Fatalf("CPU usage = %v, want nil after a partial counter reset", partial.CPU.UsagePercent)
	}

	// The collector must recover from a partial reset once counters advance.
	writeCPUFixture(t, root, "cpu 1200 10 60 800 20 5 10 5\n")
	recovered, err := collector.Collect()
	if err != nil {
		t.Fatalf("recovered Collect() error = %v", err)
	}
	if recovered.CPU.Status != CPUStatusAvailable {
		t.Fatalf("recovered CPU status = %q, want %q", recovered.CPU.Status, CPUStatusAvailable)
	}
}

func TestCollectReportsNegativeComponentDeltaAsReset(t *testing.T) {
	root := t.TempDir()
	writeProcFixtures(t, root, "cpu 200 10 100 400 10 5 5 5\n", 1000, 250, 200, 100, 50)
	collector := NewCollector(root)
	if _, err := collector.Collect(); err != nil {
		t.Fatalf("first Collect() error = %v", err)
	}

	// Total and busy time both grow, but user time shrinks: the per-component
	// delta is negative even though the aggregate looks healthy.
	writeCPUFixture(t, root, "cpu 150 10 200 800 10 5 5 5\n")
	negative, err := collector.Collect()
	if err != nil {
		t.Fatalf("negative delta Collect() error = %v", err)
	}
	if negative.CPU.Status != CPUStatusReset {
		t.Fatalf("CPU status = %q, want %q", negative.CPU.Status, CPUStatusReset)
	}
	if negative.CPU.UsagePercent != nil {
		t.Fatalf("CPU usage = %v, want nil after a negative component delta", negative.CPU.UsagePercent)
	}
}

func TestCountersResetClassification(t *testing.T) {
	base := cpuCounters{user: 100, nice: 10, system: 50, idle: 800, ioWait: 20, irq: 5, softIRQ: 10, steal: 5}

	cases := []struct {
		name      string
		current   cpuCounters
		wantReset bool
	}{
		{name: "identical", current: base, wantReset: false},
		{name: "advancing", current: cpuCounters{user: 150, nice: 10, system: 70, idle: 900, ioWait: 20, irq: 5, softIRQ: 10, steal: 5}, wantReset: false},
		{name: "total moves backwards", current: cpuCounters{user: 10, nice: 1, system: 5, idle: 80, ioWait: 2, irq: 0, softIRQ: 1, steal: 0}, wantReset: true},
		{name: "single component backwards", current: cpuCounters{user: 100, nice: 10, system: 50, idle: 700, ioWait: 20, irq: 5, softIRQ: 10, steal: 5}, wantReset: true},
		{name: "negative user delta with growing total", current: cpuCounters{user: 50, nice: 10, system: 200, idle: 800, ioWait: 20, irq: 5, softIRQ: 10, steal: 5}, wantReset: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := countersReset(base, testCase.current); got != testCase.wantReset {
				t.Fatalf("countersReset() = %v, want %v", got, testCase.wantReset)
			}
		})
	}
}

func TestCollectRejectsMalformedCPUAndMemory(t *testing.T) {
	t.Run("CPU", func(t *testing.T) {
		root := t.TempDir()
		writeProcFixtures(t, root, "cpu 100 invalid 50 800 20 5 10 5\n", 1000, 250, 200, 100, 50)
		collector := NewCollector(root)

		_, err := collector.Collect()
		if err == nil || !strings.Contains(err.Error(), "parse CPU counter") {
			t.Fatalf("Collect() error = %v, want CPU parse error", err)
		}
	})

	t.Run("memory without MemAvailable", func(t *testing.T) {
		root := t.TempDir()
		writeCPUFixture(t, root, "cpu 100 10 50 800 20 5 10 5\n")
		writeFile(t, filepath.Join(root, "meminfo"), "MemTotal: 1000 kB\nCached: 200 kB\n")
		writeFile(t, filepath.Join(root, "uptime"), "123.5 456.7\n")
		collector := NewCollector(root)

		_, err := collector.Collect()
		if err == nil || !strings.Contains(err.Error(), "MemAvailable not found") {
			t.Fatalf("Collect() error = %v, want MemAvailable error", err)
		}
	})
}

func TestCountProcessEntriesSkipsDisappearanceAndPermissionErrors(t *testing.T) {
	entries := []fs.DirEntry{
		fakeDirEntry{name: "101", info: fakeFileInfo{name: "101", directory: true}},
		fakeDirEntry{name: "202", err: fs.ErrPermission},
		fakeDirEntry{name: "303", err: fs.ErrNotExist},
		fakeDirEntry{name: "not-a-pid", info: fakeFileInfo{name: "not-a-pid", directory: true}},
	}

	count, err := countProcessEntries(entries)
	if err != nil {
		t.Fatalf("countProcessEntries() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("process count = %d, want 1", count)
	}
}

func TestCountProcessEntriesReturnsUnexpectedInspectionErrors(t *testing.T) {
	entries := []fs.DirEntry{
		fakeDirEntry{name: "101", err: errors.New("unexpected inspection failure")},
	}

	_, err := countProcessEntries(entries)
	if err == nil || !strings.Contains(err.Error(), "inspect process 101") {
		t.Fatalf("countProcessEntries() error = %v, want inspection error", err)
	}
}

func TestCollectFailsWhenProcRootIsUnavailable(t *testing.T) {
	collector := NewCollector(filepath.Join(t.TempDir(), "missing"))

	_, err := collector.Collect()
	if err == nil || !strings.Contains(err.Error(), "read CPU counters") {
		t.Fatalf("Collect() error = %v, want procfs error", err)
	}
}

func writeProcFixtures(t *testing.T, root, cpu string, total, available, cached, buffers, reclaimable uint64) {
	t.Helper()
	writeCPUFixture(t, root, cpu)
	writeFile(t, filepath.Join(root, "meminfo"), strings.Join([]string{
		"MemTotal:       " + uintString(total) + " kB",
		"MemAvailable:   " + uintString(available) + " kB",
		"Buffers:        " + uintString(buffers) + " kB",
		"Cached:         " + uintString(cached) + " kB",
		"SReclaimable:   " + uintString(reclaimable) + " kB",
		"MemFree:        100 kB",
	}, "\n")+"\n")
	writeFile(t, filepath.Join(root, "uptime"), "123.5 456.7\n")
}

func writeCPUFixture(t *testing.T, root, cpu string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "stat"), cpu+"cpu0 1 2 3\n")
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func uintString(value uint64) string {
	return strconv.FormatUint(value, 10)
}

type fakeDirEntry struct {
	name string
	info fs.FileInfo
	err  error
}

func (entry fakeDirEntry) Name() string { return entry.name }
func (entry fakeDirEntry) IsDir() bool {
	return entry.info != nil && entry.info.IsDir()
}
func (entry fakeDirEntry) Type() fs.FileMode {
	if entry.info == nil {
		return 0
	}
	return entry.info.Mode()
}
func (entry fakeDirEntry) Info() (fs.FileInfo, error) { return entry.info, entry.err }

type fakeFileInfo struct {
	name      string
	directory bool
}

func (info fakeFileInfo) Name() string       { return info.name }
func (info fakeFileInfo) Size() int64        { return 0 }
func (info fakeFileInfo) Mode() fs.FileMode  { return 0 }
func (info fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (info fakeFileInfo) IsDir() bool        { return info.directory }
func (info fakeFileInfo) Sys() any           { return nil }
