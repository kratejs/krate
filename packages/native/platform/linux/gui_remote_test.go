//go:build linux

package linux

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func (e *remoteEvents) count(pred func(caps.RemoteEvent) bool) (n int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, ev := range e.list {
		if pred(ev) {
			n++
		}
	}
	return
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

func TestGUIRemoteBasics(t *testing.T) {
	needGUI(t)
	site, h := remoteSite(t, "A")
	ev := collectRemote(t)
	w, ref := openRemote(t, ev, caps.RemoteOptions{URL: site.URL + "/", Partition: "basics"})
	if !guiApp.IsRemoteView(ref) {
		t.Fatal("not a remote view")
	}
	if guiApp.IsRemoteView(9999) {
		t.Fatal("unknown view is remote")
	}
	if got := evalIn(t, w, ref, "document.body.textContent"); got != "A home" {
		t.Fatalf("page text %q", got)
	}
	if h.count("/") != 1 {
		t.Fatalf("hits %v", h.list)
	}
	if o := guiApp.RemoteOrigin(ref); o != strings.TrimSuffix(site.URL, "/") {
		t.Fatalf("origin %q, want %q", o, site.URL)
	}
	if guiApp.RemoteOrigin(w.PrimaryView()+100) != "" {
		t.Fatal("origin of an unknown view")
	}
	// A navigation the app asks for obeys the policy too.
	if err := guiApp.NavigateRemote(ref, site.URL+"/two"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, "/two", loadedURL(ref, "/two"))
	if err := guiApp.NavigateRemote(ref, "krate://app/"); err == nil {
		t.Fatal("the app navigated a remote view to the app origin")
	}
	if err := guiApp.NavigateRemote(ref, "https://elsewhere.invalid/"); err == nil {
		t.Fatal("navigated outside the allowlist")
	}
	// Remote views have no bridge: nothing of Krate is defined in the page.
	if got := evalIn(t, w, ref, `Object.getOwnPropertyNames(window).filter(k => /krate/i.test(k)).join(",")`); got != "" {
		t.Fatalf("krate globals in a remote page: %q", got)
	}
}

// The remote page tries everything to reach the app.
func TestGUIRemoteCannotReachApp(t *testing.T) {
	needGUI(t)
	drainPings()
	site, _ := remoteSite(t, "A")
	ev := collectRemote(t)
	w, ref := openRemote(t, ev, caps.RemoteOptions{URL: site.URL + "/", Partition: "sec"})
	js := `(async () => {
  const r = {};
  const tryFetch = async (n, u, init) => { try { const x = await fetch(u, init); r[n] = "status " + x.status; } catch (e) { r[n] = "error"; } };
  await tryFetch("app", "krate://app/ping?name=fetch-app");
  await tryFetch("host", "krate://app/__krate/call", {method: "POST", body: "{}"});
  await tryFetch("hostcors", "krate://app/ping?name=fetch-cors", {mode: "no-cors"});
  await tryFetch("https-localhost", "https://app.localhost/ping?name=fetch-localhost");
  const img = new Image(); img.src = "krate://app/ping?name=img";
  const f = document.createElement("iframe"); f.src = "krate://app/pane?frame"; document.body.appendChild(f);
  r.open = String(window.open("krate://app/pane?open"));
  try { const x = new XMLHttpRequest(); x.open("GET", "krate://app/ping?name=xhr", false); x.send(); r.xhr = "status " + x.status; } catch (e) { r.xhr = "error"; }
  try { navigator.sendBeacon("krate://app/ping?name=beacon", "x"); } catch (e) {}
  r.postMessage = typeof window.webkit;
  r.webkit = window.webkit && window.webkit.messageHandlers ? Object.keys(window.webkit.messageHandlers).join(",") : "none";
  r.parent = String(window.opener);
  window.__res = JSON.stringify(r);
})()`
	evalIn(t, w, ref, js)
	var res string
	waitLong(t, "probe result", func() bool { res = evalIn(t, w, ref, "window.__res || ''"); return res != "" })
	var r map[string]string
	if err := json.Unmarshal([]byte(res), &r); err != nil {
		t.Fatal(err, res)
	}
	for _, k := range []string{"app", "host", "hostcors", "https-localhost"} {
		if r[k] != "error" {
			t.Errorf("fetch %s: %q, want a network error", k, r[k])
		}
	}
	if r["xhr"] != "error" {
		t.Errorf("xhr: %q", r["xhr"])
	}
	if r["webkit"] != "" && r["webkit"] != "none" {
		t.Errorf("message handlers exposed to a view with messages off: %q", r["webkit"])
	}
	time.Sleep(1500 * time.Millisecond)
	select {
	case p := <-viewPings:
		t.Fatalf("a request of the remote page reached the app handler: %+v", p)
	default:
	}
	for _, name := range []string{"?frame", "?open"} {
		select {
		case p := <-viewPings:
			t.Fatalf("app page %s was served to the remote view: %+v", name, p)
		default:
		}
	}
	// Navigations to the app origin are refused and reported.
	evalIn(t, w, ref, `location.href = "krate://app/pane?nav"`)
	ev.wait(t, "blocked app navigation", func(e caps.RemoteEvent) bool {
		return e.Kind == caps.RemoteNavBlocked && strings.HasPrefix(e.URL, "krate://app/") && e.Verdict == caps.NavBlock
	})
	if got := evalIn(t, w, ref, "location.origin"); got != strings.TrimSuffix(site.URL, "/") {
		t.Fatalf("the view left its origin: %q", got)
	}
}

func TestGUIRemoteNavigationRules(t *testing.T) {
	needGUI(t)
	a, ha := remoteSite(t, "A")
	b, hb := remoteSite(t, "B")
	ev := collectRemote(t)
	w, ref := openRemote(t, ev, caps.RemoteOptions{URL: a.URL + "/", Partition: "nav"})
	nWindows := windowCount()
	// Same origin works.
	evalIn(t, w, ref, `location.href = "/two"`)
	ev.wait(t, "same-origin navigation", loadedURL(ref, "/two"))
	// Another origin is handed to the system, not loaded.
	evalIn(t, w, ref, fmt.Sprintf(`location.href = %q`, b.URL+"/two"))
	blocked := ev.wait(t, "external verdict", func(e caps.RemoteEvent) bool {
		return e.Kind == caps.RemoteNavBlocked && strings.HasPrefix(e.URL, b.URL)
	})
	if blocked.Verdict != caps.NavExternal {
		t.Fatalf("verdict %v", blocked.Verdict)
	}
	// window.open follows the same rule, in both directions.
	evalIn(t, w, ref, fmt.Sprintf(`window.open(%q)`, b.URL+"/new"))
	ev.wait(t, "popup to B", func(e caps.RemoteEvent) bool { return e.Kind == caps.RemoteNavBlocked && e.URL == b.URL+"/new" })
	evalIn(t, w, ref, `window.open("/new")`)
	ev.wait(t, "popup to A loads in place", loadedURL(ref, "/new"))
	if n := windowCount(); n != nWindows {
		t.Errorf("a popup created a window: %d windows, had %d", n, nWindows)
	}
	time.Sleep(300 * time.Millisecond)
	if hb.count("/two") != 0 || hb.count("/new") != 0 {
		t.Fatalf("B was contacted: %v", hb.list)
	}
	if ha.count("/two") == 0 {
		t.Fatal("A never saw /two")
	}

	// Block mode drops instead of opening.
	w2, ref2 := openRemote(t, ev, caps.RemoteOptions{URL: a.URL + "/", Partition: "nav2", Blocked: caps.BlockedDrop})
	evalIn(t, w2, ref2, fmt.Sprintf(`location.href = %q`, b.URL+"/two"))
	got := ev.wait(t, "block verdict", func(e caps.RemoteEvent) bool {
		return e.View == ref2 && e.Kind == caps.RemoteNavBlocked && strings.HasPrefix(e.URL, b.URL)
	})
	if got.Verdict != caps.NavBlock {
		t.Fatalf("verdict %v", got.Verdict)
	}
	// An explicit allowlist entry opens the way.
	w3, ref3 := openRemote(t, ev, caps.RemoteOptions{URL: a.URL + "/", Partition: "nav3", Allow: []string{b.URL}})
	evalIn(t, w3, ref3, fmt.Sprintf(`location.href = %q`, b.URL+"/two"))
	ev.wait(t, "allowed B", loadedURL(ref3, "/two"))
	if hb.count("/two") != 1 {
		t.Fatalf("B hits: %v", hb.list)
	}
}

func TestGUIRemoteRedirectOffOrigin(t *testing.T) {
	needGUI(t)
	b, hb := remoteSite(t, "B")
	mux := http.NewServeMux()
	mux.HandleFunc("/", page("A home"))
	mux.HandleFunc("/go", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, b.URL+"/new", http.StatusFound) })
	a := httptest.NewServer(mux)
	defer a.Close()
	ev := collectRemote(t)
	w, ref := openRemote(t, ev, caps.RemoteOptions{URL: a.URL + "/", Partition: "nav4"})
	evalIn(t, w, ref, `location.href = "/go"`)
	ev.wait(t, "redirect blocked", func(e caps.RemoteEvent) bool {
		return e.Kind == caps.RemoteNavBlocked && e.URL == b.URL+"/new"
	})
	time.Sleep(300 * time.Millisecond)
	if hb.count("/new") != 0 {
		t.Fatal("the redirect target was fetched")
	}
}

// partitionPass counts the runs of TestGUIRemotePartitions in this process.
var partitionPass int

func TestGUIRemotePartitions(t *testing.T) {
	needGUI(t)
	// In-memory partitions live as long as the app, so each pass of a
	// repeated run (-count) uses partition names of its own.
	partitionPass++
	pa := fmt.Sprintf("pa%d", partitionPass)
	site, _ := remoteSite(t, "A")
	ev := collectRemote(t)
	set := `document.cookie = "mine=1; path=/"; localStorage.setItem("k", "v"); document.cookie + "|" + localStorage.getItem("k")`
	read := `document.cookie + "|" + localStorage.getItem("k")`
	open := func(part string) (*Window, caps.ViewRef) {
		return openRemote(t, ev, caps.RemoteOptions{URL: site.URL + "/", Partition: part})
	}
	wa, ra := open("persist:" + pa)
	if got := evalIn(t, wa, ra, set); got != "mine=1|v" {
		t.Fatalf("set: %q", got)
	}
	wb, rb := open(fmt.Sprintf("persist:pb%d", partitionPass))
	if got := evalIn(t, wb, rb, read); got != "|null" {
		t.Fatalf("partition B sees A's data: %q", got)
	}
	wd, rd := open("")
	if got := evalIn(t, wd, rd, read); got != "|null" {
		t.Fatalf("the default partition sees A's data: %q", got)
	}
	// The same partition shares.
	wa2, ra2 := open("persist:" + pa)
	if got := evalIn(t, wa2, ra2, read); got != "mine=1|v" {
		t.Fatalf("partition A does not share with itself: %q", got)
	}
	// The app's own storage has none of it.
	cookies, err := guiApp.Cookies(site.URL)
	if err == nil {
		for _, c := range cookies {
			if c.Name == "mine" {
				t.Fatalf("the app cookie jar has the remote cookie: %+v", c)
			}
		}
	}
	app := memWindow(t, "/pane?part")
	defer app.Close()
	// In-memory partitions are separate from the persistent one of the same name.
	wm, rm := open(pa)
	if got := evalIn(t, wm, rm, read); got != "|null" {
		t.Fatalf("memory partition sees the persistent one: %q", got)
	}
	evalIn(t, wm, rm, set)
	wm2, rm2 := open(pa)
	if got := evalIn(t, wm2, rm2, read); got != "mine=1|v" {
		t.Fatalf("memory partition lost its data between windows: %q", got)
	}
	// Persistent partitions have a directory of their own; in-memory ones live
	// in a per-process temporary directory and keep no cookie file.
	if _, err := os.Stat(filepath.Join(guiApp.dataDir, "remote-"+pa)); err != nil {
		t.Errorf("no directory for the persistent partition: %v", err)
	}
	memDir := filepath.Join(guiApp.dataDir, memPartitionsDir, fmt.Sprintf("%d-%s", os.Getpid(), pa))
	if _, err := os.Stat(memDir); err != nil {
		t.Errorf("no temporary directory for the memory partition: %v", err)
	}
	if _, err := os.Stat(filepath.Join(memDir, "cookies.sqlite")); err == nil {
		t.Error("a memory partition keeps a cookie file")
	}
	// Clearing one partition leaves the others.
	if err := guiApp.ClearPartition("persist:"+pa, caps.ClearSiteDataOptions{}); err != nil {
		t.Fatal(err)
	}
	evalIn(t, wa2, ra2, "location.reload()")
	ev.wait(t, "reload", func(e caps.RemoteEvent) bool { return e.Kind == caps.RemoteLoaded && e.View == ra2 && e.URL != "" })
	time.Sleep(500 * time.Millisecond)
	if got := evalIn(t, wa2, ra2, "document.cookie"); got != "" {
		t.Fatalf("cookies survived ClearPartition: %q", got)
	}
	if got := evalIn(t, wm2, rm2, read); got != "mine=1|v" {
		t.Fatalf("clearing partition A cleared the memory partition: %q", got)
	}
	_ = wb
	_ = wd
	_ = wa
}

func TestGUIRemoteUserAgent(t *testing.T) {
	needGUI(t)
	site, h := remoteSite(t, "A")
	ev := collectRemote(t)
	if err := guiApp.SetUserAgent("uaone", "KrateTest/1.0 (partition one)"); err != nil {
		t.Fatal(err)
	}
	w, ref := openRemote(t, ev, caps.RemoteOptions{URL: site.URL + "/hdr", Partition: "uaone"})
	if got := evalIn(t, w, ref, "navigator.userAgent"); got != "KrateTest/1.0 (partition one)" {
		t.Fatalf("navigator.userAgent = %q", got)
	}
	if x, _ := h.last("/hdr"); x.ua != "KrateTest/1.0 (partition one)" {
		t.Fatalf("server saw %q", x.ua)
	}
	// Another partition keeps the engine's.
	w2, ref2 := openRemote(t, ev, caps.RemoteOptions{URL: site.URL + "/hdr", Partition: "uatwo"})
	if got := evalIn(t, w2, ref2, "navigator.userAgent"); strings.HasPrefix(got, "KrateTest") || got == "" {
		t.Fatalf("partition two UA %q", got)
	}
	// A per-view override beats the partition's; changing the partition
	// reaches live views without one.
	w3, ref3 := openRemote(t, ev, caps.RemoteOptions{URL: site.URL + "/hdr", Partition: "uaone", UserAgent: "ViewOverride/2"})
	if got := evalIn(t, w3, ref3, "navigator.userAgent"); got != "ViewOverride/2" {
		t.Fatalf("override: %q", got)
	}
	if err := guiApp.SetUserAgent("uaone", "KrateTest/2.0"); err != nil {
		t.Fatal(err)
	}
	evalIn(t, w, ref, "location.reload()")
	waitLong(t, "new UA", func() bool { return evalIn(t, w, ref, "navigator.userAgent") == "KrateTest/2.0" })
	if got := evalIn(t, w3, ref3, "navigator.userAgent"); got != "ViewOverride/2" {
		t.Fatalf("override lost: %q", got)
	}
	if guiApp.SetUserAgent("uaone", "bad\r\nX: y") == nil {
		t.Fatal("a user agent with a line break was accepted")
	}
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

func TestGUIRemoteMessages(t *testing.T) {
	needGUI(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/", page(`<script>
window.got = [];
window.addEventListener("message", e => got.push({data: e.data, origin: e.origin}));
</script>`))
	mux.HandleFunc("/frame", page(`<script>
try { window.webkit.messageHandlers.krateRemote.postMessage("forged\n{\"from\":\"frame\"}"); } catch (e) {}
</script>`))
	mux.HandleFunc("/withframe", page(`<iframe src="/frame"></iframe>`))
	site := httptest.NewServer(mux)
	defer site.Close()
	ev := collectRemote(t)
	w, ref := openRemote(t, ev, caps.RemoteOptions{URL: site.URL + "/", Partition: "msg", Messages: true})
	origin := strings.TrimSuffix(site.URL, "/")

	evalIn(t, w, ref, `krateRemote.postMessage({hello: "app", n: 1})`)
	m := ev.wait(t, "message", func(e caps.RemoteEvent) bool { return e.Kind == caps.RemoteMessage })
	if m.Origin != origin || m.View != ref || string(m.Data) != `{"hello":"app","n":1}` {
		t.Fatalf("message %+v", m)
	}
	// The page cannot change or fake the global.
	if got := evalIn(t, w, ref, `(() => { try { window.krateRemote = 1; } catch (e) {} return typeof krateRemote.postMessage; })()`); got != "function" {
		t.Fatalf("krateRemote replaced: %q", got)
	}
	// App to page: delivered with the app's origin, only to the right origin.
	if err := guiApp.PostToRemote(ref, origin, []byte(`{"reply":true}`)); err != nil {
		t.Fatal(err)
	}
	waitLong(t, "message in page", func() bool { return evalIn(t, w, ref, "got.length") == "1" })
	if got := evalIn(t, w, ref, "JSON.stringify(got[0])"); got != `{"data":{"reply":true},"origin":"krate://app"}` {
		t.Fatalf("page got %s", got)
	}
	if err := guiApp.PostToRemote(ref, "https://other.example", []byte(`{"x":1}`)); err == nil {
		t.Fatal("posted to the wrong target origin")
	}
	if err := guiApp.PostToRemote(ref, "*", []byte(`{"wild":1}`)); err != nil {
		t.Fatal(err)
	}
	waitLong(t, "wildcard message", func() bool { return evalIn(t, w, ref, "got.length") == "2" })
	if err := guiApp.PostToRemote(ref, origin, []byte(strings.Repeat("1", caps.MaxRemoteMessage+1))); err == nil {
		t.Fatal("oversized message accepted")
	}
	// A message from a frame without the view's token is dropped.
	before := ev.count(func(e caps.RemoteEvent) bool { return e.Kind == caps.RemoteMessage })
	if err := guiApp.NavigateRemote(ref, site.URL+"/withframe"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, "frame page", loadedURL(ref, "/withframe"))
	time.Sleep(1000 * time.Millisecond)
	if n := ev.count(func(e caps.RemoteEvent) bool { return e.Kind == caps.RemoteMessage }); n != before {
		t.Fatalf("a forged message from an iframe arrived (%d new)", n-before)
	}

	// Messages are opt in: no global, no delivery.
	w2, ref2 := openRemote(t, ev, caps.RemoteOptions{URL: site.URL + "/", Partition: "msg2"})
	if got := evalIn(t, w2, ref2, "typeof window.krateRemote"); got != "undefined" {
		t.Fatalf("krateRemote in a view without messages: %q", got)
	}
	if err := guiApp.PostToRemote(ref2, "*", []byte(`1`)); err == nil {
		t.Fatal("posted to a view without messages")
	}
	if err := guiApp.PostToRemote(9999, "*", []byte(`1`)); err == nil {
		t.Fatal("posted to a view that does not exist")
	}
}

func TestGUIRemoteViewInAppWindow(t *testing.T) {
	needGUI(t)
	site, _ := remoteSite(t, "A")
	ev := collectRemote(t)
	app := memWindow(t, "/pane?host")
	defer app.Close()
	ref, err := app.AddRemoteView(caps.ViewOptions{}, caps.RemoteOptions{URL: site.URL + "/", Partition: "inwin"})
	if err != nil {
		t.Fatal(err)
	}
	ev.wait(t, "remote view loaded", loadedURL(ref, "/"))
	if !guiApp.IsRemoteView(ref) || guiApp.IsRemoteView(app.PrimaryView()) {
		t.Fatal("IsRemoteView")
	}
	if got, ok := guiApp.WindowOfView(ref); !ok || got != app.Ref() {
		t.Fatalf("WindowOfView = %d, %v", got, ok)
	}
	if got := evalIn(t, app, ref, "document.body.textContent"); got != "A home" {
		t.Fatalf("remote page %q", got)
	}
	if got := evalIn(t, app, app.PrimaryView(), "location.origin"); got != "krate://app" {
		t.Fatalf("app view origin %q", got)
	}
	// The app view cannot be navigated away by the remote one and the two
	// do not share storage.
	evalIn(t, app, app.PrimaryView(), `localStorage.setItem("app", "1")`)
	if got := evalIn(t, app, ref, `String(localStorage.getItem("app"))`); got != "null" {
		t.Fatalf("remote view sees app storage: %q", got)
	}
	if err := app.RemoveView(ref); err != nil {
		t.Fatal(err)
	}
	if guiApp.IsRemoteView(ref) {
		t.Fatal("removed view is still remote")
	}
	// App windows are never created from a remote view's web process.
	rw, _ := openRemote(t, ev, caps.RemoteOptions{URL: site.URL + "/", Partition: "rel"})
	_ = rw
	w2 := memWindow(t, "/pane?after")
	defer w2.Close()
	if got := evalIn(t, w2, w2.PrimaryView(), "location.origin"); got != "krate://app" {
		t.Fatalf("an app window opened after a remote one has origin %q", got)
	}
}

func windowCount() (n int) {
	guiApp.callMain(func() {
		guiApp.mu.Lock()
		n = len(guiApp.windows)
		guiApp.mu.Unlock()
	})
	return
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

func TestGUIRemoteAuth(t *testing.T) {
	needGUI(t)
	h := &hits{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.add(r)
		u, p, ok := r.BasicAuth()
		if !ok || u != "alice" || p != "secret" {
			w.Header().Set("WWW-Authenticate", `Basic realm="vault"`)
			w.WriteHeader(401)
			fmt.Fprint(w, "<!doctype html><body>denied")
			return
		}
		page("welcome "+u)(w, r)
	}))
	defer srv.Close()
	ev := collectRemote(t)
	var mu sync.Mutex
	var reqs []caps.AuthRequest
	tries := []struct {
		user, pass string
		ok         bool
	}{{"alice", "wrong", true}, {"alice", "secret", true}}
	guiApp.SetAuthHandler(func(r caps.AuthRequest, answer func(string, string, bool)) {
		mu.Lock()
		i := len(reqs)
		reqs = append(reqs, r)
		mu.Unlock()
		if i >= len(tries) {
			answer("", "", false)
			return
		}
		answer(tries[i].user, tries[i].pass, tries[i].ok)
	})
	defer guiApp.SetAuthHandler(nil)
	w, ref := openRemote(t, ev, caps.RemoteOptions{URL: srv.URL + "/", Partition: "auth1"})
	if got := evalIn(t, w, ref, "document.body.textContent"); got != "welcome alice" {
		t.Fatalf("page %q", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reqs) != 2 || reqs[0].Realm != "vault" || reqs[0].Scheme != "basic" || reqs[0].Retry || !reqs[1].Retry {
		t.Fatalf("auth requests %+v", reqs)
	}
}

func TestGUIRemoteAuthCancelled(t *testing.T) {
	needGUI(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
		w.WriteHeader(401)
		fmt.Fprint(w, "<!doctype html><body>denied")
	}))
	defer srv.Close()
	ev := collectRemote(t)
	// No handler: the challenge is cancelled and nothing is sent twice.
	w, ref := openRemote(t, ev, caps.RemoteOptions{URL: srv.URL + "/", Partition: "auth2"})
	time.Sleep(500 * time.Millisecond)
	if got := evalIn(t, w, ref, "document.body ? document.body.textContent : ''"); got == "welcome" {
		t.Fatalf("page %q", got)
	}
}

func TestGUIRemoteProxy(t *testing.T) {
	needGUI(t)
	h := &hits{}
	var mu sync.Mutex
	var hosts []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hosts = append(hosts, r.Host)
		mu.Unlock()
		h.add(r)
		page("via proxy "+r.Host)(w, r)
	}))
	defer proxy.Close()
	if err := guiApp.SetProxy("prx", caps.ProxyConfig{Mode: caps.ProxyFixed, URL: proxy.URL}); err != nil {
		t.Fatal(err)
	}
	if guiApp.SetProxy("prx", caps.ProxyConfig{Mode: caps.ProxyFixed, URL: "gopher://x"}) == nil {
		t.Fatal("bad proxy accepted")
	}
	ev := collectRemote(t)
	w, ref := openRemote(t, ev, caps.RemoteOptions{URL: "http://remote.proxy-test.invalid/page", Partition: "prx"})
	if got := evalIn(t, w, ref, "document.body.textContent"); got != "via proxy remote.proxy-test.invalid" {
		t.Fatalf("page %q", got)
	}
	// Another partition does not use it (the name does not resolve).
	site, _ := remoteSite(t, "A")
	w2, ref2 := openRemote(t, ev, caps.RemoteOptions{URL: site.URL + "/", Partition: "prx-other"})
	if got := evalIn(t, w2, ref2, "document.body.textContent"); got != "A home" {
		t.Fatalf("page %q", got)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, host := range hosts {
		if strings.HasPrefix(host, "127.0.0.1") {
			t.Fatalf("the other partition went through the proxy: %v", hosts)
		}
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

// writeClientCert makes a self-signed client certificate and returns the PEM
// file paths.
func writeClientCert(t *testing.T, cn string) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile, keyFile = filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		t.Fatal(err)
	}
	return
}

func TestGUIRemoteClientCertificate(t *testing.T) {
	needGUI(t)
	var mu sync.Mutex
	var cns []string
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cn := ""
		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			cn = r.TLS.PeerCertificates[0].Subject.CommonName
		}
		mu.Lock()
		cns = append(cns, cn)
		mu.Unlock()
		page("hello "+cn)(w, r)
	}))
	srv.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert}
	srv.StartTLS()
	defer srv.Close()
	certFile, keyFile := writeClientCert(t, "krate-client")
	if guiApp.SetClientCertificate("mtls", "127.0.0.1", certFile, certFile+".missing") == nil {
		t.Fatal("a missing key file was accepted")
	}
	if err := guiApp.SetClientCertificate("mtls", "127.0.0.1", certFile, keyFile); err != nil {
		t.Fatal(err)
	}
	guiApp.SetCertificateHandler(func(e caps.CertError, answer func(bool)) { answer(true) })
	defer guiApp.SetCertificateHandler(nil)
	ev := collectRemote(t)
	w, ref := openRemote(t, ev, caps.RemoteOptions{URL: srv.URL + "/", Partition: "mtls"})
	waitLong(t, "the page behind the client certificate", func() bool {
		return evalIn(t, w, ref, "document.body ? document.body.textContent : ''") == "hello krate-client"
	})
	// A partition without the certificate gets no page.
	w2, _ := guiApp.NewRemoteWindow(caps.RemoteWindowSpec{Title: "x", Width: 300, Height: 200, Remote: caps.RemoteOptions{URL: srv.URL + "/other", Partition: "mtls-none"}})
	defer w2.Close()
	time.Sleep(2 * time.Second)
	mu.Lock()
	defer mu.Unlock()
	for _, cn := range cns {
		if cn != "krate-client" {
			t.Fatalf("a request without the certificate reached the handler: %q", cns)
		}
	}
}
