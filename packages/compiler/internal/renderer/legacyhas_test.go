package renderer

import (
	"strings"
	"testing"
)

// TestLegacyHasShimContract pins the shim to the class conventions the legacy
// stylesheet emits: it must sync `krc-h` controller inputs onto `<token>-on`
// root classes, mark focused labels with `krc-f`, and be idempotent.
func TestLegacyHasShimContract(t *testing.T) {
	for _, want := range []string{"krc-h", "-on", "closest", "krc-f", "__krateHasShim", "MutationObserver"} {
		if !strings.Contains(LegacyHasJS, want) {
			t.Errorf("shim missing %q:\n%s", want, LegacyHasJS)
		}
	}
}
