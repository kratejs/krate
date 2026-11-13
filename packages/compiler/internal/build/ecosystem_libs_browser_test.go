package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestBrowserEcosystemLibraries builds and drives the real npm packages listed
// in testdata/ecosystem_libs/package.json (sonner, cmdk, embla-carousel-react,
// @dnd-kit, @tanstack/react-table, next-themes, motion/react) in a browser,
// including a layout-level Toaster with toast() called from a page and a
// layout-level ThemeProvider.
// Unlike the stand-in packages of the other island tests it needs the actual
// packages: set KRATE_ECOSYSTEM_LIBS to a node_modules directory that has them,
// or KRATE_ECOSYSTEM_INSTALL=1 to have the test run npm install (network).
// It also needs KRATE_BROWSER like the other browser gates.
func TestBrowserEcosystemLibraries(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run browser checks")
	}
	libs := os.Getenv("KRATE_ECOSYSTEM_LIBS")
	if libs == "" && os.Getenv("KRATE_ECOSYSTEM_INSTALL") != "1" {
		t.Skip("set KRATE_ECOSYSTEM_LIBS=<node_modules dir> or KRATE_ECOSYSTEM_INSTALL=1 to run the real-library checks")
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
	runtimeSrc := filepath.Join(repo, "examples", "node_modules", "@krate", "runtime")
	if _, err := os.Stat(filepath.Join(runtimeSrc, "dist", "krate-runtime.js")); err != nil {
		requireE2E(t, "the runtime package is not built: %v", err)
	}
	preactSrc := filepath.Join(repo, "examples", "node_modules", "preact")
	if _, err := os.Stat(preactSrc); err != nil {
		t.Skipf("preact fixture not installed: %v", err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "browser_ecosystem_libs.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := filepath.Abs(filepath.Join("testdata", "ecosystem_libs"))
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if err := copyTree(t, filepath.Join(fixture, "src"), filepath.Join(root, "src")); err != nil {
		t.Fatal(err)
	}
	modules := filepath.Join(root, "node_modules")
	if libs != "" {
		if err := os.MkdirAll(modules, 0755); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(libs)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.Name() == ".bin" {
				continue
			}
			if err := os.Symlink(filepath.Join(libs, e.Name()), filepath.Join(modules, e.Name())); err != nil {
				t.Skipf("cannot link %s: %v", e.Name(), err)
			}
		}
	} else {
		pkg, err := os.ReadFile(filepath.Join(fixture, "package.json"))
		if err != nil {
			t.Fatal(err)
		}
		writeProjectFile(t, root, "package.json", string(pkg))
		install := exec.Command("npm", "install", "--no-audit", "--no-fund", "--ignore-scripts", "--legacy-peer-deps")
		install.Dir = root
		if out, err := install.CombinedOutput(); err != nil {
			t.Fatalf("npm install: %v\n%s", err, out)
		}
	}
	if err := copyTree(t, preactSrc, filepath.Join(modules, "preact")); err != nil {
		t.Fatalf("copying preact fixture: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(modules, "@krate"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(runtimeSrc, filepath.Join(modules, "@krate", "runtime")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}

	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = true
	// The runtime bundle is found next to the working directory.
	t.Chdir(root)
	if _, err := captureOutput(func() error { return New(root, cfg).BuildAll() }); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	// Pages under async layouts render per request, so they need the server.
	serverBase, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	out, err := exec.Command("node", script, cfg.OutDir, browser, serverBase).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser checks failed: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}
