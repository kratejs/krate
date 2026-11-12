//go:build darwin

package darwin

import (
	"errors"

	"github.com/kratejs/krate/packages/native/platform/caps"

	"github.com/ebitengine/purego/objc"
)

const (
	nsWindowStyleMaskTitled         = 1 << 0
	nsWindowStyleMaskClosable       = 1 << 1
	nsWindowStyleMaskMiniaturizable = 1 << 2
	nsWindowStyleMaskResizable      = 1 << 3
	nsBackingStoreBuffered          = 2
	nsViewWidthSizable              = 1 << 1
	nsViewHeightSizable             = 1 << 4

	defaultWidth  = 1024
	defaultHeight = 768
)

// Window is one NSWindow hosting one WKWebView, or several laid out as split
// panes (see views_darwin.go). Fields are main-thread only.
type Window struct {
	app    *App
	win    objc.ID
	web    objc.ID // the primary view's WKWebView; mirrors pv.web
	closed bool

	ref          uint64
	preventClose bool

	// Views, see views_darwin.go.
	pv     *view
	extra  []*view
	layout *caps.LayoutNode
	root   objc.ID // the window's content view: the primary web view or the top split view
	splits []*splitState
	// layoutGen counts layout rebuilds; ratiosSettled is false from a rebuild
	// until the deferred divider pass has run, and snapshotRatios skips
	// meanwhile so a half-laid-out window cannot overwrite the ratios asked for.
	layoutGen     int
	ratiosSettled bool

	// Release state, see release_darwin.go.
	released  bool
	releasing bool
	abort     bool // Show arrived while the release waited for the page
}

// NewWindow creates a window and returns once it exists. On the main thread
// (for example inside onReady) it runs inline; from another goroutine it waits
// for the main loop, so such callers must wait for onReady first (earlier
// calls return an error instead of blocking forever).
func (a *App) NewWindow(o WindowOptions) (*Window, error) {
	if !a.ready.Load() && !isMainThread() {
		return nil, errors.New("darwin: NewWindow called off the main thread before the app is ready")
	}
	var (
		w   *Window
		err error
	)
	runOnMain(func() { withPool(func() { w, err = a.newWindow(o) }) })
	if err != nil {
		return nil, err
	}
	return w, nil
}

func (a *App) newWindow(o WindowOptions) (*Window, error) {
	width, height := o.Width, o.Height
	if width <= 0 {
		width = defaultWidth
	}
	if height <= 0 {
		height = defaultHeight
	}
	style := nsWindowStyleMaskTitled | nsWindowStyleMaskClosable | nsWindowStyleMaskMiniaturizable
	if o.Resizable {
		style |= nsWindowStyleMaskResizable
	}
	if o.Offscreen {
		// Borderless windows are not constrained to a screen (see
		// snapshot_darwin.go), so this one can sit far outside every display.
		style = 0
	}

	web := a.newWebView()
	if web == 0 {
		return nil, errors.New("darwin: WKWebView creation failed")
	}

	rect := nsRect{Size: nsSize{Width: float64(width), Height: float64(height)}}
	win := cls("NSWindow").Send(sel("alloc")).Send(sel("initWithContentRect:styleMask:backing:defer:"),
		rect, style, nsBackingStoreBuffered, false)
	if win == 0 {
		release(web)
		return nil, errors.New("darwin: NSWindow creation failed")
	}
	// We own the window and release it ourselves after it closes.
	win.Send(sel("setReleasedWhenClosed:"), false)
	win.Send(sel("setTitle:"), nsString(o.Title))
	win.Send(sel("setDelegate:"), a.winDelegate)
	win.Send(sel("center"))

	w := &Window{app: a, win: win, ref: nextRef.Add(1)}
	w.pv = a.registerView(w, web, true)
	a.windows[win] = w
	w.applyLayout()

	w.pv.load(o.Path)
	if o.Offscreen {
		parkOffscreen(win, width, height)
	} else {
		win.Send(sel("makeKeyAndOrderFront:"), objc.ID(0))
	}
	a.startMemoryPurge()
	return w, nil
}

// newWebView creates a WKWebView with the app's configuration: the shared
// process pool, the scheme handler and the navigation lock. The caller owns it.
func (a *App) newWebView() objc.ID {
	config := cls("WKWebViewConfiguration").Send(sel("new"))
	defer release(config)
	config.Send(sel("setProcessPool:"), a.processPool)
	config.Send(sel("setURLSchemeHandler:forURLScheme:"), a.schemeHandler, nsString(Scheme))
	if a.opts.Debug {
		// macOS 13.3+ uses WKWebView.inspectable (set below); older systems
		// honor this preference key.
		prefs := config.Send(sel("preferences"))
		prefs.Send(sel("setValue:forKey:"),
			cls("NSNumber").Send(sel("numberWithBool:"), true), nsString("developerExtrasEnabled"))
	}
	a.applyMemoryConfig(config)
	a.applyExtensionConfig(config)
	web := cls(webViewClassName()).Send(sel("alloc")).Send(sel("initWithFrame:configuration:"), nsRect{}, config)
	if web == 0 {
		return 0
	}
	web.Send(sel("setAutoresizingMask:"), nsViewWidthSizable|nsViewHeightSizable)
	web.Send(sel("setNavigationDelegate:"), a.navDelegate)
	web.Send(sel("setUIDelegate:"), a.uiDelegate)
	if a.opts.Debug {
		if s := sel("setInspectable:"); respondsTo(web, s) {
			web.Send(s, true)
		}
	}
	return web
}

// windowWillClose runs when an NSWindow is about to close, whether by the user
// or by Close. It detaches the webview and quits after the last window.
func (a *App) windowWillClose(nsWin objc.ID) {
	w := a.windows[nsWin]
	if w == nil {
		return
	}
	delete(a.windows, nsWin)
	w.closed = true
	views := w.allViews()
	// Views first: the SDK forgets a window's views when it hears that the
	// window closed.
	for _, v := range views {
		emitView(a, v, caps.ViewClosed, "")
	}
	emitWindow(w, caps.WindowClosed)
	var webs []objc.ID
	for _, v := range views {
		if v.web != 0 {
			webs = append(webs, v.web)
			v.web.Send(sel("stopLoading"))
			v.web.Send(sel("setNavigationDelegate:"), objc.ID(0))
		}
		a.unregisterView(v)
		v.web = 0
	}
	w.web = 0
	w.splits = nil
	// Release on a later turn: AppKit is still using the window right now.
	dispatch(func() {
		w.win.Send(sel("setDelegate:"), objc.ID(0))
		w.win.Send(sel("setContentView:"), objc.ID(0))
		for _, web := range webs {
			web.Send(sel("removeFromSuperview"))
			release(web)
		}
		release(w.win)
	})
	if caps.QuitWhenEmpty(a.opts.KeepAlive, len(a.windows)) {
		a.Quit()
	}
}

// Navigate loads path on the app origin.
func (w *Window) Navigate(path string) {
	dispatch(func() {
		if !w.closed {
			w.pv.load(path)
		}
	})
}

// Eval runs JavaScript in the page without waiting for the result.
func (w *Window) Eval(js string) {
	dispatch(func() {
		if !w.closed && w.web != 0 {
			w.web.Send(sel("evaluateJavaScript:completionHandler:"), nsString(js), objc.ID(0))
		}
	})
}

// SetTitle sets the window title.
func (w *Window) SetTitle(title string) {
	dispatch(func() {
		if !w.closed {
			w.win.Send(sel("setTitle:"), nsString(title))
		}
	})
}

// SetSize resizes the window's content area.
func (w *Window) SetSize(width, height int) {
	dispatch(func() {
		if !w.closed && width > 0 && height > 0 {
			w.win.Send(sel("setContentSize:"), nsSize{Width: float64(width), Height: float64(height)})
		}
	})
}

// Close closes the window. With KeepAlive the last live window is released
// instead (hidden, webview removed) and Show brings it back.
func (w *Window) Close() {
	dispatch(func() {
		if w.closed {
			return
		}
		if w.app.parkable(w) {
			w.release()
			return
		}
		w.win.Send(sel("close"))
	})
}
