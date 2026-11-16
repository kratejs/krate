package build

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

// Profiler accumulates wall-clock time per build phase. Phases are recorded
// from parallel page goroutines, so every method is safe for concurrent use.
// It is a no-op unless enabled, so callers can instrument freely.
//
// Phase names are "group:name". Report prints each group under its parent
// phase (profileParents) with the time no sub-phase accounts for, so the
// sub-phases of a group always add up to the parent.
type Profiler struct {
	mu      sync.Mutex
	enabled bool
	start   time.Time
	totals  map[string]time.Duration
	counts  map[string]int
	esbuild map[string]esbuildStat
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
	return func() { p.Add(name, time.Since(t0)) }
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

// Laps times consecutive steps of one run of a phase. Each Lap call records the
// time since the previous one (or since Laps) under prefix+name, so the steps
// of a run cover it without gaps. Each step is counted once per run.
type Laps struct {
	p      *Profiler
	prefix string
	last   time.Time
}

// Laps starts timing the steps of one run of the phases named prefix+step.
func (p *Profiler) Laps(prefix string) *Laps {
	if p == nil || !p.enabled {
		return nil
	}
	return &Laps{p: p, prefix: prefix, last: time.Now()}
}

// Lap ends the current step, naming it, and starts the next one.
func (l *Laps) Lap(name string) {
	if l == nil {
		return
	}
	now := time.Now()
	l.p.Add(l.prefix+name, now.Sub(l.last))
	l.last = now
}

// setEsbuild records the esbuild calls made during the build.
func (p *Profiler) setEsbuild(calls map[string]esbuildStat) {
	if p == nil || !p.enabled {
		return
	}
	p.mu.Lock()
	p.esbuild = calls
	p.mu.Unlock()
}

// profileParents names the phase each group of sub-phases divides. The
// "build" group divides the whole run.
var profileParents = map[string]string{
	"build":   "build:total",
	"page":    "page:total",
	"write":   "build:write",
	"tools":   "build:tools",
	"islands": "page:islands",
}

// profileGroupOrder is the order groups are printed in.
var profileGroupOrder = []string{"build", "write", "tools", "page", "islands"}

type profileRow struct {
	name  string
	total time.Duration
	count int
}

// Report writes the phase tables to w: the build phases, which add up to the
// total, then each group of sub-phases under the phase it divides.
func (p *Profiler) Report(w io.Writer) {
	if p == nil || !p.enabled {
		return
	}
	p.mu.Lock()
	groups := map[string][]profileRow{}
	for name, total := range p.totals {
		g, _, ok := strings.Cut(name, ":")
		if !ok {
			g = ""
		}
		if parent, has := profileParents[g]; has && parent == name {
			continue
		}
		groups[g] = append(groups[g], profileRow{name, total, p.counts[name]})
	}
	totals := make(map[string]time.Duration, len(p.totals))
	counts := make(map[string]int, len(p.counts))
	for k, v := range p.totals {
		totals[k] = v
		counts[k] = p.counts[k]
	}
	calls := p.esbuild
	elapsed := time.Since(p.start)
	p.mu.Unlock()

	if t, ok := totals["build:total"]; ok {
		elapsed = t
	}
	fmt.Fprintf(w, "\n  profile (total %s)\n", elapsed.Round(time.Millisecond))
	order := append([]string(nil), profileGroupOrder...)
	var rest []string
	for g := range groups {
		known := false
		for _, o := range order {
			known = known || o == g
		}
		if !known {
			rest = append(rest, g)
		}
	}
	sort.Strings(rest)
	order = append(order, rest...)
	for _, g := range order {
		rows := groups[g]
		if len(rows) == 0 {
			continue
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].total > rows[j].total })
		parent := profileParents[g]
		indent := "    "
		if parent != "" && g != "build" {
			fmt.Fprintf(w, "\n    %-28s %12s  x%d\n", parent, totals[parent].Round(time.Millisecond), counts[parent])
			indent = "      "
		}
		var sum time.Duration
		for _, r := range rows {
			sum += r.total
			fmt.Fprintf(w, "%s%-*s %12s  x%d\n", indent, 32-len(indent), r.name, r.total.Round(time.Millisecond), r.count)
		}
		if parent != "" {
			whole := totals[parent]
			if g == "build" {
				whole = elapsed
			}
			if other := whole - sum; other > 0 {
				fmt.Fprintf(w, "%s%-*s %12s\n", indent, 32-len(indent), "(untracked)", other.Round(time.Millisecond))
			}
		}
	}
	if len(calls) > 0 {
		rows := make([]profileRow, 0, len(calls))
		var n int
		var sum time.Duration
		for site, s := range calls {
			rows = append(rows, profileRow{site, s.d, s.n})
			n += s.n
			sum += s.d
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].total > rows[j].total })
		fmt.Fprintf(w, "\n    %-28s %12s  x%d\n", "esbuild calls (by caller)", sum.Round(time.Millisecond), n)
		for _, r := range rows {
			fmt.Fprintf(w, "      %-26s %12s  x%d\n", r.name, r.total.Round(time.Millisecond), r.count)
		}
	}
}
