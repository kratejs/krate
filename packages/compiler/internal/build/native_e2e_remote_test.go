package build

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// remoteProbePage is the web page the end-to-end test shows in a remote view.
// It tries to reach the app in every way a page can, reports what it found
// through the message channel, and echoes what the app posts to it.
const remoteProbePage = `<!doctype html><title>remote</title><body>remote page<script>
(async () => {
  const r = {origin: location.origin};
  document.cookie = "remote-cookie=1; path=/";
  localStorage.setItem("remote-key", "1");
  const tryFetch = async (u) => { try { await fetch(u); return "reachable"; } catch (e) { return "error"; } };
  r.app = await tryFetch("krate://app/");
  r.host = await tryFetch("krate://app/__krate/call");
  r.dev = await tryFetch("krate://app/__krate/dev/result?id=1");
  r.https = await tryFetch("https://app.localhost/");
  r.hostGlobal = typeof window.__krateHost;
  r.krateGlobals = Object.getOwnPropertyNames(window).filter(k => /krate/i.test(k) && k !== "krateRemote").join(",");
  r.opened = String(window.open("krate://app/"));
  r.hasSdk = typeof window.krate;
  krateRemote.postMessage({probe: r});
  const leave = () => { location.href = "http://127.0.0.1:1/elsewhere"; setTimeout(() => krateRemote.postMessage({stillAt: location.href}), 800); };
  addEventListener("message", e => { if (e.data && e.data.leave) leave(); else krateRemote.postMessage({echo: e.data, origin: e.origin}); });
})();
</script>`

// TestNativeE2ERemoteGUI runs examples/native-remote through the real shell:
// the app page opens a remote view on a local web server, the remote page
// probes everything it could reach, and the two talk over the message
// channel. It checks the build derived the permission, that the remote page
// has no access to the app, the origin allowlist, the storage partition and
// the origin checks of the message channel.
func TestNativeE2ERemoteGUI(t *testing.T) {
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		t.Skip("set KRATE_NATIVE_GUI=1 to run the native GUI end-to-end test")
	}
	e2eRequireDeps(t)
	ex := filepath.Join(repoRoot(t), "examples", "native-remote")
	if _, err := os.Stat(filepath.Join(ex, "node_modules", "@krate", "app", "dist", "remote.js")); err != nil {
		requireE2E(t, "examples/native-remote has no built @krate/app (run pnpm install and pnpm build)")
	}
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, remoteProbePage)
	}))
	defer site.Close()

	work := t.TempDir()
	// A release build derives the permissions from the code: addView,
	// setRules and setUserAgent are the three areas the page uses.
	cfg, err := config.Load(ex)
	if err != nil {
		t.Fatal(err)
	}
	res, err := New(ex, cfg).BuildNativeApp(NativeAppOptions{OutDir: filepath.Join(work, "release"), Derive: true})
	if err != nil {
		t.Fatalf("release build: %v", err)
	}
	derived, err := os.ReadFile(filepath.Join(res.AppDir, "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"remote"`, `"remote:intercept"`, `"remote:network"`} {
		if !strings.Contains(string(derived), want) {
			t.Errorf("the release build did not derive %s:\n%s", want, derived)
		}
	}
	appDir := e2eBuildNativeDev(t, ex, filepath.Join(work, "dist-native"))
	bin := e2eBuildShell(t, work)
	s := e2eStartShell(t, bin, appDir)
	s.waitReady(60 * time.Second)

	// The page defines its helpers when its script has run, which can be
	// after the shell reports ready on a slow machine.
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		var ready bool
		if err := s.query(`typeof openRemote === "function"`, &ready); err == nil && ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the page never defined openRemote")
		}
	}
	var view string
	opts := `{blocked: "block"}`
	if err := s.query(fmt.Sprintf(`openRemote(%q, %s)`, site.URL+"/", opts), &view); err != nil {
		t.Fatalf("opening the remote view: %v", err)
	}
	if view == "" {
		t.Fatal("no view id")
	}

	type seen struct {
		Messages []struct {
			View   string          `json:"view"`
			Origin string          `json:"origin"`
			Data   json.RawMessage `json:"data"`
		} `json:"messages"`
		Errors []string `json:"errors"`
		Events []struct {
			View    string `json:"view"`
			Kind    string `json:"kind"`
			URL     string `json:"url"`
			Verdict string `json:"verdict"`
		} `json:"events"`
	}
	var got seen
	poll := func(what string, done func() bool) {
		t.Helper()
		for deadline := time.Now().Add(60 * time.Second); ; time.Sleep(250 * time.Millisecond) {
			err := s.query(`globalThis.__remote`, &got)
			if err == nil && done() {
				return
			}
			if time.Now().After(deadline) {
				var errs string
				_ = s.query(`JSON.stringify(globalThis.__krateErrs ?? [], (k, v) => v instanceof Error ? String(v) + " " + v.stack : v)`, &errs)
				t.Logf("page errors: %s", errs)
				t.Fatalf("timed out waiting for %s: %+v (last error %v)", what, got, err)
			}
		}
	}
	has := func(key string) (map[string]any, bool) {
		for _, m := range got.Messages {
			var d map[string]any
			if json.Unmarshal(m.Data, &d) == nil {
				if v, ok := d[key]; ok {
					if mm, ok := v.(map[string]any); ok {
						return mm, true
					}
					return map[string]any{"value": v}, true
				}
			}
		}
		return nil, false
	}
	poll("the probe report", func() bool { _, ok := has("probe"); return ok })
	probe, _ := has("probe")
	origin := strings.TrimSuffix(site.URL, "/")
	for _, m := range got.Messages {
		if m.View != view || m.Origin != origin {
			t.Errorf("message from view %q origin %q, want %q and %q", m.View, m.Origin, view, origin)
		}
	}
	// The page reached nothing of the app.
	for _, k := range []string{"app", "host", "dev", "https"} {
		if probe[k] != "error" {
			t.Errorf("remote fetch %s = %v, want error", k, probe[k])
		}
	}
	if probe["hostGlobal"] != "undefined" || probe["krateGlobals"] != "" || probe["hasSdk"] != "undefined" {
		t.Errorf("Krate globals in the remote page: %+v", probe)
	}
	if probe["opened"] != "null" {
		t.Errorf("window.open to the app returned %v", probe["opened"])
	}
	// Its storage is not the app's.
	var appSide struct {
		Cookie string  `json:"cookie"`
		Key    *string `json:"key"`
	}
	if err := s.query(`({cookie: document.cookie, key: localStorage.getItem("remote-key")})`, &appSide); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(appSide.Cookie, "remote-cookie") || appSide.Key != nil {
		t.Errorf("the app sees the remote page's storage: %+v", appSide)
	}

	// App to page: delivered with the app's origin, only to the right origin.
	if err := s.query(fmt.Sprintf(`postRemote(%q, {n: 7}, %q)`, view, origin), nil); err != nil {
		t.Fatalf("post: %v", err)
	}
	poll("the echo", func() bool { _, ok := has("echo"); return ok })
	// The app origin the remote page sees: WebView2 serves the app from
	// https://app.localhost, the WebKit platforms from krate://app.
	appOrigin := "krate://app"
	if runtime.GOOS == "windows" {
		appOrigin = "https://app.localhost"
	}
	for _, m := range got.Messages {
		var d struct {
			Echo   map[string]any `json:"echo"`
			Origin string         `json:"origin"`
		}
		if json.Unmarshal(m.Data, &d) == nil && d.Echo != nil {
			if d.Origin != appOrigin || d.Echo["n"] != float64(7) {
				t.Errorf("echo %+v", d)
			}
		}
	}
	err = s.query(fmt.Sprintf(`postRemote(%q, {n: 8}, "https://other.example")`, view), nil)
	if err == nil {
		t.Error("a message to the wrong origin was accepted")
	}

	// The origin allowlist: leaving is refused and reported; the page stays.
	if err := s.query(fmt.Sprintf(`postRemote(%q, {leave: true}, %q)`, view, origin), nil); err != nil {
		t.Fatalf("post: %v", err)
	}
	poll("the refused navigation", func() bool {
		for _, e := range got.Events {
			if e.Kind == "navBlocked" && strings.Contains(e.URL, "127.0.0.1:1/elsewhere") && e.Verdict == "block" {
				return true
			}
		}
		return false
	})
	poll("the page to stay", func() bool { v, ok := has("stillAt"); return ok && strings.HasPrefix(fmt.Sprint(v["value"]), origin) })
	e2eNoPageErrors(t, s)
	if _, err := s.send("quit", 10*time.Second); err != nil {
		t.Errorf("quit: %v", err)
	}
}
