package caps

import (
	"net/http"
	"os"
	"path/filepath"
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
		if _, err := w.Write([]byte("hello")); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
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
