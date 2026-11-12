package caps

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"
)

// SchemeDownloader performs downloads of URLs on the app origin. The web
// engines cannot download a custom scheme (the scheme handler lives in the UI
// process, the download engine in a network process), so a link with the
// download attribute or a file response on the app origin is fetched here by
// calling the app's handler directly, with the view identity attached like any
// request from that view.
//
// A platform creates one, feeds it the downloads the engine refuses, and
// forwards the events. Downloads of other schemes (blob:, https:) stay with
// the engine.
type SchemeDownloader struct {
	Handler http.Handler
	// NextID returns a process-unique download id.
	NextID func() uint64
	// Decide chooses the destination, like Downloads.OnDownloadStart. A nil
	// Decide cancels every download.
	Decide func(r DownloadRequest, decide func(path string))
	// Emit receives the events.
	Emit func(DownloadEvent)

	mu      sync.Mutex
	running map[uint64]context.CancelFunc
}

// ErrDownloadCancelled is returned by Cancel paths and used internally.
var errCancelled = errors.New("krate: download cancelled")

// Start downloads rawURL for a view in the background. nameHint is the file
// name the page proposed (the download attribute); it can be empty, then the
// Content-Disposition header or the last path segment names the file.
func (s *SchemeDownloader) Start(view ViewRef, rawURL, nameHint string) {
	id := s.NextID()
	ctx, cancel := context.WithCancel(WithView(context.Background(), view))
	s.mu.Lock()
	if s.running == nil {
		s.running = map[uint64]context.CancelFunc{}
	}
	s.running[id] = cancel
	s.mu.Unlock()
	go func() {
		defer func() {
			cancel()
			s.mu.Lock()
			delete(s.running, id)
			s.mu.Unlock()
		}()
		s.run(ctx, id, view, rawURL, nameHint)
	}()
}

// Cancel stops a running download and reports whether id was running.
func (s *SchemeDownloader) Cancel(id uint64) bool {
	s.mu.Lock()
	cancel := s.running[id]
	s.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (s *SchemeDownloader) run(ctx context.Context, id uint64, view ViewRef, rawURL, hint string) {
	ev := DownloadEvent{ID: id, View: view, URL: rawURL, Total: -1}
	fail := func(err error) {
		switch {
		case errors.Is(err, errCancelled), ctx.Err() != nil:
			ev.State, ev.Error = DownloadCancelled, ""
		default:
			ev.State, ev.Error = DownloadFailed, err.Error()
		}
		s.Emit(ev)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		fail(err)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		fail(err)
		return
	}
	req.Host = u.Host
	req.RequestURI = u.RequestURI()
	req.RemoteAddr = "krate-app"

	w := &downloadWriter{s: s, ctx: ctx, id: id, view: view, url: rawURL, hint: hint, header: http.Header{}, ev: &ev}
	func() {
		defer func() {
			if p := recover(); p != nil && p != http.ErrAbortHandler {
				w.err = fmt.Errorf("krate: the handler panicked: %v", p)
			}
		}()
		s.Handler.ServeHTTP(w, req)
	}()
	if w.err == nil && !w.wrote {
		w.WriteHeader(http.StatusOK) // a handler that wrote nothing
	}
	if w.err == nil {
		w.err = w.finish()
	}
	if w.err != nil {
		w.discard()
		fail(w.err)
		return
	}
	ev.State = DownloadCompleted
	ev.Path = w.dest
	ev.Received = w.n
	s.Emit(ev)
}

// downloadWriter is the ResponseWriter a download handler writes to. The
// first WriteHeader asks for the destination, which can block on a dialog.
type downloadWriter struct {
	s      *SchemeDownloader
	ctx    context.Context
	id     uint64
	view   ViewRef
	url    string
	hint   string
	header http.Header
	ev     *DownloadEvent

	wrote bool
	err   error
	f     *os.File
	tmp   string
	dest  string
	n     int64
	total int64
	last  time.Time
}

func (w *downloadWriter) Header() http.Header { return w.header }

func (w *downloadWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.wrote = true
	if status < 200 || status > 299 {
		w.err = fmt.Errorf("the server answered %d %s", status, http.StatusText(status))
		return
	}
	w.total = -1
	if n := w.header.Get("Content-Length"); n != "" {
		var v int64
		if _, err := fmt.Sscan(n, &v); err == nil && v >= 0 {
			w.total = v
		}
	}
	name := w.hint
	if name == "" {
		if _, params, err := mime.ParseMediaType(w.header.Get("Content-Disposition")); err == nil {
			name = params["filename"]
		}
	}
	if name == "" {
		if u, err := url.Parse(w.url); err == nil {
			name = path.Base(u.Path)
		}
	}
	ct, _, _ := mime.ParseMediaType(w.header.Get("Content-Type"))
	req := DownloadRequest{ID: w.id, View: w.view, URL: w.url, SuggestedName: SafeFileName(name), MIME: ct, Total: w.total}
	dest := make(chan string, 1)
	if w.s.Decide == nil {
		dest <- ""
	} else {
		once := sync.Once{}
		w.s.Decide(req, func(p string) { once.Do(func() { dest <- p }) })
	}
	var p string
	select {
	case p = <-dest:
	case <-w.ctx.Done():
		w.err = errCancelled
		return
	}
	if p == "" {
		w.err = errCancelled
		return
	}
	if !filepath.IsAbs(p) {
		w.err = fmt.Errorf("the destination %q is not an absolute path", p)
		return
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".krate-download-*")
	if err != nil {
		w.err = err
		return
	}
	w.f, w.tmp, w.dest = f, f.Name(), p
	w.ev.Path, w.ev.Total = p, w.total
	w.ev.State = DownloadStarted
	w.s.Emit(*w.ev)
}

func (w *downloadWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	if w.err != nil {
		return 0, w.err
	}
	if err := w.ctx.Err(); err != nil {
		w.err = errCancelled
		return 0, w.err
	}
	n, err := w.f.Write(b)
	w.n += int64(n)
	if err != nil {
		w.err = err
		return n, err
	}
	if now := time.Now(); now.Sub(w.last) >= 100*time.Millisecond {
		w.last = now
		w.ev.State, w.ev.Received = DownloadProgress, w.n
		w.s.Emit(*w.ev)
	}
	return n, nil
}

// Flush lets streaming handlers run.
func (w *downloadWriter) Flush() {}

func (w *downloadWriter) finish() error {
	if err := w.ctx.Err(); err != nil {
		return errCancelled
	}
	if w.f == nil {
		return errors.New("krate: the download has no destination")
	}
	if err := w.f.Close(); err != nil {
		return err
	}
	w.f = nil
	if err := os.Rename(w.tmp, w.dest); err != nil {
		return err
	}
	w.tmp = ""
	return nil
}

func (w *downloadWriter) discard() {
	if w.f != nil {
		w.f.Close()
		w.f = nil
	}
	if w.tmp != "" {
		os.Remove(w.tmp)
		w.tmp = ""
	}
}

var _ io.Writer = (*downloadWriter)(nil)
