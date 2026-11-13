//go:build linux

package linux

import (
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Tests for several webviews in one window: native view identity of requests,
// split panes (GtkPaned), dividers, minimum sizes, events, release and the
// shared web process.

type viewPing struct {
	name   string
	view   caps.ViewRef
	native bool
	header string
}

var viewPings = make(chan viewPing, 64)

func registerViewHandlers(s *testServer) {
	html := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, body)
		}
	}
	// The page reports itself and forges the window header on its own call.
	s.mux.HandleFunc("/pane", html(`<!doctype html><title>pane</title><body style="margin:0">pane<script>
fetch("/ping?name="+encodeURIComponent(location.search),{headers:{"x-krate-window":"forged"}});
</script></body>`))
	s.mux.HandleFunc("/closer", html(`<!doctype html><title>closer</title><body>closer<script>
fetch("/ping?name=closer").then(function(){setTimeout(function(){window.close()},100)});
</script></body>`))
	s.mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		v, ok := caps.ViewOf(r)
		viewPings <- viewPing{name: r.URL.Query().Get("name"), view: v, native: ok, header: r.Header.Get("x-krate-window")}
		w.WriteHeader(204)
	})
}

// pingBacklog holds pings that arrived while waiting for another one, so the
// order in which two views load does not matter.
var pingBacklog []viewPing

func waitPing(t *testing.T, name string) viewPing {
	t.Helper()
	for i, p := range pingBacklog {
		if p.name == name {
			pingBacklog = append(pingBacklog[:i], pingBacklog[i+1:]...)
			return p
		}
	}
	deadline := time.After(20 * time.Second)
	for {
		select {
		case p := <-viewPings:
			if p.name == name {
				return p
			}
			pingBacklog = append(pingBacklog, p)
		case <-deadline:
			t.Fatalf("timed out waiting for the ping of %q (backlog %v)", name, pingBacklog)
		}
	}
}

func drainPings() {
	pingBacklog = nil
	for {
		select {
		case <-viewPings:
		default:
			return
		}
	}
}

func evalIn(t *testing.T, w *Window, ref caps.ViewRef, js string) string {
	t.Helper()
	ch := make(chan string, 1)
	guiApp.Dispatch(func() {
		v := w.viewByRef(ref)
		if v == nil || v.widget == 0 {
			ch <- "<no view>"
			return
		}
		guiApp.evalString(v.widget, js, func(s string) { ch <- s })
	})
	select {
	case s := <-ch:
		return s
	case <-time.After(10 * time.Second):
		t.Fatalf("eval %q did not answer", js)
		return ""
	}
}

func viewWindow(t *testing.T, path string) *Window {
	t.Helper()
	drainPings()
	w := memWindow(t, path)
	return w
}

func TestGUIViewIdentity(t *testing.T) {
	needGUI(t)
	a := viewWindow(t, "/pane?a")
	pa := waitPing(t, "?a")
	if !pa.native || pa.view != a.PrimaryView() || pa.view == 0 {
		t.Fatalf("ping from window a: %+v, primary view %d", pa, a.PrimaryView())
	}
	// The page forged the header; the native identity does not depend on it.
	if pa.header != "forged" {
		t.Fatalf("header %q: the page's header should arrive unchanged", pa.header)
	}
	b := viewWindow(t, "/pane?b")
	pb := waitPing(t, "?b")
	if pb.view != b.PrimaryView() || pb.view == pa.view {
		t.Fatalf("window b: %+v, a's view %d", pb, pa.view)
	}
	extra, err := a.AddView(caps.ViewOptions{Path: "/pane?a2"})
	if err != nil {
		t.Fatal(err)
	}
	p2 := waitPing(t, "?a2")
	if p2.view != extra {
		t.Fatalf("a second view of window a reported %d, want %d", p2.view, extra)
	}
	for ref, want := range map[caps.ViewRef]caps.WindowRef{pa.view: a.Ref(), pb.view: b.Ref(), extra: a.Ref()} {
		if got, ok := guiApp.WindowOfView(ref); !ok || got != want {
			t.Errorf("WindowOfView(%d) = %d, %v; want %d", ref, got, ok, want)
		}
	}
	if _, ok := guiApp.WindowOfView(9999); ok {
		t.Error("WindowOfView found a view that does not exist")
	}
	a.Close()
	b.Close()
	waitFor(t, "windows closed", func() bool { return !a.alive && !b.alive })
	if _, ok := guiApp.WindowOfView(extra); ok {
		t.Error("a closed window's view is still known")
	}
}
