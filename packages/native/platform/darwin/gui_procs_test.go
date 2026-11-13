//go:build darwin

package darwin

import (
	"errors"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Offscreen capture and extension checks, run under KRATE_NATIVE_GUI=1 in the
// CI native job.

func TestGUIOffscreenSnapshot(t *testing.T) {
	requireGUI(t)
	w, err := guiApp.NewWindow(WindowOptions{Title: "off", Width: 320, Height: 200, Path: "/viewping?tag=off", Offscreen: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	deadline := time.Now().Add(30 * time.Second)
	for {
		s, err := guiApp.SnapshotView(w.PrimaryView(), caps.SnapshotOptions{})
		if err == nil && s.Width > 0 && s.Height > 0 && len(s.Pix) >= s.Stride*s.Height {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no snapshot within 30s (last error %v)", err)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// Without Options.Extensions the controller is never created, so the calls
// report ErrUnsupported on every macOS; the test app does not enable them.
func TestGUIExtensionsOffByDefault(t *testing.T) {
	requireGUI(t)
	if guiApp.ExtensionsSupported() {
		t.Skip("the test app enables extensions")
	}
	if _, err := guiApp.ListExtensions(); !errors.Is(err, krate.ErrUnsupported) {
		t.Fatalf("ListExtensions = %v, want ErrUnsupported", err)
	}
}
