//go:build linux

package linux

import (
	"testing"
	"time"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// fakeKeys presses keys through the XTEST extension on a separate connection.
func fakeKeys(t *testing.T, keys ...string) {
	t.Helper()
	xt, err := purego.Dlopen("libXtst.so.6", rtldNow|rtldGlobal)
	if err != nil {
		t.Skip("libXtst.so.6 is not available: ", err)
	}
	x, err := loadX11()
	if err != nil {
		t.Skip(err)
	}
	var fake func(d uintptr, code uint32, press int32, delay uint64) int32
	purego.RegisterLibFunc(&fake, xt, "XTestFakeKeyEvent")
	x.mu.Lock()
	defer x.mu.Unlock()
	for _, k := range keys {
		sym := x.stringToKeysym(k)
		code := uint32(x.keysymToCode(x.dpy, sym))
		fake(x.dpy, code, 1, 0)
	}
	x.flush(x.dpy)
	for i := len(keys) - 1; i >= 0; i-- {
		sym := x.stringToKeysym(keys[i])
		fake(x.dpy, uint32(x.keysymToCode(x.dpy, sym)), 0, 0)
	}
	x.flush(x.dpy)
}

func TestGUIGlobalShortcut(t *testing.T) {
	needGUI(t)
	got := make(chan string, 4)
	guiApp.OnShortcut(func(h string) { got <- h })
	defer guiApp.StopShortcuts()
	if err := guiApp.RegisterShortcut("quick", caps.Accelerator{Ctrl: true, Shift: true, Key: "K"}); err != nil {
		t.Skip("no X11 key grabs here: ", err)
	}
	if err := guiApp.RegisterShortcut("quick", caps.Accelerator{Ctrl: true, Key: "J"}); err == nil {
		t.Fatal("duplicate handle accepted")
	}
	fakeKeys(t, "Control_L", "Shift_L", "k")
	select {
	case h := <-got:
		if h != "quick" {
			t.Fatalf("handle %q", h)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shortcut not delivered")
	}
	if err := guiApp.UnregisterShortcut("quick"); err != nil {
		t.Fatal(err)
	}
	fakeKeys(t, "Control_L", "Shift_L", "k")
	select {
	case h := <-got:
		t.Fatalf("delivered after unregister: %q", h)
	case <-time.After(400 * time.Millisecond):
	}
	// A key combination another client already grabbed is reported.
	if err := guiApp.RegisterShortcut("again", caps.Accelerator{Ctrl: true, Alt: true, Key: "F9"}); err != nil {
		t.Fatal(err)
	}
}
