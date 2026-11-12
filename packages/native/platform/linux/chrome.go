//go:build linux

package linux

import (
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Window chrome on GTK: frameless windows, transparency, native move and
// resize started from the injected page script (see internal/chrome), and the
// double-click maximize of a title bar. Backdrops, vibrancy, caption overlays
// and the rest of the macOS and Windows features answer krate.ErrUnsupported.

var _ caps.WindowChrome = (*Window)(nil)

// chromeAPI holds the GTK symbols the chrome code needs. A missing symbol
// leaves its field nil and the feature answers ErrUnsupported.
type chromeAPI struct {
	gtkBeginMove    func(w uintptr, button, x, y int32, ts uint32)
	gtkBeginResize  func(w uintptr, edge, button, x, y int32, ts uint32)
	gtkIsMaximized  func(w uintptr) int32
	gtkAppPaintable func(w uintptr, on int32)
	gtkWidgetScreen func(w uintptr) uintptr
	gtkSetVisual    func(w, visual uintptr)
	gdkRGBAVisual   func(screen uintptr) uintptr
	gdkComposited   func(screen uintptr) int32
	gdkWinDevPos    func(win, dev uintptr, x, y *int32, mask *uint32) uintptr
	gtkCSSNew       func() uintptr
	gtkCSSLoad      func(p uintptr, data string, n int64, gerr *uintptr) int32
	gtkStyleCtx     func(w uintptr) uintptr
	gtkCtxAddProv   func(ctx, prov uintptr, prio uint32)
	gtkCtxAddClass  func(ctx uintptr, name string)
	wkSetBackground func(view uintptr, rgba unsafe.Pointer)
}

var chromeLib struct {
	once sync.Once
	api  *chromeAPI
	cb   uintptr
}

func loadChromeAPI() *chromeAPI {
	chromeLib.once.Do(func() {
		c := &chromeAPI{}
		b := &binder{}
		open := func(names ...string) uintptr {
			for _, n := range names {
				if h, err := purego.Dlopen(n, rtldNow|rtldGlobal); err == nil {
					return h
				}
			}
			return 0
		}
		gtk, gdk, wk := open("libgtk-3.so.0"), open("libgdk-3.so.0"), open("libwebkit2gtk-4.1.so.0")
		bind := func(lib uintptr, fn any, sym string) {
			if lib != 0 {
				b.bind(fn, lib, sym)
			}
		}
		bind(gtk, &c.gtkBeginMove, "gtk_window_begin_move_drag")
		bind(gtk, &c.gtkBeginResize, "gtk_window_begin_resize_drag")
		bind(gtk, &c.gtkIsMaximized, "gtk_window_is_maximized")
		bind(gtk, &c.gtkAppPaintable, "gtk_widget_set_app_paintable")
		bind(gtk, &c.gtkWidgetScreen, "gtk_widget_get_screen")
		bind(gtk, &c.gtkSetVisual, "gtk_widget_set_visual")
		bind(gdk, &c.gdkRGBAVisual, "gdk_screen_get_rgba_visual")
		bind(gdk, &c.gdkComposited, "gdk_screen_is_composited")
		bind(gdk, &c.gdkWinDevPos, "gdk_window_get_device_position")
		bind(gtk, &c.gtkCSSNew, "gtk_css_provider_new")
		bind(gtk, &c.gtkCSSLoad, "gtk_css_provider_load_from_data")
		bind(gtk, &c.gtkStyleCtx, "gtk_widget_get_style_context")
		bind(gtk, &c.gtkCtxAddProv, "gtk_style_context_add_provider")
		bind(gtk, &c.gtkCtxAddClass, "gtk_style_context_add_class")
		bind(wk, &c.wkSetBackground, "webkit_web_view_set_background_color")
		chromeLib.api = c
		chromeLib.cb = purego.NewCallback(pressCallback)
	})
	return chromeLib.api
}

// press is the last button press seen on one of the window's webviews. GTK
// needs its button, position and time to start a move or resize.
type press struct {
	button uint32
	xr, yr float64
	time   uint32
	at     time.Time
}

const gdkButtonPress = 4

// pressCallback records a button press on a webview. It only observes: the
// webview still handles the event.
func pressCallback(widget, event, ref uintptr) uintptr {
	if event == 0 {
		return 0
	}
	ev := *(*unsafe.Pointer)(unsafe.Pointer(&event))
	if *(*int32)(ev) != gdkButtonPress {
		return 0
	}
	v := theApp.viewByRef(ref)
	if v == nil {
		return 0
	}
	v.w.lastPress = press{
		time:   *(*uint32)(unsafe.Add(ev, 20)),
		button: *(*uint32)(unsafe.Add(ev, 52)),
		xr:     *(*float64)(unsafe.Add(ev, 64)),
		yr:     *(*float64)(unsafe.Add(ev, 72)),
		at:     time.Now(),
	}
	return 0
}

// connectChrome listens for button presses on a view.
func (a *App) connectChrome(v *view) {
	if c := loadChromeAPI(); c != nil && chromeLib.cb != 0 {
		a.api.gSignalConnectData(v.widget, "button-press-event", chromeLib.cb, uintptr(v.ref), 0, 0)
	}
	v.w.clearViewBackground(v)
}

// chromeState is the chrome of a window (main thread only).
type chromeState struct {
	titleBar    string
	transparent bool
}

// applyChrome sets what the options ask for before the window is shown.
func (w *Window) applyChrome(o caps.ChromeOptions) error {
	o, err := o.Validate()
	if err != nil {
		return err
	}
	w.chrome.titleBar = o.TitleBar
	a := w.app
	if o.TitleBar != caps.TitleBarNative && a.x.gtkWindowDecorated != nil {
		a.x.gtkWindowDecorated(w.win, 0)
	}
	if o.Transparent {
		w.makeTransparent()
	}
	return nil
}

// makeTransparent gives the window an RGBA visual, a clear theme background
// and a clear webview. Without a compositing window manager the result shows
// the root window's contents behind, not the desktop.
func (w *Window) makeTransparent() {
	a := w.app
	c := loadChromeAPI()
	if c == nil || c.gtkWidgetScreen == nil || c.gdkRGBAVisual == nil || c.gtkSetVisual == nil {
		return
	}
	screen := c.gtkWidgetScreen(w.win)
	if screen == 0 {
		return
	}
	vis := c.gdkRGBAVisual(screen)
	if vis == 0 {
		logf("krate: transparent window: the screen has no RGBA visual")
		return
	}
	if c.gtkAppPaintable != nil {
		c.gtkAppPaintable(w.win, 1)
	}
	c.gtkSetVisual(w.win, vis)
	if c.gtkCSSNew != nil && c.gtkCSSLoad != nil && c.gtkStyleCtx != nil && c.gtkCtxAddProv != nil {
		prov := c.gtkCSSNew()
		var gerr uintptr
		css := ".background, window { background-color: rgba(0,0,0,0); background-image: none; }"
		if prov != 0 && c.gtkCSSLoad(prov, css, int64(len(css)), &gerr) != 0 {
			c.gtkCtxAddProv(c.gtkStyleCtx(w.win), prov, 800)
		}
	}
	w.chrome.transparent = true
	for _, v := range w.allViews() {
		w.clearViewBackground(v)
	}
	_ = a
}

// clearViewBackground makes a view's own background transparent.
func (w *Window) clearViewBackground(v *view) {
	c := loadChromeAPI()
	if c == nil || c.wkSetBackground == nil || v.widget == 0 || !w.chrome.transparent {
		return
	}
	rgba := [4]float64{0, 0, 0, 0}
	c.wkSetBackground(v.widget, unsafe.Pointer(&rgba))
}

func unsupported(what string) error {
	return fmt.Errorf("%w: %s", krate.ErrUnsupported, what)
}

// ChromeState reports the window's chrome.
func (w *Window) ChromeState() (st caps.ChromeState) {
	w.app.callMain(func() {
		if !w.alive {
			return
		}
		st.TitleBar = w.chrome.titleBar
		if st.TitleBar == "" {
			st.TitleBar = caps.TitleBarNative
		}
		st.Transparent = w.chrome.transparent
		st.Resizable = w.resizable
		st.ScriptResize = true
		st.Features = []string{caps.FeatureTitleBar, caps.FeatureDrag, caps.FeatureResize}
		if c := loadChromeAPI(); c != nil && c.gdkRGBAVisual != nil && c.gtkSetVisual != nil {
			st.Features = append(st.Features, caps.FeatureTransparent)
		}
		if x := w.app.x; x.gtkWindowGetDecorated != nil {
			st.Decorated = x.gtkWindowGetDecorated(w.win) != 0
		}
		st.Tabs = 1
	})
	return
}

// SetTitleBar switches between the window manager's title bar and none.
func (w *Window) SetTitleBar(style string) error {
	style, ok := caps.NormalizeTitleBar(style)
	if !ok {
		return fmt.Errorf("titleBar must be native, hidden or none")
	}
	w.do(func(x *ext) {
		w.chrome.titleBar = style
		if x.gtkWindowDecorated != nil {
			x.gtkWindowDecorated(w.win, b2i(style == caps.TitleBarNative))
		}
	})
	return nil
}

// SetBackdrop is a Windows feature.
func (w *Window) SetBackdrop(name string) error {
	if name == "" || name == caps.BackdropNone {
		return nil
	}
	return unsupported("window backdrops (Mica, Acrylic) are a Windows 11 feature")
}

// SetVibrancy is a macOS feature.
func (w *Window) SetVibrancy(material string) error {
	if material == "" {
		return nil
	}
	return unsupported("vibrancy is a macOS feature")
}

// SetTitleBarOverlay is a Windows feature.
func (w *Window) SetTitleBarOverlay(o *caps.TitleBarOverlay) error {
	if o == nil {
		return nil
	}
	return unsupported("a title bar overlay is a Windows feature; draw your own controls and mark the bar with data-krate-drag")
}

// SetTrafficLightPosition is a macOS feature.
func (w *Window) SetTrafficLightPosition(p caps.Point) error {
	return unsupported("traffic lights are a macOS feature")
}

// recentPress returns the last button press if it is fresh.
func (w *Window) recentPress() (press, bool) {
	p := w.lastPress
	return p, !p.at.IsZero() && time.Since(p.at) < 3*time.Second
}

// buttonHeld reports whether the pointer button of p is still down, so a
// move or resize does not start after the user already let go (the window
// manager would then keep the window stuck to the pointer).
func (w *Window) buttonHeld(c *chromeAPI, button uint32) bool {
	x := w.app.x
	if c.gdkWinDevPos == nil || x.gtkWidgetGetWindow == nil || x.gdkDisplaySeat == nil {
		return true
	}
	gw := x.gtkWidgetGetWindow(w.win)
	if gw == 0 {
		return true
	}
	dev := x.gdkSeatPointer(x.gdkDisplaySeat(x.gdkDisplayDefault()))
	var px, py int32
	var mask uint32
	c.gdkWinDevPos(gw, dev, &px, &py, &mask)
	if button < 1 || button > 5 {
		button = 1
	}
	return mask&(1<<(7+button)) != 0 // GDK_BUTTON1_MASK is 1 << 8
}

// StartDrag starts a window-manager move from the press that is happening.
func (w *Window) StartDrag() error {
	c := loadChromeAPI()
	if c == nil || c.gtkBeginMove == nil {
		return unsupported("gtk_window_begin_move_drag is missing")
	}
	var err error
	w.app.callMain(func() {
		if !w.alive {
			err = fmt.Errorf("window closed")
			return
		}
		p, ok := w.recentPress()
		if !ok {
			err = fmt.Errorf("no recent button press to drag from")
			return
		}
		if !w.buttonHeld(c, p.button) {
			return // released again: nothing to drag
		}
		c.gtkBeginMove(w.win, int32(p.button), int32(p.xr), int32(p.yr), p.time)
	})
	return err
}

// StartResize starts a window-manager resize from an edge.
func (w *Window) StartResize(edge string) error {
	c := loadChromeAPI()
	idx := caps.EdgeIndex(edge)
	if idx < 0 {
		return fmt.Errorf("unknown resize edge %q", edge)
	}
	if c == nil || c.gtkBeginResize == nil {
		return unsupported("gtk_window_begin_resize_drag is missing")
	}
	var err error
	w.app.callMain(func() {
		if !w.alive {
			err = fmt.Errorf("window closed")
			return
		}
		p, ok := w.recentPress()
		if !ok {
			err = fmt.Errorf("no recent button press to resize from")
			return
		}
		if !w.buttonHeld(c, p.button) {
			return
		}
		c.gtkBeginResize(w.win, int32(idx), int32(p.button), int32(p.xr), int32(p.yr), p.time)
	})
	return err
}

// ToggleMaximize maximizes the window or restores it.
func (w *Window) ToggleMaximize() error {
	w.do(func(x *ext) {
		maxed := false
		if c := loadChromeAPI(); c != nil && c.gtkIsMaximized != nil {
			maxed = c.gtkIsMaximized(w.win) != 0
		} else if gw := x.gtkWidgetGetWindow(w.win); gw != 0 && x.gdkWindowGetState != nil {
			maxed = x.gdkWindowGetState(gw)&winMaximized != 0
		}
		if maxed {
			x.gtkWindowUnmaximize(w.win)
		} else {
			x.gtkWindowMaximize(w.win)
		}
	})
	return nil
}
