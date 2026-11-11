package memory

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	for in, want := range map[string]Profile{"": Balanced, "balanced": Balanced, "minimal": Minimal, "performance": Performance, "bogus": Balanced} {
		if got := Parse(in); got != want {
			t.Errorf("Parse(%q) = %q, want %q", in, got, want)
		}
	}
	if Performance.Enabled() || !Balanced.Enabled() || !Minimal.Enabled() {
		t.Error("Enabled")
	}
	if Balanced.HostMemoryMB() != 64 || Minimal.HostMemoryMB() != 32 || Performance.HostMemoryMB() != 256 {
		t.Error("HostMemoryMB")
	}
}

func TestPerformanceHasNoReaper(t *testing.T) {
	if StartReaper(Performance, nil) != nil {
		t.Fatal("performance started a reaper")
	}
	(*Reaper)(nil).Touch()
	(*Reaper)(nil).Stop()
}

func TestReaperTrimsAfterQuiet(t *testing.T) {
	var n atomic.Int32
	r := startReaper(300*time.Millisecond, 20*time.Millisecond, func() { n.Add(1) })
	defer r.Stop()
	waitFor(t, func() bool { return n.Load() == 1 })
	// Activity restarts the wait; the next trim needs the long quiet period.
	r.Touch()
	time.Sleep(100 * time.Millisecond)
	if n.Load() != 1 {
		t.Fatal("trimmed before the idle period")
	}
	waitFor(t, func() bool { return n.Load() == 2 })
}

func TestRequestTrimsQuickly(t *testing.T) {
	var n atomic.Int32
	r := startReaper(time.Hour, 20*time.Millisecond, func() { n.Add(1) })
	defer r.Stop()
	waitFor(t, func() bool { return n.Load() == 1 })
	Request()
	waitFor(t, func() bool { return n.Load() == 2 })
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}
