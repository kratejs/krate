//go:build darwin

package darwin

import (
	"io"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// GUI tests of several views in one window. They run with the other GUI tests
// (KRATE_NATIVE_GUI=1) and have not been run yet; the CI native job is the
// first run. The layout arithmetic they rely on is unit-tested in
// platform/caps.

type viewPing struct {
	tag  string
	view caps.ViewRef
	ok   bool
}

var viewPings = make(chan viewPing, 32)

// withViewPing wraps h: a request for /viewping answers with a small page and
// reports the native view it came from.
func withViewPing(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/viewping" {
			v, ok := caps.ViewOf(r)
			viewPings <- viewPing{tag: r.URL.Query().Get("tag"), view: v, ok: ok}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, "<!doctype html><title>viewping</title><body>ping")
			return
		}
		h.ServeHTTP(w, r)
	})
}

func waitViewPing(t *testing.T, tag string) viewPing {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case p := <-viewPings:
			if p.tag == tag {
				return p
			}
		case <-deadline:
			t.Fatalf("no request from the view tagged %q within 30s", tag)
		}
	}
}

func TestGUIViews(t *testing.T) {
	requireGUI(t)
	w, err := guiApp.NewWindow(WindowOptions{Title: "views", Width: 800, Height: 500, Resizable: true, Path: "/viewping?tag=a"})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	a := waitViewPing(t, "a")
	if !a.ok || a.view == 0 || a.view != w.PrimaryView() {
		t.Fatalf("request from the primary view: %+v, primary %d", a, w.PrimaryView())
	}
	ref, err := w.AddView(caps.ViewOptions{Path: "/viewping?tag=b", Axis: caps.Horizontal, Share: 0.3, MinWidth: 100})
	if err != nil {
		t.Fatal(err)
	}
	b := waitViewPing(t, "b")
	if !b.ok || b.view != ref {
		t.Fatalf("request from the added view: %+v, want view %d", b, ref)
	}
	if got, ok := guiApp.WindowOfView(ref); !ok || got != w.Ref() {
		t.Fatalf("WindowOfView = %d, %v; want %d", got, ok, w.Ref())
	}
	if vs := w.Views(); len(vs) != 2 || !vs[0].Primary || vs[1].Ref != ref {
		t.Fatalf("views %+v", vs)
	}
	l := w.Layout()
	if l.IsLeaf() || l.Axis != caps.Horizontal || l.First.View != w.PrimaryView() || l.Second.View != ref {
		t.Fatalf("layout %+v", l)
	}
	// The ratio asked for is applied (the new view gets 30 percent).
	deadline := time.Now().Add(10 * time.Second)
	for math.Abs(w.Layout().Ratio-0.7) > 0.05 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if got := w.Layout().Ratio; math.Abs(got-0.7) > 0.05 {
		t.Errorf("ratio = %v, want about 0.7", got)
	}

	// Rearranged, and refused when it does not name every view once.
	swapped := &caps.LayoutNode{Axis: caps.Vertical, Ratio: 0.4, First: caps.Leaf(ref), Second: caps.Leaf(w.PrimaryView())}
	if err := w.SetLayout(swapped); err != nil {
		t.Fatal(err)
	}
	if err := w.SetLayout(caps.Leaf(w.PrimaryView())); err == nil {
		t.Error("a layout that leaves a view out was accepted")
	}
	if got := w.Layout(); got.Axis != caps.Vertical || got.First.View != ref {
		t.Errorf("layout after SetLayout: %+v", got)
	}
	if err := w.FocusView(ref); err != nil {
		t.Error(err)
	}
	if err := w.RemoveView(w.PrimaryView()); err == nil {
		t.Error("the primary view was removed")
	}
	if err := w.RemoveView(ref); err != nil {
		t.Fatal(err)
	}
	if vs := w.Views(); len(vs) != 1 {
		t.Fatalf("views after removal: %+v", vs)
	}
	if _, ok := guiApp.WindowOfView(ref); ok {
		t.Error("a removed view is still known")
	}
}
