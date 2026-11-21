package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestBrowserKonva builds a react-konva-style canvas editor that imports
// @krate/konva twice: compiled into a Krate page, where <Stage> is the
// @renderer component and its children are the compiled h() form, and as a
// "use client" Preact island on a server-rendered page, where the build
// resolves @krate/konva to its island build. Both are driven in a browser:
// reactive props, click and hover hit-testing, drag, the Transformer,
// double-click, keyed lists, conditionals, refs and context through the Stage.
// Neither ships React.
func TestBrowserKonva(t *testing.T) {
	t.Parallel()
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
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "packages", "runtime", "dist", "krate-runtime.js")); err != nil {
		requireE2E(t, "the runtime package is not built: %v", err)
	}
	konvaPkg := filepath.Join(repo, "packages", "konva")
	konvaLib := filepath.Join(konvaPkg, "node_modules", "konva")
	if _, err := os.Stat(filepath.Join(konvaLib, "package.json")); err != nil {
		requireE2E(t, "konva is not installed for packages/konva (run pnpm install): %v", err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "browser_konva.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := filepath.Abs(filepath.Join("testdata", "konva"))
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	e2eEnv(t)
	if err := copyTree(t, filepath.Join(fixture, "src"), filepath.Join(root, "src")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "@krate"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	for link, target := range map[string]string{
		filepath.Join(root, "node_modules", "@krate", "konva"): konvaPkg,
		filepath.Join(root, "node_modules", "konva"):           konvaLib,
	} {
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("cannot link %s: %v", target, err)
		}
	}
	if out, err := captureBuild(New(root, cfg)); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}

	// No React anywhere in the output: the page compiles the editor, and the
	// island runs on Preact.
	reactDOM := regexp.MustCompile(`__SECRET_INTERNALS|__CLIENT_INTERNALS_DO_NOT_USE|react-reconciler`)
	_ = filepath.Walk(cfg.OutDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".js") {
			return nil
		}
		name := filepath.Base(path)
		if strings.HasPrefix(name, "preact-") {
			return nil // preact/compat carries React's internals names as a shim
		}
		data, _ := os.ReadFile(path)
		if strings.HasPrefix(name, islandRealSharedStem) || reactDOM.Match(data) {
			t.Errorf("@krate/konva shipped React in %s", name)
		}
		return nil
	})

	if !canStopInProcessServer {
		t.Skip("cannot stop an in-process server on this platform")
	}
	base, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	out, err := exec.Command("node", script, browser, base).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser checks failed: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}
