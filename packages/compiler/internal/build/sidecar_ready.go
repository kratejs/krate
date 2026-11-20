package build

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
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
//
// A sidecar that exited before it listened (a crash at startup, or failed
// instrumentation) is known to have failed: requests stop waiting for it and
// answer 503 at once until a later start listens.
type sidecarReady struct {
	timeout time.Duration
	// name is how messages call the sidecar; default "Node API sidecar".
	name string
	// hint is added to the logged reason of a failure.
	hint string
	// showReason puts the failure reason in the 503 body (dev); production
	// answers keep it in the server log.
	showReason bool
	up         atomic.Bool
	logged     atomic.Bool

	mu      sync.Mutex
	gen     uint64
	ch      chan struct{} // closed once the sidecar is up
	failCh  chan struct{} // closed once the sidecar is known to have failed
	failMsg string        // why, while failCh is closed
}

func (s *sidecarReady) label() string {
	if s.name == "" {
		return "Node API sidecar"
	}
	return s.name
}

func (s *sidecarReady) slowStartCause() string {
	if s.name == "" {
		return "instrumentation register() still pending, or the sidecar keeps crashing"
	}
	return "the sidecar keeps crashing"
}

func (s *sidecarReady) hintSuffix() string {
	if s.hint == "" {
		return ""
	}
	return "\n    hint: " + s.hint
}

func newSidecarReady(timeout time.Duration) *sidecarReady {
	return &sidecarReady{timeout: timeout, ch: make(chan struct{}), failCh: make(chan struct{})}
}

// begin marks a new process start and returns its generation.
func (s *sidecarReady) begin() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gen++
	s.resetLocked()
	return s.gen
}

// down marks the sidecar as not accepting connections (process exited). A
// process that exited before it listened failed to start.
func (s *sidecarReady) down() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gen++
	if !s.up.Load() {
		s.failLocked("the " + s.label() + " exited before it started listening; it is restarting")
	}
	s.resetLocked()
}

// fail marks the sidecar as failed with a reason requests are answered with.
func (s *sidecarReady) fail(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failLocked(msg)
}

func (s *sidecarReady) failLocked(msg string) {
	s.failMsg = msg
	select {
	case <-s.failCh:
	default:
		close(s.failCh)
		s.logged.Store(false)
	}
}

// failure is the reason the sidecar is known to have failed, or "".
func (s *sidecarReady) failure() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.failCh:
		return s.failMsg
	default:
		return ""
	}
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
	select {
	case <-s.failCh:
		s.failCh = make(chan struct{})
		s.failMsg = ""
		s.logged.Store(false)
	default:
	}
}

// sidecarReadyPath answers the start token of the sidecar process
// (KRATE_SIDECAR_START), so the host knows the listener on the port is the
// process it started and not another server that took the port while this one
// was not yet listening.
const sidecarReadyPath = "/__krate/internal/ready"

// newSidecarStartToken is a random token identifying one sidecar start.
func newSidecarStartToken() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// watch polls the loopback port until the sidecar started with token answers
// on it, the process exits (done) or a newer generation began. An empty token
// only waits for the port to accept connections.
func (s *sidecarReady) watch(gen uint64, port int, token string, done <-chan struct{}) {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	client := &http.Client{Timeout: 500 * time.Millisecond}
	ours := func() bool {
		if token == "" {
			c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
			if err != nil {
				return false
			}
			_ = c.Close()
			return true
		}
		resp, err := client.Get("http://" + addr + sidecarReadyPath)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return resp.StatusCode == http.StatusOK && string(body) == token
	}
	for {
		select {
		case <-done:
			return
		default:
		}
		if ours() {
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
// when the bound elapsed, ctx ended or the sidecar is known to have failed.
func (s *sidecarReady) wait(ctx context.Context) bool {
	if s.up.Load() {
		return true
	}
	s.mu.Lock()
	ch, failCh := s.ch, s.failCh
	s.mu.Unlock()
	select {
	case <-failCh:
		return s.up.Load()
	default:
	}
	t := time.NewTimer(s.timeout)
	defer t.Stop()
	select {
	case <-ch:
		return true
	case <-failCh:
		return s.up.Load()
	case <-ctx.Done():
		return false
	case <-t.C:
		return s.up.Load()
	}
}

// unavailable answers a request whose wait failed: 503 + Retry-After when the
// bound elapsed or the sidecar failed, nothing when the client went away. It
// logs once per failure.
func (s *sidecarReady) unavailable(w http.ResponseWriter, r *http.Request) {
	if r.Context().Err() != nil {
		return
	}
	w.Header().Set("Retry-After", "1")
	w.Header().Set("Cache-Control", "no-store")
	if msg := s.failure(); msg != "" {
		if s.logged.CompareAndSwap(false, true) {
			fmt.Fprintf(os.Stderr, "  %s! answering 503 to requests that need the %s: %s%s%s\n", cYellow, s.label(), msg, s.hintSuffix(), cReset)
		}
		body := "server unavailable: the " + s.label() + " failed to start (see the server log)"
		if s.showReason {
			body = "server unavailable: " + msg
		}
		http.Error(w, body, http.StatusServiceUnavailable)
		return
	}
	if s.logged.CompareAndSwap(false, true) {
		fmt.Fprintf(os.Stderr, "  %s! %s did not start listening within %s; answering 503 until it does (%s)%s%s\n", cYellow, s.label(), s.timeout, s.slowStartCause(), s.hintSuffix(), cReset)
	}
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
