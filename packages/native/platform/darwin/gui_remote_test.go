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
	// Pings other tests' pages sent (an offscreen window loads
	// /viewping?tag=off) are not the remote page's.
	for drained := false; !drained; {
		select {
		case <-viewPings:
		default:
			drained = true
		}
	}
	pageEval(t, w, ref, `window.__res = ""; (async () => {
  const r = {};
  for (const u of ["krate://app/viewping?tag=remote", "krate://app/__krate/call"]) {
    try { await fetch(u); r[u] = "reachable"; } catch (e) { r[u] = "error"; }
  }
  r.open = String(window.open("krate://app/viewping?tag=open"));
  r.globals = Object.getOwnPropertyNames(window).filter(k => /krate/i.test(k)).join(",");
  window.__res = Object.entries(r).map(([k, v]) => k + "=" + v).join(";") + ";";
})(); 1`)
	var res string
	for i := 0; i < 100 && res == ""; i++ {
		time.Sleep(100 * time.Millisecond)
		res = pageEval(t, w, ref, "window.__res")
	}
	if res == "" || strings.Contains(res, "reachable") || !strings.Contains(res, "globals=;") {
		t.Fatalf("probe result %q", res)
	}
	for deadline := time.After(1500 * time.Millisecond); ; {
		select {
		case p := <-viewPings:
			if p.tag == "remote" || p.tag == "open" {
				t.Fatalf("a request of the remote page reached the app handler: %+v", p)
			}
			continue
		case <-deadline:
		}
		break
	}
	pageEval(t, w, ref, `location.href = "krate://app/viewping?tag=nav"; 1`)
	l.wait(t, "the refused app navigation", func(e caps.RemoteEvent) bool {
		return e.Kind == caps.RemoteNavBlocked && strings.HasPrefix(e.URL, "krate://app/") && e.Verdict == caps.NavBlock
	})
}
