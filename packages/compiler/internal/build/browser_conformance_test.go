package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestBrowserConformance drives the React demo in headless Chrome and checks
// the cross-runtime behavior that unit and happy-dom tests cannot: live island
// props/callbacks, signal-backed controlled primitives, and portals.
//
// Chrome is optional. When it is not installed the test skips so the normal
// cross-platform suite still runs; install Chrome (or set KRATE_CHROME) to run
// it locally.
func TestBrowserConformance(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	chrome := findChrome()
	if chrome == "" {
		t.Skip("Chrome not found; set KRATE_CHROME to run browser conformance")
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
	cmd := exec.Command("node", script, cfg.OutDir, chrome)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser conformance failed: %v\n%s", err, out)
	}
	t.Logf("browser conformance output:\n%s", out)
}

func findChrome() string {
	if path := os.Getenv("KRATE_CHROME"); path != "" {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	var candidates []string
	switch runtime.GOOS {
	case "windows":
		candidates = []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
			`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
		}
	case "darwin":
		candidates = []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"}
	default:
		candidates = []string{"/usr/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/chromium-browser"}
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "chrome"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}
