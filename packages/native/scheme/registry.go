// Package scheme is the in-process http.Handler for the app origin: static
// files, the batched call endpoint, streams and signals described in
// PROTOCOL.md.
package scheme

import (
	"fmt"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/wire"
)

// Func is a generated wrapper around one exported Go function. args is
// positioned before the arguments array: the wrapper calls BeginArray, reads
// each argument and calls EndArray. On success out must receive exactly one
// value. Anything written to out before an error is discarded.
type Func func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error

// StreamFunc produces a stream. It runs in its own goroutine once the UI has
// opened the stream. emit encodes one value through write and blocks until
// the frame is written (backpressure); it returns the context's error after
// cancellation, and the producer should then return.
type StreamFunc func(ctx krate.Ctx, args *wire.Decoder, emit func(write func(*wire.Encoder)) error) error

// SignalFunc subscribes to a host signal. It must call onChange with the
// current value soon after it is called and again after every change, and
// return a cancel that stops further calls. onChange may be called from any
// goroutine and does not block.
type SignalFunc func(onChange func(write func(*wire.Encoder))) (cancel func())

// Registry maps names (<module id>.<export name>) to generated wrappers.
// Register everything before serving; lookups are safe for concurrent use.
type Registry struct {
	mu      sync.RWMutex
	funcs   map[string]Func
	streams map[string]StreamFunc
	signals map[string]SignalFunc
	// trusted holds ungated twins of functions registered behind a
	// permission check; in-process callers (Invoke) prefer them.
	trusted       map[string]Func
	trustedStream map[string]StreamFunc
	// signalGates check, per subscribing window, whether a signal may be read.
	signalGates map[string]func(window string) error
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		funcs:   make(map[string]Func),
		streams: make(map[string]StreamFunc),
		signals: make(map[string]SignalFunc),

		trusted:       make(map[string]Func),
		trustedStream: make(map[string]StreamFunc),
		signalGates:   make(map[string]func(window string) error),
	}
}

// Snapshot is the set of registrations of a Registry at one moment.
type Snapshot struct {
	Funcs   map[string]Func
	Streams map[string]StreamFunc
	Signals map[string]SignalFunc
}

// Snapshot returns copies of the registered functions, streams and signals
// (the gated registrations, not the Trusted twins). A plugin host uses it to
// inspect what generated glue registered before it forwards anything.
func (r *Registry) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s := Snapshot{Funcs: map[string]Func{}, Streams: map[string]StreamFunc{}, Signals: map[string]SignalFunc{}}
	for k, v := range r.funcs {
		s.Funcs[k] = v
	}
	for k, v := range r.streams {
		s.Streams[k] = v
	}
	for k, v := range r.signals {
		s.Signals[k] = v
	}
	return s
}

// SignalGate makes the UI's subscription to the signal name fail with the
// error gate returns for the subscribing window (the x-krate-window header of
// the request, "" when absent). In-process callers (Subscribe) are not gated.
func (r *Registry) SignalGate(name string, gate func(window string) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.signalGates[name] = gate
}

func (r *Registry) signalGate(name string) func(string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.signalGates[name]
}

// Trusted registers f as the implementation of name for in-process callers
// (Invoke), which are trusted app code and not subject to the permission
// check that the gated registration of name applies to the UI. The UI cannot
// reach it. It panics on a duplicate.
func (r *Registry) Trusted(name string, f Func) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.trusted[name]; dup {
		panic(fmt.Sprintf("scheme: trusted func %q registered twice", name))
	}
	r.trusted[name] = f
}

// TrustedStream is Trusted for streams.
func (r *Registry) TrustedStream(name string, f StreamFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.trustedStream[name]; dup {
		panic(fmt.Sprintf("scheme: trusted stream %q registered twice", name))
	}
	r.trustedStream[name] = f
}

// lookupTrusted is lookup for in-process callers: the trusted twin when there
// is one.
func (r *Registry) lookupTrusted(name string) (Func, StreamFunc) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, s := r.trusted[name], r.trustedStream[name]
	if f == nil {
		f = r.funcs[name]
	}
	if s == nil {
		s = r.streams[name]
	}
	return f, s
}

// CopyTrusted registers on dst the trusted form (the Trusted twin, else the
// function itself) of every function and stream of r whose name starts with
// prefix, skipping names dst already has. It serves a private socket for trusted
// child processes: they get the ungated implementations, as in-process callers do.
func (r *Registry) CopyTrusted(dst *Registry, prefix string) {
	r.mu.RLock()
	fns := map[string]Func{}
	sts := map[string]StreamFunc{}
	for name, f := range r.funcs {
		if strings.HasPrefix(name, prefix) {
			if t := r.trusted[name]; t != nil {
				f = t
			}
			fns[name] = f
		}
	}
	for name, s := range r.streams {
		if strings.HasPrefix(name, prefix) {
			if t := r.trustedStream[name]; t != nil {
				s = t
			}
			sts[name] = s
		}
	}
	r.mu.RUnlock()
	dst.mu.Lock()
	defer dst.mu.Unlock()
	for name, f := range fns {
		if _, ok := dst.funcs[name]; ok {
			continue
		}
		if _, ok := dst.streams[name]; ok {
			continue
		}
		dst.funcs[name] = f
	}
	for name, s := range sts {
		if _, ok := dst.funcs[name]; ok {
			continue
		}
		if _, ok := dst.streams[name]; ok {
			continue
		}
		dst.streams[name] = s
	}
}

// Func registers a request/response function. It panics on a duplicate name.
func (r *Registry) Func(name string, f Func) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checkFree(name, "func")
	r.funcs[name] = f
}

// Stream registers a streaming function. It panics on a duplicate name.
func (r *Registry) Stream(name string, f StreamFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checkFree(name, "stream")
	r.streams[name] = f
}

// Signal registers a host signal. It panics on a duplicate name.
func (r *Registry) Signal(name string, f SignalFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.signals[name]; dup {
		panic(fmt.Sprintf("scheme: signal %q registered twice", name))
	}
	r.signals[name] = f
}

// checkFree panics if name is already a func or stream. Callers hold r.mu.
func (r *Registry) checkFree(name, kind string) {
	_, f := r.funcs[name]
	_, s := r.streams[name]
	if f || s {
		panic(fmt.Sprintf("scheme: %s %q registered twice", kind, name))
	}
}

func (r *Registry) lookup(name string) (Func, StreamFunc) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.funcs[name], r.streams[name]
}

func (r *Registry) signal(name string) SignalFunc {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.signals[name]
}

// SignalOf adapts a krate.Signal to a SignalFunc; enc writes one value.
// Generated glue uses it.
func SignalOf[T any](s *krate.Signal[T], enc func(*wire.Encoder, T)) SignalFunc {
	return func(onChange func(write func(*wire.Encoder))) (cancel func()) {
		return s.Watch(func(v T) {
			onChange(func(e *wire.Encoder) { enc(e, v) })
		})
	}
}
