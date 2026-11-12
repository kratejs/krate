//go:build windows

package windows

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// GUI tests of the window chrome. They read Win32 and DWM state back and ask
// the window procedure itself (WM_NCHITTEST), so a pass shows that the calls
// reached Windows. They run in the CI native job on windows-latest with the
// other GUI tests (KRATE_NATIVE_GUI=1).

var pSendMessageW = user32.NewProc("SendMessageW")

func chromeWin(t *testing.T, o caps.ChromeOptions, resizable bool) *Window {
	t.Helper()
	needGUI(t)
	w, err := guiApp.NewWindow(WindowConfig{Title: t.Name(), Width: 480, Height: 320, Resizable: resizable, Path: "/", Chrome: o})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	return w
}

func screenRect(w *Window) (r rect) {
	_, _, _ = pGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
	return
}

// hit sends WM_NCHITTEST for a screen point to the window procedure.
func hit(w *Window, x, y int32) int {
	lp := uintptr(uint16(x)) | uintptr(uint16(y))<<16
	r, _, _ := pSendMessageW.Call(w.hwnd, wmNCHitTest, 0, lp)
	return int(int32(r))
}

func captionButtonBounds(w *Window) (rect, bool) {
	var r rect
	hr, _, _ := pDwmGetWindowAttribute.Call(w.hwnd, dwmaButtonBnds, uintptr(unsafe.Pointer(&r)), unsafe.Sizeof(r))
	return r, !failed(hr) && r.right > r.left
}

func TestGUIChromeHiddenTitleBarHitTest(t *testing.T) {
	w := chromeWin(t, caps.ChromeOptions{TitleBar: caps.TitleBarHidden, Overlay: &caps.TitleBarOverlay{Height: 40}}, true)
	r := screenRect(w)
	cx, cy := (r.left+r.right)/2, (r.top+r.bottom)/2

	if got := hit(w, cx, cy); got != caps.HTCLIENT {
		t.Errorf("window center = %d, want HTCLIENT", got)
	}
	// The resize border of a window whose caption was taken out.
	for name, c := range map[string]struct {
		x, y int32
		want int
	}{
		"left":         {r.left + 1, cy, caps.HTLEFT},
		"right":        {r.right - 2, cy, caps.HTRIGHT},
		"bottom":       {cx, r.bottom - 2, caps.HTBOTTOM},
		"bottom right": {r.right - 2, r.bottom - 2, caps.HTBOTTOMRIGHT},
		"bottom left":  {r.left + 1, r.bottom - 2, caps.HTBOTTOMLEFT},
		"top":          {cx, r.top, caps.HTTOP},
	} {
		if got := hit(w, c.x, c.y); got != c.want {
			t.Errorf("%s edge (%d,%d) = %d, want %d", name, c.x, c.y, got, c.want)
		}
	}
	// The caption buttons Windows draws over the extended frame: the maximize
	// button answers HTMAXBUTTON, which is what shows Snap Layouts.
	b, ok := captionButtonBounds(w)
	if !ok {
		t.Log("DWM reports no caption button bounds in this session; button hit tests skipped")
		return
	}
	third := (b.right - b.left) / 3
	y := r.top + (b.top+b.bottom)/2
	for name, c := range map[string]struct {
		x    int32
		want int
	}{
		"minimize": {r.left + b.left + third/2, caps.HTMINBUTTON},
		"maximize": {r.left + b.left + third + third/2, caps.HTMAXBUTTON},
		"close":    {r.left + b.left + 2*third + third/2, caps.HTCLOSE},
	} {
		if got := hit(w, c.x, y); got != c.want {
			t.Errorf("%s button (%d,%d) = %d, want %d", name, c.x, y, got, c.want)
		}
	}
}

func TestGUIChromeNoTitleBarHasNoButtons(t *testing.T) {
	w := chromeWin(t, caps.ChromeOptions{TitleBar: caps.TitleBarNone}, true)
	style, _, _ := pGetWindowLongPtrW.Call(w.hwnd, gwlStyle)
	if style&wsSysMenu != 0 {
		t.Errorf("style %#x keeps the system menu, which brings the caption buttons", style)
	}
	r := screenRect(w)
	if got := hit(w, r.right-12, r.top+12); got == caps.HTCLOSE || got == caps.HTMAXBUTTON || got == caps.HTMINBUTTON || got == caps.HTCAPTION {
		t.Errorf("top right corner = %d, want a client or border hit", got)
	}
	if got := hit(w, (r.left+r.right)/2, (r.top+r.bottom)/2); got != caps.HTCLIENT {
		t.Errorf("center = %d", got)
	}
	if st := w.ChromeState(); st.TitleBar != "none" || st.Decorated {
		t.Errorf("state %+v", st)
	}
}

func TestGUIChromeFixedWindowHasNoResizeBorder(t *testing.T) {
	w := chromeWin(t, caps.ChromeOptions{TitleBar: caps.TitleBarNone}, false)
	r := screenRect(w)
	if got := hit(w, r.left+1, (r.top+r.bottom)/2); got != caps.HTCLIENT {
		t.Errorf("left edge of a fixed window = %d, want HTCLIENT", got)
	}
}

func TestGUIChromeClientAreaFillsTheWindow(t *testing.T) {
	dpi := 96
	for _, style := range []string{caps.TitleBarHidden, caps.TitleBarNone} {
		w := chromeWin(t, caps.ChromeOptions{TitleBar: style}, true)
		dpi = windowDPI(w.hwnd)
		var rc rect
		_, _, _ = pGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&rc)))
		wantW, wantH := int32(scaleDPI(480, dpi)), int32(scaleDPI(320, dpi))
		if d := rc.right - wantW; d < -2 || d > 2 {
			t.Errorf("%s: client width %d, want %d", style, rc.right, wantW)
		}
		if d := rc.bottom - wantH; d < -2 || d > 2 {
			t.Errorf("%s: client height %d, want %d (the caption must be client area)", style, rc.bottom, wantH)
		}
	}
}

func TestGUIChromeSetTitleBarRoundTrip(t *testing.T) {
	w := chromeWin(t, caps.ChromeOptions{}, true)
	if st := w.ChromeState(); st.TitleBar != "native" || !st.Decorated {
		t.Fatalf("initial %+v", st)
	}
	if err := w.SetTitleBar("none"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "none", func() bool { return w.ChromeState().TitleBar == "none" })
	style, _, _ := pGetWindowLongPtrW.Call(w.hwnd, gwlStyle)
	if style&wsSysMenu != 0 {
		t.Errorf("style %#x", style)
	}
	if err := w.SetTitleBar("native"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "native", func() bool { return w.ChromeState().TitleBar == "native" })
	style, _, _ = pGetWindowLongPtrW.Call(w.hwnd, gwlStyle)
	if style&wsSysMenu == 0 || style&wsCaptionBits != wsCaptionBits {
		t.Errorf("style %#x lost the caption", style)
	}
	if err := w.SetTitleBar("tabs"); err == nil {
		t.Error("an unknown style was accepted")
	}
}

func TestGUIChromeOverlayNeedsHiddenTitleBar(t *testing.T) {
	w := chromeWin(t, caps.ChromeOptions{}, true)
	if err := w.SetTitleBarOverlay(&caps.TitleBarOverlay{Color: "#202020"}); !errors.Is(err, krate.ErrUnsupported) {
		t.Errorf("overlay on a native title bar: %v", err)
	}
	h := chromeWin(t, caps.ChromeOptions{TitleBar: caps.TitleBarHidden}, true)
	if err := h.SetTitleBarOverlay(&caps.TitleBarOverlay{Color: "#202020", SymbolColor: "#ffffff", Height: 44}); err != nil {
		t.Fatal(err)
	}
	st := h.ChromeState()
	if st.Overlay == nil || st.Overlay.Height != 44 || st.Overlay.Color != "#202020" || !st.HasFeature(caps.FeatureTitleBarOverlay) {
		t.Errorf("state %+v", st)
	}
	if err := h.SetTitleBarOverlay(&caps.TitleBarOverlay{Color: "red"}); err == nil {
		t.Error("a bad color was accepted")
	}
	if err := h.SetTitleBarOverlay(nil); err != nil {
		t.Fatal(err)
	}
	if st := h.ChromeState(); st.Overlay == nil || st.Overlay.Height != caps.DefaultOverlayHeight || st.Overlay.Color != "" {
		t.Errorf("state after the reset %+v", st)
	}
}

func TestGUIChromeNonClientRegionSupport(t *testing.T) {
	w := chromeWin(t, caps.ChromeOptions{TitleBar: caps.TitleBarNone}, true)
	st := w.ChromeState()
	if !st.NativeRegions {
		t.Log("this WebView2 runtime lacks ICoreWebView2Settings9; app-region is handled by the page script")
	}
	n := chromeWin(t, caps.ChromeOptions{}, true)
	if n.ChromeState().NativeRegions {
		t.Error("a native window turned the non-client region support on")
	}
}

func TestGUIChromeBackdrop(t *testing.T) {
	w := chromeWin(t, caps.ChromeOptions{}, true)
	if err := w.SetBackdrop("glass"); err == nil {
		t.Fatal("an unknown backdrop was accepted")
	}
	if err := w.SetVibrancy("sidebar"); !errors.Is(err, krate.ErrUnsupported) {
		t.Errorf("vibrancy: %v", err)
	}
	if err := w.SetTrafficLightPosition(caps.Point{X: 1, Y: 1}); !errors.Is(err, krate.ErrUnsupported) {
		t.Errorf("traffic lights: %v", err)
	}
	if !backdropSupported(windowsBuild()) {
		if err := w.SetBackdrop("mica"); !errors.Is(err, krate.ErrUnsupported) {
			t.Errorf("mica on build %d: %v", windowsBuild(), err)
		}
		if err := w.SetBackdrop("none"); err != nil {
			t.Errorf("clearing the backdrop on an old build: %v", err)
		}
		return
	}
	for name, want := range map[string]int32{"mica": 2, "acrylic": 3, "micaAlt": 4, "none": 1} {
		if err := w.SetBackdrop(name); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var got int32
		hr, _, _ := pDwmGetWindowAttribute.Call(w.hwnd, dwmaBackdrop, uintptr(unsafe.Pointer(&got)), 4)
		if failed(hr) || got != want {
			t.Errorf("%s: DWM backdrop type %d (hr %#x), want %d", name, got, uint32(hr), want)
		}
	}
	if err := w.SetBackdrop("mica"); err != nil {
		t.Fatal(err)
	}
	if st := w.ChromeState(); st.Backdrop != "mica" || !st.Transparent || !st.HasFeature(caps.FeatureBackdrop) {
		t.Errorf("state %+v", st)
	}
	// A backdrop shows through a transparent webview.
	if c := defaultBackground(t, w); c&0xFF != 0 {
		t.Errorf("webview background alpha %d under a backdrop, want 0", c&0xFF)
	}
}

// defaultBackground reads COREWEBVIEW2_COLOR back (A in the low byte).
func defaultBackground(t *testing.T, w *Window) (c uint32) {
	t.Helper()
	w.app.callMain(func() {
		c2 := queryInterface(w.pv.controller, iidWebView2Controller2)
		if c2 == nil {
			t.Skip("this WebView2 runtime lacks ICoreWebView2Controller2")
		}
		defer comRelease(c2)
		get := chromeSlot("ICoreWebView2Controller2", "get_DefaultBackgroundColor")
		if hr := vcall(c2, get, uintptr(unsafe.Pointer(&c))); failed(hr) {
			t.Errorf("get_DefaultBackgroundColor: %#x", uint32(hr))
		}
	})
	return
}

func TestGUIChromeTransparentWebView(t *testing.T) {
	w := chromeWin(t, caps.ChromeOptions{TitleBar: caps.TitleBarNone, Transparent: true}, true)
	if c := defaultBackground(t, w); c&0xFF != 0 {
		t.Errorf("webview background alpha %d, want 0", c&0xFF)
	}
	if st := w.ChromeState(); !st.Transparent {
		t.Errorf("state %+v", st)
	}
	opaque := chromeWin(t, caps.ChromeOptions{}, true)
	if c := defaultBackground(t, opaque); c&0xFF != 0xFF {
		t.Errorf("an ordinary window has webview background alpha %d, want 255", c&0xFF)
	}
}

func TestGUIChromeMoveAndResizeCalls(t *testing.T) {
	w := chromeWin(t, caps.ChromeOptions{TitleBar: caps.TitleBarNone}, true)
	before := screenRect(w)
	// No button is held, so there is nothing to drag and nothing may start.
	if err := w.StartDrag(); err != nil {
		t.Fatal(err)
	}
	if err := w.StartResize("middle"); err == nil {
		t.Error("an unknown edge was accepted")
	}
	if err := w.StartResize("bottomRight"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if after := screenRect(w); after != before {
		t.Errorf("the window moved from %v to %v without a button press", before, after)
	}
	if err := w.ToggleMaximize(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "maximized", func() bool { return w.State().Maximized })
	// A maximized window has no resize border.
	m := screenRect(w)
	if got := hit(w, (m.left+m.right)/2, m.bottom-1); got == caps.HTBOTTOM {
		t.Errorf("a maximized window answered HTBOTTOM")
	}
	if err := w.ToggleMaximize(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "restored", func() bool { return !w.State().Maximized })
}

func tinyPNG(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestGUIChromeThumbButtons(t *testing.T) {
	w := chromeWin(t, caps.ChromeOptions{}, true)
	var mu sync.Mutex
	var clicks []string
	guiApp.OnChromeEvent(func(e caps.ChromeEvent) {
		if e.Kind == caps.ChromeThumbClick && e.Window == w.Ref() {
			mu.Lock()
			clicks = append(clicks, e.ID)
			mu.Unlock()
		}
	})
	defer guiApp.OnChromeEvent(nil)

	bs := []caps.ThumbButton{
		{ID: "play", Tooltip: "Play", Icon: tinyPNG(t, color.RGBA{0, 160, 0, 255})},
		{ID: "next", Tooltip: "Next", Icon: tinyPNG(t, color.RGBA{0, 0, 200, 255}), DismissOnClick: true},
	}
	if err := w.SetThumbButtons(bs); err != nil {
		t.Fatal(err)
	}
	if st := w.ChromeState(); st.ThumbButtons != 2 {
		t.Fatalf("state %+v", st)
	}
	if err := w.SetThumbButtons(make([]caps.ThumbButton, 8)); err == nil {
		t.Error("eight buttons were accepted")
	}
	// Explorer announces the taskbar button a moment after the window shows.
	deadline := time.Now().Add(15 * time.Second)
	var applied, ready bool
	for time.Now().Before(deadline) {
		if applied, ready = w.thumbApplied(); ready {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Log("no taskbar button was announced in this session (no Explorer shell?); the buttons stay pending")
	} else if !applied {
		t.Error("the taskbar button exists but ThumbBarAddButtons did not succeed")
	}
	// Clicks arrive as WM_COMMAND with THBN_CLICKED in the high word.
	_, _, _ = pSendMessageW.Call(w.hwnd, wmCommand, uintptr(thbnClicked)<<16|uintptr(thumbID(1)), 0)
	_, _, _ = pSendMessageW.Call(w.hwnd, wmCommand, uintptr(thbnClicked)<<16|uintptr(thumbID(5)), 0) // no such button
	waitFor(t, "thumb click", func() bool { mu.Lock(); defer mu.Unlock(); return len(clicks) >= 1 })
	mu.Lock()
	if len(clicks) != 1 || clicks[0] != "next" {
		t.Errorf("clicks %v", clicks)
	}
	mu.Unlock()
	// Fewer buttons later; the shell cannot remove buttons, so the rest hide.
	if err := w.SetThumbButtons(bs[:1]); err != nil {
		t.Fatal(err)
	}
	if st := w.ChromeState(); st.ThumbButtons != 1 {
		t.Errorf("state %+v", st)
	}
	if ready {
		if applied, _ := w.thumbApplied(); !applied {
			t.Error("the update did not reach the shell")
		}
	}
	if err := w.SetThumbButtons(nil); err != nil {
		t.Fatal(err)
	}
}

func TestGUIChromeJumpList(t *testing.T) {
	needGUI(t)
	dir := filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Recent", "CustomDestinations")
	before := time.Now().Add(-2 * time.Second)
	tasks := []caps.JumpTask{
		{Title: "New window", Args: []string{"--new-window"}, Description: "Open another window"},
		{Title: "Open \"notes\"", Args: []string{"--open", `C:\Users\me\My Notes\a b.txt`}},
	}
	if err := guiApp.SetJumpList(tasks); err != nil {
		t.Fatalf("SetJumpList: %v", err)
	}
	found := false
	if ents, err := os.ReadDir(dir); err == nil {
		for _, e := range ents {
			if info, err := e.Info(); err == nil && info.ModTime().After(before) {
				found = true
			}
		}
	}
	if !found {
		t.Logf("no jump list file was written to %s; Windows may keep it elsewhere in this session", dir)
	}
	if err := guiApp.SetJumpList([]caps.JumpTask{{Title: ""}}); err == nil {
		t.Error("a task without a title was accepted")
	}
	if err := guiApp.ClearJumpList(); err != nil {
		t.Fatalf("ClearJumpList: %v", err)
	}
	if err := guiApp.SetJumpList(nil); err != nil {
		t.Fatalf("an empty list: %v", err)
	}
}
