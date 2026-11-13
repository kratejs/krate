package shell

import (
	"bytes"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/internal/gohosttest"
	"github.com/kratejs/krate/packages/native/wire"
)

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
	if err := os.WriteFile(app.dir+"/host/host.js", []byte("(function(){ throw new Error('bad bundle'); })();"), 0o644); err != nil {
		t.Fatal(err)
	}
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
