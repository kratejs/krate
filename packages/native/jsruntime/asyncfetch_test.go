package jsruntime

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// loopRunner is a minimal single-goroutine job queue for async completions.
type loopRunner struct {
	mu   sync.Mutex
	jobs []func()
	wake chan struct{}
}

func newLoopRunner() *loopRunner { return &loopRunner{wake: make(chan struct{}, 1)} }

func (l *loopRunner) post(fn func()) {
	l.mu.Lock()
	l.jobs = append(l.jobs, fn)
	l.mu.Unlock()
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

// runUntil runs posted jobs and drains microtasks until done() or a timeout.
func (l *loopRunner) runUntil(t *testing.T, rt *Runtime, done func() bool) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		rt.DrainJobs()
		if done() {
			return
		}
		select {
		case <-l.wake:
			l.mu.Lock()
			jobs := l.jobs
			l.jobs = nil
			l.mu.Unlock()
			for _, j := range jobs {
				j()
			}
		case <-deadline:
			t.Fatal("timed out")
		}
	}
}

func TestAsyncFetchDoesNotBlock(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			<-release
		}
		if _, err := io.WriteString(w, r.URL.Path); err != nil {
			t.Error(err)
		}
	}))
	defer srv.Close()
	rt, err := NewWithOptions(Options{Preload: []string{"fetch"}})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	l := newLoopRunner()
	if err := rt.EnableAsyncFetch(l.post); err != nil {
		t.Fatal(err)
	}
	if err := rt.EnableAsyncFetch(l.post); err == nil {
		t.Fatal("enabling twice must fail")
	}
	_, err = rt.Execute(`
		globalThis.out = [];
		fetch(` + jsQuote(srv.URL+"/slow") + `).then(r => r.text()).then(t => out.push("slow:" + t));
		fetch(` + jsQuote(srv.URL+"/fast") + `).then(r => r.text()).then(t => out.push("fast:" + t));
	`)
	if err != nil {
		t.Fatal(err)
	}
	count := func() int {
		v, _ := rt.Execute("out.length")
		n, _ := jsNumber(v)
		return int(n)
	}
	// Execute returned with both requests in flight; the fast one finishes
	// while the slow one is still held by the server.
	l.runUntil(t, rt, func() bool { return count() == 1 })
	if v, _ := rt.Execute("out[0]"); v != "fast:/fast" {
		t.Fatalf("got %v", v)
	}
	close(release)
	l.runUntil(t, rt, func() bool { return count() == 2 })
	if v, _ := rt.Execute("out[1]"); v != "slow:/slow" {
		t.Fatalf("got %v", v)
	}
}

func TestOptionsLimits(t *testing.T) {
	rt, err := NewWithOptions(Options{MemoryLimit: 64 << 20, EvalTimeout: -1})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if rt.memLimit != 64<<20 || rt.timeout != 0 {
		t.Fatalf("memLimit=%d timeout=%v", rt.memLimit, rt.timeout)
	}
	// A nested evaluation (a lazy prelude load) works with no deadline.
	if _, err := rt.Execute("typeof URL"); err != nil {
		t.Fatal(err)
	}
	rt2, err := NewWithOptions(Options{EvalTimeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer rt2.Close()
	if _, err := rt2.Execute("for(;;){}"); err == nil {
		t.Fatal("the eval timeout must stop a runaway loop")
	}
}

func TestPublicBinaryTransfer(t *testing.T) {
	rt, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if rt.BinaryTransfer() != (BinaryTransferSupported() && !forceBase64) {
		t.Fatal("BinaryTransfer disagrees with BinaryTransferSupported")
	}
}
