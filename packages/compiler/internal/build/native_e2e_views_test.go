package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestNativeE2EViewsGUI runs examples/native-views in the real webview: the
// main page adds a sidebar and a log view through the SDK, each of them posts
// its size back to the main page once it has loaded, and the main page lists
// them. The platform view tests call the platform layer directly; this one
// goes through the shell, the SDK bridge and the page as an app does.
func TestNativeE2EViewsGUI(t *testing.T) {
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		t.Skip("set KRATE_NATIVE_GUI=1 to run the native GUI end-to-end test")
	}
	e2eRequireDeps(t)
	ex := filepath.Join(repoRoot(t), "examples", "native-views")
	if _, err := os.Stat(filepath.Join(ex, "node_modules", "@krate", "app", "dist", "index.js")); err != nil {
		requireE2E(t, "examples/native-views has no built @krate/app (run pnpm install and pnpm build)")
	}
	work := t.TempDir()
	appDir := e2eBuildNativeDev(t, ex, filepath.Join(work, "dist-native"))
	bin := e2eBuildShell(t, work)

	s := e2eStartShell(t, bin, appDir)
	s.waitReady(60 * time.Second)

	var page struct {
		Status string `json:"status"`
		Loaded string `json:"loaded"`
	}
	var err error
	sizeRe := regexp.MustCompile(`(sidebar|log) (\d+)x(\d+)`)
	for deadline := time.Now().Add(60 * time.Second); ; time.Sleep(200 * time.Millisecond) {
		err = s.query(`({status: document.getElementById("status")?.textContent ?? "",
			loaded: document.getElementById("loaded")?.textContent ?? ""})`, &page)
		if err == nil && strings.Contains(page.Status, "sidebar") && strings.Contains(page.Status, "log") &&
			len(sizeRe.FindAllString(page.Loaded, -1)) == 2 {
			break
		}
		if time.Now().After(deadline) {
			// The runtime keeps the errors thrown while the page started.
			var errs string
			_ = s.query(`JSON.stringify(globalThis.__krateErrs ?? [], (k, v) => v instanceof Error ? String(v) + " " + v.stack : v)`, &errs)
			t.Logf("page errors: %s", errs)
			t.Fatalf("the views did not all open within 60s: status %q, loaded %q, last query error: %v",
				page.Status, page.Loaded, err)
		}
	}
	t.Logf("status %q, loaded %q", page.Status, page.Loaded)
	for _, m := range sizeRe.FindAllStringSubmatch(page.Loaded, -1) {
		w, _ := strconv.Atoi(m[2])
		h, _ := strconv.Atoi(m[3])
		if w < 50 || h < 50 {
			t.Errorf("view %s loaded at %dx%d; its pane has no room", m[1], w, h)
		}
	}
	e2eNoPageErrors(t, s)
	if _, err := s.send("quit", 10*time.Second); err != nil {
		t.Errorf("quit: %v", err)
	}
}
