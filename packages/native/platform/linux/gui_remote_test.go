//go:build linux

package linux

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// GUI tests of remote content: the security rules, partitions, interception,
// user agent, proxy, TLS, authentication and the message channel. The remote
// pages come from local httptest servers.

// hits records the requests a server saw.
type hits struct {
	mu   sync.Mutex
	list []hit
}

type hit struct {
	path, query, ua string
	hdr             http.Header
}

func (h *hits) add(r *http.Request) {
	h.mu.Lock()
	h.list = append(h.list, hit{path: r.URL.Path, query: r.URL.RawQuery, ua: r.UserAgent(), hdr: r.Header.Clone()})
	h.mu.Unlock()
}

func (h *hits) count(path string) (n int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, x := range h.list {
		if x.path == path {
			n++
		}
	}
	return
}

func (h *hits) last(path string) (hit, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.list) - 1; i >= 0; i-- {
		if h.list[i].path == path {
			return h.list[i], true
		}
	}
	return hit{}, false
}

func page(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<!doctype html><title>remote</title><body>"+body)
	}
}

// remoteSite is a small web site for the tests.
func remoteSite(t *testing.T, name string) (*httptest.Server, *hits) {
	t.Helper()
	h := &hits{}
	mux := http.NewServeMux()
	wrap := func(p string, f http.HandlerFunc) {
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) { h.add(r); f(w, r) })
	}
	wrap("/", page(name+" home"))
	wrap("/two", page(name+" two"))
	wrap("/cookie", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "who", Value: name, Path: "/", MaxAge: 3600})
		page("cookie")(w, r)
	})
	wrap("/ads/a.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, "window.__ad = 'loaded';")
	})
	wrap("/ads/pixel.png", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	wrap("/res", page(`<script src="/ads/a.js"></script><img id=i src="/ads/pixel.png"><script src="/ok.js"></script>`))
	wrap("/ok.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, "window.__ok = 'loaded';")
	})
	wrap("/old", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/new", http.StatusFound) })
	wrap("/new", page(name+" new"))
	wrap("/hdr", func(w http.ResponseWriter, r *http.Request) {
		page("headers:"+r.Header.Get("X-Krate-Test")+" ua:"+r.UserAgent())(w, r)
	})
	wrap("/ping", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, h
}

type remoteEvents struct {
	mu   sync.Mutex
	list []caps.RemoteEvent
}

func collectRemote(t *testing.T) *remoteEvents {
	t.Helper()
	e := &remoteEvents{}
	guiApp.OnRemoteEvent(func(ev caps.RemoteEvent) {
		e.mu.Lock()
		e.list = append(e.list, ev)
		e.mu.Unlock()
	})
	t.Cleanup(func() { guiApp.OnRemoteEvent(nil) })
	return e
}

func (e *remoteEvents) find(pred func(caps.RemoteEvent) bool) (caps.RemoteEvent, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, ev := range e.list {
		if pred(ev) {
			return ev, true
		}
	}
	return caps.RemoteEvent{}, false
}

func (e *remoteEvents) wait(t *testing.T, what string, pred func(caps.RemoteEvent) bool) caps.RemoteEvent {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if ev, ok := e.find(pred); ok {
			return ev
		}
		time.Sleep(25 * time.Millisecond)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	t.Fatalf("timed out waiting for %s; events: %+v", what, e.list)
	return caps.RemoteEvent{}
}

func loadedURL(view caps.ViewRef, suffix string) func(caps.RemoteEvent) bool {
	return func(ev caps.RemoteEvent) bool {
		return ev.Kind == caps.RemoteLoaded && (view == 0 || ev.View == view) && strings.HasSuffix(ev.URL, suffix)
	}
}

// openRemote opens a remote window and waits for its first page.
func openRemote(t *testing.T, ev *remoteEvents, o caps.RemoteOptions) (*Window, caps.ViewRef) {
	t.Helper()
	rw, err := guiApp.NewRemoteWindow(caps.RemoteWindowSpec{Title: "remote", Width: 500, Height: 400, Resizable: true, Remote: o})
	if err != nil {
		t.Fatal(err)
	}
	w := rw.(*Window)
	t.Cleanup(func() {
		if w.alive {
			w.Close()
		}
	})
	ev.wait(t, "the first page of "+o.URL, func(e caps.RemoteEvent) bool { return e.Kind == caps.RemoteLoaded && e.View == w.PrimaryView() })
	return w, w.PrimaryView()
}

func TestGUIRemoteInterception(t *testing.T) {
	needGUI(t)
	site, h := remoteSite(t, "A")
	ev := collectRemote(t)
	c := guiApp.RemoteCapabilities()
	if c.Block != caps.LevelAll || c.Observe != caps.LevelAll || c.Redirect != caps.LevelNavigation || c.RequestHeaders != caps.LevelNavigation {
		t.Fatalf("capabilities %+v", c)
	}
	rules := []caps.RequestRule{
		{URLs: []string{"*://*/ads/*"}, Action: caps.RuleBlock},
		{URLs: []string{"*://*/old"}, Action: caps.RuleRedirect, Redirect: site.URL + "/two"},
		{URLs: []string{"*://*/hdr"}, Action: caps.RuleHeaders, Headers: map[string]string{"X-Krate-Test": "injected"}},
		{URLs: []string{"*://*/blocked-nav"}, Action: caps.RuleBlock},
	}
	if err := guiApp.SetRequestRules("icpt", rules); err != nil {
		t.Fatal(err)
	}
	w, ref := openRemote(t, ev, caps.RemoteOptions{URL: site.URL + "/res", Partition: "icpt"})
	waitLong(t, "ok.js", func() bool { return evalIn(t, w, ref, "window.__ok || ''") == "loaded" })
	if h.count("/ok.js") != 1 {
		t.Fatalf("/ok.js hits %d", h.count("/ok.js"))
	}
	time.Sleep(500 * time.Millisecond)
	if h.count("/ads/a.js") != 0 || h.count("/ads/pixel.png") != 0 {
		t.Fatalf("blocked subresources were fetched: %v", h.list)
	}
	if got := evalIn(t, w, ref, "window.__ad || ''"); got != "" {
		t.Fatalf("the blocked script ran: %q", got)
	}
	// Observation sees the allowed request.
	ev.wait(t, "observed /ok.js", func(e caps.RemoteEvent) bool {
		return e.Kind == caps.RemoteRequest && strings.HasSuffix(e.URL, "/ok.js") && e.Action == ""
	})
	// A navigation redirect rule.
	evalIn(t, w, ref, `location.href = "/old"`)
	ev.wait(t, "redirected to /two", loadedURL(ref, "/two"))
	if h.count("/old") != 0 {
		t.Fatal("/old was requested")
	}
	// A navigation header rule.
	evalIn(t, w, ref, `location.href = "/hdr"`)
	ev.wait(t, "/hdr", loadedURL(ref, "/hdr"))
	if x, ok := h.last("/hdr"); !ok || x.hdr.Get("X-Krate-Test") != "injected" {
		t.Fatalf("header rule: %+v", x)
	}
	// A navigation block rule.
	evalIn(t, w, ref, `location.href = "/blocked-nav"`)
	ev.wait(t, "blocked nav", func(e caps.RemoteEvent) bool {
		return e.Kind == caps.RemoteRequest && strings.HasSuffix(e.URL, "/blocked-nav") && e.Action == "block"
	})
	if h.count("/blocked-nav") != 0 {
		t.Fatal("blocked navigation was fetched")
	}
	// Rules are per partition: another partition loads the ads.
	w2, ref2 := openRemote(t, ev, caps.RemoteOptions{URL: site.URL + "/res", Partition: "icpt-other"})
	waitLong(t, "ads in other partition", func() bool { return evalIn(t, w2, ref2, "window.__ad || ''") == "loaded" })
	// Replacing the rules with none lets new loads through.
	if err := guiApp.SetRequestRules("icpt", nil); err != nil {
		t.Fatal(err)
	}
	evalIn(t, w, ref, `location.href = "/res"`)
	waitLong(t, "ads after clearing rules", func() bool { return evalIn(t, w, ref, "window.__ad || ''") == "loaded" })
	// Bad rules are refused and change nothing.
	if guiApp.SetRequestRules("icpt", []caps.RequestRule{{URLs: []string{"bogus"}, Action: caps.RuleBlock}}) == nil {
		t.Fatal("invalid rule accepted")
	}
}

func TestGUIRemoteTLS(t *testing.T) {
	needGUI(t)
	h := &hits{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.add(r)
		page("secure")(w, r)
	}))
	defer srv.Close()
	ev := collectRemote(t)
	// Default: a certificate the engine does not trust ends the load.
	rw, err := guiApp.NewRemoteWindow(caps.RemoteWindowSpec{Title: "tls", Width: 300, Height: 200, Remote: caps.RemoteOptions{URL: srv.URL + "/", Partition: "tls1"}})
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()
	time.Sleep(2 * time.Second)
	if h.count("/") != 0 {
		t.Fatal("the page of a server with an untrusted certificate was fetched")
	}
	// A handler can refuse too.
	var mu sync.Mutex
	var seen []caps.CertError
	allow := false
	guiApp.SetCertificateHandler(func(e caps.CertError, answer func(bool)) {
		mu.Lock()
		seen = append(seen, e)
		a := allow
		mu.Unlock()
		answer(a)
	})
	defer guiApp.SetCertificateHandler(nil)
	rw2, err := guiApp.NewRemoteWindow(caps.RemoteWindowSpec{Title: "tls", Width: 300, Height: 200, Remote: caps.RemoteOptions{URL: srv.URL + "/", Partition: "tls2"}})
	if err != nil {
		t.Fatal(err)
	}
	defer rw2.Close()
	waitLong(t, "certificate callback", func() bool { mu.Lock(); defer mu.Unlock(); return len(seen) > 0 })
	mu.Lock()
	e := seen[0]
	mu.Unlock()
	if e.Host != "127.0.0.1" || !strings.HasPrefix(e.URL, srv.URL) || len(e.Errors) == 0 {
		t.Fatalf("cert error %+v", e)
	}
	time.Sleep(time.Second)
	if h.count("/") != 0 {
		t.Fatal("a refused certificate still loaded the page")
	}
	// Allowing it loads the page, for that partition only.
	mu.Lock()
	allow = true
	mu.Unlock()
	w3, ref3 := openRemote(t, ev, caps.RemoteOptions{URL: srv.URL + "/", Partition: "persist:tlsA"})
	waitLong(t, "the page after allowing the certificate", func() bool { return evalIn(t, w3, ref3, "document.body ? document.body.textContent : ''") == "secure" })
	if h.count("/") != 1 {
		t.Fatalf("hits %d", h.count("/"))
	}
	// The allowance belongs to the partition: the same server in another
	// one (persistent or in memory) raises the error again.
	for _, part := range []string{"persist:tlsB", "tlsmem"} {
		mu.Lock()
		allow = false
		n := len(seen)
		mu.Unlock()
		wx, err := guiApp.NewRemoteWindow(caps.RemoteWindowSpec{Title: "tls", Width: 300, Height: 200, Remote: caps.RemoteOptions{URL: srv.URL + "/two", Partition: part}})
		if err != nil {
			t.Fatal(err)
		}
		waitLong(t, "certificate callback of "+part, func() bool { mu.Lock(); defer mu.Unlock(); return len(seen) > n })
		wx.Close()
	}
	if h.count("/two") != 0 {
		t.Fatal("the allowance leaked into another partition")
	}
}

// waitLong is waitFor with a timeout that survives a loaded machine.
func waitLong(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
