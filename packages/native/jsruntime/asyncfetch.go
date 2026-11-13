package jsruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// asyncFetch is the state of EnableAsyncFetch.
type asyncFetch struct {
	post func(func())

	mu      sync.Mutex
	cancels map[int64]context.CancelFunc
	closed  bool
}

// EnableAsyncFetch makes fetch non-blocking: each request runs on its own
// goroutine and its completion is delivered by calling post(fn). post must run
// fn later on the goroutine that drives the VM, and must not block; fn may
// evaluate JavaScript. The sync path (API and middleware workers) stays the
// default, where fetch blocks the VM until the response is read.
//
// Call it before any script runs fetch. Pending requests are cancelled by
// Close.
func (r *Runtime) EnableAsyncFetch(post func(func())) error {
	if r.async != nil {
		return fmt.Errorf("async fetch is already enabled")
	}
	a := &asyncFetch{post: post, cancels: map[int64]context.CancelFunc{}}
	// The fetch feature reads __go_fetch_async and __go_fetch_cancel on each
	// call, so it may load before or after this.
	if err := r.registerTakeHost(); err != nil {
		return err
	}
	if err := r.registerBinaryHost("__go_fetch_async", "nsb", func(args []any) (any, error) {
		id, ok := intArg(args, 0)
		if !ok {
			return nil, fmt.Errorf("fetch id must be a number")
		}
		a.start(r, int64(id), strArg(args, 1), bytesArg(args, 2))
		return "", nil
	}); err != nil {
		return err
	}
	if err := r.registerHost("__go_fetch_cancel", func(args []any) (any, error) {
		if id, ok := intArg(args, 0); ok {
			a.cancel(int64(id))
		}
		return nil, nil
	}); err != nil {
		return err
	}
	r.async = a
	return nil
}

func (a *asyncFetch) start(r *Runtime, id int64, payload string, body []byte) {
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		cancel()
		return
	}
	a.cancels[id] = cancel
	a.mu.Unlock()
	go func() {
		head, respBody, err := r.fetchRound(ctx, payload, body)
		a.mu.Lock()
		delete(a.cancels, id)
		closed := a.closed
		a.mu.Unlock()
		cancel()
		if closed {
			return
		}
		a.post(func() {
			msg := ""
			if err != nil {
				msg = err.Error()
			}
			r.take = respBody
			js := fmt.Sprintf("globalThis[Symbol.for('krate.web')].fetchDone(%d,%s,%s)", id, jsQuote(head), jsQuote(msg))
			_, _ = r.Execute(js)
			r.take = nil
		})
	}()
}

func (a *asyncFetch) cancel(id int64) {
	a.mu.Lock()
	c := a.cancels[id]
	a.mu.Unlock()
	if c != nil {
		c()
	}
}

// cancelAsyncFetches stops the pending requests; their completions are dropped.
func (r *Runtime) cancelAsyncFetches() {
	a := r.async
	if a == nil {
		return
	}
	a.mu.Lock()
	a.closed = true
	cs := make([]context.CancelFunc, 0, len(a.cancels))
	for _, c := range a.cancels {
		cs = append(cs, c)
	}
	a.mu.Unlock()
	for _, c := range cs {
		c()
	}
}

// RegisterBinaryFunc registers a global host function whose arguments or result
// may be bytes. kinds gives the kind of each argument: 'b' bytes (f receives a
// []byte copied out of a typed array or ArrayBuffer), anything else a value
// converted as for RegisterFunc. f returns a []byte (delivered as an
// ArrayBuffer) or a string. Failure is reported by returning an error; JS
// calls the function through the prelude's host() helper, which rethrows it.
// With BinaryTransfer false the bytes cross as base64 text and the prelude's
// toHost and fromHost helpers convert them.
func (r *Runtime) RegisterBinaryFunc(name, kinds string, f func(args []any) (any, error)) error {
	return r.registerBinaryHost(name, kinds, f)
}

// BinaryTransfer reports whether bytes cross the Go and JS boundary as raw
// ArrayBuffers (true) or as base64 text (false) for this runtime.
func (r *Runtime) BinaryTransfer() bool { return r.bin }

// BinaryTransferSupported reports whether this build of the QuickJS bindings
// supports raw byte transfer. It is false when the bindings' internal layout
// changed, in which case every crossing is base64.
func BinaryTransferSupported() bool { return binaryTransfer }

// jsQuote quotes s as a JavaScript string literal.
func jsQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
