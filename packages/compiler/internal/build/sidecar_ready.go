package build

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// sidecarStartTimeout bounds how long a request that needs the Node API
// sidecar waits for it to start listening. Instrumentation's register() runs
// before the sidecar listens, so a slow one holds requests back instead of
// failing them. A variable so tests can shorten it.
var sidecarStartTimeout = 30 * time.Second

// sidecarReady tracks whether the supervised Node API sidecar accepts
// connections. The fast path is one atomic load; only requests that arrive
// while the sidecar is starting (first start or a restart) block.
type sidecarReady struct {
	timeout time.Duration
	up      atomic.Bool
	logged  atomic.Bool

	mu  sync.Mutex
	gen uint64
	ch  chan struct{} // closed once the sidecar is up
}

func newSidecarReady(timeout time.Duration) *sidecarReady {
	return &sidecarReady{timeout: timeout, ch: make(chan struct{})}
}

// begin marks a new process start and returns its generation.
func (s *sidecarReady) begin() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gen++
	s.resetLocked()
	return s.gen
}

// down marks the sidecar as not accepting connections (process exited).
func (s *sidecarReady) down() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gen++
	s.resetLocked()
}

func (s *sidecarReady) resetLocked() {
	if s.up.Swap(false) {
		s.ch = make(chan struct{})
	}
}

// markUp records that generation gen accepts connections; a stale generation
// is ignored.
func (s *sidecarReady) markUp(gen uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen != s.gen || s.up.Load() {
		return
	}
	s.up.Store(true)
	close(s.ch)
}

// watch polls the loopback port until it accepts, the process exits (done) or
// the generation changes.
func (s *sidecarReady) watch(gen uint64, port int, done <-chan struct{}) {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	for {
		select {
		case <-done:
			return
		default:
		}
		if c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
			_ = c.Close()
			s.markUp(gen)
			return
		}
		select {
		case <-done:
			return
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// wait returns true once the sidecar accepts connections. It returns false
// when the bound elapsed or ctx ended.
func (s *sidecarReady) wait(ctx context.Context) bool {
	if s.up.Load() {
		return true
	}
	s.mu.Lock()
	ch := s.ch
	s.mu.Unlock()
	t := time.NewTimer(s.timeout)
	defer t.Stop()
	select {
	case <-ch:
		return true
	case <-ctx.Done():
		return false
	case <-t.C:
		return s.up.Load()
	}
}

// unavailable answers a request whose wait failed: 503 + Retry-After when the
// bound elapsed, nothing when the client went away. It logs once.
func (s *sidecarReady) unavailable(w http.ResponseWriter, r *http.Request) {
	if r.Context().Err() != nil {
		return
	}
	if s.logged.CompareAndSwap(false, true) {
		fmt.Fprintf(os.Stderr, "  %s! Node API sidecar did not start listening within %s; answering 503 until it does (instrumentation register() still pending, or the sidecar keeps crashing)%s\n", cYellow, s.timeout, cReset)
	}
	w.Header().Set("Retry-After", "1")
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "server is starting", http.StatusServiceUnavailable)
}

// wrap makes next wait for the sidecar first.
func (s *sidecarReady) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.up.Load() && !s.wait(r.Context()) {
			s.unavailable(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
