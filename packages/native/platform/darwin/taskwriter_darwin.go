package darwin

import (
	"errors"
	"fmt"
	"log"
	"net/http"
)

var errAborted = errors.New("darwin: handler aborted")

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
	defer w.stop()
	defer func() {
		if r := recover(); r != nil {
			w.panicked(r)
		}
	}()
	h.ServeHTTP(w, req)
	w.finish()
}
