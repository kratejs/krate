package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestRouterBrowserE2E drives the SPA router against a photo-gallery app
// served by the real Go server: an intercepting @modal route opens on soft
// navigation (URL changes, the feed stays), a hard load renders the real
// page, back closes the modal, layout state persists across soft
// navigations, a template remounts, and catch-all routes resolve with the
// right specificity. Runs when KRATE_BROWSER is set (chromium recommended).
func TestRouterBrowserE2E(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run the router e2e")
	}
	if !canStopInProcessServer {
		t.Skip("the in-process server needs a unix host")
	}
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	root := t.TempDir()
	if err := copyTree(t, filepath.Join("testdata", "router_gallery"), root); err != nil {
		t.Fatal(err)
	}
	// The build finds the runtime bundle under <root>/packages/runtime/dist.
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "packages"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repoRoot, "packages", "runtime"), filepath.Join(root, "packages", "runtime")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "packages", "runtime", "dist", "krate-hydrate.js")); err != nil {
		requireE2E(t, "runtime dist not built (pnpm --filter @krate/runtime build): %v", err)
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	base, serr := startKrateServer(t, root, cfg)
	err = serr
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "router_e2e.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", script, base, browser)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("router e2e failed: %v\n%s", err, out)
	}
	t.Logf("router e2e (%s):\n%s", browser, out)
}
