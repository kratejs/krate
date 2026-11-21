package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestUseClientIslandBrowserMotionTooltip runs the Genesis sidebar pattern on
// the real packages (testdata/motion_tooltip: motion's LazyMotion, LayoutGroup
// and layoutId with radix-ui tooltips around the items) and checks that
// labels animate on a prop change made while a tooltip is open, as on React.
// The tooltip's portal commits in the middle of that render; the effect-order
// shims then ran motion's useEffect before its useInsertionEffect, so the
// labels stayed at opacity 0. It needs KRATE_BROWSER, and the packages:
// KRATE_MOTION_LIBS=<node_modules dir> or KRATE_ECOSYSTEM_INSTALL=1 (npm
// install, network).
func TestUseClientIslandBrowserMotionTooltip(t *testing.T) {
	t.Parallel()
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run browser checks")
	}
	libs := os.Getenv("KRATE_MOTION_LIBS")
	if libs == "" && os.Getenv("KRATE_ECOSYSTEM_INSTALL") != "1" {
		t.Skip("set KRATE_MOTION_LIBS=<node_modules dir> or KRATE_ECOSYSTEM_INSTALL=1 to run the motion and radix-ui checks")
	}
	e2eEnv(t)
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	fixture, err := filepath.Abs(filepath.Join("testdata", "motion_tooltip"))
	if err != nil {
		t.Fatal(err)
	}
	b, root := scProject(t, nil)
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
			if e.Name() == ".bin" || e.Name() == "preact" || e.Name() == "@krate" {
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
	withPreact(t, root)
	if err := linkRuntimePackage(t, filepath.Join(modules, "@krate")); err != nil {
		t.Fatal(err)
	}
	if out, err := captureBuild(b); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "browser_motion_tooltip.mjs"))
	if err != nil {
		t.Fatal(err)
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
