package build

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSidecarReadyWaitsThenReleases(t *testing.T) {
	s := newSidecarReady(5 * time.Second)
	gen := s.begin()
	done := make(chan bool, 1)
	go func() { done <- s.wait(context.Background()) }()
	select {
	case <-done:
		t.Fatal("wait returned before the sidecar was up")
	case <-time.After(100 * time.Millisecond):
	}
	s.markUp(gen)
	if !<-done {
		t.Fatal("wait failed after markUp")
	}
	if !s.up.Load() || !s.wait(context.Background()) {
		t.Fatal("fast path not taken once up")
	}
	// A restart makes requests wait again; a stale generation cannot revive it.
	s.down()
	s.markUp(gen)
	if s.up.Load() {
		t.Fatal("stale generation marked the sidecar up")
	}
	s.timeout = 50 * time.Millisecond
	if s.wait(context.Background()) {
		t.Fatal("wait succeeded while down")
	}
}

func TestSidecarReadyWrapAnswers503WithRetryAfter(t *testing.T) {
	s := newSidecarReady(50 * time.Millisecond)
	s.begin()
	called := false
	h := s.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/x", nil))
		if rec.Code != 503 || rec.Header().Get("Retry-After") == "" || called {
			t.Fatalf("got %d retry-after=%q called=%v", rec.Code, rec.Header().Get("Retry-After"), called)
		}
	}
}

func TestMiddlewareGateWaitsForSidecar(t *testing.T) {
	s := newSidecarReady(50 * time.Millisecond)
	s.begin()
	g := &middlewareGate{sidecarURL: "http://127.0.0.1:1/__krate/middleware", required: true, sidecarReady: s}
	rec := httptest.NewRecorder()
	g.viaSidecar(rec, httptest.NewRequest("GET", "/", nil), http.NotFoundHandler())
	if rec.Code != 503 || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("got %d retry-after=%q", rec.Code, rec.Header().Get("Retry-After"))
	}
}

// A request sent right after serve start waits for a slow register() and is
// answered 200, never 502.
func TestSidecarSlowRegisterRequestWaitsInsteadOf502(t *testing.T) {
	e2eEnv(t)
	b, root := buildInstProject(t, instFiles(`export async function register() {
  await new Promise((r) => setTimeout(r, 1500));
  (globalThis as any).__krateInst = "ready";
}`))
	b.Cfg.SSR.NodeWorkers = 1
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	code, body := getText(t, base+"/api/pid")
	if code != 200 || !strings.Contains(body, `"state":"ready"`) {
		t.Fatalf("immediate request: %d %s", code, body)
	}
}

// A register() that never resolves answers 503 + Retry-After after the bound,
// while static files are served without waiting.
func TestSidecarNeverReadyAnswers503AndStaticDoesNotWait(t *testing.T) {
	e2eEnv(t)
	old := sidecarStartTimeout
	sidecarStartTimeout = 1500 * time.Millisecond
	t.Cleanup(func() { sidecarStartTimeout = old })
	b, root := buildInstProject(t, instFiles(`export async function register() { await new Promise(() => {}); }`))
	logs := captureStderrFile(t)
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	start := time.Now()
	if code, _ := getText(t, base+"/"); code != 200 || time.Since(start) > time.Second {
		t.Errorf("static page: %d after %s", code, time.Since(start))
	}
	for i := 0; i < 2; i++ {
		start = time.Now()
		resp, err := http.Get(base + "/api/pid")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 503 || resp.Header.Get("Retry-After") == "" {
			t.Errorf("api: %d retry-after=%q", resp.StatusCode, resp.Header.Get("Retry-After"))
		}
		if d := time.Since(start); d < time.Second {
			t.Errorf("answered after %s, before the bound", d)
		}
	}
	if n := strings.Count(logs(), "did not start listening"); n != 1 {
		t.Errorf("logged %d times, want once", n)
	}
}
