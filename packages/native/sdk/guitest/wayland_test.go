//go:build linux

package guitest

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/sdk/window"
)

// onWayland reports whether this run uses GTK's Wayland backend. The whole
// package also runs under a Wayland compositor (GDK_BACKEND=wayland with
// WAYLAND_DISPLAY set, for example weston nested on Xvfb); these tests are the
// ones that are specific to it.
func onWayland() bool { return strings.HasPrefix(os.Getenv("GDK_BACKEND"), "wayland") }

func needX11(t *testing.T) {
	t.Helper()
	if onWayland() {
		t.Skip("X11-only: the Wayland backend has no XEmbed tray or key grabs")
	}
}

// A Wayland client cannot place its windows or keep them above others. The
// calls are accepted (not errors), do nothing, and the capability report says
// so, so that an app can hide the controls.
func TestWaylandPlacementIsIgnored(t *testing.T) {
	need(t)
	if !onWayland() {
		t.Skip("set GDK_BACKEND=wayland with a compositor to run the Wayland checks")
	}
	for _, c := range []string{"window:position", "window:alwaysOnTop"} {
		if host.SupportsForTest(c) {
			t.Errorf("%s is reported as supported on Wayland", c)
		}
	}
	w, err := window.Open(window.Options{Title: "wl", Path: "/wl", Width: 400, Height: 300})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	eventually(t, "the window to be shown", func() bool { st, err := w.State(); return err == nil && st.Visible })
	if err := w.SetPosition(333, 222); err != nil {
		t.Fatalf("SetPosition must be accepted and ignored, got %v", err)
	}
	if err := w.SetAlwaysOnTop(true); err != nil {
		t.Fatalf("SetAlwaysOnTop must be accepted and ignored, got %v", err)
	}
	time.Sleep(400 * time.Millisecond)
	st, err := w.State()
	if err != nil {
		t.Fatal(err)
	}
	if st.X == 333 && st.Y == 222 {
		t.Fatalf("the compositor placed the window where the client asked: %+v", st)
	}
	// The window stays usable.
	if err := w.SetTitle("still works"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "title", func() bool { st, _ := w.State(); return st.Title == "still works" })
}
