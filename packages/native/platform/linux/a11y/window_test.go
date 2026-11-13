//go:build linux

package a11y

import (
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/internal/atspi"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/platform/linux"
)

// The window and its webview must be reachable in every window configuration:
// a frame named by the title, with the page's document inside it.

func TestWindowIsNamedByItsTitle(t *testing.T) {
	need(t)
	w := open(t, linux.WindowOptions{Title: "Named window"})
	fr := waitTopLevel(t, "Named window")
	if fr.Role() != atspi.RoleFrame {
		t.Errorf("window role %d, want frame %d", fr.Role(), atspi.RoleFrame)
	}
	if !fr.HasState(atspi.StateShowing) || !fr.HasState(atspi.StateEnabled) {
		t.Errorf("window states %v lack showing or enabled", fr.States())
	}
	w.SetTitle("Renamed window")
	waitTopLevel(t, "Renamed window")
	waitFor(t, "the old title to go", func() bool { _, ok := topLevel("Named window"); return !ok })
	w.SetTitle("Named window")
}

func TestWebviewReachableInEveryWindowConfiguration(t *testing.T) {
	need(t)
	for _, c := range []struct {
		name   string
		chrome caps.ChromeOptions
	}{
		{"native", caps.ChromeOptions{}},
		{"hidden title bar", caps.ChromeOptions{TitleBar: caps.TitleBarHidden}},
		{"frameless", caps.ChromeOptions{TitleBar: caps.TitleBarNone}},
		{"transparent frameless", caps.ChromeOptions{TitleBar: caps.TitleBarNone, Transparent: true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			title := "Config " + c.name
			open(t, linux.WindowOptions{Title: title, Chrome: c.chrome})
			fr := waitTopLevel(t, title)
			var doc atspi.Node
			waitFor(t, "the page document", func() bool {
				d := documents(fr)
				if len(d) == 0 {
					return false
				}
				doc = d[0]
				return doc.Name() == "Home page"
			})
			if !doc.HasState(atspi.StateShowing) {
				t.Errorf("document states %v lack showing", doc.States())
			}
			// The page's own content is exposed too, with its roles and names.
			btn, ok := fr.Find(func(n atspi.Node) bool { return n.Role() == atspi.RolePushButton && n.Name() == "Go" })
			if !ok {
				t.Fatalf("button Go not in the tree:\n%s", fr.Dump())
			}
			if !btn.HasState(atspi.StateFocusable) {
				t.Errorf("button states %v are not focusable", btn.States())
			}
		})
	}
}

func TestOffscreenWindowIsNotExposed(t *testing.T) {
	need(t)
	if !app.OffscreenSupported() {
		t.Skip("offscreen windows are not supported here")
	}
	// A visible window first, so that the tree is known to be populated.
	open(t, linux.WindowOptions{Title: "Visible companion"})
	waitTopLevel(t, "Visible companion")
	off := open(t, linux.WindowOptions{Title: "Offscreen secret", Offscreen: true})
	// Give the bridge time to register what it is going to register.
	time.Sleep(2 * time.Second)
	if _, ok := topLevel("Offscreen secret"); ok {
		t.Fatalf("the offscreen window is in the accessibility tree:\n%s", treeDump())
	}
	a, _ := appNode()
	for _, c := range a.Children() {
		for _, d := range documents(c) {
			if d.Name() == "Home page" && c.Name() != "Visible companion" {
				t.Errorf("a page document of %q leaks into the tree", c.Name())
			}
		}
	}
	// Hiding it from assistive technology must not stop it rendering.
	snap, err := app.SnapshotView(off.PrimaryView(), caps.SnapshotOptions{})
	if err != nil {
		t.Fatalf("snapshot of the offscreen view: %v", err)
	}
	if snap.Width == 0 || snap.Height == 0 {
		t.Errorf("empty snapshot %dx%d", snap.Width, snap.Height)
	}
}

func TestFrameHasKeyboardFocusInsideTheWebview(t *testing.T) {
	needWM(t)
	w := open(t, linux.WindowOptions{Title: "Keyboard form", Path: "/form"})
	fr := focusWindow(t, w, "Keyboard form")
	waitFor(t, "the form document", func() bool { return len(documents(fr)) > 0 && documents(fr)[0].Name() == "Form" })
	// Tab reaches the page's controls in order; nothing before them traps it.
	var got []string
	for i := 0; i < 3; i++ {
		if err := client.KeySym("Tab"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(300 * time.Millisecond)
		fr, _ = topLevel("Keyboard form")
		if f, ok := fr.Find(func(n atspi.Node) bool {
			return n.HasState(atspi.StateFocused) && (n.Role() == atspi.RoleEntry || n.Role() == atspi.RolePushButton)
		}); ok {
			got = append(got, f.Name())
		} else {
			got = append(got, "")
		}
	}
	want := []string{"one", "two", "Go"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tab stop %d focused %q, want %q (all %v)", i+1, got[i], want[i], got)
		}
	}
}
