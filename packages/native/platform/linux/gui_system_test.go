//go:build linux

package linux

import (
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

func TestGUIDisplays(t *testing.T) {
	needGUI(t)
	ds, err := guiApp.Displays()
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) == 0 {
		t.Fatal("no displays")
	}
	primaries := 0
	for _, d := range ds {
		if d.ID == "" || d.Width <= 0 || d.Height <= 0 || d.WorkWidth <= 0 || d.WorkHeight <= 0 || d.Scale < 1 {
			t.Errorf("bad display %+v", d)
		}
		if d.WorkWidth > d.Width || d.WorkHeight > d.Height {
			t.Errorf("work area larger than the display: %+v", d)
		}
		if d.Primary {
			primaries++
		}
	}
	if primaries != 1 || !ds[0].Primary {
		t.Errorf("want exactly one primary, listed first: %+v", ds)
	}
	// A window opens on a listed display.
	w := ext_newWin(t, "onscreen")
	st := w.State()
	d, ok := caps.DisplayForRect(ds, caps.Rect{X: st.X, Y: st.Y, W: st.Width, H: st.Height})
	if !ok || d.ID == "" {
		t.Errorf("window %+v is on no display of %+v", st, ds)
	}
	// Centering uses the work area. A Wayland client cannot place its windows
	// (SetPosition is ignored there), so the move is only checked on X11.
	if os.Getenv("GDK_BACKEND") == "wayland" {
		return
	}
	x, y := caps.CenterIn(d, st.Width, st.Height)
	// A move that reaches the window manager before it has finished placing
	// the new frame is overridden by that placement, so ask again until the
	// position holds.
	waitFor(t, fmt.Sprintf("centered at %d,%d on %+v", x, y, d), func() bool {
		s := w.State()
		if abs(s.X-x) <= 40 && abs(s.Y-y) <= 40 {
			return true
		}
		w.SetPosition(x, y)
		return false
	})
	guiApp.OnDisplays(func() {})
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func TestGUIWindowTheme(t *testing.T) {
	needGUI(t)
	for _, c := range []struct {
		mode string
		dark bool
	}{{"dark", true}, {"light", false}, {"dark", true}, {"system", false}} {
		if err := guiApp.SetWindowTheme(c.mode); err != nil {
			t.Fatal(err)
		}
		if got := guiApp.preferDark(); got != c.dark {
			t.Errorf("after %q prefer-dark = %v, want %v", c.mode, got, c.dark)
		}
	}
}

// Killing the web process raises web-process-terminated, which the platform
// reports with a reason; ReloadView brings the page back.
func TestGUIWebviewCrash(t *testing.T) {
	needGUI(t)
	w := ext_newWin(t, "crash")
	crashes := make(chan caps.ViewCrash, 4)
	guiApp.OnViewCrash(func(c caps.ViewCrash) { crashes <- c })
	t.Cleanup(func() { guiApp.OnViewCrash(nil) })
	loads := make(chan struct{}, 8)
	guiApp.OnViewEvent(func(e caps.ViewEvent) {
		if e.Kind == caps.ViewNavigated && e.View == w.PrimaryView() {
			select {
			case loads <- struct{}{}:
			default:
			}
		}
	})
	t.Cleanup(func() { guiApp.OnViewEvent(nil) })

	procs, _ := guiApp.WebviewProcesses()
	killed := 0
	for _, p := range procs {
		if p.Role == "webview:web" {
			if err := syscall.Kill(p.PID, syscall.SIGKILL); err != nil {
				t.Fatalf("kill %d: %v", p.PID, err)
			}
			killed++
		}
	}
	if killed == 0 {
		t.Fatalf("no web process among %+v", procs)
	}
	select {
	case c := <-crashes:
		if c.View != w.PrimaryView() && c.View == 0 {
			t.Errorf("crash for view %d", c.View)
		}
		if c.Reason != "crashed" && c.Reason != "killed" {
			t.Errorf("reason %q", c.Reason)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("no crash event")
	}
	for len(loads) > 0 {
		<-loads
	}
	if err := guiApp.ReloadView(w.PrimaryView()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-loads:
	case <-time.After(20 * time.Second):
		t.Fatal("the view did not load again after ReloadView")
	}
}
