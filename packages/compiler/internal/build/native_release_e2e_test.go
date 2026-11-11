package build

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// e2eBuildRelease runs the equivalent of `krate native build --release` on the
// example and returns the executable.
func e2eBuildRelease(t *testing.T, ex, out string, extraTags ...string) (string, *NativeReleaseResult) {
	t.Helper()
	cfg, err := config.Load(ex)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	b := New(ex, cfg)
	res, err := b.BuildNativeRelease(NativeReleaseOptions{OutDir: out, ExtraTags: extraTags})
	if err != nil {
		t.Fatalf("native release build: %v", err)
	}
	if !res.Compiled || len(res.Binaries) != 1 {
		t.Fatalf("release build produced %+v", res)
	}
	t.Logf("release executable: %s, tags %v", FormatBytes(res.Binaries[0].Size), res.Tags)
	return res.Binaries[0].Path, res
}

// The example (Go and TypeScript host code) as one executable, driven by
// Playwright over --browser.
func TestNativeReleaseE2EBrowser(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run the native release browser end-to-end test")
	}
	switch browser {
	case "chromium", "firefox", "webkit":
	default:
		t.Fatalf("unsupported KRATE_BROWSER %q", browser)
	}
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	root := repoRoot(t)
	if err := exec.Command("node", "-e", "require('"+filepath.ToSlash(filepath.Join(root, "node_modules", "playwright"))+"')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	ex := e2eRequireDeps(t)
	bin, _ := e2eBuildRelease(t, ex, filepath.Join(t.TempDir(), "dist-native"))

	app := runReleaseBinary(t, bin)
	script, _ := filepath.Abs(filepath.Join("testdata", "native_e2e", "browser.mjs"))
	mainFlow, _ := filepath.Abs(filepath.Join("testdata", "native_e2e", "flow_main.js"))
	taskFlow, _ := filepath.Abs(filepath.Join("testdata", "native_e2e", "flow_task.js"))
	cmd := exec.Command("node", script, app.url, browser, mainFlow, taskFlow)
	cmd.Dir = root
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if strings.Contains(stderr.String(), "Executable doesn't exist") || strings.Contains(stderr.String(), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, stderr.String())
		}
		t.Fatalf("browser driver failed: %v\n%s\n%s", err, out, stderr.String())
	}
	var res struct {
		Reports []e2eStep `json:"reports"`
		Errors  []string  `json:"errors"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("driver output: %v\n%s", err, out)
	}
	e2eCheckSteps(t, res.Reports, 16)
	for _, e := range res.Errors {
		t.Errorf("browser error: %s", e)
	}
}

// The example as one executable in the real webview. The executable is built
// with the krate_devcontrol tag so the test can drive it through --dev.
func TestNativeReleaseE2EGUI(t *testing.T) {
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		t.Skip("set KRATE_NATIVE_GUI=1 to run the native release GUI end-to-end test")
	}
	ex := e2eRequireDeps(t)
	bin, _ := e2eBuildRelease(t, ex, filepath.Join(t.TempDir(), "dist-native"), "krate_devcontrol")

	s := e2eStartShell(t, bin, "")
	s.waitReady(60 * time.Second)
	e2eGUIFlows(t, s)
	e2eFootprint(t, s.cmd.Process.Pid)
	if _, err := s.send("quit", 10*time.Second); err != nil {
		t.Errorf("quit: %v", err)
	}
}

// The example builds as a release executable and serves its Go and TypeScript
// host calls without a browser.
func TestNativeReleaseExample(t *testing.T) {
	requireRelease(t)
	ex := e2eRequireDeps(t)
	bin, res := e2eBuildRelease(t, ex, filepath.Join(t.TempDir(), "dist-native"))
	if !res.Manifest.StrictPermissions || len(res.Manifest.Permissions) != 0 {
		t.Fatalf("permissions = %v", res.Manifest.Permissions)
	}
	app := runReleaseBinary(t, bin)
	if code, body := app.get(t, "/"); code != 200 || !strings.Contains(body, "list") {
		t.Fatalf("index: %d", code)
	}
	if code, _ := app.get(t, "/tasks/42"); code != 200 {
		t.Fatalf("dynamic route fallback: %d", code)
	}
	if got := app.call(t, "src/lib/tasks.List", nil); !strings.Contains(got, `"ok"`) {
		t.Fatalf("Go host call: %s", got)
	}
}
