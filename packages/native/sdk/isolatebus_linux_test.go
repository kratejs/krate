//go:build linux

package sdk_test

import "testing"

// isolateSessionBus points the SDK at a private bus with no services, so a
// test that only checks argument shapes cannot reach (or wake, through D-Bus
// activation) a notification daemon of the machine running it.
func isolateSessionBus(t *testing.T) {
	t.Helper()
	session(t)
}
