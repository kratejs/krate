//go:build linux

package linux

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
)

type fakeResp struct {
	mu     sync.Mutex
	status int
	header http.Header
	fd     int
	calls  int
	failed string
	ready  chan struct{}
}

func newFakeResp() *fakeResp { return &fakeResp{ready: make(chan struct{}, 4)} }

func (f *fakeResp) respond(status int, header http.Header, fd int) {
	f.mu.Lock()
	f.status, f.header, f.fd = status, header, fd
	f.calls++
	f.mu.Unlock()
	f.ready <- struct{}{}
}

func (f *fakeResp) fail(msg string) {
	f.mu.Lock()
	f.failed = msg
	f.mu.Unlock()
	f.ready <- struct{}{}
}

func runServe(h http.Handler, method string) (*fakeResp, context.Context, chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(method, "krate://app/x", nil).WithContext(ctx)
	f := newFakeResp()
	done := make(chan struct{})
	go func() { serve(h, req, cancel, f); close(done) }()
	return f, ctx, done
}

func TestServeStreamsChunks(t *testing.T) {
	gate := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-A", "1")
		w.WriteHeader(201)
		w.(http.Flusher).Flush()
		if _, err := io.WriteString(w, "one"); err != nil {
			t.Error(err)
		}
		<-gate
		if _, err := io.WriteString(w, "two"); err != nil {
			t.Error(err)
		}
	})
	f, _, done := runServe(h, "GET")
	<-f.ready
	if f.status != 201 || f.header.Get("X-A") != "1" {
		t.Fatalf("status %d header %v", f.status, f.header)
	}
	rf := os.NewFile(uintptr(f.fd), "r")
	defer rf.Close()
	buf := make([]byte, 16)
	n, err := rf.Read(buf)
	if err != nil || string(buf[:n]) != "one" {
		t.Fatalf("first read %q %v (handler still blocked)", buf[:n], err)
	}
	close(gate)
	rest, _ := io.ReadAll(rf)
	if string(rest) != "two" {
		t.Fatalf("rest = %q", rest)
	}
	<-done
	if f.calls != 1 {
		t.Fatalf("respond called %d times", f.calls)
	}
}

func TestAllowedNavigation(t *testing.T) {
	for uri, want := range map[string]bool{
		"krate://app/":          true,
		"krate://app/a?b=1#c":   true,
		"about:blank":           true,
		"krate://evil/":         false,
		"https://example.com/":  false,
		"http://app/":           false,
		"file:///etc/passwd":    false,
		"krate://app@evil.com/": false,
		"":                      false,
	} {
		if got := allowedNavigation(uri); got != want {
			t.Errorf("allowedNavigation(%q) = %v, want %v", uri, got, want)
		}
	}
}

func TestCleanPath(t *testing.T) {
	for in, want := range map[string]string{"": "/", "a": "/a", "/a?b": "/a?b"} {
		if got := cleanPath(in); got != want {
			t.Errorf("cleanPath(%q) = %q", in, got)
		}
	}
}
