package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestBrowserThree builds pages that render @krate/three scenes (a React-style
// page and a Krate-style page) and drives them in a browser with WebGL: the
// scene mounts from the compiled children of the @renderer Canvas, renders,
// raycasts clicks and hovers, and follows state from outside the canvas.
func TestBrowserThree(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run browser checks")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	e2eEnv(t)
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	runtimeSrc := filepath.Join(repo, "packages", "runtime")
	if _, err := os.Stat(filepath.Join(runtimeSrc, "dist", "krate-runtime.js")); err != nil {
		requireE2E(t, "the runtime package is not built: %v", err)
	}
	threePkg := filepath.Join(repo, "packages", "three")
	threeLib := filepath.Join(threePkg, "node_modules", "three")
	if _, err := os.Stat(filepath.Join(threeLib, "package.json")); err != nil {
		requireE2E(t, "three is not installed for packages/three (run pnpm install): %v", err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "browser_three.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := filepath.Abs(filepath.Join("testdata", "three"))
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if err := copyTree(t, filepath.Join(fixture, "src"), filepath.Join(root, "src")); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(t, filepath.Join(fixture, "public"), filepath.Join(root, "public")); err != nil {
		t.Fatal(err)
	}
	writeProjectFile(t, root, "package.json", `{"name":"three-fixture","private":true,"type":"module"}`)
	modules := filepath.Join(root, "node_modules")
	if err := os.MkdirAll(filepath.Join(modules, "@krate"), 0755); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{
		filepath.Join(modules, "@krate", "runtime"): runtimeSrc,
		filepath.Join(modules, "@krate", "three"):   threePkg,
		filepath.Join(modules, "three"):             threeLib,
	} {
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("cannot link %s: %v", target, err)
		}
	}

	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.PublicDir = filepath.Join(root, "public")
	cfg.Minify = true
	t.Chdir(root)
	if _, err := captureOutput(func() error { return New(root, cfg).BuildAll() }); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	out, err := exec.Command("node", script, cfg.OutDir, browser).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser checks failed: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}
