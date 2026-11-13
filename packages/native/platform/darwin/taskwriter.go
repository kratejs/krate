package darwin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
)

// taskSink receives the pieces of one response. Implementations must not
// block: the darwin sink only queues work for the main thread. Calls arrive
// serialized and in order.
type taskSink interface {
	receiveResponse(status int, header http.Header)
	// receiveData takes ownership of p.
	receiveData(p []byte)
	finish()
	fail(err error)
}

var (
	errWriteAfterFinish = errors.New("darwin: write after response finished")
	errAborted          = errors.New("darwin: handler aborted")
)

// taskWriter is the http.ResponseWriter for one scheme task. The header goes
// out lazily on the first Write, Flush or at finish (as in net/http), so a
// Content-Type can still be sniffed from the first chunk.
type taskWriter struct {
	sink   taskSink
	cancel context.CancelFunc

	hdr http.Header // handler-visible header, not locked (same as net/http)

	mu      sync.Mutex
	status  int
	sent    bool
	done    bool
	stopped bool
}

func newTaskWriter(sink taskSink, cancel context.CancelFunc) *taskWriter {
	return &taskWriter{sink: sink, cancel: cancel, hdr: make(http.Header)}
}

func (w *taskWriter) Header() http.Header { return w.hdr }

func (w *taskWriter) WriteHeader(code int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sent || w.done || w.stopped || w.status != 0 {
		return
	}
	if code >= 100 && code < 200 {
		return
	}
	if code < 100 || code > 999 {
		code = http.StatusInternalServerError
	}
	w.status = code
}

// sendLocked emits the response head once. first is the first body chunk, used
// only for content type sniffing.
func (w *taskWriter) sendLocked(first []byte) {
	if w.sent {
		return
	}
	w.sent = true
	if w.status == 0 {
		w.status = http.StatusOK
	}
	h := w.hdr.Clone()
	if h == nil {
		h = make(http.Header)
	}
	if h.Get("Content-Type") == "" && len(first) > 0 {
		h.Set("Content-Type", http.DetectContentType(first))
	}
	w.sink.receiveResponse(w.status, h)
}

func (w *taskWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return 0, context.Canceled
	}
	if w.done {
		return 0, errWriteAfterFinish
	}
	w.sendLocked(p)
	if len(p) == 0 {
		return 0, nil
	}
	w.sink.receiveData(bytes.Clone(p))
	return len(p), nil
}

func (w *taskWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped || w.done {
		return
	}
	w.sendLocked(nil)
}

// finish completes the response. It is idempotent and a no-op after stop.
func (w *taskWriter) finish() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.done {
		return
	}
	w.done = true
	if w.stopped {
		return
	}
	w.sendLocked(nil)
	w.sink.finish()
}

// failWith terminates the task with an error.
func (w *taskWriter) failWith(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.done {
		return
	}
	w.done = true
	if w.stopped {
		return
	}
	w.sink.fail(err)
}

// stop records that the webview cancelled the task. No sink call happens
// afterwards and the request context is cancelled.
func (w *taskWriter) stop() {
	w.mu.Lock()
	w.stopped = true
	w.mu.Unlock()
	if w.cancel != nil {
		w.cancel()
	}
}

// panicked handles a recovered handler panic.
func (w *taskWriter) panicked(v any) {
	if v == http.ErrAbortHandler {
		w.failWith(errAborted)
		return
	}
	log.Printf("krate/native: handler panic: %v", v)
	w.mu.Lock()
	if !w.sent && !w.done && !w.stopped {
		w.hdr = http.Header{"Content-Type": {"text/plain; charset=utf-8"}}
		w.status = http.StatusInternalServerError
		w.sendLocked(nil)
		w.sink.receiveData([]byte("internal server error\n"))
		w.mu.Unlock()
		w.finish()
		return
	}
	w.mu.Unlock()
	w.failWith(fmt.Errorf("darwin: handler panic: %v", v))
}

// serve runs h for req and drives w to completion. It always finishes w.
func serve(h http.Handler, req *http.Request, w *taskWriter) {
	defer w.stop() // releases the request context
	defer func() {
		if r := recover(); r != nil {
			w.panicked(r)
		}
	}()
	h.ServeHTTP(w, req)
	w.finish()
}
