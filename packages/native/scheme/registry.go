// Package scheme is the in-process http.Handler for the app origin: static
// files, the batched call endpoint, streams and signals described in
// PROTOCOL.md.
package scheme

import (
	"fmt"
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
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		funcs:   make(map[string]Func),
		streams: make(map[string]StreamFunc),
		signals: make(map[string]SignalFunc),
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
