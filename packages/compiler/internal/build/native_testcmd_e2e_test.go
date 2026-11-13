package build

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestNativeTestCommandGUI runs `krate native test` (RunNativeTest) on the two
// examples that ship tests: the runner builds the app, bundles
// tests/*.native.test.ts, and the driver launches the real shell in a real
// webview and drives the pages. It is the proof that an app author's test
// suite runs.
func TestNativeTestCommandGUI(t *testing.T) {
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		t.Skip("set KRATE_NATIVE_GUI=1 to run the native GUI end-to-end test")
	}
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	tasks := e2eRequireDeps(t)
	views := filepath.Join(repoRoot(t), "examples", "native-views")
	for _, ex := range []string{tasks, views} {
		if _, err := os.Stat(filepath.Join(ex, "node_modules", "@krate", "app", "dist", "driver.js")); err != nil {
			requireE2E(t, "%s has no built @krate/app driver (run pnpm install and pnpm build)", ex)
		}
	}
	shell := e2eBuildShell(t, t.TempDir())
	for _, c := range []struct {
		ex   string
		want int // tests that must pass
	}{{tasks, 5}, {views, 4}} {
		t.Run(filepath.Base(c.ex), func(t *testing.T) {
			var out bytes.Buffer
			code, err := RunNativeTest(NativeTestOptions{
				LoadConfig: func() (string, *config.Config, map[string]string, error) {
					cfg, err := config.Load(c.ex)
					return c.ex, cfg, nil, err
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
			if m == nil || m[1] != strconv.Itoa(c.want) || strings.Contains(out.String(), "ℹ fail 1") {
				t.Errorf("expected %d passing tests, summary %v", c.want, m)
			}
		})
	}
}
