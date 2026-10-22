//go:build !unix

package build

import "testing"

// Without SIGTERM the in-process server cannot be stopped cleanly, so server
// fixtures are skipped on this platform.
const canStopInProcessServer = false

func registerServerStop(t *testing.T, errCh <-chan error) {}
