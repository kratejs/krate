package build

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// timedRecorder records every Write and Flush with its arrival time.
type timedRecorder struct {
	*httptest.ResponseRecorder
	mu     sync.Mutex
	start  time.Time
	writes int
	flush  []flushEvent
	sent   bytes.Buffer // bytes written since the last flush
}

type flushEvent struct {
	at   time.Duration
	data string
}

func newTimedRecorder() *timedRecorder {
	return &timedRecorder{ResponseRecorder: httptest.NewRecorder(), start: time.Now()}
}

func (r *timedRecorder) Write(b []byte) (int, error) {
	r.mu.Lock()
	r.writes++
	r.sent.Write(b)
	r.mu.Unlock()
	return r.ResponseRecorder.Write(b)
}

func (r *timedRecorder) Flush() {
	r.mu.Lock()
	r.flush = append(r.flush, flushEvent{at: time.Since(r.start), data: r.sent.String()})
	r.sent.Reset()
	r.mu.Unlock()
}

// delayedFrame is one NDJSON frame the sidecar sends after delay.
type delayedFrame struct {
	delay time.Duration
	line  string
}

// startDelayedSidecar serves /__krate/regions, flushing each frame after its
// delay (delays are relative to the previous frame).
func startDelayedSidecar(t *testing.T, frames []delayedFrame) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/x-ndjson")
		fl := w.(http.Flusher)
		for _, f := range frames {
			time.Sleep(f.delay)
			_, _ = io.WriteString(w, f.line+"\n")
			fl.Flush()
		}
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().(*net.TCPAddr).Port
}

// widenCoalesceWindow lets the loopback sidecar answer inside the window even
// under the race detector.
func widenCoalesceWindow(t *testing.T) {
	t.Helper()
	old := regionCoalesceWindow
	regionCoalesceWindow = 100 * time.Millisecond
	t.Cleanup(func() { regionCoalesceWindow = old })
}

const coalesceShell = `<!DOCTYPE html><html><body><div id=root>` +
	`<!--suspense:1-1--><i>a</i><!--/suspense:1-1-->` +
	`<!--suspense:1-2--><i>b</i><!--/suspense:1-2-->` +
	`</div><script>boot()</script></body></html>`

func regionLine(id, html string) string {
	return fmt.Sprintf(`{"type":"region","id":%q,"status":200,"html":%q}`, id, html)
}

func TestStreamSiblingFastRegionsShareOneWrite(t *testing.T) {
	widenCoalesceWindow(t)
	absOut := t.TempDir()
	writeRoute(t, absOut, "/live", coalesceShell)
	port := startDelayedSidecar(t, []delayedFrame{
		{0, regionLine("1-1", "<p>one</p>")},
		{0, regionLine("1-2", "<p>two</p>")},
		{0, `{"type":"end","count":2}`},
	})
	rec := newTimedRecorder()
	rr := streamRegionPageMode(rec, rec, newRegionShellCache(), absOut, "/live", port, nil, nil, nil, nil, nil, false, 0, "", true)
	if !rr.served || rr.renderErr != "" {
		t.Fatalf("served=%v renderErr=%q", rr.served, rr.renderErr)
	}

	// Same bytes, same order as the old one-flush-per-region stream.
	prefix, tail := splitBodyScripts(coalesceShell)
	want := prefix + regionReplaceScript("1-1", "<p>one</p>", "", "") +
		regionReplaceScript("1-2", "<p>two</p>", "", "") + tail
	if got := rec.Body.String(); got != want {
		t.Fatalf("body differs from the old stream:\n got: %s\nwant: %s", got, want)
	}
	if rec.writes != 1 || len(rec.flush) != 1 {
		t.Errorf("fast regions should go out in one write and one flush, got %d writes, %d flushes", rec.writes, len(rec.flush))
	}
}

func TestStreamSiblingSlowRegionDoesNotHoldBackShell(t *testing.T) {
	widenCoalesceWindow(t)
	absOut := t.TempDir()
	writeRoute(t, absOut, "/live", coalesceShell)
	const slow = 500 * time.Millisecond
	port := startDelayedSidecar(t, []delayedFrame{
		{0, regionLine("1-1", "<p>one</p>")},
		{slow, regionLine("1-2", "<p>two</p>")},
		{0, `{"type":"end","count":2}`},
	})
	rec := newTimedRecorder()
	rr := streamRegionPageMode(rec, rec, newRegionShellCache(), absOut, "/live", port, nil, nil, nil, nil, nil, false, 0, "", true)
	if !rr.served {
		t.Fatal("not served")
	}
	if len(rec.flush) < 2 {
		t.Fatalf("want the shell and the slow region in separate flushes, got %d", len(rec.flush))
	}
	first := rec.flush[0]
	if first.at > slow-100*time.Millisecond {
		t.Errorf("shell arrived at %v, too close to the slow region (%v)", first.at, slow)
	}
	if !strings.Contains(first.data, `var id="1-1"`) || strings.Contains(first.data, `var id="1-2"`) {
		t.Errorf("first flush should carry the shell and the fast region only:\n%s", first.data)
	}
	late := false
	for _, f := range rec.flush[1:] {
		if strings.Contains(f.data, `var id="1-2"`) {
			late = f.at >= slow
		}
	}
	if !late {
		t.Errorf("the slow region should flush on its own once it resolves: %+v", rec.flush)
	}
	prefix, tail := splitBodyScripts(coalesceShell)
	want := prefix + regionReplaceScript("1-1", "<p>one</p>", "", "") +
		regionReplaceScript("1-2", "<p>two</p>", "", "") + tail
	if got := rec.Body.String(); got != want {
		t.Errorf("body differs from the old stream:\n got: %s\nwant: %s", got, want)
	}
}

func TestStreamSiblingFailedRegionKeepsFallback(t *testing.T) {
	widenCoalesceWindow(t)
	absOut := t.TempDir()
	writeRoute(t, absOut, "/live", coalesceShell)
	port := startDelayedSidecar(t, []delayedFrame{
		{0, `{"type":"region","id":"1-1","status":500,"error":"boom"}`},
		{0, regionLine("1-2", "<p>two</p>")},
		{0, `{"type":"end","count":2}`},
	})
	rec := newTimedRecorder()
	rr := streamRegionPageMode(rec, rec, newRegionShellCache(), absOut, "/live", port, nil, nil, nil, nil, nil, false, 0, "", true)
	if rr.renderErr != "boom" {
		t.Errorf("renderErr = %q, want boom", rr.renderErr)
	}
	prefix, tail := splitBodyScripts(coalesceShell)
	want := prefix + regionReplaceScript("1-2", "<p>two</p>", "", "") + tail
	if got := rec.Body.String(); got != want {
		t.Errorf("body differs:\n got: %s\nwant: %s", got, want)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestStreamSiblingUnreachableRendererStillCompletes(t *testing.T) {
	absOut := t.TempDir()
	writeRoute(t, absOut, "/live", coalesceShell)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	rec := newTimedRecorder()
	rr := streamRegionPageMode(rec, rec, newRegionShellCache(), absOut, "/live", port, nil, nil, nil, nil, nil, false, 0, "", true)
	if rr.renderErr == "" {
		t.Error("expected a render error")
	}
	if got := rec.Body.String(); got != coalesceShell {
		t.Errorf("baked page changed:\n%s", got)
	}
}

func TestStreamOrderedRegionsFlushOnce(t *testing.T) {
	absOut := t.TempDir()
	writeRoute(t, absOut, "/live", coalesceShell)
	port := startDelayedSidecar(t, []delayedFrame{
		{0, regionLine("1-1", "<p>one</p>")},
		{20 * time.Millisecond, regionLine("1-2", "<p>two</p>")},
		{0, `{"type":"end","count":2}`},
	})
	rec := newTimedRecorder()
	rr := streamRegionPageMode(rec, rec, newRegionShellCache(), absOut, "/live", port, nil, nil, nil, nil, nil, false, 0, "", false)
	if !rr.served {
		t.Fatal("not served")
	}
	want := `<!DOCTYPE html><html><body><div id=root>` +
		`<!--suspense:1-1--><p>one</p><!--/suspense:1-1-->` +
		`<!--suspense:1-2--><p>two</p><!--/suspense:1-2-->` +
		`</div><script>boot()</script></body></html>`
	if got := rec.Body.String(); got != want {
		t.Errorf("ordered body differs:\n got: %s\nwant: %s", got, want)
	}
	if len(rec.flush) != 1 {
		t.Errorf("ordered page should flush once after assembly, got %d", len(rec.flush))
	}
}

func TestGzipFlushWithoutNewBytesIsFree(t *testing.T) {
	var plain, twice bytes.Buffer
	run := func(dst *bytes.Buffer, extraFlushes int) {
		h := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<p>hello</p>")
			for i := 0; i <= extraFlushes; i++ {
				w.(http.Flusher).Flush()
			}
		}))
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		h.ServeHTTP(rec, req)
		dst.Write(rec.Body.Bytes())
		zr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(zr)
		if string(b) != "<p>hello</p>" {
			t.Errorf("body = %q", b)
		}
	}
	run(&plain, 0)
	run(&twice, 3)
	if plain.Len() != twice.Len() {
		t.Errorf("empty flushes added %d bytes", twice.Len()-plain.Len())
	}
}
