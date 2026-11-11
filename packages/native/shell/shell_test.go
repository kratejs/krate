package shell

import (
	"bytes"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/internal/gohosttest"
	"github.com/kratejs/krate/packages/native/internal/memory"
	"github.com/kratejs/krate/packages/native/wire"
)

func TestParseManifestDefaults(t *testing.T) {
	m, err := ParseManifest([]byte(`{"appId":"com.acme.app"}`), "app.json")
	if err != nil {
		t.Fatal(err)
	}
	if m.Assets != "dist" || m.Window.Path != "/" || m.Window.Width != 1024 || m.Window.Height != 768 ||
		!m.Window.IsResizable() || m.Window.Title != "com.acme.app" || m.ProductName != "com.acme.app" {
		t.Errorf("defaults: %+v", m)
	}
	m, err = ParseManifest([]byte(`{"appId":"a.b","window":{"resizable":false},"gohost":{"binary":"gohost/x"}}`), "app.json")
	if err != nil {
		t.Fatal(err)
	}
	if m.Window.IsResizable() {
		t.Error("resizable false lost")
	}
	if want := "gohost/x"; !strings.HasPrefix(m.GoHost.Binary, want) {
		t.Errorf("binary = %q", m.GoHost.Binary)
	}
}

func TestParseManifestErrors(t *testing.T) {
	cases := map[string]string{
		"no id":           `{}`,
		"bad id":          `{"appId":"has space"}`,
		"abs assets":      `{"appId":"a.b","assets":"/etc"}`,
		"escaping assets": `{"appId":"a.b","assets":"../x"}`,
		"escaping bundle": `{"appId":"a.b","hostjs":{"bundle":"../h.js"}}`,
		"no bundle":       `{"appId":"a.b","hostjs":{}}`,
		"escaping binary": `{"appId":"a.b","gohost":{"binary":"a/../../b"}}`,
		"bad pattern":     `{"appId":"a.b","fallbacks":[{"pattern":"/a/[b","file":"x.html"}]}`,
		"no slash":        `{"appId":"a.b","fallbacks":[{"pattern":"a","file":"x.html"}]}`,
		"rest not last":   `{"appId":"a.b","fallbacks":[{"pattern":"/[...a]/b","file":"x.html"}]}`,
		"file escapes":    `{"appId":"a.b","fallbacks":[{"pattern":"/a","file":"../x.html"}]}`,
		"window path":     `{"appId":"a.b","window":{"path":"x"}}`,
		"not json":        `{`,
	}
	for name, src := range cases {
		if _, err := ParseManifest([]byte(src), "app.json"); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestMatcher(t *testing.T) {
	m, err := NewMatcher([]Fallback{
		{"/[...rest]", "catch.html"},
		{"/tasks/[id]", "tasks/[id]/index.html"},
		{"/tasks/new", "tasks/new.html"},
		{"/docs/[[...slug]]", "docs.html"},
		{"/u/[user]/p/[post]", "post.html"},
		{"/", "root.html"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ path, want string }{
		{"/", "root.html"},
		{"/tasks/42", "tasks/[id]/index.html"},
		{"/tasks/new", "tasks/new.html"},
		{"/tasks/42/edit", "catch.html"},
		{"/tasks", "catch.html"},
		{"/docs", "docs.html"},
		{"/docs/a/b/c", "docs.html"},
		{"/u/ann/p/7", "post.html"},
		{"/u/ann/p", "catch.html"},
		{"/anything/else", "catch.html"},
	}
	for _, c := range cases {
		got, ok := m.Match(c.path)
		if !ok || got != c.want {
			t.Errorf("Match(%q) = %q, %v; want %q", c.path, got, ok, c.want)
		}
	}
	m2, _ := NewMatcher([]Fallback{{"/tasks/[id]", "t.html"}})
	if _, ok := m2.Match("/tasks"); ok {
		t.Error("[id] must not match an empty segment")
	}
	if _, ok := m2.Match("/tasks/1/2"); ok {
		t.Error("[id] must not match two segments")
	}
	if _, ok := m2.Match("/other"); ok {
		t.Error("unrelated path matched")
	}
}

func TestDefaultAppDir(t *testing.T) {
	if got := DefaultAppDir("/opt/acme/acme"); got != "/opt/acme/app" {
		t.Errorf("plain: %q", got)
	}
	if got := DefaultAppDir("/Apps/Acme.app/Contents/MacOS/acme"); got != "/Apps/Acme.app/Contents/Resources/app" {
		t.Errorf("bundle: %q", got)
	}
}

type fakeCtl struct {
	calls []string
	fail  string
	quit  bool
}

func (f *fakeCtl) rec(s string) error {
	f.calls = append(f.calls, s)
	if f.fail == s {
		return errFake("boom\nsecond line")
	}
	return nil
}
func (f *fakeCtl) ReloadHostJS() error  { return f.rec("reload") }
func (f *fakeCtl) RestartGoHost() error { return f.rec("restart") }
func (f *fakeCtl) Eval(js string) error { return f.rec("eval " + js) }
func (f *fakeCtl) HMR(m string) error   { return f.rec("hmr " + m) }
func (f *fakeCtl) Quit()                { f.quit = true }

type errFake string

func (e errFake) Error() string { return string(e) }

func TestServeControl(t *testing.T) {
	f := &fakeCtl{fail: "restart"}
	in := "reload-hostjs\nrestart-gohost\neval window.x = 1; foo()\neval\nbogus\n\nquit\nreload-hostjs\n"
	var out bytes.Buffer
	if !ServeControl(strings.NewReader(in), &out, f) {
		t.Error("quit not reported")
	}
	want := "ok\nerror boom | second line\nok\nerror eval needs JavaScript\nerror unknown command \"bogus\"\nok\n"
	if out.String() != want {
		t.Errorf("replies:\n%q\nwant\n%q", out.String(), want)
	}
	if got := strings.Join(f.calls, ";"); got != "reload;restart;eval window.x = 1; foo()" {
		t.Errorf("calls = %q", got)
	}
	if !f.quit {
		t.Error("Quit not called")
	}
	// EOF without quit.
	f = &fakeCtl{}
	if ServeControl(strings.NewReader("reload-hostjs\n"), io.Discard, f) {
		t.Error("EOF reported as quit")
	}
}

// runBrowser starts Run in browser mode and returns its control.
func runBrowser(t *testing.T, dir string, sink *reportSink) (*Control, func()) {
	t.Helper()
	var out bytes.Buffer
	var omu sync.Mutex
	ready := make(chan *Control, 1)
	done := make(chan error, 1)
	go func() {
		done <- Run(dir, RunOptions{
			Browser:      true,
			Dev:          true,
			Out:          lockedBuf{&omu, &out},
			Logf:         t.Logf,
			ExtraHandler: sink.handle,
			OnReady:      func(c *Control) { ready <- c },
		})
	}()
	var ctl *Control
	select {
	case ctl = <-ready:
	case err := <-done:
		t.Fatalf("Run returned early: %v", err)
	case <-time.After(60 * time.Second):
		t.Fatal("not ready")
	}
	omu.Lock()
	line := out.String()
	omu.Unlock()
	if !strings.HasPrefix(line, "url http://127.0.0.1:") || ctl.URL() == "" || !strings.Contains(line, ctl.URL()) {
		t.Fatalf("printed %q, URL %q", line, ctl.URL())
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		ctl.Quit()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run: %v", err)
			}
		case <-time.After(20 * time.Second):
			t.Error("Run did not return after Quit")
		}
	}
	t.Cleanup(stop)
	return ctl, stop
}

type lockedBuf struct {
	mu *sync.Mutex
	b  *bytes.Buffer
}

func (l lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func get(t *testing.T, c *http.Client, u string, mod func(*http.Request)) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		t.Fatal(err)
	}
	if mod != nil {
		mod(req)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func callVia(t *testing.T, c *http.Client, base, fn string, build func(*wire.Encoder)) string {
	t.Helper()
	resp, err := c.Post(base+"/__krate/call", "application/x-krate-frames", bytes.NewReader(frameReq(fn, build)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("call %s: status %d %q", fn, resp.StatusCode, b)
	}
	i := bytes.Index(b, []byte("{"))
	j := bytes.LastIndex(b, []byte("}"))
	return string(b[i : j+1])
}

func TestBrowserMode(t *testing.T) {
	root, cleanup, err := gohosttest.TempDir("shell-")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	app, err := buildTestApp(root, "<!doctype html><title>idx</title>hello", true, 10)
	if err != nil {
		if strings.Contains(err.Error(), "toolchain not found") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	sink := newReportSink()
	ctl, stop := runBrowser(t, app.dir, sink)

	u, _ := url.Parse(ctl.URL())
	base := "http://" + u.Host
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	// No token: refused, on every path.
	for _, p := range []string{"/", "/__krate/signals?names=t.Count", "/__test/ping", "/tasks/1"} {
		if resp, _ := get(t, noFollow, base+p, nil); resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s without token: %d", p, resp.StatusCode)
		}
	}
	// POST without token is refused too.
	resp, err := noFollow.Post(base+"/__krate/call", "application/x-krate-frames", bytes.NewReader(frameReq("h.add", nil)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("POST without token: %d", resp.StatusCode)
	}
	// Wrong token.
	if resp, _ := get(t, noFollow, base+"/?krate_token=nope", nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("wrong token: %d", resp.StatusCode)
	}
	// Wrong cookie.
	if resp, _ := get(t, noFollow, base+"/", func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: tokenCookieName(u.Port()), Value: "nope"})
	}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("wrong cookie: %d", resp.StatusCode)
	}
	// Right token on a wrong Host (DNS rebinding).
	if resp, _ := get(t, noFollow, ctl.URL(), func(r *http.Request) { r.Host = "evil.example:" + u.Port() }); resp.StatusCode != http.StatusForbidden {
		t.Errorf("foreign host: %d", resp.StatusCode)
	}

	// The initial URL sets the cookie and redirects to the clean URL.
	resp, _ = get(t, noFollow, ctl.URL(), nil)
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		t.Fatalf("token visit: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if strings.HasPrefix(c.Name, "krate_token_") {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie = %+v", cookie)
	}

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	if resp, body := get(t, c, ctl.URL(), nil); resp.StatusCode != 200 || !strings.Contains(body, "hello") {
		t.Fatalf("with token: %d %q", resp.StatusCode, body)
	}
	if strings.Contains(ctl.URL(), "krate_token") == false {
		t.Fatal("URL lacks token")
	}
	// Later requests need only the cookie.
	if resp, body := get(t, c, base+"/tasks/42", nil); resp.StatusCode != 200 || !strings.Contains(body, "task page") {
		t.Errorf("fallback: %d %q", resp.StatusCode, body)
	}
	if resp, body := get(t, c, base+"/nothing/here", nil); resp.StatusCode != 404 || !strings.Contains(body, "not found page") {
		t.Errorf("404: %d %q", resp.StatusCode, body)
	}
	if _, body := get(t, c, base+"/__test/ping", nil); body != "pong" {
		t.Errorf("extra handler: %q", body)
	}
	// The header form works for clients without a cookie jar.
	if resp, _ := get(t, noFollow, base+"/", func(r *http.Request) { r.Header.Set("X-Krate-Token", u.Query().Get("krate_token")) }); resp.StatusCode != 200 {
		t.Errorf("header token: %d", resp.StatusCode)
	}

	// Host JS and Go host calls through the same endpoint.
	if got := callVia(t, c, base, "h.add", func(e *wire.Encoder) { e.Int(2); e.Int(3) }); !strings.Contains(got, `"ok":5`) {
		t.Errorf("h.add: %s", got)
	}
	if got := callVia(t, c, base, "t.Add", func(e *wire.Encoder) { e.Int(20); e.Int(22) }); !strings.Contains(got, `"ok":42`) {
		t.Errorf("t.Add: %s", got)
	}
	if got := callVia(t, c, base, "t.Version", nil); !strings.Contains(got, `"ok":"v10"`) {
		t.Errorf("t.Version: %s", got)
	}
	if err := ctl.Eval("1"); err == nil {
		t.Error("Eval in browser mode should fail")
	}

	// Reload host JS: new code answers, a new id appears.
	if err := os.WriteFile(app.dir+"/host/host.js", []byte(hostJSv2), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ctl.ReloadHostJS(); err != nil {
		t.Fatal(err)
	}
	if got := callVia(t, c, base, "h.add", func(e *wire.Encoder) { e.Int(2); e.Int(3) }); !strings.Contains(got, `"ok":1005`) {
		t.Errorf("h.add after reload: %s", got)
	}
	if got := callVia(t, c, base, "h.extra", nil); !strings.Contains(got, `"ok":"new"`) {
		t.Errorf("h.extra after reload: %s", got)
	}
	// A broken bundle is reported and leaves the old code running.
	os.WriteFile(app.dir+"/host/host.js", []byte("(function(){ throw new Error('bad bundle'); })();"), 0o644)
	if err := ctl.ReloadHostJS(); err == nil || !strings.Contains(err.Error(), "bad bundle") {
		t.Errorf("broken reload: %v", err)
	}
	if got := callVia(t, c, base, "h.add", func(e *wire.Encoder) { e.Int(1); e.Int(1) }); !strings.Contains(got, `"ok":1002`) {
		t.Errorf("h.add after failed reload: %s", got)
	}

	// Restart the Go host with a rebuilt binary.
	if _, err := app.buildGoHost(77); err != nil {
		t.Fatal(err)
	}
	if err := ctl.RestartGoHost(); err != nil {
		t.Fatal(err)
	}
	if got := callVia(t, c, base, "t.Version", nil); !strings.Contains(got, `"ok":"v77"`) {
		t.Errorf("t.Version after restart: %s", got)
	}

	stop()
	if _, err := c.Get(base + "/"); err == nil {
		t.Error("server still answering after Quit")
	}
}

func TestRunErrors(t *testing.T) {
	dir := t.TempDir()
	if err := Run(dir, RunOptions{Browser: true, Logf: t.Logf}); err == nil || !strings.Contains(err.Error(), "manifest") {
		t.Errorf("missing manifest: %v", err)
	}
	os.WriteFile(dir+"/app.json", []byte(`{"appId":"a.b"}`), 0o644)
	if err := Run(dir, RunOptions{Browser: true, Logf: t.Logf}); err == nil || !strings.Contains(err.Error(), "assets directory") {
		t.Errorf("missing assets: %v", err)
	}
	os.Mkdir(dir+"/dist", 0o755)
	os.WriteFile(dir+"/app.json", []byte(`{"appId":"a.b","hostjs":{"bundle":"nope.js"}}`), 0o644)
	if err := Run(dir, RunOptions{Browser: true, Logf: t.Logf}); err == nil || !strings.Contains(err.Error(), "host bundle") {
		t.Errorf("missing bundle: %v", err)
	}
	// An id registered by both the host JS and the Go host is an error, not a panic.
	os.WriteFile(dir+"/host.js", []byte(hostJSv1), 0o644)
	os.WriteFile(dir+"/app.json", []byte(`{"appId":"a.b","hostjs":{"bundle":"host.js"},"gohost":{"binary":"nope","funcs":["h.add"]}}`), 0o644)
	if err := Run(dir, RunOptions{Browser: true, Logf: t.Logf}); err == nil {
		t.Error("missing go host binary: no error")
	}
}

func TestParseManifestPermissions(t *testing.T) {
	m, err := ParseManifest([]byte(`{"appId":"a.b","permissions":["fs","kv","os:secrets"]}`), "app.json")
	if err != nil || len(m.Permissions) != 3 {
		t.Fatalf("%+v %v", m, err)
	}
	if _, err := ParseManifest([]byte(`{"appId":"a.b","permissions":["fs","root"]}`), "app.json"); err == nil || !strings.Contains(err.Error(), `"root"`) {
		t.Fatalf("unknown permission: %v", err)
	}
	if m, err := ParseManifest([]byte(`{"appId":"a.b"}`), "app.json"); err != nil || len(m.Permissions) != 0 {
		t.Fatalf("%+v %v", m, err)
	}
}

// The SDK is registered for every app: defaults are open, the rest answers
// permission_denied.
func TestSDKRegisteredByShell(t *testing.T) {
	root, cleanup, err := gohosttest.TempDir("shell-sdk-")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	app, err := buildTestApp(root, "<!doctype html>hello", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("APPDATA", filepath.Join(root, "roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "local"))
	t.Setenv("HOME", root)

	ctl, _ := runBrowser(t, app.dir, newReportSink())
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	get(t, c, ctl.URL(), nil)
	u, _ := url.Parse(ctl.URL())
	base := "http://" + u.Host

	str := func(s string) func(*wire.Encoder) { return func(e *wire.Encoder) { e.String(s) } }
	if got := callVia(t, c, base, "krate/os.info", nil); !strings.Contains(got, `"appId":"dev.krate.shelltest"`) || !strings.Contains(got, `"appVersion":"1.0.0"`) {
		t.Errorf("os.info: %s", got)
	}
	if got := callVia(t, c, base, "krate/kv.keys", nil); !strings.Contains(got, `"ok":[]`) {
		t.Errorf("kv.keys: %s", got)
	}
	if got := callVia(t, c, base, "krate/fs.readText", str("x")); !strings.Contains(got, `"code":"permission_denied"`) || !strings.Contains(got, `"permission":"fs:read"`) {
		t.Errorf("fs.readText: %s", got)
	}
	if got := callVia(t, c, base, "krate/permission.query", str("db")); !strings.Contains(got, `"ok":true`) {
		t.Errorf("permission.query: %s", got)
	}
	if got := callVia(t, c, base, "h.tag", nil); !strings.Contains(got, "host-v1") {
		t.Errorf("host code still works: %s", got)
	}
}

func TestParseManifestMemory(t *testing.T) {
	m, err := ParseManifest([]byte(`{"appId":"a.b"}`), "app.json")
	if err != nil {
		t.Fatal(err)
	}
	if m.MemoryProfile() != memory.Balanced {
		t.Errorf("default profile = %q, want balanced", m.MemoryProfile())
	}
	if pm := platformMemory(m); pm.Profile != "balanced" || !pm.Features.Media || !pm.Features.WebGL || !pm.Features.SpellCheck {
		t.Errorf("a manifest without features must keep every feature on: %+v", pm)
	}
	m, err = ParseManifest([]byte(`{"appId":"a.b","memory":"minimal","features":{"webgl":true},"hostjs":{"bundle":"h.js","memoryMB":48}}`), "app.json")
	if err != nil {
		t.Fatal(err)
	}
	if pm := platformMemory(m); pm.Profile != "minimal" || !pm.Features.WebGL || pm.Features.Media || pm.Features.WebAudio {
		t.Errorf("platformMemory = %+v", pm)
	}
	if m.HostJS.MemoryMB != 48 {
		t.Errorf("memoryMB = %d", m.HostJS.MemoryMB)
	}
	for _, bad := range []string{`{"appId":"a.b","memory":"tiny"}`, `{"appId":"a.b","hostjs":{"bundle":"h.js","memoryMB":-1}}`} {
		if _, err := ParseManifest([]byte(bad), "app.json"); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
}
