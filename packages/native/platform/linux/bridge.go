//go:build linux

package linux

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// responder receives a handler's response. respond takes ownership of readFD
// (the read end of a pipe the handler keeps writing to); fail reports a
// request that never produced headers.
type responder interface {
	respond(status int, header http.Header, readFD int)
	fail(msg string)
}

const watchInterval = 100 * time.Millisecond

// serve runs h for one request on the calling goroutine and streams the
// response into a pipe. cancel is called when the peer closes the read end
// (WebKit cancelled the request) or a write fails.
func serve(h http.Handler, req *http.Request, cancel context.CancelFunc, r responder) {
	w := &pipeWriter{
		header: http.Header{},
		r:      r,
		cancel: cancel,
		head:   req.Method == http.MethodHead,
		done:   make(chan struct{}),
	}
	defer cancel()
	defer w.finish()
	defer func() {
		if p := recover(); p != nil && p != http.ErrAbortHandler {
			logf("krate: handler panic for %s %s: %v", req.Method, req.URL.Path, p)
			w.panicked = true
		}
	}()
	h.ServeHTTP(w, req)
}

// pipeWriter is the http.ResponseWriter handed to handlers. Writes are not
// buffered: each Write goes straight into the pipe, so Flush has nothing to
// do beyond committing the headers.
type pipeWriter struct {
	r      responder
	cancel context.CancelFunc
	head   bool
	done   chan struct{}

	mu        sync.Mutex
	header    http.Header
	committed bool
	closed    bool
	wfile     *os.File
	wfd       int
	panicked  bool
}

var (
	_ http.ResponseWriter = (*pipeWriter)(nil)
	_ http.Flusher        = (*pipeWriter)(nil)
)

func (w *pipeWriter) Header() http.Header { return w.header }

func (w *pipeWriter) WriteHeader(code int) {
	if code >= 100 && code < 200 && code != 101 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.commitLocked(code)
}

func (w *pipeWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	if !w.committed {
		if w.header.Get("Content-Type") == "" && w.header.Get("Content-Encoding") == "" && len(p) > 0 {
			w.header.Set("Content-Type", http.DetectContentType(p))
		}
		w.commitLocked(http.StatusOK)
	}
	f, closed := w.wfile, w.closed
	w.mu.Unlock()
	if closed || f == nil {
		return 0, http.ErrHandlerTimeout
	}
	if w.head {
		return len(p), nil
	}
	n, err := f.Write(p)
	if err != nil {
		w.cancel()
	}
	return n, err
}

func (w *pipeWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.commitLocked(http.StatusOK)
}

// commitLocked creates the pipe and hands the read end to the responder.
func (w *pipeWriter) commitLocked(code int) {
	if w.committed {
		return
	}
	w.committed = true
	var fds [2]int
	if err := syscall.Pipe2(fds[:], syscall.O_CLOEXEC); err != nil {
		w.closed = true
		w.r.fail("krate: pipe: " + err.Error())
		return
	}
	// A larger pipe lets a fast handler run ahead of the webview.
	const fSetPipeSz = 1031
	_, _, _ = syscall.Syscall(syscall.SYS_FCNTL, uintptr(fds[1]), fSetPipeSz, 1<<20)
	w.wfd = fds[1]
	w.wfile = os.NewFile(uintptr(fds[1]), "krate-response")
	w.r.respond(code, w.header.Clone(), fds[0])
	go w.watch()
}

// watch cancels the request when the read end is closed by the peer. The
// kernel reports that as POLLERR on the write end.
func (w *pipeWriter) watch() {
	t := time.NewTicker(watchInterval)
	defer t.Stop()
	for {
		select {
		case <-w.done:
			return
		case <-t.C:
		}
		w.mu.Lock()
		if w.closed {
			w.mu.Unlock()
			return
		}
		gone := pollHup(w.wfd)
		w.mu.Unlock()
		if gone {
			w.cancel()
			return
		}
	}
}

func pollHup(fd int) bool {
	pfd := []unix.PollFd{{Fd: int32(fd)}}
	if _, err := unix.Poll(pfd, 0); err != nil {
		return false
	}
	return pfd[0].Revents&(unix.POLLERR|unix.POLLHUP) != 0
}

// finish runs when the handler returns: it sends headers if the handler
// never did, then closes the write end so the reader sees EOF.
func (w *pipeWriter) finish() {
	w.mu.Lock()
	if !w.committed {
		code := http.StatusOK
		if w.panicked {
			code = http.StatusInternalServerError
			w.header = http.Header{"Content-Type": {"text/plain; charset=utf-8"}}
		}
		w.commitLocked(code)
	}
	w.closed = true
	f := w.wfile
	w.mu.Unlock()
	close(w.done)
	if f != nil {
		_ = f.Close()
	}
}

// splitContentType returns the media type without parameters.
func splitContentType(v string) string {
	if i := strings.IndexByte(v, ';'); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// skipResponseHeader reports headers the transport sets itself.
func skipResponseHeader(name string) bool {
	switch strings.ToLower(name) {
	case "content-length", "transfer-encoding", "connection", "keep-alive", "content-type":
		return true
	}
	return false
}

var errBadRequest = errors.New("bad request")

func badURI(uri string) error { return fmt.Errorf("%w: %q", errBadRequest, uri) }
