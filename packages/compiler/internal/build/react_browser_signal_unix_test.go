//go:build unix

package build

import (
	"os"
	"sync"
	"syscall"
	"testing"
	"time"
)

const canStopInProcessServer = true

// stopSignal records the last SIGTERM the test binary sent itself. Every
// in-process Serve registered before it received that signal, and a second
// signal makes a shutting-down server exit the process, so a server that is
// still draining must not be signaled again.
var stopSignal struct {
	mu   sync.Mutex
	last time.Time
}

// registerServerStop stops an in-process Serve at test cleanup. Serve shuts
// down (and kills its sidecars) on SIGTERM, so the test binary signals itself.
func registerServerStop(t *testing.T, errCh <-chan error) {
	registered := time.Now()
	t.Cleanup(func() {
		select {
		case <-errCh:
			return
		default:
		}
		stopSignal.mu.Lock()
		if !stopSignal.last.After(registered) {
			_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
			stopSignal.last = time.Now()
		}
		stopSignal.mu.Unlock()
		select {
		case <-errCh:
		case <-time.After(15 * time.Second):
			t.Logf("in-process server did not stop within 15s")
		}
	})
}
