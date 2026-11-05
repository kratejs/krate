package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestComponentsKeyboardBrowser drives the interactive components (OTPField,
// Select, Command, ContextMenu, Tooltip, Pagination, mapped Tabs, modal
// Dialog, Checkbox/Switch) with the keyboard in a real browser against the
// built examples project.
func TestComponentsKeyboardBrowser(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run the components keyboard browser test")
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
	script, err := filepath.Abs(filepath.Join("testdata", "components_keyboard.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", script, cfg.OutDir, browser).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("components keyboard browser test failed: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}
