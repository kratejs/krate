package build

import (
	"context"
	"testing"
	"time"
)

// serveInProcess runs an in-process server (ServeContext or ServeDevContext
// called with the context it is given) and stops it at test cleanup by
// cancelling that context, which shuts the server down as SIGTERM would
// without touching any other server of the test binary. The returned channel
// receives the server's result.
func serveInProcess(t *testing.T, run func(ctx context.Context) error) <-chan error {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		errCh <- run(ctx)
	}()
	t.Cleanup(func() {
		stop()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Logf("in-process server did not stop within 15s")
		}
	})
	return errCh
}
