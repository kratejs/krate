package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
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

// TestNativeE2EMediaGUI runs examples/native-media in the real webview with
// the mock capture devices: the page asks for the camera, the microphone and
// the location, and native.media decides (camera allowed, the other two
// denied). It goes through the shell, the permission handler of the platform
// and the manager as an app does.
func TestNativeE2EMediaGUI(t *testing.T) {
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		t.Skip("set KRATE_NATIVE_GUI=1 to run the native GUI end-to-end test")
	}
	e2eRequireDeps(t)
	ex := filepath.Join(repoRoot(t), "examples", "native-media")
	if _, err := os.Stat(filepath.Join(ex, "node_modules", "@krate", "app", "dist", "media.js")); err != nil {
		requireE2E(t, "examples/native-media has no built @krate/app (run pnpm install and pnpm build)")
	}
	t.Setenv("KRATE_NATIVE_MOCK_CAPTURE", "1")
	// WebView2 fails getUserMedia with NotFoundError before asking for
	// permission when the machine has no camera or microphone (CI runners);
	// Chromium's fake devices stand in, like KRATE_NATIVE_MOCK_CAPTURE does for
	// WebKitGTK. The shell merges this variable into its browser arguments.
	if runtime.GOOS == "windows" {
		t.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", "--use-fake-device-for-media-stream")
	}
	work := t.TempDir()
	appDir := e2eBuildNative(t, ex, filepath.Join(work, "dist-native"))

	raw, err := os.ReadFile(filepath.Join(appDir, "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"media"`, `"camera": "allow"`, `"microphone": "deny"`, `"uses"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("app.json lacks %s:\n%s", want, raw)
		}
	}

	bin := e2eBuildShell(t, work)
	s := e2eStartShell(t, bin, appDir)
	s.waitReady(60 * time.Second)

	var res string
	for deadline := time.Now().Add(60 * time.Second); ; time.Sleep(300 * time.Millisecond) {
		err = s.query(`document.getElementById("result")?.textContent ?? ""`, &res)
		if err == nil && strings.HasPrefix(res, "{") {
			break
		}
		if time.Now().After(deadline) {
			var errs string
			_ = s.query(`JSON.stringify(globalThis.__krateErrs ?? [], (k, v) => v instanceof Error ? String(v) + " " + v.stack : v)`, &errs)
			t.Logf("page errors: %s", errs)
			t.Fatalf("the page did not report within 60s: %q (last query error: %v)", res, err)
		}
	}
	t.Logf("page result: %s", res)
	var got map[string]string
	if err := json.Unmarshal([]byte(res), &got); err != nil {
		t.Fatal(err)
	}
	if got["camera"] == "NotAllowedError" || got["camera"] == "" {
		t.Errorf("camera (policy allow) = %q", got["camera"])
	}
	if got["microphone"] != "NotAllowedError" {
		t.Errorf("microphone (policy deny) = %q, want NotAllowedError", got["microphone"])
	}
	if got["location"] != "code1" {
		t.Errorf("location (policy deny) = %q, want code1 (PERMISSION_DENIED)", got["location"])
	}
	// The dev build derives media and location from the code.
	if got["mediaPermission"] != "true" || got["locationPermission"] != "true" {
		t.Errorf("permissions: media %q, location %q", got["mediaPermission"], got["locationPermission"])
	}
	if !strings.Contains(got["policy"], `"camera":"allow"`) || !strings.Contains(got["policy"], `"microphone":"deny"`) {
		t.Errorf("policy = %q", got["policy"])
	}
	if _, err := s.send("quit", 10*time.Second); err != nil {
		t.Errorf("quit: %v", err)
	}
}

// TestNativeMediaDerivedPermissions: a derived build grants "media" and
// "location" because the UI code calls getUserMedia and geolocation, and the
// manifest lists what the page uses for packaging.
func TestNativeMediaDerivedPermissions(t *testing.T) {
	e2eRequireDeps(t)
	ex := filepath.Join(repoRoot(t), "examples", "native-media")
	if _, err := os.Stat(filepath.Join(ex, "node_modules", "@krate", "app", "dist", "media.js")); err != nil {
		requireE2E(t, "examples/native-media has no built @krate/app (run pnpm install and pnpm build)")
	}
	cfg, err := config.Load(ex)
	if err != nil {
		t.Fatal(err)
	}
	b := New(ex, cfg)
	res, err := b.BuildNativeApp(NativeAppOptions{OutDir: filepath.Join(t.TempDir(), "dist-native"), Derive: true})
	if err != nil {
		t.Fatalf("native build: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(res.AppDir, "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	var man struct {
		Permissions       []string `json:"permissions"`
		StrictPermissions bool     `json:"strictPermissions"`
		Media             struct {
			Uses []string `json:"uses"`
		} `json:"media"`
	}
	if err := json.Unmarshal(raw, &man); err != nil {
		t.Fatal(err)
	}
	has := func(l []string, s string) bool {
		for _, x := range l {
			if x == s {
				return true
			}
		}
		return false
	}
	// media:manage is not needed by a page that only reads the policy; the
	// SDK ids it calls (media.policy, media.decisions, media.osStatus) need media.
	for _, p := range []string{"media", "location"} {
		if !has(man.Permissions, p) {
			t.Errorf("derived permissions %v lack %q", man.Permissions, p)
		}
	}
	if !man.StrictPermissions || has(man.Permissions, "media:manage") {
		t.Errorf("strict %v, permissions %v", man.StrictPermissions, man.Permissions)
	}
	if strings.Join(man.Media.Uses, ",") != "camera,microphone,location" {
		t.Errorf("media.uses = %v", man.Media.Uses)
	}
}
