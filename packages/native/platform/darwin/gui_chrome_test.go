//go:build darwin

package darwin

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"
	"github.com/kratejs/krate/packages/native/krate"
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

func waitCond(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestGUIChromeHiddenTitleBar(t *testing.T) {
	w := chromeWin(t, caps.ChromeOptions{TitleBar: caps.TitleBarHidden, TrafficLight: &caps.Point{X: 20, Y: 24}})
	var mask uint
	var transparent bool
	var vis int
	var closeHidden bool
	var closeX float64
	onMain(func() {
		mask = objc.Send[uint](w.win, sel("styleMask"))
		transparent = objc.Send[bool](w.win, sel("titlebarAppearsTransparent"))
		vis = objc.Send[int](w.win, sel("titleVisibility"))
		b := w.win.Send(sel("standardWindowButton:"), nsWindowCloseButton)
		closeHidden = objc.Send[bool](b, sel("isHidden"))
		closeX = objc.Send[nsRect](b, sel("frame")).Origin.X
	})
	if mask&nsWindowStyleMaskFullSizeContentView == 0 || mask&nsWindowStyleMaskTitled == 0 {
		t.Errorf("style mask %#x lacks full size content or the title", mask)
	}
	if !transparent || vis != nsWindowTitleHidden {
		t.Errorf("title bar transparent=%v visibility=%d", transparent, vis)
	}
	if closeHidden {
		t.Error("a hidden title bar hid the traffic lights")
	}
	if closeX != 20 {
		t.Errorf("close button at x=%v, want 20", closeX)
	}
	st := w.ChromeState()
	if st.TitleBar != "hidden" || st.TrafficLight == nil || st.TrafficLight.X != 20 || !st.HasFeature(caps.FeatureVibrancy) {
		t.Errorf("chrome state %+v", st)
	}
	// Moving the lights later.
	if err := w.SetTrafficLightPosition(caps.Point{X: 30, Y: 30}); err != nil {
		t.Fatal(err)
	}
	waitCond(t, "traffic lights moved", func() bool {
		var x float64
		onMain(func() {
			x = objc.Send[nsRect](w.win.Send(sel("standardWindowButton:"), nsWindowCloseButton), sel("frame")).Origin.X
		})
		return x == 30
	})
	// Back to a native title bar.
	if err := w.SetTitleBar("native"); err != nil {
		t.Fatal(err)
	}
	waitCond(t, "native title bar", func() bool {
		var m uint
		onMain(func() { m = objc.Send[uint](w.win, sel("styleMask")) })
		return m&nsWindowStyleMaskFullSizeContentView == 0
	})
}

func TestGUIChromeNoTitleBarHidesTheButtons(t *testing.T) {
	w := chromeWin(t, caps.ChromeOptions{TitleBar: caps.TitleBarNone})
	var hidden [3]bool
	onMain(func() {
		for i, b := range []int{nsWindowCloseButton, nsWindowMiniaturizeButton, nsWindowZoomButton} {
			hidden[i] = objc.Send[bool](w.win.Send(sel("standardWindowButton:"), b), sel("isHidden"))
		}
	})
	if hidden != [3]bool{true, true, true} {
		t.Errorf("traffic lights hidden: %v", hidden)
	}
	if err := w.SetTitleBar("hidden"); err != nil {
		t.Fatal(err)
	}
	waitCond(t, "lights back", func() bool {
		var h bool
		onMain(func() {
			h = objc.Send[bool](w.win.Send(sel("standardWindowButton:"), nsWindowCloseButton), sel("isHidden"))
		})
		return !h
	})
	if err := w.SetTitleBar("floating"); err == nil {
		t.Error("an unknown style was accepted")
	}
}

func TestGUIChromeTransparent(t *testing.T) {
	w := chromeWin(t, caps.ChromeOptions{Transparent: true})
	var opaque bool
	onMain(func() { opaque = objc.Send[bool](w.win, sel("isOpaque")) })
	if opaque {
		t.Error("a transparent window is opaque")
	}
	if !w.ChromeState().Transparent {
		t.Error("state lacks transparent")
	}
}

func TestGUIChromeVibrancy(t *testing.T) {
	w := chromeWin(t, caps.ChromeOptions{})
	if err := w.SetVibrancy("jelly"); err == nil {
		t.Fatal("an unknown material was accepted")
	}
	if err := w.SetVibrancy("sidebar"); err != nil {
		t.Fatal(err)
	}
	var material int
	var opaque bool
	var effects int
	onMain(func() {
		opaque = objc.Send[bool](w.win, sel("isOpaque"))
		fx := w.chrome.fx
		if fx != 0 {
			material = objc.Send[int](fx, sel("material"))
			effects = 1
			if objc.Send[objc.ID](fx, sel("superview")) == 0 {
				effects = 0
			}
		}
	})
	if effects != 1 || material != 7 {
		t.Errorf("effect view present=%d material=%d, want sidebar (7)", effects, material)
	}
	if opaque {
		t.Error("a vibrant window is opaque")
	}
	st := w.ChromeState()
	if st.Vibrancy != "sidebar" || !st.Transparent {
		t.Errorf("state %+v", st)
	}
	if err := w.SetVibrancy("hudWindow"); err != nil {
		t.Fatal(err)
	}
	onMain(func() { material = objc.Send[int](w.chrome.fx, sel("material")) })
	if material != 13 {
		t.Errorf("material after the change = %d, want 13", material)
	}
	if err := w.SetVibrancy(""); err != nil {
		t.Fatal(err)
	}
	onMain(func() { effects = int(w.chrome.fx); opaque = objc.Send[bool](w.win, sel("isOpaque")) })
	if effects != 0 || !opaque || w.ChromeState().Vibrancy != "" {
		t.Errorf("vibrancy not removed: fx=%d opaque=%v", effects, opaque)
	}
}

func TestGUIChromeVibrancyAtCreation(t *testing.T) {
	w := chromeWin(t, caps.ChromeOptions{Vibrancy: "underWindowBackground", TitleBar: caps.TitleBarHidden})
	var material int
	onMain(func() { material = objc.Send[int](w.chrome.fx, sel("material")) })
	if material != 21 {
		t.Errorf("material %d, want 21", material)
	}
}

func TestGUIChromeDocumentState(t *testing.T) {
	w := chromeWin(t, caps.ChromeOptions{})
	if err := w.SetDocumentEdited(true); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.SetRepresentedFile(f); err != nil {
		t.Fatal(err)
	}
	waitCond(t, "document state", func() bool {
		st := w.ChromeState()
		return st.DocumentEdited && st.RepresentedFile == f
	})
	if err := w.SetDocumentEdited(false); err != nil {
		t.Fatal(err)
	}
	if err := w.SetRepresentedFile(""); err != nil {
		t.Fatal(err)
	}
	waitCond(t, "document state cleared", func() bool {
		st := w.ChromeState()
		return !st.DocumentEdited && st.RepresentedFile == ""
	})
}

func TestGUIChromeTabbing(t *testing.T) {
	a := chromeWin(t, caps.ChromeOptions{TabbingID: "krate-test", TabbingMode: caps.TabbingPreferred})
	b := chromeWin(t, caps.ChromeOptions{TabbingID: "krate-test", TabbingMode: caps.TabbingPreferred})
	st := a.ChromeState()
	if st.TabbingID != "krate-test" || st.TabbingMode != "preferred" {
		t.Fatalf("state %+v", st)
	}
	var mode int
	onMain(func() { mode = objc.Send[int](a.win, sel("tabbingMode")) })
	if mode != 1 {
		t.Errorf("tabbing mode %d, want 1 (preferred)", mode)
	}
	if err := a.SetTabbing("krate-test", "bogus"); err == nil {
		t.Error("a bad tabbing mode was accepted")
	}
	if got := a.Tabs(); len(got) != 1 || got[0] != a.Ref() {
		t.Errorf("a lone window's tab group is %v", got)
	}
	events := make(chan caps.ChromeEvent, 8)
	guiApp.OnChromeEvent(func(e caps.ChromeEvent) { events <- e })
	defer guiApp.OnChromeEvent(nil)
	if err := a.MergeAllWindows(); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-events:
		if e.Kind != caps.ChromeTabsChanged {
			t.Errorf("event %+v", e)
		}
	case <-time.After(5 * time.Second):
		t.Error("no tabs-changed event")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(a.Tabs()) < 2 {
		time.Sleep(50 * time.Millisecond)
	}
	if got := a.Tabs(); len(got) != 2 {
		t.Skipf("the session did not merge the windows into tabs (tab group %v); the merge needs a regular, visible app", got)
	}
	if err := b.MoveTabToNewWindow(); err != nil {
		t.Fatal(err)
	}
	waitCond(t, "tab moved out", func() bool { return len(a.Tabs()) == 1 })
}

func TestGUIChromeSheet(t *testing.T) {
	parent := chromeWin(t, caps.ChromeOptions{})
	child := chromeWin(t, caps.ChromeOptions{})
	if err := parent.AttachSheet(parent.Ref()); err == nil {
		t.Error("a window became its own sheet")
	}
	if err := parent.AttachSheet(caps.WindowRef(999999)); err == nil {
		t.Error("an unknown window became a sheet")
	}
	events := make(chan caps.ChromeEvent, 8)
	guiApp.OnChromeEvent(func(e caps.ChromeEvent) { events <- e })
	defer guiApp.OnChromeEvent(nil)
	if err := parent.AttachSheet(child.Ref()); err != nil {
		t.Fatal(err)
	}
	waitCond(t, "sheet attached", func() bool {
		var sheet objc.ID
		onMain(func() { sheet = parent.win.Send(sel("attachedSheet")) })
		return sheet == child.win && child.ChromeState().Sheet
	})
	if err := child.EndSheet(); err != nil {
		t.Fatal(err)
	}
	waitCond(t, "sheet ended", func() bool {
		var sheet objc.ID
		onMain(func() { sheet = parent.win.Send(sel("attachedSheet")) })
		return sheet == 0
	})
	select {
	case e := <-events:
		if e.Kind != caps.ChromeSheetEnded || e.Window != parent.Ref() {
			t.Errorf("event %+v", e)
		}
	case <-time.After(5 * time.Second):
		t.Error("no sheet-ended event")
	}
}

func TestGUIChromeActivationPolicy(t *testing.T) {
	requireGUI(t)
	if got := guiApp.ActivationPolicy(); got != caps.PolicyRegular {
		t.Fatalf("initial policy %q", got)
	}
	if err := guiApp.SetActivationPolicy("agent"); err == nil {
		t.Fatal("a bad policy was accepted")
	}
	if err := guiApp.SetDockVisible(false); err != nil {
		t.Fatal(err)
	}
	if got := guiApp.ActivationPolicy(); got != caps.PolicyAccessory {
		t.Errorf("after hiding the dock icon: %q", got)
	}
	if err := guiApp.SetDockVisible(true); err != nil {
		t.Fatal(err)
	}
	if got := guiApp.ActivationPolicy(); got != caps.PolicyRegular {
		t.Errorf("after showing the dock icon: %q", got)
	}
}

func TestGUIChromeShareValidates(t *testing.T) {
	w := chromeWin(t, caps.ChromeOptions{})
	if err := guiApp.Share(w.Ref(), caps.ShareItems{}); err == nil {
		t.Error("an empty share was accepted")
	}
	if err := guiApp.Share(w.Ref(), caps.ShareItems{URLs: []string{"javascript:1"}}); err == nil {
		t.Error("a javascript URL was accepted")
	}
}

func TestGUIChromeUnsupportedPieces(t *testing.T) {
	w := chromeWin(t, caps.ChromeOptions{})
	for name, err := range map[string]error{
		"backdrop": w.SetBackdrop("mica"),
		"overlay":  w.SetTitleBarOverlay(&caps.TitleBarOverlay{}),
		"resize":   w.StartResize("top"),
	} {
		if !errors.Is(err, krate.ErrUnsupported) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := w.SetBackdrop("none"); err != nil {
		t.Errorf("clearing the backdrop: %v", err)
	}
}

func TestGUIChromeDragWithoutAnEventDoesNotCrash(t *testing.T) {
	w := chromeWin(t, caps.ChromeOptions{TitleBar: caps.TitleBarNone})
	_ = w.StartDrag()
	if err := w.TitleBarDoubleClick(); err != nil {
		t.Fatal(err)
	}
	if err := w.ToggleMaximize(); err != nil {
		t.Fatal(err)
	}
}
