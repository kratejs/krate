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

// TestCSPSpeculationRulesInBrowser checks that the router's runtime-injected
// speculation rules are not blocked by the generated CSP.
func TestCSPSpeculationRulesInBrowser(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run the CSP browser check")
	}
	if !canStopInProcessServer {
		t.Skip("the in-process server needs a unix host")
	}
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	root := t.TempDir()
	if err := copyTree(t, filepath.Join("testdata", "router_gallery"), root); err != nil {
		t.Fatal(err)
	}
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
		requireE2E(t, "runtime dist not built: %v", err)
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.CSP.Enabled = true
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	base, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "csp_speculation.cjs"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", script, base, browser).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "Cannot find module") {
			requireE2E(t, "browser unavailable: %s", out)
		}
		t.Fatalf("CSP check failed: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}

// TestSegmentPagesBrowser drives per-segment not-found and error pages in a
// real browser against the real Go server (testdata/segments_app): a nested
// 404 inside the layouts on hard load and soft navigation with layout state
// kept, the client error boundary with reset(), nested boundaries, the
// server-rendered error page and soft navigation away clearing an error.
// Runs when KRATE_BROWSER is set.
func TestSegmentPagesBrowser(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run the segment pages browser test")
	}
	if !canStopInProcessServer {
		t.Skip("the in-process server needs a unix host")
	}
	ensureRuntimeDist(t)
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	root := t.TempDir()
	if err := copyTree(t, filepath.Join("testdata", "segments_app"), root); err != nil {
		t.Fatal(err)
	}
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
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
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	base, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "segments_e2e.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", script, base, browser).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("segment pages browser test failed: %v\n%s", err, out)
	}
	t.Logf("segment pages (%s):\n%s", browser, out)
}

// TestRouterHooksBrowser drives usePathname, useParams, useSearchParams,
// useRouter and useSelectedLayoutSegment(s) in a real browser against the real
// Go server (testdata/nav_hooks_app). Runs when KRATE_BROWSER is set.
func TestRouterHooksBrowser(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run the router hooks browser test")
	}
	if !canStopInProcessServer {
		t.Skip("the in-process server needs a unix host")
	}
	ensureRuntimeDist(t)
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	root, cfg := serverlessProject(t, "nav_hooks_app")
	base, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "nav_hooks_e2e.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", script, base, browser).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("router hooks browser test failed: %v\n%s", err, out)
	}
	t.Logf("router hooks (%s):\n%s", browser, out)
}
