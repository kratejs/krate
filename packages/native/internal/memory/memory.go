// Package memory holds the memory profiles of a Krate Native app and the
// process-side levers they drive: the Go soft memory limit, returning freed
// memory to the OS, and an idle reaper that does so when the app is quiet.
//
// Profiles (app.json "memory", config native.memory):
//
//	performance  the engine defaults; none of the levers run.
//	balanced     the default: trims what a typical app does not need.
//	minimal      trims harder, at some CPU cost.
package memory

import (
	"os"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// Profile names a memory profile.
type Profile string

// The profiles.
const (
	Performance Profile = "performance"
	Balanced    Profile = "balanced"
	Minimal     Profile = "minimal"
)

// EnvProfile carries the profile to child processes (the Go host child).
const EnvProfile = "KRATE_MEMORY_PROFILE"

// Parse maps a manifest value to a profile. Empty and unknown values are
// Balanced, the default.
func Parse(s string) Profile {
	switch Profile(s) {
	case Performance, Minimal:
		return Profile(s)
	}
	return Balanced
}

// Enabled reports whether the profile applies any lever.
func (p Profile) Enabled() bool { return p != Performance }

// HostMemoryMB is the default QuickJS heap limit in MiB.
func (p Profile) HostMemoryMB() int {
	switch p {
	case Balanced:
		return 64
	case Minimal:
		return 32
	}
	return 256
}

// GoMemoryLimit is the soft Go memory limit in bytes, 0 for none.
func (p Profile) GoMemoryLimit() int64 {
	switch p {
	case Balanced:
		return 64 << 20
	case Minimal:
		return 32 << 20
	}
	return 0
}

// IdleAfter is how long the app must be quiet before the idle trim.
func (p Profile) IdleAfter() time.Duration {
	if p == Minimal {
		return 15 * time.Second
	}
	return 30 * time.Second
}

// quickTrim is the quiet period after which the first trim (the page has
// loaded) and requested trims (a window closed or hid) run.
const quickTrim = 2 * time.Second

// ApplyGo sets the Go soft memory limit for the profile. An explicit
// GOMEMLIMIT in the environment wins. It reports whether a limit was set.
func ApplyGo(p Profile) bool {
	n := p.GoMemoryLimit()
	if n == 0 || os.Getenv("GOMEMLIMIT") != "" {
		return false
	}
	debug.SetMemoryLimit(n)
	return true
}

var (
	hookMu sync.Mutex
	hooks  []func()
)

// AddTrimHook registers a function Trim calls after returning the Go heap to
// the OS. The Linux platform layer registers malloc_trim here.
func AddTrimHook(fn func()) {
	hookMu.Lock()
	hooks = append(hooks, fn)
	hookMu.Unlock()
}

// Trim returns freed memory to the operating system: the Go heap
// (debug.FreeOSMemory) and whatever the registered hooks release.
func Trim() {
	debug.FreeOSMemory()
	hookMu.Lock()
	hs := append([]func(){}, hooks...)
	hookMu.Unlock()
	for _, h := range hs {
		h()
	}
}

// active is the reaper that Request nudges.
var active atomic.Pointer[Reaper]

// Request asks the running reaper for a trim after a short quiet period. Call
// it when a window closes or hides. It does nothing without a reaper.
func Request() {
	if r := active.Load(); r != nil {
		r.fast.Store(true)
		r.Touch()
	}
}

// Reaper runs Trim, and an optional callback, when the app has been quiet.
type Reaper struct {
	idle   time.Duration
	onIdle func()

	last  atomic.Int64 // unix nanos of the latest activity
	fast  atomic.Bool
	wake  chan struct{}
	stop  chan struct{}
	done  chan struct{}
	first bool
}

// StartReaper starts the idle reaper for a profile, or returns nil for
// Performance. onIdle (may be nil) runs before Trim, for example to collect
// the host runtime's garbage. The first trim runs shortly after the app
// first goes quiet (the first page has loaded); later ones after IdleAfter.
func StartReaper(p Profile, onIdle func()) *Reaper {
	if !p.Enabled() {
		return nil
	}
	return startReaper(p.IdleAfter(), quickTrim, onIdle)
}

func startReaper(idle, quick time.Duration, onIdle func()) *Reaper {
	r := &Reaper{idle: idle, onIdle: onIdle, wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}), first: true}
	r.last.Store(time.Now().UnixNano())
	active.Store(r)
	go r.run(quick)
	return r
}

// Touch records activity (a request reached the host).
func (r *Reaper) Touch() {
	if r == nil {
		return
	}
	r.last.Store(time.Now().UnixNano())
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Stop ends the reaper.
func (r *Reaper) Stop() {
	if r == nil {
		return
	}
	active.CompareAndSwap(r, nil)
	select {
	case <-r.stop:
	default:
		close(r.stop)
	}
	<-r.done
}

func (r *Reaper) run(quick time.Duration) {
	defer close(r.done)
	pending := true // the start counts as activity
	for {
		if !pending {
			select {
			case <-r.wake:
			case <-r.stop:
				return
			}
		}
		pending = false
		for {
			quiet := r.idle
			if r.first || r.fast.Load() {
				quiet = quick
			}
			d := quiet - time.Since(time.Unix(0, r.last.Load()))
			if d <= 0 {
				break
			}
			t := time.NewTimer(d)
			select {
			case <-t.C:
			case <-r.stop:
				t.Stop()
				return
			}
		}
		r.first = false
		r.fast.Store(false)
		if r.onIdle != nil {
			r.onIdle()
		}
		Trim()
	}
}
