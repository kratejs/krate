package build

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSidecarReadyWaitsThenReleases(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

// A sidecar that exited before it listened is known to have failed: waiting
// requests are released at once and later ones do not wait, until a start
// listens. A sidecar that crashes after it listened is waited for as before.
func TestSidecarReadyKnownFailureFailsFast(t *testing.T) {
	t.Parallel()
	s := newSidecarReady(30 * time.Second)
	s.begin()
	done := make(chan bool, 1)
	go func() { done <- s.wait(context.Background()) }()
	time.Sleep(50 * time.Millisecond)
	s.down()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("wait succeeded for a failed sidecar")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a waiting request was not released when the sidecar failed")
	}
	start := time.Now()
	h := s.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("handler called") }))
	s.fail("instrumentation failed: Error: secret detail")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/x", nil))
	if rec.Code != 503 || time.Since(start) > time.Second {
		t.Fatalf("got %d after %s", rec.Code, time.Since(start))
	}
	if strings.Contains(rec.Body.String(), "secret detail") {
		t.Errorf("production answers must not carry the reason: %q", rec.Body.String())
	}
	s.showReason = true
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/x", nil))
	if !strings.Contains(rec.Body.String(), "secret detail") {
		t.Errorf("dev answers carry the reason: %q", rec.Body.String())
	}

	// A start that listens clears the failure.
	gen := s.begin()
	s.markUp(gen)
	if s.failure() != "" || !s.wait(context.Background()) {
		t.Fatal("the failure must clear once a start listens")
	}
	// A crash after listening is not a known failure: requests wait.
	s.down()
	if s.failure() != "" {
		t.Fatalf("crash after listening marked failed: %q", s.failure())
	}
	s.timeout = 150 * time.Millisecond
	start = time.Now()
	if s.wait(context.Background()) || time.Since(start) < 100*time.Millisecond {
		t.Fatal("requests must wait for a restart after a crash at run time")
	}
}

// A server that took the port while the sidecar was not listening yet (an
// instrumentation register() still running or failed) must not count as the
// sidecar: only a listener answering this start's token does.
func TestSidecarReadyWatchIgnoresAnotherListener(t *testing.T) {
	t.Parallel()
	var answer atomic.Value
	answer.Store("someone else")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != sidecarReadyPath {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, answer.Load().(string))
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	port := ln.Addr().(*net.TCPAddr).Port

	s := newSidecarReady(5 * time.Second)
	gen := s.begin()
	token := newSidecarStartToken()
	done := make(chan struct{})
	defer close(done)
	go s.watch(gen, port, token, done)
	time.Sleep(300 * time.Millisecond)
	if s.up.Load() {
		t.Fatal("a listener without this start's token marked the sidecar up")
	}
	answer.Store(token)
	deadline := time.Now().Add(5 * time.Second)
	for !s.up.Load() {
		if time.Now().After(deadline) {
			t.Fatal("the sidecar answering its token was not marked up")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
