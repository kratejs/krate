//go:build unix

package build

import (
	"os"
	"syscall"
	"testing"
	"time"
)

const canStopInProcessServer = true

// registerServerStop stops an in-process Serve at test cleanup. Serve shuts
// down (and kills its sidecars) on SIGTERM, so the test binary signals itself.
func registerServerStop(t *testing.T, errCh <-chan error) {
	t.Cleanup(func() {
		select {
		case <-errCh:
			return
		default:
		}
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
		select {
		case <-errCh:
		case <-time.After(15 * time.Second):
			t.Logf("in-process server did not stop within 15s")
		}
	})
}
