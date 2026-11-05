//go:build !windows

package build

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func writeRegionShell(t *testing.T, absOut string) {
	t.Helper()
	p := filepath.Join(absOut, "p", "index.html")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`<main><!--region:region-1-->baked<!--/region:region-1--></main>`), 0o644); err != nil {
		t.Fatal(err)
	}
}

// An unreachable renderer is an outage, not an empty page: the result is
// flagged and nothing is written, so the caller can answer 503.
func TestStreamRegionPageRendererUnreachable(t *testing.T) {
	absOut := t.TempDir()
	writeRegionShell(t, absOut)
	port := freePort(t) // nothing listens here
	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	rr := streamRegionPage(rec, rec, newRegionShellCache(), absOut, "/p", port, nil, nil, nil, nil, nil, true, 30, "")
	if !rr.served || rr.unavailable == "" {
		t.Fatalf("served=%v unavailable=%q, want served with an unavailable error", rr.served, rr.unavailable)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("baked shell must not be written when the renderer is down, got %q", rec.Body.String())
	}
}

// A stream cut before every region arrived must not look like a good render:
// the ISR response becomes private, no-store.
func TestStreamRegionPageTruncatedStreamIsNotCacheable(t *testing.T) {
	absOut := t.TempDir()
	writeRegionShell(t, absOut)
	sidecar := newFakeRegionsSidecar(t, []string{}) // closes without a frame or an end
	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	rr := streamRegionPage(rec, rec, newRegionShellCache(), absOut, "/p", sidecar.port, nil, nil, nil, nil, nil, true, 30, "")
	if rr.renderErr == "" {
		t.Fatalf("truncated stream must set renderErr")
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Errorf("Cache-Control = %q, want private, no-store", cc)
	}
}

// The renderer is restarted after it dies and stays down only briefly.
func TestSSRSuperviseRestartsRenderer(t *testing.T) {
	_, err := exec.LookPath("node")
	if err != nil {
		requireE2E(t, "node not available: %v", err)
		return
	}
	root := t.TempDir()
	out := filepath.Join(root, "dist")
	if err := os.MkdirAll(filepath.Join(out, ".krate"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "server-manifest.json"), []byte(`{"pages":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := `import http from "node:http";
http.createServer((req, res) => { res.end("ok " + process.pid); }).listen(parseInt(process.env.KRATE_SSR_PORT, 10), "127.0.0.1");
`
	if err := os.WriteFile(filepath.Join(out, ".krate", "server-renderer.mjs"), []byte(fake), 0o644); err != nil {
		t.Fatal(err)
	}

	port := freePort(t)
	s := NewSSRServer(root, port, "node")
	s.SetOutDir(out)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	supervisorDone := make(chan struct{})
	go func() { s.Supervise(stop); close(supervisorDone) }()
	t.Cleanup(func() {
		close(stop)
		s.Shutdown()
		select {
		case <-supervisorDone:
		case <-time.After(5 * time.Second):
			t.Error("supervisor did not return after Shutdown")
		}
	})

	s.mu.Lock()
	first := s.cmd.Process.Pid
	s.mu.Unlock()
	if err := syscall.Kill(first, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		second := s.cmd.Process.Pid
		s.mu.Unlock()
		if second != first && s.IsRunning() {
			resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
			if err == nil {
				resp.Body.Close()
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("renderer was not restarted after being killed (running=%v)", s.IsRunning())
}
