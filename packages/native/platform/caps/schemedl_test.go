package caps

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type dlRig struct {
	sd     *SchemeDownloader
	mu     sync.Mutex
	events []DownloadEvent
	ch     chan DownloadEvent
	reqs   chan DownloadRequest
}

func newDLRig(h http.Handler, decide func(DownloadRequest) string) *dlRig {
	r := &dlRig{ch: make(chan DownloadEvent, 256), reqs: make(chan DownloadRequest, 8)}
	var id atomic.Uint64
	r.sd = &SchemeDownloader{
		Handler: h,
		NextID:  func() uint64 { return id.Add(1) },
		Emit: func(e DownloadEvent) {
			r.mu.Lock()
			r.events = append(r.events, e)
			r.mu.Unlock()
			r.ch <- e
		},
		Decide: func(req DownloadRequest, d func(string)) {
			r.reqs <- req
			go d(decide(req))
		},
	}
	return r
}

func (r *dlRig) last(t *testing.T) DownloadEvent {
	t.Helper()
	for {
		select {
		case e := <-r.ch:
			if e.State == DownloadCompleted || e.State == DownloadFailed || e.State == DownloadCancelled {
				return e
			}
		case <-time.After(10 * time.Second):
			t.Fatal("download did not finish")
		}
	}
}

func TestSchemeDownloadCompletes(t *testing.T) {
	var gotView ViewRef
	mux := http.NewServeMux()
	mux.HandleFunc("/file", func(w http.ResponseWriter, r *http.Request) {
		gotView, _ = ViewOf(r)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "5")
		w.Write([]byte("hello"))
	})
	dir := t.TempDir()
	r := newDLRig(mux, func(q DownloadRequest) string { return filepath.Join(dir, q.SuggestedName) })
	r.sd.Start(7, "krate://app/file?x=1", "named.txt")
	e := r.last(t)
	if e.State != DownloadCompleted || e.Path != filepath.Join(dir, "named.txt") || e.Received != 5 {
		t.Fatalf("event %+v", e)
	}
	if b, _ := os.ReadFile(e.Path); string(b) != "hello" {
		t.Fatalf("content %q", b)
	}
	if gotView != 7 {
		t.Fatalf("handler saw view %d", gotView)
	}
	req := <-r.reqs
	if req.SuggestedName != "named.txt" || req.Total != 5 || req.MIME != "application/octet-stream" {
		t.Fatalf("request %+v", req)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("leftover files: %v", entries)
	}
	if r.events[0].State != DownloadStarted {
		t.Fatalf("first event %+v", r.events[0])
	}
}

func TestSchemeDownloadNames(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/cd", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="../evil.txt"`)
		w.Write([]byte("x"))
	})
	mux.HandleFunc("/dir/plain.bin", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("y")) })
	dir := t.TempDir()
	r := newDLRig(mux, func(q DownloadRequest) string { return filepath.Join(dir, q.SuggestedName) })
	r.sd.Start(1, "krate://app/cd", "")
	if e := r.last(t); e.State != DownloadCompleted || filepath.Dir(e.Path) != dir {
		t.Fatalf("content-disposition: %+v", e)
	}
	r.sd.Start(1, "krate://app/dir/plain.bin", "")
	if e := r.last(t); e.Path != filepath.Join(dir, "plain.bin") {
		t.Fatalf("path name: %+v", e)
	}
}

func TestSchemeDownloadFailures(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/missing", http.NotFound)
	dir := t.TempDir()
	r := newDLRig(mux, func(q DownloadRequest) string { return filepath.Join(dir, "x") })
	r.sd.Start(1, "krate://app/missing", "")
	if e := r.last(t); e.State != DownloadFailed || !strings.Contains(e.Error, "404") {
		t.Fatalf("404: %+v", e)
	}
	// Declined destination.
	mux.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("z")) })
	r2 := newDLRig(mux, func(DownloadRequest) string { return "" })
	r2.sd.Start(1, "krate://app/ok", "")
	if e := r2.last(t); e.State != DownloadCancelled {
		t.Fatalf("declined: %+v", e)
	}
	// Relative destination is refused.
	r3 := newDLRig(mux, func(DownloadRequest) string { return "rel/path" })
	r3.sd.Start(1, "krate://app/ok", "")
	if e := r3.last(t); e.State != DownloadFailed {
		t.Fatalf("relative: %+v", e)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("files left: %v", entries)
	}
}

func TestSchemeDownloadCancel(t *testing.T) {
	started := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 1024))
		close(started)
		for i := 0; i < 100; i++ {
			if _, err := w.Write(make([]byte, 1024)); err != nil {
				return
			}
			select {
			case <-r.Context().Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	})
	dir := t.TempDir()
	r := newDLRig(mux, func(q DownloadRequest) string { return filepath.Join(dir, "slow.bin") })
	r.sd.Start(1, "krate://app/slow", "")
	<-started
	time.Sleep(50 * time.Millisecond)
	if !r.sd.Cancel(1) {
		t.Fatal("Cancel did not find the download")
	}
	if e := r.last(t); e.State != DownloadCancelled {
		t.Fatalf("cancel: %+v", e)
	}
	if r.sd.Cancel(1) {
		t.Fatal("a finished download was cancelled")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("partial file left: %v", entries)
	}
}
