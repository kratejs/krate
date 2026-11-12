package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestNativeE2EPageGUI runs examples/native-page in the real webview: the
// page zooms, searches itself, captures itself, prints itself to PDF, saves a
// download into a folder and round-trips a cookie, all through
// @krate/app/{page,downloads,cookies}, the shell and the platform layer. The
// platform tests call the platform directly; this one goes through the SDK
// bridge as an app does, and reads every step's outcome from the page.
func TestNativeE2EPageGUI(t *testing.T) {
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		t.Skip("set KRATE_NATIVE_GUI=1 to run the native GUI end-to-end test")
	}
	e2eRequireDeps(t)
	ex := filepath.Join(repoRoot(t), "examples", "native-page")
	for _, mod := range []string{"index", "page", "downloads", "cookies"} {
		if _, err := os.Stat(filepath.Join(ex, "node_modules", "@krate", "app", "dist", mod+".js")); err != nil {
			requireE2E(t, "examples/native-page has no built @krate/app/%s (run pnpm install and pnpm build)", mod)
		}
	}
	work := t.TempDir()
	// Derive: the permissions the page needs come from what it imports, the
	// way a build for release writes them.
	cfg, err := config.Load(ex)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	res, err := New(ex, cfg).BuildNativeApp(NativeAppOptions{OutDir: filepath.Join(work, "dist-native"), Derive: true})
	if err != nil {
		t.Fatalf("native build: %v", err)
	}
	appDir := res.AppDir
	if got := strings.Join(res.Capabilities.Permissions, ","); !strings.Contains(got, "page") || !strings.Contains(got, "downloads") || !strings.Contains(got, "cookies") {
		t.Fatalf("derived permissions %q lack page, downloads or cookies", got)
	}
	bin := e2eBuildShell(t, work)

	// The download lands under the app data directory; keep it out of the
	// developer's real one.
	data := filepath.Join(work, "data")
	t.Setenv("XDG_DATA_HOME", data) // Linux
	t.Setenv("APPDATA", data)       // Windows
	if runtime.GOOS == "darwin" {
		t.Setenv("HOME", filepath.Join(work, "home"))
	}

	s := e2eStartShell(t, bin, appDir)
	s.waitReady(60 * time.Second)

	var flow struct {
		Done  bool      `json:"done"`
		Steps []e2eStep `json:"steps"`
	}
	for deadline := time.Now().Add(120 * time.Second); ; time.Sleep(300 * time.Millisecond) {
		err = s.query(`globalThis.__pageFlow ?? {done: false, steps: []}`, &flow)
		if err == nil && flow.Done {
			break
		}
		if time.Now().After(deadline) {
			var errs string
			_ = s.query(`JSON.stringify(globalThis.__krateErrs ?? [], (k, v) => v instanceof Error ? String(v) + " " + v.stack : v)`, &errs)
			b, _ := json.Marshal(flow)
			t.Logf("page errors: %s", errs)
			t.Fatalf("the flow did not finish within 120s: %s (last query error: %v)", b, err)
		}
	}
	want := map[string]bool{"zoom": true, "find": true, "capture": true, "pdf": true, "download": true, "cookies": true, "app-origin cookies": true}
	for _, st := range flow.Steps {
		delete(want, st.Name)
	}
	if len(want) > 0 {
		t.Errorf("steps that never ran: %v", want)
	}
	e2eCheckSteps(t, flow.Steps, 7)
	if _, err := s.send("quit", 10*time.Second); err != nil {
		t.Errorf("quit: %v", err)
	}
}
