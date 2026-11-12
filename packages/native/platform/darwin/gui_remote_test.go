//go:build darwin

package darwin

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

// GUI tests of remote content on WKWebView. They run with the other GUI tests
// (KRATE_NATIVE_GUI=1) in the CI native job on macos-latest. The policy,
// partition and rule logic they rely on is unit-tested in platform/caps.

type remoteLog struct {
	mu   sync.Mutex
	list []caps.RemoteEvent
}

func collectRemote(t *testing.T) *remoteLog {
	t.Helper()
	l := &remoteLog{}
	guiApp.OnRemoteEvent(func(e caps.RemoteEvent) {
		l.mu.Lock()
		l.list = append(l.list, e)
		l.mu.Unlock()
	})
	t.Cleanup(func() { guiApp.OnRemoteEvent(nil) })
	return l
}

func (l *remoteLog) wait(t *testing.T, what string, pred func(caps.RemoteEvent) bool) caps.RemoteEvent {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		l.mu.Lock()
		for _, e := range l.list {
			if pred(e) {
				l.mu.Unlock()
				return e
			}
		}
		l.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
	return caps.RemoteEvent{}
}

func remotePage(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<!doctype html><title>remote</title><body>"+body)
	}
}

func openRemote(t *testing.T, l *remoteLog, o caps.RemoteOptions) (*Window, caps.ViewRef) {
	t.Helper()
	rw, err := guiApp.NewRemoteWindow(caps.RemoteWindowSpec{Title: "remote", Width: 500, Height: 400, Resizable: true, Remote: o})
	if err != nil {
		t.Fatal(err)
	}
	w := rw.(*Window)
	t.Cleanup(func() { w.Close() })
	l.wait(t, "the first page of "+o.URL, func(e caps.RemoteEvent) bool {
		return e.Kind == caps.RemoteLoaded && e.View == w.PrimaryView()
	})
	return w, w.PrimaryView()
}

// The remote page tries every way to reach the app; nothing arrives.
func TestGUIRemoteCannotReachApp(t *testing.T) {
	requireGUI(t)
	site := httptest.NewServer(remotePage("remote"))
	defer site.Close()
	l := collectRemote(t)
	w, ref := openRemote(t, l, caps.RemoteOptions{URL: site.URL + "/", Partition: "sec"})
	if !guiApp.IsRemoteView(ref) || guiApp.IsRemoteView(9999) {
		t.Fatal("IsRemoteView")
	}
	pageEval(t, w, ref, `window.__res = ""; (async () => {
  const r = {};
  for (const u of ["krate://app/viewping?tag=remote", "krate://app/__krate/call"]) {
    try { await fetch(u); r[u] = "reachable"; } catch (e) { r[u] = "error"; }
  }
  r.open = String(window.open("krate://app/viewping?tag=open"));
  r.globals = Object.getOwnPropertyNames(window).filter(k => /krate/i.test(k)).join(",");
  window.__res = JSON.stringify(r);
})(); 1`)
	var res string
	for i := 0; i < 100 && res == ""; i++ {
		time.Sleep(100 * time.Millisecond)
		res = pageEval(t, w, ref, "window.__res")
	}
	if res == "" || strings.Contains(res, "reachable") || !strings.Contains(res, `"globals":""`) {
		t.Fatalf("probe result %q", res)
	}
	select {
	case p := <-viewPings:
		t.Fatalf("a request of the remote page reached the app handler: %+v", p)
	case <-time.After(1500 * time.Millisecond):
	}
	pageEval(t, w, ref, `location.href = "krate://app/viewping?tag=nav"; 1`)
	l.wait(t, "the refused app navigation", func(e caps.RemoteEvent) bool {
		return e.Kind == caps.RemoteNavBlocked && strings.HasPrefix(e.URL, "krate://app/") && e.Verdict == caps.NavBlock
	})
}

func TestGUIRemoteNavigationAndPartitions(t *testing.T) {
	requireGUI(t)
	a := httptest.NewServer(remotePage("A"))
	defer a.Close()
	b := httptest.NewServer(remotePage("B"))
	defer b.Close()
	l := collectRemote(t)
	w, ref := openRemote(t, l, caps.RemoteOptions{URL: a.URL + "/", Partition: "navp1"})
	pageEval(t, w, ref, `location.href = `+fmt.Sprintf("%q", b.URL+"/x")+`; 1`)
	e := l.wait(t, "the external verdict", func(e caps.RemoteEvent) bool {
		return e.Kind == caps.RemoteNavBlocked && strings.HasPrefix(e.URL, b.URL)
	})
	if e.Verdict != caps.NavExternal {
		t.Fatalf("verdict %v", e.Verdict)
	}
	if err := guiApp.NavigateRemote(ref, "krate://app/"); err == nil {
		t.Fatal("the app navigated a remote view to the app origin")
	}
	// Storage is per partition and separate from the app's.
	pageEval(t, w, ref, `localStorage.setItem("k", "v"); document.cookie = "c=1; path=/"; 1`)
	w2, ref2 := openRemote(t, l, caps.RemoteOptions{URL: a.URL + "/", Partition: "navp2"})
	if got := pageEval(t, w2, ref2, `String(localStorage.getItem("k")) + "|" + document.cookie`); got != "null|" {
		t.Fatalf("partition two sees partition one: %q", got)
	}
	w3, ref3 := openRemote(t, l, caps.RemoteOptions{URL: a.URL + "/", Partition: "navp1"})
	if got := pageEval(t, w3, ref3, `localStorage.getItem("k")`); got != "v" {
		t.Fatalf("a partition does not share with itself: %q", got)
	}
}

func TestGUIRemoteMessagesAndRules(t *testing.T) {
	requireGUI(t)
	hits := &struct {
		mu sync.Mutex
		n  map[string]int
	}{n: map[string]int{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/", remotePage(`<script>
window.got = [];
addEventListener("message", e => got.push({data: e.data, origin: e.origin}));
</script><script src="/ads/a.js"></script>`))
	mux.HandleFunc("/ads/a.js", func(w http.ResponseWriter, r *http.Request) {
		hits.mu.Lock()
		hits.n[r.URL.Path]++
		hits.mu.Unlock()
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, "window.__ad = 1;")
	})
	site := httptest.NewServer(mux)
	defer site.Close()
	if err := guiApp.SetUserAgent("msg", "KrateTest/1"); err != nil {
		t.Fatal(err)
	}
	if err := guiApp.SetRequestRules("msg", []caps.RequestRule{{URLs: []string{"*://*/ads/*"}, Action: caps.RuleBlock}}); err != nil {
		t.Fatal(err)
	}
	l := collectRemote(t)
	w, ref := openRemote(t, l, caps.RemoteOptions{URL: site.URL + "/", Partition: "msg", Messages: true})
	if got := pageEval(t, w, ref, "navigator.userAgent"); !strings.Contains(got, "KrateTest/1") {
		t.Fatalf("user agent %q", got)
	}
	time.Sleep(500 * time.Millisecond)
	hits.mu.Lock()
	n := hits.n["/ads/a.js"]
	hits.mu.Unlock()
	if n != 0 {
		t.Fatalf("the blocked script was fetched %d times", n)
	}
	pageEval(t, w, ref, `krateRemote.postMessage({hello: 1}); 1`)
	m := l.wait(t, "the page's message", func(e caps.RemoteEvent) bool { return e.Kind == caps.RemoteMessage })
	origin := strings.TrimSuffix(site.URL, "/")
	if m.Origin != origin || string(m.Data) != `{"hello":1}` {
		t.Fatalf("message %+v", m)
	}
	if err := guiApp.PostToRemote(ref, origin, []byte(`{"reply":true}`)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100 && pageEval(t, w, ref, "got.length") != "1"; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if got := pageEval(t, w, ref, "got[0].origin"); got != OriginURL {
		t.Fatalf("the page saw origin %q", got)
	}
	if guiApp.PostToRemote(ref, "https://other.example", []byte(`1`)) == nil {
		t.Fatal("posted to the wrong origin")
	}
}
