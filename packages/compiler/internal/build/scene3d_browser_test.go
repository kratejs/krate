package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/scene3d/matc"
)

// TestBrowserScene3D builds a compiled @krate/3d page against the real
// filament package and drives it in a browser with WebGL 2: the engine loads
// from the copied assets, the scene renders non-background pixels, a click
// reaches the dynamic sphere through view.pick, and a signal moves its
// transform.
func TestBrowserScene3D(t *testing.T) {
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
	script, err := filepath.Abs(filepath.Join("testdata", "browser_scene3d.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	root, cfg := scene3dProject(t, "scene3d_browser", true)
	// The custom-shader page needs a real matc (KRATE_FILAMENT_MATC or the
	// downloaded tool); without one it is left out of the build.
	shader := "shader"
	if !realMatcAvailable() {
		shader = "noshader"
		if err := os.Remove(filepath.Join(root, "src", "pages", "shader.tsx")); err != nil {
			t.Fatal(err)
		}
	}
	cfg.Minify = true
	if _, err := captureBuild(New(root, cfg)); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	out, err := exec.Command("node", script, cfg.OutDir, browser, shader).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser checks failed: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}

// realMatcAvailable reports whether a real matc can be found without a
// download: KRATE_FILAMENT_MATC, or the cached krate tool.
func realMatcAvailable() bool {
	if p := os.Getenv(matc.EnvMatc); p != "" {
		_, err := os.Stat(p)
		return err == nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	name := "matc"
	if runtime.GOOS == "windows" {
		name = "matc.exe"
	}
	_, err = os.Stat(filepath.Join(home, ".krate", "tools", "filament", matc.FilamentVersion, "bin", name))
	return err == nil
}
