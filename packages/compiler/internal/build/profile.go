package build

import (
	"fmt"
	"io"
	"sort"
	"sync"
	"time"
)

// Profiler accumulates wall-clock time per build phase. Phases are recorded
// from parallel page goroutines, so every method is safe for concurrent use.
// It is a no-op unless enabled, so callers can instrument freely.
type Profiler struct {
	mu      sync.Mutex
	enabled bool
	start   time.Time
	totals  map[string]time.Duration
	counts  map[string]int
}

// NewProfiler returns a profiler that records when enabled is true.
func NewProfiler(enabled bool) *Profiler {
	return &Profiler{
		enabled: enabled,
		start:   time.Now(),
		totals:  make(map[string]time.Duration),
		counts:  make(map[string]int),
	}
}

// Enabled reports whether phase timings are being recorded.
func (p *Profiler) Enabled() bool { return p != nil && p.enabled }

// Phase begins a phase and returns a function that ends it. When profiling is
// disabled (or p is nil) the returned function is a no-op.
func (p *Profiler) Phase(name string) func() {
	if p == nil || !p.enabled {
		return func() {}
	}
	t0 := time.Now()
	return func() {
		d := time.Since(t0)
		p.mu.Lock()
		p.totals[name] += d
		p.counts[name]++
		p.mu.Unlock()
	}
}

// Add records a duration directly (for phases measured elsewhere).
func (p *Profiler) Add(name string, d time.Duration) {
	if p == nil || !p.enabled {
		return
	}
	p.mu.Lock()
	p.totals[name] += d
	p.counts[name]++
	p.mu.Unlock()
}

// Report writes a sorted, human-readable phase table to w.
func (p *Profiler) Report(w io.Writer) {
	if p == nil || !p.enabled {
		return
	}
	p.mu.Lock()
	type row struct {
		name  string
		total time.Duration
		count int
	}
	rows := make([]row, 0, len(p.totals))
	for name, total := range p.totals {
		rows = append(rows, row{name, total, p.counts[name]})
	}
	elapsed := time.Since(p.start)
	p.mu.Unlock()

	sort.Slice(rows, func(i, j int) bool { return rows[i].total > rows[j].total })

	fmt.Fprintf(w, "\n  profile (total %s)\n", elapsed.Round(time.Millisecond))
	for _, r := range rows {
		fmt.Fprintf(w, "    %-22s %10s  x%d\n", r.name, r.total.Round(time.Millisecond), r.count)
	}
}
