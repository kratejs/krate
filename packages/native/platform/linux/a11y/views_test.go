//go:build linux

package a11y

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/internal/atspi"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/platform/linux"
)

func splitWindow(t *testing.T, title string) (*linux.Window, caps.ViewRef) {
	t.Helper()
	w := open(t, linux.WindowOptions{Title: title, Path: "/form"})
	second, err := w.AddView(caps.ViewOptions{Path: "/form", Beside: w.PrimaryView(), Axis: caps.Horizontal})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "both documents", func() bool {
		fr, ok := topLevel(title)
		return ok && len(documents(fr)) == 2
	})
	return w, second
}

func TestSplitPanesAreReachableAndNamed(t *testing.T) {
	need(t)
	splitWindow(t, "Split a11y")
	fr, _ := topLevel("Split a11y")
	panes := fr.FindAll(func(n atspi.Node) bool { return n.Role() == atspi.RoleSplitPane })
	if len(panes) != 1 {
		t.Fatalf("want one split pane, got %d:\n%s", len(panes), fr.Dump())
	}
	div := panes[0]
	if div.Name() == "" {
		t.Error("the divider has no accessible name")
	}
	if div.Description() == "" {
		t.Error("the divider does not say how to resize it")
	}
	if !div.HasState(atspi.StateFocusable) {
		t.Errorf("the divider is not focusable: %v", div.States())
	}
	// Each view is told apart by name.
	names := map[string]bool{}
	for _, c := range div.Children() {
		names[c.Name()] = true
	}
	if !names["Pane 1 of 2"] || !names["Pane 2 of 2"] {
		t.Errorf("pane names = %v, want Pane 1 of 2 and Pane 2 of 2\n%s", names, fr.Dump())
	}
	for _, d := range documents(fr) {
		if !d.HasState(atspi.StateShowing) {
			t.Errorf("a view's document is not showing: %v", d.States())
		}
	}
}

func TestSplitDividerResizesFromTheKeyboard(t *testing.T) {
	need(t)
	w, _ := splitWindow(t, "Split keys")
	focusWindow(t, w, "Split keys")
	fr, _ := topLevel("Split keys")
	div, ok := fr.Find(func(n atspi.Node) bool { return n.Role() == atspi.RoleSplitPane })
	if !ok {
		t.Fatal("no divider")
	}
	if !div.GrabFocus() {
		t.Fatal("the divider refused the focus")
	}
	waitFor(t, "the divider to be focused", func() bool {
		d, _ := topLevel("Split keys")
		n, ok := d.Find(func(n atspi.Node) bool { return n.Role() == atspi.RoleSplitPane })
		return ok && n.HasState(atspi.StateFocused)
	})
	before, ok := div.Value()
	if !ok {
		t.Fatal("the divider has no Value interface")
	}
	for i := 0; i < 5; i++ {
		_ = client.KeySym("Right")
		time.Sleep(100 * time.Millisecond)
	}
	waitFor(t, "the divider to move right", func() bool {
		v, _ := div.Value()
		return v > before
	})
	after, _ := div.Value()
	for i := 0; i < 5; i++ {
		_ = client.KeySym("Left")
		time.Sleep(100 * time.Millisecond)
	}
	waitFor(t, "the divider to move back", func() bool {
		v, _ := div.Value()
		return v < after
	})
	// The layout the app sees follows the keyboard.
	if l := w.Layout(); l == nil || l.IsLeaf() {
		t.Error("the window has no split layout")
	}
}

func TestFocusTraversalVisitsEveryView(t *testing.T) {
	need(t)
	w, _ := splitWindow(t, "Split tab")
	focusWindow(t, w, "Split tab")
	// The page records the last key and its target in its title; Tab until
	// both documents have seen a key, which proves focus entered each view.
	seen := func() int {
		fr, _ := topLevel("Split tab")
		n := 0
		for _, d := range documents(fr) {
			if len(d.Name()) > 4 && d.Name()[:4] == "key:" {
				n++
			}
		}
		return n
	}
	for i := 0; i < 12 && seen() < 2; i++ {
		_ = client.KeySym("Tab")
		time.Sleep(250 * time.Millisecond)
	}
	if n := seen(); n != 2 {
		fr, _ := topLevel("Split tab")
		t.Errorf("Tab reached %d of 2 views (focus trapped?)\n%s", n, fr.Dump())
	}
}

func TestRemoteViewIsReachable(t *testing.T) {
	need(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><title>Remote page</title><body><button>Remote go</button></body>`))
	}))
	defer srv.Close()
	rw, err := app.NewRemoteWindow(caps.RemoteWindowSpec{Title: "Remote a11y", Width: 500, Height: 300, Resizable: true,
		Remote: caps.RemoteOptions{URL: srv.URL + "/", Partition: "a11y"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		rw.Close()
		waitGone("Remote a11y")
	}()
	fr := waitTopLevel(t, "Remote a11y")
	waitFor(t, "the remote document", func() bool {
		fr, _ = topLevel("Remote a11y")
		for _, d := range documents(fr) {
			if d.Name() == "Remote page" {
				return true
			}
		}
		return false
	})
	if _, ok := fr.Find(named(atspi.RolePushButton, "Remote go")); !ok {
		t.Errorf("the remote page's button is not exposed:\n%s", fr.Dump())
	}
}
