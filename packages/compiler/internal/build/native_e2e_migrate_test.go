package build

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestNativeFromElectronGUI runs the ported app of the Electron migration guide
// (examples/native-from-electron) through `krate native test`: draft saving
// through a host function, file reads and writes inside the fs scope and the
// refusal outside it, a menu item, a second window with a host signal, a tray
// event and a notification, all in the real shell and webview. The Electron
// original is in examples/native-from-electron/electron and is not run.
func TestNativeFromElectronGUI(t *testing.T) {
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		t.Skip("set KRATE_NATIVE_GUI=1 to run the native GUI end-to-end test")
	}
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	e2eRequireDeps(t)
	ex := filepath.Join(repoRoot(t), "examples", "native-from-electron")
	if _, err := os.Stat(filepath.Join(ex, "node_modules", "@krate", "app", "dist", "driver.js")); err != nil {
		requireE2E(t, "%s has no built @krate/app driver (run pnpm install and pnpm build)", ex)
	}
	shell := e2eBuildShell(t, t.TempDir())
	var out bytes.Buffer
	code, err := RunNativeTest(NativeTestOptions{
		LoadConfig: func() (string, *config.Config, map[string]string, error) {
			cfg, err := config.Load(ex)
			return ex, cfg, nil, err
		},
		Shell:  shell,
		Stdout: &out,
		Stderr: &out,
	})
	t.Logf("krate native test output:\n%s", out.String())
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("krate native test exited with %d", code)
	}
	m := regexp.MustCompile(`(?m)^ℹ pass (\d+)$`).FindStringSubmatch(out.String())
	if m == nil || m[1] != "7" || strings.Contains(out.String(), "ℹ fail 1") {
		t.Errorf("expected 7 passing tests, summary %v", m)
	}
}
