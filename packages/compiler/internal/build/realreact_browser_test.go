package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBrowserRealReactIsland renders a library built on react-reconciler (a
// tiny canvas renderer) in a real browser: the island paints a canvas, follows
// a prop that comes from a Krate signal, follows its own React state, and a
// page without such an island loads no React. It needs KRATE_BROWSER like the
// other browser gates.
func TestBrowserRealReactIsland(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run browser checks")
	}
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	script, err := filepath.Abs(filepath.Join("testdata", "browser_real_react.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	cfg.Minify = true
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	runtimeSrc := filepath.Join(repo, "examples", "node_modules", "@krate", "runtime")
	if _, err := os.Stat(filepath.Join(runtimeSrc, "dist", "krate-runtime.js")); err != nil {
		requireE2E(t, "the runtime package is not built: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "@krate"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(runtimeSrc, filepath.Join(root, "node_modules", "@krate", "runtime")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	t.Chdir(root)
	realReactFixtureFrom(t, root, repo)
	realReactSceneFiles(t, root)
	if out, err := captureOutput(func() error { return New(root, cfg).BuildAll() }); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	cmd := exec.Command("node", script, cfg.OutDir, browser)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser checks failed: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}
