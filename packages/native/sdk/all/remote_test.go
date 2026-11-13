package all

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk"
	"github.com/kratejs/krate/packages/native/sdk/internal/fakeplat"
	"github.com/kratejs/krate/packages/native/sdk/internal/sdktest"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/sdk/remote"
	"github.com/kratejs/krate/packages/native/wire"
)

func newRemoteRig(t *testing.T, perms ...string) *pageRig {
	t.Helper()
	return newPageRigWith(t, append([]string{"kv"}, perms...)...)
}

// newPageRigWith is newPageRig under another app id, so these tests do not
// share state with the page tests.
func newPageRigWith(t *testing.T, perms ...string) *pageRig {
	t.Helper()
	isolate(t)
	set, err := permission.Parse(append([]string{"window"}, perms...))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Register(scheme.NewRegistry(), Options{AppID: "com.acme.remote"}); err != nil {
		t.Fatal(err)
	}
	reg := scheme.NewRegistry()
	RegisterAreas(reg, set)
	host := sdk.NewHost()
	plat := fakeplat.New()
	host.Attach(plat)
	t.Cleanup(host.Close)
	RegisterUI(reg, host, set, nil)
	w, err := plat.NewWindow(platform.WindowOptions{Title: "main", Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	host.AddWindow("main", w)
	return &pageRig{host: host, plat: plat, main: w.(*fakeplat.Window), c: sdktest.New(t, reg)}
}

func remoteOpts(url string, more func(e *wire.Encoder)) func(e *wire.Encoder) {
	return func(e *wire.Encoder) {
		e.BeginObject()
		e.Key("url")
		e.String(url)
		if more != nil {
			more(e)
		}
		e.EndObject()
	}
}

func TestRemoteNeedsPermission(t *testing.T) {
	r := newRemoteRig(t)
	for _, id := range []string{"krate/remote.open"} {
		res := r.c.CallFrom("main", id, remoteOpts("https://a.test/", nil))
		if res.ErrCode != "permission_denied" || !strings.Contains(res.ErrData, "remote") {
			t.Errorf("%s: %+v", id, res)
		}
	}
	// remote alone is not enough for rules, the network settings or the answers.
	r = newRemoteRig(t, "remote")
	for id, perm := range map[string]string{
		"krate/remote.setRules":          "remote:intercept",
		"krate/remote.setUserAgent":      "remote:network",
		"krate/remote.setProxy":          "remote:network",
		"krate/remote.clearData":         "remote:network",
		"krate/remote.answerCertificate": "remote:network",
	} {
		res := r.c.CallFrom("main", id, str(""))
		if res.ErrCode != "permission_denied" || !strings.Contains(res.ErrData, perm) {
			t.Errorf("%s: %+v", id, res)
		}
	}
}

// A remote window and a remote view have an empty scope, whatever the caller
// holds.
func TestRemoteViewsHaveNoScope(t *testing.T) {
	r := newRemoteRig(t, "remote")
	if res := r.c.CallFrom("main", "krate/kv.get", str("k")); res.Failed {
		t.Fatalf("the app itself cannot use kv: %+v", res)
	}
	res := r.c.CallFrom("main", "krate/remote.open", remoteOpts("https://a.test/", func(e *wire.Encoder) { e.Key("id"); e.String("shop") }))
	if res.Failed {
		t.Fatalf("open: %+v", res)
	}
	for _, id := range []string{"krate/kv.get", "krate/remote.open", "krate/window.list", "krate/os.info", "krate/remote.capabilities"} {
		res := r.c.CallFrom("shop", id, str("k"))
		if res.ErrCode != "permission_denied" {
			t.Errorf("a remote window called %s: %+v", id, res)
		}
	}
	res = r.c.CallFrom("main", "krate/remote.addView", func(e *wire.Encoder) {
		e.String("")
		remoteOpts("https://b.test/", func(e *wire.Encoder) { e.Key("id"); e.String("pane") })(e)
	})
	if res.Failed {
		t.Fatalf("addView: %+v", res)
	}
	if res := r.c.CallFrom("pane", "krate/kv.get", str("k")); res.ErrCode != "permission_denied" {
		t.Errorf("a remote view called kv: %+v", res)
	}
	// An unknown view and the forged header of a remote page get nothing either.
	if res := r.c.CallFrom("main", "krate/kv.get", str("k")); res.Failed {
		t.Fatalf("main lost its scope: %+v", res)
	}
	if got := len(r.plat.Windows); got != 2 {
		t.Fatalf("%d windows", got)
	}
}

func TestRemoteOpenValidates(t *testing.T) {
	r := newRemoteRig(t, "remote")
	for name, url := range map[string]string{"app": "krate://app/", "file": "file:///etc/passwd", "empty": "", "creds": "https://u:p@a.test/", "js": "javascript:1"} {
		res := r.c.CallFrom("main", "krate/remote.open", remoteOpts(url, nil))
		if res.ErrCode != "bad_args" {
			t.Errorf("%s: %+v", name, res)
		}
	}
	res := r.c.CallFrom("main", "krate/remote.open", remoteOpts("https://a.test/", func(e *wire.Encoder) {
		e.Key("partition")
		e.String("../escape")
	}))
	if res.ErrCode != "bad_args" {
		t.Errorf("partition: %+v", res)
	}
	res = r.c.CallFrom("main", "krate/remote.open", remoteOpts("https://a.test/", func(e *wire.Encoder) { e.Key("id"); e.String("main") }))
	if res.ErrCode != "already_exists" {
		t.Errorf("duplicate id: %+v", res)
	}
	if len(r.plat.Windows) != 1 {
		t.Fatalf("a refused open created a window: %d", len(r.plat.Windows))
	}
}

func TestRemoteNavigateAndPost(t *testing.T) {
	r := newRemoteRig(t, "remote")
	if res := r.c.CallFrom("main", "krate/remote.addView", func(e *wire.Encoder) {
		e.String("")
		remoteOpts("https://a.test/start", func(e *wire.Encoder) {
			e.Key("id")
			e.String("pane")
			e.Key("messages")
			e.Bool(true)
		})(e)
	}); res.Failed {
		t.Fatal(res)
	}
	nav := func(view, url string) sdktest.Result {
		return r.c.CallFrom("main", "krate/remote.navigate", func(e *wire.Encoder) { e.String(view); e.String(url) })
	}
	if res := nav("pane", "https://a.test/next"); res.Failed {
		t.Fatalf("allowed navigation: %+v", res)
	}
	if res := nav("pane", "https://evil.test/"); !res.Failed {
		t.Fatal("navigation off the allowlist")
	}
	if res := nav("pane", "krate://app/"); !res.Failed {
		t.Fatal("navigation to the app origin")
	}
	if res := nav("main", "https://a.test/"); res.ErrCode != "not_found" {
		t.Fatalf("navigating an app view: %+v", res)
	}
	post := func(view, target, data string) sdktest.Result {
		return r.c.CallFrom("main", "krate/remote.post", func(e *wire.Encoder) { e.String(view); e.String(target); e.Raw([]byte(data)) })
	}
	if res := post("pane", "https://a.test", `{"x":1}`); res.Failed {
		t.Fatalf("post: %+v", res)
	}
	if res := post("pane", "", `{"x":1}`); res.ErrCode != "bad_args" {
		t.Fatalf("post without a target origin: %+v", res)
	}
	if res := post("pane", "https://other.test", `1`); !res.Failed {
		t.Fatal("post to the wrong origin")
	}
	if res := post("main", "*", `1`); res.ErrCode != "not_found" {
		t.Fatalf("post to an app view through the remote id: %+v", res)
	}
	posts := r.plat.Remote().Posts
	if len(posts) != 1 || posts[0].Data != `{"x":1}` {
		t.Fatalf("posts %+v", posts)
	}
}

func TestRemoteRulesAndNetwork(t *testing.T) {
	r := newRemoteRig(t, "remote", "remote:intercept", "remote:network")
	res := r.c.CallFrom("main", "krate/remote.setRules", func(e *wire.Encoder) {
		e.String("persist:shop")
		e.BeginArray()
		e.BeginObject()
		e.Key("urls")
		e.BeginArray()
		e.String("*://*.ads.test/*")
		e.EndArray()
		e.Key("action")
		e.String("block")
		e.EndObject()
		e.BeginObject()
		e.Key("urls")
		e.BeginArray()
		e.String("https://a.test/*")
		e.EndArray()
		e.Key("action")
		e.String("headers")
		e.Key("headers")
		e.BeginObject()
		e.Key("X-One")
		e.String("1")
		e.EndObject()
		e.EndObject()
		e.EndArray()
	})
	if res.Failed {
		t.Fatalf("setRules: %+v", res)
	}
	rules := r.plat.Remote().Rules["persist:shop"]
	if len(rules) != 2 || rules[0].Action != caps.RuleBlock || rules[1].Headers["X-One"] != "1" {
		t.Fatalf("rules %+v", rules)
	}
	bad := r.c.CallFrom("main", "krate/remote.setRules", func(e *wire.Encoder) {
		e.String("")
		e.BeginArray()
		e.BeginObject()
		e.Key("urls")
		e.BeginArray()
		e.String("nonsense")
		e.EndArray()
		e.Key("action")
		e.String("block")
		e.EndObject()
		e.EndArray()
	})
	if bad.ErrCode != "bad_args" {
		t.Fatalf("bad rule: %+v", bad)
	}
	if res := r.c.CallFrom("main", "krate/remote.setUserAgent", func(e *wire.Encoder) { e.String("p"); e.String("Agent/1") }); res.Failed {
		t.Fatal(res)
	}
	if r.plat.Remote().UA["p"] != "Agent/1" {
		t.Fatal("user agent not set")
	}
	if res := r.c.CallFrom("main", "krate/remote.setProxy", func(e *wire.Encoder) {
		e.String("p")
		e.BeginObject()
		e.Key("mode")
		e.String("fixed")
		e.Key("url")
		e.String("http://127.0.0.1:3128")
		e.EndObject()
	}); res.Failed {
		t.Fatal(res)
	}
	if res := r.c.CallFrom("main", "krate/remote.setProxy", func(e *wire.Encoder) {
		e.String("p")
		e.BeginObject()
		e.Key("mode")
		e.String("fixed")
		e.Key("url")
		e.String("ftp://x")
		e.EndObject()
	}); res.ErrCode != "bad_args" {
		t.Fatalf("bad proxy: %+v", res)
	}
	res = r.c.CallFrom("main", "krate/remote.capabilities", nil)
	if res.Failed || !strings.Contains(res.OK, `"block":"all"`) || !strings.Contains(res.OK, `"redirect":"navigation"`) {
		t.Fatalf("capabilities %+v", res)
	}
}

func TestRemoteMessagesEventsAndRequests(t *testing.T) {
	r := newRemoteRig(t, "remote", "remote:intercept")
	if res := r.c.CallFrom("main", "krate/remote.addView", func(e *wire.Encoder) {
		e.String("")
		remoteOpts("https://a.test/", func(e *wire.Encoder) {
			e.Key("id")
			e.String("pane")
			e.Key("messages")
			e.Bool(true)
		})(e)
	}); res.Failed {
		t.Fatal(res)
	}
	ref := r.plat.Windows[0].Views()[1].Ref
	var mu sync.Mutex
	var msgs []remote.Message
	var evs []remote.Event
	var reqs []remote.Request
	c1, err := remote.OnMessage(func(m remote.Message) { mu.Lock(); msgs = append(msgs, m); mu.Unlock() })
	if err != nil {
		t.Fatal(err)
	}
	defer c1()
	c2, _ := remote.OnEvent(func(e remote.Event) { mu.Lock(); evs = append(evs, e); mu.Unlock() })
	defer c2()
	c3, _ := remote.OnRequest(func(q remote.Request) { mu.Lock(); reqs = append(reqs, q); mu.Unlock() })
	defer c3()
	time.Sleep(200 * time.Millisecond)
	r.plat.EmitRemote(caps.RemoteEvent{View: ref, Kind: caps.RemoteMessage, Origin: "https://a.test", Data: []byte(`{"hi":true}`)})
	r.plat.EmitRemote(caps.RemoteEvent{View: ref, Kind: caps.RemoteNavBlocked, URL: "krate://app/", Verdict: caps.NavBlock})
	r.plat.EmitRemote(caps.RemoteEvent{View: ref, Kind: caps.RemoteRequest, URL: "https://ads.test/x", Method: "GET", Action: "block"})
	// An event of a view the SDK does not know is dropped, not misattributed.
	r.plat.EmitRemote(caps.RemoteEvent{View: 9999, Kind: caps.RemoteMessage, Origin: "https://x", Data: []byte(`1`)})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := len(msgs) == 1 && len(evs) == 1 && len(reqs) == 1
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(msgs) != 1 || msgs[0].View != "pane" || msgs[0].Origin != "https://a.test" || string(msgs[0].Data) != `{"hi":true}` {
		t.Fatalf("messages %+v", msgs)
	}
	if len(evs) != 1 || evs[0].Kind != "navBlocked" || evs[0].Verdict != "block" {
		t.Fatalf("events %+v", evs)
	}
	if len(reqs) != 1 || reqs[0].Action != "block" || reqs[0].View != "pane" {
		t.Fatalf("requests %+v", reqs)
	}
}

func TestRemoteCertificatesAndAuth(t *testing.T) {
	r := newRemoteRig(t, "remote", "remote:network")
	// Nobody listens: refused at once.
	if ok := <-r.plat.AskCertificate(caps.CertError{Host: "a.test"}); ok {
		t.Fatal("a certificate was accepted without a handler")
	}
	if got := <-r.plat.AskAuth(caps.AuthRequest{Host: "a.test"}); got != "" {
		t.Fatalf("a login was answered without a handler: %q", got)
	}
	var seen []remote.CertificateError
	var mu sync.Mutex
	cancel, err := remote.OnCertificateError(func(e remote.CertificateError) bool {
		mu.Lock()
		seen = append(seen, e)
		mu.Unlock()
		return e.Host == "ok.test"
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	cancel2, err := remote.OnAuth(func(c remote.AuthChallenge) remote.Credentials {
		if c.Realm == "vault" {
			return remote.Credentials{User: "u", Password: "p", OK: true}
		}
		return remote.Credentials{}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cancel2()
	time.Sleep(200 * time.Millisecond)
	for host, want := range map[string]bool{"ok.test": true, "bad.test": false} {
		select {
		case got := <-r.plat.AskCertificate(caps.CertError{Host: host, URL: "https://" + host + "/", Errors: []string{"untrusted"}}):
			if got != want {
				t.Errorf("%s: %v", host, got)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no answer for %s", host)
		}
	}
	select {
	case got := <-r.plat.AskAuth(caps.AuthRequest{Host: "a.test:80", Realm: "vault", Scheme: "basic"}):
		if got != "u:p" {
			t.Errorf("login %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no answer for the login")
	}
	if got := <-r.plat.AskAuth(caps.AuthRequest{Host: "a.test:80", Realm: "other"}); got != "" {
		t.Errorf("a cancelled login gave %q", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[0].Errors[0] != "untrusted" {
		t.Fatalf("certificate errors %+v", seen)
	}
}

// A navigation the allowlist hands to the system opens the browser, at most a
// few times in a burst.
func TestRemoteExternalOpenIsRateLimited(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uses a fake xdg-open")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "opened")
	script := "#!/bin/sh\necho \"$1\" >> " + log + "\n"
	if err := os.WriteFile(filepath.Join(dir, "xdg-open"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	r := newRemoteRig(t, "remote")
	if res := r.c.CallFrom("main", "krate/remote.addView", func(e *wire.Encoder) {
		e.String("")
		remoteOpts("https://a.test/", func(e *wire.Encoder) { e.Key("id"); e.String("pane") })(e)
	}); res.Failed {
		t.Fatal(res)
	}
	ref := r.plat.Windows[0].Views()[1].Ref
	for i := 0; i < 10; i++ {
		r.plat.EmitRemote(caps.RemoteEvent{View: ref, Kind: caps.RemoteNavBlocked, URL: "https://elsewhere.test/" + string(rune('a'+i)), Verdict: caps.NavExternal})
	}
	r.plat.EmitRemote(caps.RemoteEvent{View: ref, Kind: caps.RemoteNavBlocked, URL: "krate://app/", Verdict: caps.NavBlock})
	time.Sleep(time.Second)
	b, _ := os.ReadFile(log)
	lines := strings.Fields(string(b))
	if len(lines) != 3 {
		t.Fatalf("opened %d URLs, want a burst of 3: %v", len(lines), lines)
	}
	for _, l := range lines {
		if !strings.HasPrefix(l, "https://elsewhere.test/") {
			t.Errorf("opened %q", l)
		}
	}
}
