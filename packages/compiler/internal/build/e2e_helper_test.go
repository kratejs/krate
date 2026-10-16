package build

import (
	"os"
	"testing"
)

// requireE2E skips a test when an end-to-end prerequisite is missing, unless
// KRATE_REQUIRE_E2E=1 is set - in which case the missing prerequisite is a hard
// failure. CI sets KRATE_REQUIRE_E2E=1 so a misconfigured environment (no Node,
// no built @krate/runtime) cannot silently pass without exercising SSR/ISR/region
// behavior.
func requireE2E(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv("KRATE_REQUIRE_E2E") != "" {
		t.Fatalf("E2E prerequisite missing (KRATE_REQUIRE_E2E is set): "+format, args...)
	}
	t.Skipf(format, args...)
}
