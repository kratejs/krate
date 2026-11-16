package membudget

import (
	"os"
	"runtime/debug"
	"runtime/metrics"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// checkEvery is how stale a pressure reading may be.
	checkEvery = 100 * time.Millisecond
	// pollEvery is how often a job held back by memory pressure looks again.
	pollEvery = 50 * time.Millisecond
)

// monitor measures the build's memory against the budget.
type monitor struct {
	mu       sync.Mutex
	checked  time.Time
	pressure bool
	external func() int64
	// read is readSystem, replaced in tests.
	read func() system
	// usage returns the process's live heap and resident Go memory, replaced
	// in tests.
	usage func() (live, resident int64)
}

var defaultMonitor monitor

func (m *monitor) reset() {
	m.mu.Lock()
	m.checked = time.Time{}
	m.pressure = false
	m.mu.Unlock()
}

// SetExternal registers fn, which returns the memory the build's helper
// processes (JavaScript workers) use, so it counts against the budget.
func SetExternal(fn func() int64) {
	defaultMonitor.mu.Lock()
	defaultMonitor.external = fn
	defaultMonitor.checked = time.Time{}
	defaultMonitor.mu.Unlock()
}

// Pressure reports whether memory is short: the build's live data nears the
// budget, the build uses nearly all of it, or the machine (or container) has
// little memory left. It re-measures at most every 100ms.
func Pressure() bool {
	return defaultMonitor.check(time.Now())
}

func (m *monitor) check(now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.checked.IsZero() && now.Sub(m.checked) < checkEvery {
		return m.pressure
	}
	m.checked = now
	b, _ := Current()
	usage, read := m.usage, m.read
	if usage == nil {
		usage = goUsage
	}
	if read == nil {
		read = readSystem
	}
	live, resident := usage()
	var ext int64
	if m.external != nil {
		ext = m.external()
	}
	sys := read()
	m.pressure = short(b.Bytes, live+ext, resident+ext, sys)
	if m.pressure {
		pressured.Add(1)
	}
	return m.pressure
}

// short decides pressure from the budget, the build's live and resident
// memory, and what the system has left.
func short(budget, live, resident int64, sys system) bool {
	if live > budget/10*6 || resident > budget/10*9 {
		return true
	}
	if sys.avail > 0 && !sys.guessed {
		reserve := max(256*mib, sys.total/20)
		return sys.avail < reserve
	}
	return false
}

var goSamples = []metrics.Sample{
	{Name: "/gc/heap/live:bytes"},
	{Name: "/memory/classes/total:bytes"},
	{Name: "/memory/classes/heap/released:bytes"},
}

var goSamplesMu sync.Mutex

func goUsage() (live, resident int64) {
	goSamplesMu.Lock()
	defer goSamplesMu.Unlock()
	metrics.Read(goSamples)
	val := func(i int) int64 {
		if goSamples[i].Value.Kind() != metrics.KindUint64 {
			return 0
		}
		return int64(goSamples[i].Value.Uint64())
	}
	return val(0), val(1) - val(2)
}

var (
	pressured atomic.Int64
	throttled atomic.Int64
)

// Throttled returns how many times a job waited for memory.
func Throttled() int64 { return throttled.Load() }

// Gate admits up to slots jobs at once, in the order they asked. While memory
// is short it admits a job only when none of its own run, so the work slows
// down instead of running out of memory, and a job always runs.
type Gate struct {
	mu       sync.Mutex
	slots    int
	running  int
	queue    []chan struct{}
	polling  bool
	pressure func() bool
}

// NewGate returns a gate of slots (at least 1) jobs that reads the process's
// memory pressure.
func NewGate(slots int) *Gate {
	return newGate(slots, Pressure)
}

func newGate(slots int, pressure func() bool) *Gate {
	return &Gate{slots: max(slots, 1), pressure: pressure}
}

// Slots returns the most jobs the gate admits at once.
func (g *Gate) Slots() int { return g.slots }

// Acquire blocks until the gate admits a job.
func (g *Gate) Acquire() {
	g.mu.Lock()
	if len(g.queue) == 0 && g.admits() {
		g.running++
		g.mu.Unlock()
		return
	}
	ch := make(chan struct{})
	g.queue = append(g.queue, ch)
	g.dispatchLocked()
	g.mu.Unlock()
	<-ch
}

// Release ends a job Acquire admitted.
func (g *Gate) Release() {
	g.mu.Lock()
	g.running--
	g.dispatchLocked()
	g.mu.Unlock()
}

// admits reports whether one more job may start now; g.mu is held.
func (g *Gate) admits() bool {
	if g.running >= g.slots {
		return false
	}
	return g.running == 0 || !g.pressure()
}

// dispatchLocked admits the queued jobs that may start. When memory pressure
// holds them back it looks again shortly; g.mu is held.
func (g *Gate) dispatchLocked() {
	for len(g.queue) > 0 && g.admits() {
		ch := g.queue[0]
		g.queue[0] = nil
		g.queue = g.queue[1:]
		g.running++
		close(ch)
	}
	if len(g.queue) == 0 || g.running >= g.slots || g.polling {
		return
	}
	// A slot is free but memory is short.
	throttled.Add(1)
	g.polling = true
	time.AfterFunc(pollEvery, func() {
		g.mu.Lock()
		g.polling = false
		g.dispatchLocked()
		g.mu.Unlock()
	})
}

// GoLimit is the Go heap's soft limit for a budget: a third of it, at least
// heapTarget, and never more than the budget less reserve for the helper
// processes (which takes at most half the budget).
func GoLimit(budget, reserve int64) int64 {
	ceiling := budget - min(reserve, budget/2)
	return max(min(ceiling, max(heapTarget, budget/3)), minBudget)
}

// heapTarget is the smallest Go heap limit below the budget's ceiling: less
// would make the collector run nearly all the time on large sites.
const heapTarget = 4 * gib

// ApplyGoLimit sets the Go soft memory limit from the process's budget,
// unless GOMEMLIMIT is set. reserve is left for helper processes. It returns
// the limit set, or 0.
func ApplyGoLimit(reserve int64) int64 {
	if os.Getenv("GOMEMLIMIT") != "" {
		return 0
	}
	b, _ := Current()
	lim := GoLimit(b.Bytes, reserve)
	debug.SetMemoryLimit(lim)
	return lim
}
