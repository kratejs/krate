//go:build windows

package windows

// Window chrome on Win32 and DWM: hidden and removed title bars (the caption
// is taken out of the non-client area by WM_NCCALCSIZE; Windows keeps drawing
// the caption buttons over the extended frame of a hidden title bar), system
// backdrops (Mica, Mica Alt, Acrylic), a transparent WebView2 background,
// native window moves and resizes started by the page script, WebView2's own
// support for the CSS app-region property, and the hit test that makes the
// maximize button show Snap Layouts. The pure parts are in chrome.go and
// caps/chrome.go and are unit-tested on every OS; this file is compiled for
// Windows and exercised by gui_chrome_test.go, which CI runs.

import (
	"fmt"
	"sync"
	"unsafe"

	xw "golang.org/x/sys/windows"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var (
	_ caps.WindowChrome   = (*Window)(nil)
	_ caps.WindowThumbBar = (*Window)(nil)
	_ caps.JumpLister     = (*App)(nil)
	_ caps.ChromeEvents   = (*App)(nil)
)

var (
	dwmapi = xw.NewLazySystemDLL("dwmapi.dll")

	pDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
	pDwmGetWindowAttribute = dwmapi.NewProc("DwmGetWindowAttribute")
	pDwmExtendFrame        = dwmapi.NewProc("DwmExtendFrameIntoClientArea")
	pDwmDefWindowProc      = dwmapi.NewProc("DwmDefWindowProc")
	pGetAsyncKeyState      = user32.NewProc("GetAsyncKeyState")
	pFillRect              = user32.NewProc("FillRect")
	pGetStockObject        = gdi32.NewProc("GetStockObject")
)

const (
	wmEraseBkgnd            = 0x0014
	wmNCCalcSize            = 0x0083
	wmNCHitTest             = 0x0084
	wmNCLButtonDown         = 0x00A1
	wmDwmCompositionChanged = 0x031E
	vkLButton               = 0x01
	smCXSizeFrame           = 32
	smCYCaption             = 4
	smCXPaddedBorder        = 92
	blackBrush              = 4
)

// nccalcsizeParams is NCCALCSIZE_PARAMS; only rgrc[0] is used.
type nccalcsizeParams struct {
	rgrc   [3]rect
	wndpos uintptr
}

// chromeState is a window's chrome (main thread only, except where noted).
type chromeState struct {
	titleBar      string
	transparent   bool
	backdrop      string
	overlay       *caps.TitleBarOverlay
	nativeRegions bool

	// Taskbar thumbnail buttons, see taskbar_windows.go.
	thumbs       []caps.ThumbButton
	taskbar      unsafe.Pointer // ITaskbarList3
	taskbarReady bool
	thumbsAdded  bool
	icons        []uintptr
}

var (
	chromeMsgOnce sync.Once
	// msgTaskbarButtonCreated is the registered message Explorer sends once a
	// window's taskbar button exists.
	msgTaskbarButtonCreated uint32

	chromeMu  sync.Mutex
	chromeFn  func(caps.ChromeEvent)
	buildOnce sync.Once
	buildNum  uint32
)

func ensureChromeMessages() {
	chromeMsgOnce.Do(func() {
		r, _, _ := pRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(wstr("TaskbarButtonCreated"))))
		msgTaskbarButtonCreated = uint32(r)
	})
}

// windowsBuild is the Windows build number.
func windowsBuild() uint32 {
	buildOnce.Do(func() { buildNum = xw.RtlGetVersion().BuildNumber })
	return buildNum
}

// OnChromeEvent sets the handler for chrome events.
func (a *App) OnChromeEvent(fn func(caps.ChromeEvent)) {
	chromeMu.Lock()
	chromeFn = fn
	chromeMu.Unlock()
}

func emitChrome(w *Window, kind, id string) {
	chromeMu.Lock()
	fn := chromeFn
	chromeMu.Unlock()
	if fn != nil {
		fn(caps.ChromeEvent{Window: stateOf(w).ref, Kind: kind, ID: id})
	}
}

func unsupported(format string, args ...any) error {
	return fmt.Errorf("%w: %s", krate.ErrUnsupported, fmt.Sprintf(format, args...))
}

func metric(index int, dpi int) int {
	if pGetSystemMetricsForDpi.Find() == nil {
		r, _, _ := pGetSystemMetricsForDpi.Call(uintptr(index), uintptr(dpi))
		return int(int32(r))
	}
	r, _, _ := pGetSystemMetrics.Call(uintptr(index))
	return int(int32(r))
}

// resizeBorder is the thickness of the resize border in pixels.
func resizeBorder(dpi int) int {
	return metric(smCXSizeFrame, dpi) + metric(smCXPaddedBorder, dpi)
}

// glass reports whether the window shows what is behind the page: a
// transparent window or one with a system backdrop.
func (c *chromeState) glass() bool {
	return c.transparent || (c.backdrop != "" && c.backdrop != caps.BackdropNone)
}

// captionPx is the height of the extended frame of a hidden title bar in
// pixels.
func (w *Window) captionPx() int {
	h := caps.DefaultOverlayHeight
	if o := w.chrome.overlay; o != nil && o.Height > 0 {
		h = o.Height
	}
	return scaleDPI(h, windowDPI(w.hwnd))
}

// initChrome applies the creation options (main thread, window registered).
func (w *Window) initChrome(o caps.ChromeOptions) {
	c := &w.chrome
	c.titleBar = o.TitleBar
	if c.titleBar == "" {
		c.titleBar = caps.TitleBarNative
	}
	c.transparent = o.Transparent
	if c.titleBar == caps.TitleBarHidden {
		ov := caps.TitleBarOverlay{Height: caps.DefaultOverlayHeight}
		if o.Overlay != nil {
			ov = *o.Overlay
		}
		c.overlay = &ov
	}
	w.refreshFrame()
	if o.Backdrop != "" {
		if err := w.setBackdrop(o.Backdrop); err != nil {
			logf("windows: backdrop %q: %v", o.Backdrop, err)
		}
	}
	w.applyCaptionColors()
}

// refreshFrame re-runs the non-client calculation and the DWM margins after a
// change to the title bar style.
func (w *Window) refreshFrame() {
	c := &w.chrome
	m := frameMargins(c.titleBar, w.captionPx(), c.backdrop != "" && c.backdrop != caps.BackdropNone)
	if m == (margins{}) && c.transparent {
		m = margins{-1, -1, -1, -1}
	}
	_, _, _ = pDwmExtendFrame.Call(w.hwnd, uintptr(unsafe.Pointer(&m)))
	_, _, _ = pSetWindowPos.Call(w.hwnd, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChg)
}

// applyCaptionColors sets the caption and symbol colors of a hidden title
// bar (Windows 11).
func (w *Window) applyCaptionColors() {
	if !captionColorSupported(windowsBuild()) {
		return
	}
	set := func(attr uintptr, color string) {
		v := uint32(colorDefault)
		if color != "" {
			if rgba, err := caps.ParseColor(color); err == nil {
				v = colorRef(rgba)
			}
		}
		_, _, _ = pDwmSetWindowAttribute.Call(w.hwnd, attr, uintptr(unsafe.Pointer(&v)), 4)
	}
	var cc, sc string
	if o := w.chrome.overlay; o != nil {
		cc, sc = o.Color, o.SymbolColor
	}
	set(dwmaCaptionClr, cc)
	set(dwmaTextColor, sc)
}

// chromeMessage handles the messages the chrome features need. It runs before
// the other handlers; handled=false lets the rest of the window procedure
// run.
func (w *Window) chromeMessage(message, wParam, lParam uintptr) (handled bool, ret uintptr) {
	c := &w.chrome
	switch message {
	case wmNCCalcSize:
		if !customFrame(c.titleBar) || wParam == 0 {
			return false, 0
		}
		p := (*nccalcsizeParams)(ptr(lParam))
		proposed := p.rgrc[0]
		r, _, _ := pDefWindowProcW.Call(w.hwnd, message, wParam, lParam)
		z, _, _ := pIsZoomed.Call(w.hwnd)
		p.rgrc[0].top = ncTop(proposed.top, p.rgrc[0].top, z != 0)
		return true, r
	case wmNCHitTest:
		if !customFrame(c.titleBar) {
			return false, 0
		}
		if c.titleBar == caps.TitleBarHidden {
			var lr uintptr
			if ok, _, _ := pDwmDefWindowProc.Call(w.hwnd, message, wParam, lParam, uintptr(unsafe.Pointer(&lr))); ok != 0 {
				return true, lr
			}
		}
		return true, uintptr(w.hitTest(int(int16(lParam&0xFFFF)), int(int16((lParam>>16)&0xFFFF))))
	case wmEraseBkgnd:
		if !c.glass() {
			return false, 0
		}
		var rc rect
		_, _, _ = pGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&rc)))
		brush, _, _ := pGetStockObject.Call(blackBrush)
		_, _, _ = pFillRect.Call(wParam, uintptr(unsafe.Pointer(&rc)), brush)
		return true, 1
	case wmCommand:
		if code, id := splitCommand(wParam); code == thbnClicked {
			w.thumbClicked(id)
			return true, 0
		}
	case wmDwmCompositionChanged:
		w.app.Dispatch(func() {
			if !w.destroyed {
				w.refreshFrame()
			}
		})
	case wmDpiChanged:
		w.app.Dispatch(func() {
			if !w.destroyed && customFrame(c.titleBar) {
				w.refreshFrame()
			}
		})
	case wmDestroy:
		w.releaseTaskbar()
	default:
		if msgTaskbarButtonCreated != 0 && message == uintptr(msgTaskbarButtonCreated) {
			c.taskbarReady = true
			w.applyThumbs()
			return true, 0
		}
	}
	return false, 0
}

// hitTest answers WM_NCHITTEST for a custom frame from the point in screen
// coordinates (the caption buttons of a hidden title bar are answered by
// DwmDefWindowProc before this).
func (w *Window) hitTest(px, py int) int {
	var rc rect
	_, _, _ = pGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&rc)))
	style, _, _ := pGetWindowLongPtrW.Call(w.hwnd, gwlStyle)
	z, _, _ := pIsZoomed.Call(w.hwnd)
	in := ncHit(int(rc.left), int(rc.top), int(rc.right), int(rc.bottom), px, py,
		resizeBorder(windowDPI(w.hwnd)), style&wsThickFrameB != 0, z != 0, caps.HitRect{}, 0)
	// The extended frame of a hidden title bar belongs to the page: dragging
	// comes from the page's regions, not from a strip the OS would claim.
	return caps.HitTest(in)
}

// ChromeState reports the window's chrome.
func (w *Window) ChromeState() (st caps.ChromeState) {
	w.app.callMain(func() {
		if w.destroyed {
			return
		}
		c := &w.chrome
		st.TitleBar = c.titleBar
		if st.TitleBar == "" {
			st.TitleBar = caps.TitleBarNative
		}
		st.Transparent = c.glass()
		st.Backdrop = c.backdrop
		if c.overlay != nil {
			o := *c.overlay
			st.Overlay = &o
		}
		st.NativeRegions = c.nativeRegions
		st.ThumbButtons = len(c.thumbs)
		style, _, _ := pGetWindowLongPtrW.Call(w.hwnd, gwlStyle)
		st.Resizable = style&wsThickFrameB != 0
		st.Decorated = style&wsCaptionBits == wsCaptionBits && !customFrame(c.titleBar)
		st.Tabs = 1
		st.Features = []string{caps.FeatureTitleBar, caps.FeatureTransparent, caps.FeatureDrag, caps.FeatureResize, caps.FeatureThumbButtons}
		if backdropSupported(windowsBuild()) {
			st.Features = append(st.Features, caps.FeatureBackdrop)
		}
		if c.titleBar == caps.TitleBarHidden {
			st.Features = append(st.Features, caps.FeatureTitleBarOverlay)
		}
	})
	return
}

// SetTitleBar switches the title bar style.
func (w *Window) SetTitleBar(style string) error {
	style, ok := caps.NormalizeTitleBar(style)
	if !ok {
		return fmt.Errorf("titleBar must be native, hidden or none")
	}
	w.onMain(func() {
		c := &w.chrome
		cur, _, _ := pGetWindowLongPtrW.Call(w.hwnd, gwlStyle)
		next := chromeStyle(uint32(cur)|wsSysMenu|wsCaptionBits, style)
		_, _, _ = pSetWindowLongPtrW.Call(w.hwnd, gwlStyle, uintptr(next))
		c.titleBar = style
		if style == caps.TitleBarHidden && c.overlay == nil {
			c.overlay = &caps.TitleBarOverlay{Height: caps.DefaultOverlayHeight}
		}
		if style != caps.TitleBarHidden {
			c.overlay = nil
		}
		w.refreshFrame()
		w.applyCaptionColors()
		w.placeViews()
	})
	return nil
}

// setBackdrop sets the DWM system backdrop (main thread).
func (w *Window) setBackdrop(name string) error {
	t, ok := caps.BackdropType(name)
	if !ok {
		return fmt.Errorf("unknown backdrop %q", name)
	}
	if !backdropSupported(windowsBuild()) {
		if name == "" || name == caps.BackdropNone {
			return nil
		}
		return unsupported("window backdrops need Windows 11 22H2 (build %d); this is build %d", minBackdropBuild, windowsBuild())
	}
	v := int32(t)
	hr, _, _ := pDwmSetWindowAttribute.Call(w.hwnd, dwmaBackdrop, uintptr(unsafe.Pointer(&v)), 4)
	if failed(hr) {
		return fmt.Errorf("windows: DwmSetWindowAttribute(backdrop): 0x%08x", uint32(hr))
	}
	if name == caps.BackdropNone {
		name = ""
	}
	w.chrome.backdrop = name
	w.refreshFrame()
	for _, v := range w.allViews() {
		v.applyChromeToController()
	}
	return nil
}

// SetBackdrop sets a Windows 11 system backdrop. The webview becomes
// transparent so that the backdrop shows through the page.
func (w *Window) SetBackdrop(name string) (err error) {
	if _, ok := caps.BackdropType(name); !ok {
		return fmt.Errorf("unknown backdrop %q", name)
	}
	w.app.callMain(func() {
		if w.destroyed {
			err = fmt.Errorf("window closed")
			return
		}
		err = w.setBackdrop(name)
	})
	return
}

// SetVibrancy is a macOS feature.
func (w *Window) SetVibrancy(material string) error {
	if material == "" {
		return nil
	}
	return unsupported("vibrancy is a macOS feature; use a backdrop (mica, micaAlt, acrylic) on Windows 11")
}

// SetTitleBarOverlay sets the colors and height of the caption button area of
// a hidden title bar; nil restores the defaults.
func (w *Window) SetTitleBarOverlay(o *caps.TitleBarOverlay) (err error) {
	var n caps.TitleBarOverlay
	if o != nil {
		if n, err = o.Normalize(); err != nil {
			return err
		}
	} else {
		n = caps.TitleBarOverlay{Height: caps.DefaultOverlayHeight}
	}
	w.app.callMain(func() {
		if w.destroyed {
			err = fmt.Errorf("window closed")
			return
		}
		if w.chrome.titleBar != caps.TitleBarHidden {
			err = unsupported("the caption button overlay belongs to a hidden title bar; set titleBar to \"hidden\" first")
			return
		}
		w.chrome.overlay = &n
		w.refreshFrame()
		w.applyCaptionColors()
	})
	return
}

// SetTrafficLightPosition is a macOS feature.
func (w *Window) SetTrafficLightPosition(p caps.Point) error {
	return unsupported("traffic lights are a macOS feature")
}

// StartDrag starts the system window move from the left button press that is
// happening.
func (w *Window) StartDrag() error {
	w.onMain(func() {
		if down, _, _ := pGetAsyncKeyState.Call(vkLButton); down&0x8000 == 0 {
			return // already released: there is nothing to drag
		}
		w.postNC(caps.HTCAPTION)
	})
	return nil
}

// StartResize starts the system resize from an edge.
func (w *Window) StartResize(edge string) error {
	ht := htForEdge(edge)
	if ht < 0 {
		return fmt.Errorf("unknown resize edge %q", edge)
	}
	w.onMain(func() {
		if down, _, _ := pGetAsyncKeyState.Call(vkLButton); down&0x8000 == 0 {
			return
		}
		w.postNC(ht)
	})
	return nil
}

// postNC posts the non-client press that makes Windows run its move or
// resize loop.
func (w *Window) postNC(ht int) {
	var pt point
	_, _, _ = pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	_, _, _ = pReleaseCapture.Call()
	lp := uintptr(uint16(pt.x)) | uintptr(uint16(pt.y))<<16
	_, _, _ = pPostMessageW.Call(w.hwnd, wmNCLButtonDown, uintptr(ht), lp)
}

// ToggleMaximize maximizes the window or restores it.
func (w *Window) ToggleMaximize() error {
	w.onMain(func() {
		if z, _, _ := pIsZoomed.Call(w.hwnd); z != 0 {
			_, _, _ = pShowWindow.Call(w.hwnd, swRestore)
		} else {
			_, _, _ = pShowWindow.Call(w.hwnd, swMaximize)
		}
	})
	return nil
}

// applyChromeToController sets the WebView2 side of the chrome on a view: a
// transparent background when the window is glass, and the engine's own
// handling of app-region for windows without a native title bar.
func (v *view) applyChromeToController() {
	w := v.w
	c := &w.chrome
	if v.controller == nil {
		return
	}
	if c2 := queryInterface(v.controller, iidWebView2Controller2); c2 != nil {
		col := caps.RGBA{R: 255, G: 255, B: 255, A: 255}
		if c.glass() {
			col = caps.RGBA{}
		}
		if hr := vcall(c2, c2PutDefaultBackground, uintptr(webviewColor(col))); failed(hr) {
			logOnce("defaultbg", "chrome: put_DefaultBackgroundColor failed: 0x%08x", uint32(hr))
		}
		comRelease(c2)
	} else {
		logOnce("c2", "chrome: ICoreWebView2Controller2 not available (older WebView2 runtime); the page background stays opaque")
	}
	if !customFrame(c.titleBar) || v.webview == nil {
		return
	}
	var settings uintptr
	if failed(vcall(v.webview, wvGetSettings, uintptr(unsafe.Pointer(&settings)))) || settings == 0 {
		return
	}
	defer comRelease(ptr(settings))
	s9 := queryInterface(ptr(settings), iidWebView2Settings9)
	if s9 == nil {
		logOnce("s9", "chrome: ICoreWebView2Settings9 not available (older WebView2 runtime); app-region is handled by the page script")
		return
	}
	defer comRelease(s9)
	if hr := vcall(s9, set9PutNonClientRegion, 1); failed(hr) {
		logOnce("s9put", "chrome: put_IsNonClientRegionSupportEnabled failed: 0x%08x", uint32(hr))
		return
	}
	c.nativeRegions = true
}
