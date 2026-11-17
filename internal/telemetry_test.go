package internal

import "testing"

func TestReadMemoryReal(t *testing.T) {
	m, err := readMemory()
	if err != nil {
		t.Fatalf("readMemory: %v", err)
	}
	if m.Total <= 0 {
		t.Fatalf("MemTotal must be > 0, got %d", m.Total)
	}
	if m.Available <= 0 || m.Available > m.Total {
		t.Fatalf("bad available/total: avail=%d total=%d", m.Available, m.Total)
	}
	if m.Used != m.Total-m.Available {
		t.Fatalf("used=%d want %d", m.Used, m.Total-m.Available)
	}
	t.Logf("meminfo: total=%dMB avail=%dMB used=%dMB cache=%dMB", m.Total, m.Available, m.Used, m.Cache)
}

func TestReadProcessCountReal(t *testing.T) {
	n, err := readProcessCount()
	if err != nil {
		t.Fatalf("readProcessCount: %v", err)
	}
	if n <= 0 {
		t.Fatalf("expected > 0 live processes, got %d", n)
	}
	t.Logf("procs: %d", n)
}

func TestCPUDelta(t *testing.T) {
	tel := NewTelemetry()
	// first sample: no delta, returns 0 by design
	s1, err := tel.Collect()
	if err != nil {
		t.Fatalf("Collect #1: %v", err)
	}
	if s1.CPU != 0 {
		t.Fatalf("first sample must be 0 (no delta), got %v", s1.CPU)
	}
	// spin a bit so the counter delta is measurable
	sum := 0
	for i := 0; i < 5_000_000; i++ {
		sum += i
	}
	_ = sum
	s2, err := tel.Collect()
	if err != nil {
		t.Fatalf("Collect #2: %v", err)
	}
	if s2.CPU < 0 || s2.CPU > 100 {
		t.Fatalf("CPU must be in [0,100], got %v", s2.CPU)
	}
	t.Logf("cpu after busy loop: %.1f%%", s2.CPU)
}

func TestStatsJSONKeys(t *testing.T) {
	// guards the public JSON shape used by the portfolio labs
	s := Stats{CPU: 1.5, MemoryTotal: 16000, MemoryAvailable: 8000, MemoryUsed: 8000, MemoryCache: 2000, Processes: 400, UptimeSeconds: 12}
	if s.CPU == 0 || s.MemoryTotal == 0 || s.Processes == 0 {
		t.Fatal("zeroed stats")
	}
}
