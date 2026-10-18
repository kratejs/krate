package tsexec

import (
	"os"
	"path/filepath"
	"runtime"
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

// TestResolveTSXRunnerHonorsEnv verifies KRATE_TSX wins over discovery on every
// platform, and that the "off" sentinel forces the npx path.
func TestResolveTSXRunnerHonorsEnv(t *testing.T) {
	want := filepath.Join(t.TempDir(), "custom-tsx")
	t.Setenv("KRATE_TSX", want)
	if got := resolveTSXRunner(t.TempDir()); got != want {
		t.Fatalf("resolveTSXRunner = %q, want %q", got, want)
	}
	for _, off := range []string{"off", "0", "none", "OFF"} {
		t.Setenv("KRATE_TSX", off)
		if got := resolveTSXRunner(t.TempDir()); got != "" {
			t.Fatalf("KRATE_TSX=%q: resolveTSXRunner = %q, want \"\" (force npx)", off, got)
		}
	}
}

// TestResolveTSXRunnerFindsLocalBin verifies a locally installed tsx is found by
// walking up from cwd, so npx (and its shared, racy install cache) is skipped.
func TestResolveTSXRunnerFindsLocalBin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses npx; .bin shims are not directly executable")
	}
	t.Setenv("KRATE_TSX", "")
	root := t.TempDir()
	binDir := filepath.Join(root, "node_modules", ".bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(binDir, "tsx")
	if err := os.WriteFile(want, []byte("#!/usr/bin/env node\n"), 0755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "packages", "app")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	if got := resolveTSXRunner(nested); got != want {
		t.Fatalf("resolveTSXRunner = %q, want %q", got, want)
	}
}
