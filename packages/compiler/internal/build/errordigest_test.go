package build

import "testing"

// The error digest shown on a 500 page must be the same value on every host:
// the Go server, the Node sidecar and the portable handler (see
// packages/runtime/src/ssr-core-behavior.test.ts, which pins the same value).
func TestErrorDigestIsPinned(t *testing.T) {
	if got := errorDigest("boom", "at x"); got != "8a2474a04d" {
		t.Fatalf("errorDigest(boom, at x) = %q, want 8a2474a04d (sha256 of \"boom\\nat x\", first 10 hex digits)", got)
	}
}
