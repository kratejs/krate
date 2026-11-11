//go:build linux

package linux

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"
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

func TestServeDefaultsAndSniff(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.WriteString(w, "<html><body>hi</body></html>"); err != nil {
			t.Error(err)
		}
	})
	f, _, _ := runServe(h, "GET")
	<-f.ready
	if f.status != 200 || f.header.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("status %d ctype %q", f.status, f.header.Get("Content-Type"))
	}
	rf := os.NewFile(uintptr(f.fd), "r")
	defer rf.Close()
	b, _ := io.ReadAll(rf)
	if string(b) != "<html><body>hi</body></html>" {
		t.Fatalf("body %q", b)
	}
}

func TestServeNoWriteIsEmpty200(t *testing.T) {
	f, _, _ := runServe(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), "GET")
	<-f.ready
	rf := os.NewFile(uintptr(f.fd), "r")
	defer rf.Close()
	b, _ := io.ReadAll(rf)
	if f.status != 200 || len(b) != 0 {
		t.Fatalf("status %d body %q", f.status, b)
	}
}

func TestServePanicBeforeHeadersIs500(t *testing.T) {
	old := Logf
	Logf = func(string, ...any) {}
	defer func() { Logf = old }()
	f, _, done := runServe(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }), "GET")
	<-f.ready
	rf := os.NewFile(uintptr(f.fd), "r")
	defer rf.Close()
	if _, err := io.ReadAll(rf); err != nil {
		t.Fatal(err)
	}
	<-done
	if f.status != 500 {
		t.Fatalf("status %d", f.status)
	}
}

func TestServeHeadDropsBody(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.WriteString(w, "body"); err != nil {
			t.Error(err)
		}
	})
	f, _, _ := runServe(h, "HEAD")
	<-f.ready
	rf := os.NewFile(uintptr(f.fd), "r")
	defer rf.Close()
	b, _ := io.ReadAll(rf)
	if len(b) != 0 {
		t.Fatalf("HEAD body %q", b)
	}
}

func TestServeCancelsWhenReaderCloses(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.WriteString(w, "x"); err != nil {
			t.Error(err)
		}
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	f, ctx, done := runServe(h, "GET")
	<-f.ready
	syscall.Close(f.fd) // the webview went away
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("context not cancelled after reader closed")
	}
	<-done
}

func TestServeWriteAfterReaderClosedFails(t *testing.T) {
	errc := make(chan error, 1)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.WriteString(w, "x"); err != nil {
			t.Error(err)
		}
		w.(http.Flusher).Flush()
		time.Sleep(300 * time.Millisecond)
		_, err := io.WriteString(w, "y")
		errc <- err
	})
	f, _, _ := runServe(h, "GET")
	<-f.ready
	syscall.Close(f.fd)
	if err := <-errc; err == nil {
		t.Fatal("write to a closed reader succeeded")
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

func TestVersionOK(t *testing.T) {
	for _, c := range []struct {
		maj, min uint32
		ok       bool
	}{{2, 38, false}, {2, 40, true}, {2, 54, true}, {3, 0, true}, {1, 99, false}} {
		if versionOK(c.maj, c.min) != c.ok {
			t.Errorf("versionOK(%d,%d) != %v", c.maj, c.min, c.ok)
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
