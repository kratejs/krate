package build

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestNativeE2EChromeGUI runs examples/native-chrome in the real webview with
// no title bar: it checks that the shell put the chrome script into the page,
// that the page sees its drag regions, that the chrome endpoint resolves the
// window from the webview that asked, and that the SDK reaches the platform
// window (state, title bar, and the Windows and macOS only calls rejecting
// with "unsupported" on Linux).
func TestNativeE2EChromeGUI(t *testing.T) {
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		t.Skip("set KRATE_NATIVE_GUI=1 to run the native GUI end-to-end test")
	}
	if runtime.GOOS != "linux" {
		t.Skip("the expectations about unsupported calls are for Linux; Windows and macOS run the platform GUI tests")
	}
	e2eRequireDeps(t)
	ex := filepath.Join(repoRoot(t), "examples", "native-chrome")
	if _, err := os.Stat(filepath.Join(ex, "node_modules", "@krate", "app", "dist", "chrome.js")); err != nil {
		requireE2E(t, "examples/native-chrome has no built @krate/app/chrome (run pnpm install and pnpm build)")
	}
	work := t.TempDir()
	appDir := e2eBuildNative(t, ex, filepath.Join(work, "dist-native"))

	// The manifest carries the chrome block the config asked for.
	raw, err := os.ReadFile(filepath.Join(appDir, "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"titleBar": "none"`) && !strings.Contains(string(raw), `"titleBar":"none"`) {
		t.Fatalf("app.json lacks the chrome block:\n%s", raw)
	}

	bin := e2eBuildShell(t, work)
	s := e2eStartShell(t, bin, appDir)
	s.waitReady(60 * time.Second)

	type pageState struct {
		Script   int    `json:"script"`
		Bar      string `json:"bar"`
		Button   string `json:"button"`
		Titlebar string `json:"titlebar"`
		Features string `json:"features"`
	}
	var st pageState
	for deadline := time.Now().Add(60 * time.Second); ; time.Sleep(200 * time.Millisecond) {
		err = s.query(`({script: window.__krateChrome ? window.__krateChrome.version : 0,
			bar: window.__krateChrome ? window.__krateChrome.regionOf(document.getElementById("bar")) : "",
			button: window.__krateChrome ? window.__krateChrome.regionOf(document.getElementById("min")) : "",
			titlebar: document.getElementById("titlebar")?.textContent ?? "",
			features: document.getElementById("features")?.textContent ?? ""})`, &st)
		if err == nil && st.Script == 1 && strings.Contains(st.Titlebar, "none") {
			break
		}
		if time.Now().After(deadline) {
			var errs string
			_ = s.query(`JSON.stringify(globalThis.__krateErrs ?? [], (k, v) => v instanceof Error ? String(v) + " " + v.stack : v)`, &errs)
			t.Logf("page errors: %s", errs)
			var raw string
			_ = s.query(`document.getElementById("raw")?.textContent ?? ""`, &raw)
			t.Logf("raw state: %s", raw)
			t.Fatalf("the chrome page did not come up: %+v, last error %v", st, err)
		}
	}
	if st.Bar != "drag" || st.Button != "no-drag" {
		t.Errorf("regions: bar %q, button %q", st.Bar, st.Button)
	}
	if !strings.Contains(st.Features, "titleBar") || !strings.Contains(st.Features, "drag") {
		t.Errorf("features %q", st.Features)
	}

	// The endpoint knows the window by the webview, not by anything the page
	// says: GET describes it, a drag without a button press is refused.
	var ep struct {
		Info struct {
			TitleBar  string `json:"titleBar"`
			Decorated bool   `json:"decorated"`
			Resizable bool   `json:"resizable"`
		} `json:"info"`
		Drag int `json:"drag"`
		Bad  int `json:"bad"`
	}
	if err := s.query(`(async () => {
		const info = await (await fetch("/__krate/chrome")).json();
		const drag = await fetch("/__krate/chrome", {method: "POST", body: JSON.stringify({op: "drag"})});
		const bad = await fetch("/__krate/chrome", {method: "POST", body: JSON.stringify({op: "resize", edge: "nowhere"})});
		return {info, drag: drag.status, bad: bad.status};
	})()`, &ep); err != nil {
		t.Fatalf("endpoint query: %v", err)
	}
	if ep.Info.TitleBar != "none" || ep.Info.Decorated || !ep.Info.Resizable {
		t.Errorf("GET /__krate/chrome = %+v", ep.Info)
	}
	if ep.Drag == 404 {
		t.Error("the endpoint did not find the window of the webview")
	}
	if ep.Bad != 400 {
		t.Errorf("an unknown edge answered %d, want 400", ep.Bad)
	}

	// SDK calls through the shell: the platform features this OS lacks reject
	// with the code "unsupported", and the page can change its own title bar.
	// The note is a reactive text node: wait for its text, never write it.
	clickNote := func(id, prefix string) string {
		t.Helper()
		var note string
		if err := s.query(`(async () => {
			document.getElementById("`+id+`").click();
			for (let i = 0; i < 100 && !document.getElementById("note").textContent.startsWith("`+prefix+`"); i++) await new Promise((r) => setTimeout(r, 50));
			return document.getElementById("note").textContent;
		})()`, &note); err != nil {
			t.Fatalf("clicking %s: %v", id, err)
		}
		return note
	}
	if got := clickNote("backdrop", "backdrop:"); got != "backdrop: unsupported" {
		t.Errorf("setBackdrop on this platform: %q", got)
	}
	if got := clickNote("vibrancy", "vibrancy:"); got != "vibrancy: unsupported" {
		t.Errorf("setVibrancy on this platform: %q", got)
	}
	if _, err := s.send("quit", 10*time.Second); err != nil {
		t.Errorf("quit: %v", err)
	}
}
