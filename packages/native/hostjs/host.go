// Package hostjs runs a Krate Native app's TypeScript host code in one
// persistent embedded QuickJS VM and exposes it to the webview through a
// scheme.Registry.
//
// One goroutine owns the VM for the life of the Host. Go code never touches
// the VM directly: calls, timers and I/O completions are jobs on a queue that
// the VM goroutine runs, draining microtasks after each. fetch runs on its own
// goroutines and posts its completion back, so a slow request never stops
// other calls.
//
// A host bundle is an IIFE that registers its exports on the global
// __krateHost:
//
//	__krateHost.register(id, fn, "func" | "stream")
//	__krateHost.registerSignal(id, sig) // sig has get() and subscribe(fn) -> unsubscribe
//
// Functions receive their arguments decoded from the wire format (Uint8Array,
// Date, Map and Set included) and may be async. Inside a function, this.signal
// is an AbortSignal that aborts when the call is cancelled and this.window is
// the id of the calling window. A stream function returns an async iterable.
package hostjs

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kratejs/krate/packages/native/jsruntime"
	"github.com/kratejs/krate/packages/native/scheme"
)

//go:embed host.js
var hostSource string

// ErrClosed is returned by calls made after Close.
var ErrClosed = errors.New("hostjs: host is closed")

// lineBase is how many blank lines precede a host bundle when it is
// evaluated. QuickJS names every script "<eval>", so this offset tells the
// bundle's stack frames from those of the web API prelude.
const lineBase = 100000

// Options configures a Host.
type Options struct {
	// MemoryMB is the VM memory limit in MiB. Default 256.
	MemoryMB int
	// Watchdog bounds one synchronous step of JavaScript (a call's start, a
	// timer callback, a microtask drain), so a runaway loop cannot freeze the
	// app. Waiting on promises does not count. Default 10 minutes; negative
	// disables it.
	Watchdog time.Duration
	// Dev includes the mapped JS stack in the Data of errors that carry no
	// data of their own. Unexpected errors always carry it.
	Dev bool
	// Env becomes process.env.
	Env map[string]string
	// LogPrefix is prepended to console output.
	LogPrefix string
	// Log receives diagnostics about host errors. Default: stderr.
	Log func(msg string)
}

// Host is a persistent QuickJS VM with a job queue and an event loop.
type Host struct {
	o  Options
	rt *jsruntime.Runtime

	mu     sync.Mutex
	jobs   []func()
	closed bool
	wake   chan struct{}
	done   chan struct{}
	once   sync.Once

	nextID atomic.Int64

	// Touched only on the VM goroutine.
	sinks    map[int64]*sink
	sm       *sourceMap
	filename string
	nextBC   []byte
	runBC    []byte

	// Guarded by mu.
	listed listing
	bridge *scheme.Registry
	regd   map[*scheme.Registry]map[string]bool
}

// New creates a Host and starts its event loop. The VM has the Web API
// prelude (fetch, timers, URL, streams, crypto and the rest load on first
// use) and no user code until Load.
func New(opts Options) (*Host, error) {
	if opts.MemoryMB <= 0 {
		opts.MemoryMB = 256
	}
	if opts.Watchdog == 0 {
		opts.Watchdog = 10 * time.Minute
	}
	if opts.Log == nil {
		opts.Log = func(msg string) { fmt.Fprintln(os.Stderr, msg) }
	}
	rt, err := jsruntime.NewWithOptions(jsruntime.Options{
		Preload:     []string{"console", "events", "timers", "url", "fetch"},
		MemoryLimit: uintptr(opts.MemoryMB) << 20,
		EvalTimeout: opts.Watchdog,
	})
	if err != nil {
		return nil, err
	}
	h := &Host{
		o:     opts,
		rt:    rt,
		wake:  make(chan struct{}, 1),
		done:  make(chan struct{}),
		sinks: map[int64]*sink{},
		regd:  map[*scheme.Registry]map[string]bool{},
	}
	fail := func(err error) (*Host, error) {
		_ = rt.Close()
		return nil, err
	}
	rt.SetLogPrefix(opts.LogPrefix)
	if opts.Env != nil {
		if err := rt.SetEnv(opts.Env); err != nil {
			return fail(err)
		}
	}
	if err := rt.EnableAsyncFetch(func(fn func()) { h.post(fn) }); err != nil {
		return fail(err)
	}
	if err := h.registerHosts(); err != nil {
		return fail(err)
	}
	if err := h.registerBridge(); err != nil {
		return fail(err)
	}
	if _, err := rt.Execute(hostSource); err != nil {
		return fail(fmt.Errorf("hostjs: installing the host runtime: %w", err))
	}
	if h.nextBC, err = rt.Compile("__krate_timers_next()", jsruntime.EvalGlobal); err != nil {
		return fail(err)
	}
	if h.runBC, err = rt.Compile("__krate_timers_run()", jsruntime.EvalGlobal); err != nil {
		return fail(err)
	}
	go h.loop()
	return h, nil
}

// Close stops the event loop and releases the VM. Pending calls fail with
// ErrClosed. It is safe to call more than once.
func (h *Host) Close() error {
	h.once.Do(func() {
		h.mu.Lock()
		h.closed = true
		h.mu.Unlock()
		h.signal()
	})
	<-h.done
	return nil
}

func (h *Host) signal() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// post queues fn for the VM goroutine. It reports false if the host is closed.
func (h *Host) post(fn func()) bool {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return false
	}
	h.jobs = append(h.jobs, fn)
	h.mu.Unlock()
	h.signal()
	return true
}

// do runs fn on the VM goroutine and returns its error.
func (h *Host) do(fn func() error) error {
	res := make(chan error, 1)
	if !h.post(func() { res <- fn() }) {
		return ErrClosed
	}
	select {
	case err := <-res:
		return err
	case <-h.done:
		select {
		case err := <-res:
			return err
		default:
			return ErrClosed
		}
	}
}

func (h *Host) logf(format string, args ...any) {
	h.o.Log(fmt.Sprintf(format, args...))
}

// Load evaluates a host bundle, an IIFE that registers its exports with
// __krateHost. sourceMapJSON (may be nil) maps the bundle back to the original
// sources for error positions; filename names the bundle when there is no map.
// Load drains the microtasks the bundle started before it returns.
func (h *Host) Load(bundleJS, sourceMapJSON []byte, filename string) error {
	return h.load(bundleJS, sourceMapJSON, filename)
}

// Reload re-evaluates a bundle in the same VM, for development.
//
// Functions, streams and signals registered by the new bundle replace those
// with the same id; calls already running finish with the old code. Ids the new
// bundle no longer registers answer not_found. New ids appear once Register is
// called again with the same Registry. The old bundle's timers are cleared.
// A signal whose id persists gets the old value (when the new signal has set)
// and connected readers move to the new signal. Module-level state of the old
// bundle is not kept. If the new bundle throws, the old registrations stay.
func (h *Host) Reload(bundleJS, sourceMapJSON []byte, filename string) error {
	return h.load(bundleJS, sourceMapJSON, filename)
}

func (h *Host) load(bundle, mapJSON []byte, filename string) error {
	var sm *sourceMap
	if len(mapJSON) > 0 {
		var err error
		if sm, err = parseSourceMap(mapJSON); err != nil {
			return err
		}
	}
	src := "try{" + strings.Repeat("\n", lineBase) + string(bundle) + "\n}catch(e){__kh_load_failed(e);throw e}"
	var lst listing
	err := h.do(func() (err error) {
		if _, err = h.rt.Execute("__kh_load_begin()"); err != nil {
			return err
		}
		committed := false
		defer func() {
			if !committed {
				_, _ = h.rt.Execute("__kh_load_end(false)")
			}
		}()
		h.sm, h.filename = sm, filename
		if _, err = h.rt.Execute(src); err != nil {
			return h.loadError(err)
		}
		h.rt.DrainJobs()
		committed = true
		if _, err = h.rt.Execute("__kh_load_end(true)"); err != nil {
			return err
		}
		h.rt.DrainJobs()
		v, err := h.rt.Execute("__kh_list()")
		if err != nil {
			return err
		}
		s, _ := v.(string)
		lst, err = parseListing(s)
		return err
	})
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.listed = lst
	h.mu.Unlock()
	return nil
}

// loadError maps the position in a bundle's syntax or runtime error.
func (h *Host) loadError(err error) error {
	msg := err.Error()
	if v, e := h.rt.Execute("typeof __kh_load_error === 'string' ? __kh_load_error : ''"); e == nil {
		if s, _ := v.(string); s != "" {
			var info struct{ Message, Stack string }
			if json.Unmarshal([]byte(s), &info) == nil {
				msg = info.Message
				if st := h.mapStack(info.Stack); st != "" {
					msg += "\n" + st
				}
			}
			_, _ = h.rt.Execute("delete globalThis.__kh_load_error")
		}
	}
	return fmt.Errorf("hostjs: loading %s: %s", h.filename, msg)
}
