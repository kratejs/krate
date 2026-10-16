package tsexec

import (
	"strings"
	"testing"
	"time"
)

// TestRunBootstrapTimeoutDoesNotHang verifies that a bootstrap which never
// exits is killed at the deadline and that RunBootstrap returns promptly.
//
// Regression test: `npx` is a shim that spawns a child `node`; killing only the
// shim left the child holding the stdout/stderr pipes, so cmd.Wait() blocked
// forever and the timeout never surfaced.
func TestRunBootstrapTimeoutDoesNotHang(t *testing.T) {
	start := time.Now()
	_, _, err := RunBootstrap("krate-test-hang", "setInterval(function () {}, 1000);", t.TempDir(), 3*time.Second, nil)
	elapsed := time.Since(start)

	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected a timeout error, got %v (after %v)", err, elapsed)
	}
	if elapsed > 45*time.Second {
		t.Fatalf("RunBootstrap took %v; the timeout was not enforced", elapsed)
	}
}

// TestRunBootstrapEnvPassthrough verifies the KEY=VALUE entries passed to
// RunBootstrap reach the script's process.env. Appended entries come after the
// shell environment, so they override it (dotenv override=false semantics).
func TestRunBootstrapEnvPassthrough(t *testing.T) {
	content := `console.log(process.env.KRATE_TEST_ENV);`
	out, _, err := RunBootstrap("krate-test-env", content, t.TempDir(), 30*time.Second, []string{"KRATE_TEST_ENV=from-bootstrap"})
	if err != nil {
		t.Fatalf("RunBootstrap: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "from-bootstrap" {
		t.Errorf("process.env.KRATE_TEST_ENV = %q, want %q", got, "from-bootstrap")
	}
}
