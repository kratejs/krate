//go:build darwin

package darwin

import (
	"testing"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// GUI tests of the window chrome. They read the AppKit state back, so a test
// proves that the call reached the NSWindow, not only that it did not fail.
// They run with the other GUI tests (KRATE_NATIVE_GUI=1) in the CI native job
// on macos-latest.

func chromeWin(t *testing.T, o caps.ChromeOptions) *Window {
	t.Helper()
	requireGUI(t)
	w, err := guiApp.NewWindow(WindowOptions{Title: t.Name(), Width: 480, Height: 320, Resizable: true, Path: "/blank", Chrome: o})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	return w
}

func onMain(fn func()) { runOnMain(func() { withPool(fn) }) }
