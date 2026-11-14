//go:build linux

package a11y

import (
	"testing"

	"github.com/kratejs/krate/packages/native/internal/atspi"
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
