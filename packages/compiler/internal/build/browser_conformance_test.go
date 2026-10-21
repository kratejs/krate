package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestBrowserConformance drives the React conformance corpus in a real browser.
// KRATE_BROWSER selects chromium, firefox, or webkit. The dedicated CI jobs set
// it explicitly; the normal cross-platform suite skips when it is unset so
// installing browsers remains optional for local development.
func TestBrowserConformance(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run browser conformance")
	}
	switch browser {
	case "chromium", "firefox", "webkit":
	default:
		t.Fatalf("unsupported KRATE_BROWSER %q", browser)
	}
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}

	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	projectRoot := filepath.Join(repoRoot, "examples")
	if _, err := os.Stat(projectRoot); err != nil {
		requireE2E(t, "examples not found: %v", err)
	}
	cfg, err := config.Load(projectRoot)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if err := New(projectRoot, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	script, err := filepath.Abs(filepath.Join("testdata", "browser_conformance.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", script, cfg.OutDir, browser)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") ||
			strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser conformance failed: %v\n%s", err, out)
	}
	t.Logf("browser conformance (%s) output:\n%s", browser, out)
}
