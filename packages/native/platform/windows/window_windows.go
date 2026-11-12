//go:build windows

package windows

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Window is one top-level window with one WebView2 controller, or several
// laid out as split panes inside the same HWND (see views_windows.go).
type Window struct {
	app  *App
	hwnd uintptr

	// Main-thread state.
	pv        *view   // the primary view
	extra     []*view // views added with AddView, in creation order
	layout    *caps.LayoutNode
	sp        splitter // divider hit-testing and drag for layout
	destroyed bool
	noQuit    bool

	// Release state, see release_windows.go.
	released  bool
	releasing bool
	abort     bool // Show arrived while the release waited for the page
}

// A view is one WebView2 controller and its CoreWebView2. Every view of an app
// is created from the app's one environment, so all of them run under one
// WebView2 browser process.
type view struct {
	w       *Window
	ref     caps.ViewRef
	primary bool

	// Main-thread state.
	controller unsafe.Pointer
	webview    unsafe.Pointer
	hRes       unsafe.Pointer // this view's WebResourceRequested handler
	hNav       unsafe.Pointer // NavigationCompleted
	hClose     unsafe.Pointer // WindowCloseRequested
	hFail      unsafe.Pointer // ProcessFailed
	hMsg       unsafe.Pointer // WebMessageReceived (streamed responses)
	hPerm      unsafe.Pointer // PermissionRequested
	hScreen    unsafe.Pointer // ScreenCaptureStarting (WebView2 runtimes with ICoreWebView2_27)
	streams    streamTable
	pending    []func()
	path       string
	minW, minH int
	viewHid    bool // the controller is invisible (hidden or minimized)
	state      caps.ViewState
	scrollJS   string

	ready chan struct{} // closed when the webview is usable or creation failed
	err   error
}

func (w *Window) markNoQuit() { w.noQuit = true }

// createWindow runs on the main thread.
func (a *App) createWindow(cfg WindowConfig, w *Window) error {
	if cfg.Width <= 0 {
		cfg.Width = defaultClientWidth
	}
	if cfg.Height <= 0 {
		cfg.Height = defaultClientHeight
	}
	style := uint32(wsOverlappedWindow | wsClipChildren | wsClipSiblings)
	if !cfg.Resizable {
		style &^= wsThickFrame | wsMaximizeBox
	}
	dpi := systemDPI()
	ow, oh := frameSize(style, scaleDPI(cfg.Width, dpi), scaleDPI(cfg.Height, dpi), dpi)

	hwnd, _, e := pCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(wstr(appClassName))),
		uintptr(unsafe.Pointer(wstr(cfg.Title))),
		uintptr(style),
		cwUseDefault, cwUseDefault, uintptr(ow), uintptr(oh),
		0, 0, a.hInst, 0)
	if hwnd == 0 {
		return fmt.Errorf("windows: CreateWindowEx: %v", e)
	}
	w.hwnd = hwnd
	w.pv = a.registerView(w, true)
	w.pv.path = cfg.Path
	a.windows[hwnd] = w
	a.windowCreated(hwnd)
	a.everOpened = true
	_, _, _ = pShowWindow.Call(hwnd, swShow)
	_, _, _ = pUpdateWindow.Call(hwnd)

	w.pv.createController()
	return nil
}

// createController asks the app's one shared environment for a controller of
// the window. Every window of the app uses the same environment (same user
// data folder), so all webviews run under one browser process; a released
// window gets new controllers here when it is shown again.
func (v *view) createController() {
	w := v.w
	a := w.app
	a.withEnv(func() {
		if w.destroyed {
			v.finish(errors.New("windows: window closed before the webview was created"))
			return
		}
		h := newHandler(iidControllerCompletedHandler, true, func(hr, controller uintptr) uintptr {
			v.controllerCompleted(hr, controller)
			return sOK
		})
		hr := vcall(a.env, envCreateController, w.hwnd, uintptr(h))
		if failed(hr) {
			v.finish(fmt.Errorf("windows: CreateCoreWebView2Controller: 0x%08x", uint32(hr)))
			if v.primary {
				_, _, _ = pDestroyWindow.Call(w.hwnd)
			}
		}
	})
}

// finish records the creation result and releases NewWindow and AddView
// waiters.
func (v *view) finish(err error) {
	select {
	case <-v.ready:
	default:
		v.err = err
		close(v.ready)
	}
}

// fail reports a creation failure: a primary view takes its window with it, an
// added view is removed.
func (v *view) fail(err error) {
	v.finish(err)
	if v.primary {
		_, _, _ = pDestroyWindow.Call(v.w.hwnd)
		return
	}
	v.w.dropView(v, caps.ViewClosed)
}

func (v *view) controllerCompleted(hr, controller uintptr) {
	w := v.w
	if w.destroyed {
		v.finish(errors.New("windows: window closed before the webview was created"))
		return
	}
	if failed(hr) || controller == 0 {
		v.fail(fmt.Errorf("windows: creating the WebView2 controller failed: 0x%08x", uint32(hr)))
		return
	}
	a := w.app
	v.controller = ptr(controller)
	comAddRef(v.controller)

	var wv uintptr
	if failed(vcall(v.controller, ctlGetCoreWebView2, uintptr(unsafe.Pointer(&wv)))) || wv == 0 {
		v.fail(errors.New("windows: get_CoreWebView2 failed"))
		return
	}
	v.webview = ptr(wv) // get_ returned an owned reference
	a.byWebview[v.webview] = v

	var settings uintptr
	if !failed(vcall(v.webview, wvGetSettings, uintptr(unsafe.Pointer(&settings)))) && settings != 0 {
		dev := uintptr(0)
		if a.cfg.Debug {
			dev = 1
		}
		vcall(ptr(settings), setPutAreDevToolsEnabled, dev)
		comRelease(ptr(settings))
	}

	// One handler object per view: the request is bound to the view whose
	// handler raised it, which page script cannot influence.
	var token int64
	v.hRes = newHandler(iidWebResourceRequestedHandler, false, a.resourceHandlerFor(v.ref))
	if hr := vcall(v.webview, wvAddWebResourceRequested, uintptr(v.hRes), uintptr(unsafe.Pointer(&token))); failed(hr) {
		v.fail(fmt.Errorf("windows: add_WebResourceRequested: 0x%08x", uint32(hr)))
		return
	}
	if hr := vcall(v.webview, wvAddWebResourceFilter,
		uintptr(unsafe.Pointer(wstr(resourceFilter))), resourceContextAll); failed(hr) {
		v.fail(fmt.Errorf("windows: AddWebResourceRequestedFilter: 0x%08x", uint32(hr)))
		return
	}
	vcall(v.webview, wvAddNavigationStarting, uintptr(a.hNavigation), uintptr(unsafe.Pointer(&token)))
	vcall(v.webview, wvAddFrameNavigationStarting, uintptr(a.hFrameNav), uintptr(unsafe.Pointer(&token)))
	vcall(v.webview, wvAddNewWindowRequested, uintptr(a.hNewWindow), uintptr(unsafe.Pointer(&token)))
	v.hNav = newHandler(iidNavigationCompletedHandler, false, func(sender, args uintptr) uintptr {
		emitView(a, v, caps.ViewNavigated, v.currentRoute())
		return sOK
	})
	vcall(v.webview, wvAddNavigationCompleted, uintptr(v.hNav), uintptr(unsafe.Pointer(&token)))
	v.hClose = newHandler(iidWindowCloseRequestedHandler, false, func(sender, args uintptr) uintptr {
		// window.close() from the page: closes the window from its primary
		// view, removes any other view.
		if v.primary {
			_, _, _ = pPostMessageW.Call(w.hwnd, wmClose, 0, 0)
		} else {
			_ = w.removeView(v.ref, caps.ViewClosed)
		}
		return sOK
	})
	vcall(v.webview, wvAddWindowCloseRequested, uintptr(v.hClose), uintptr(unsafe.Pointer(&token)))
	a.connectProcessFailed(v)

	v.setupStreams()
	a.tuneMemory(v)
	a.hookPermissions(v)

	w.placeViews()
	vcall(v.controller, ctlPutIsVisible, 1)
	v.viewHid = false
	v.addScrollRestore()
	vcall(v.webview, wvNavigate, uintptr(unsafe.Pointer(wstr(navURL(v.path)))))
	if v.primary {
		v.focus()
	}

	pending := v.pending
	v.pending = nil
	for _, f := range pending {
		f()
	}
	v.finish(nil)
}

// destroy runs on WM_DESTROY.
func (w *Window) destroy() {
	w.destroyed = true
	// The views' closed events were sent by winExtMessage, before the window's.
	for _, v := range w.allViews() {
		v.closeController()
		v.pending = nil
		v.finish(errors.New("windows: window destroyed"))
		w.app.unregisterView(v)
	}
}

// closeController closes the WebView2 controller and drops the references
// to it and to its CoreWebView2.
func (v *view) closeController() {
	v.streams.closeAll()
	if v.controller != nil {
		vcall(v.controller, ctlClose)
		comRelease(v.controller)
		v.controller = nil
	}
	if v.webview != nil {
		delete(v.w.app.byWebview, v.webview)
		comRelease(v.webview)
		v.webview = nil
	}
	for _, h := range []*unsafe.Pointer{&v.hRes, &v.hNav, &v.hClose, &v.hMsg, &v.hFail, &v.hPerm, &v.hScreen} {
		if *h != nil {
			comRelease(*h)
			*h = nil
		}
	}
}

func (v *view) focus() {
	if v.controller != nil {
		vcall(v.controller, ctlMoveFocus, webviewMoveProgram)
	}
}

// resize places the controllers for the window's current size.
func (w *Window) resize() { w.placeViews() }

// focus gives the window's primary view the keyboard focus.
func (w *Window) focus() { w.pv.focus() }

// do runs f on the main thread against the view's webview, queueing it until
// the webview exists.
func (v *view) do(f func()) {
	w := v.w
	w.app.Dispatch(func() {
		switch {
		case w.destroyed:
		case v.webview == nil:
			v.pending = append(v.pending, f)
		default:
			f()
		}
	})
}

// currentRoute is the route the view shows according to WebView2, or the last
// route it was told to load.
func (v *view) currentRoute() string {
	if v.webview != nil {
		if uri := comString(v.webview, wvGetSource); strings.HasPrefix(uri, Origin+"/") {
			return uri[len(Origin):]
		}
	}
	return v.path
}

// Navigate loads path on the app origin.
func (w *Window) Navigate(path string) {
	v := w.pv
	v.do(func() {
		v.path = path
		vcall(v.webview, wvNavigate, uintptr(unsafe.Pointer(wstr(navURL(path)))))
	})
}

// Eval runs JavaScript in the page without waiting for a result.
func (w *Window) Eval(js string) {
	v := w.pv
	v.do(func() {
		vcall(v.webview, wvExecuteScript, uintptr(unsafe.Pointer(wstr(js))), 0)
	})
}

// SetTitle sets the window title.
func (w *Window) SetTitle(title string) {
	w.app.Dispatch(func() {
		if !w.destroyed {
			_, _, _ = pSetWindowTextW.Call(w.hwnd, uintptr(unsafe.Pointer(wstr(title))))
		}
	})
}

// SetSize resizes the client area (device-independent pixels).
func (w *Window) SetSize(width, height int) {
	w.app.Dispatch(func() {
		if w.destroyed || width <= 0 || height <= 0 {
			return
		}
		dpi := windowDPI(w.hwnd)
		style := uint32(wsOverlappedWindow | wsClipChildren | wsClipSiblings)
		ow, oh := frameSize(style, scaleDPI(width, dpi), scaleDPI(height, dpi), dpi)
		_, _, _ = pSetWindowPos.Call(w.hwnd, 0, 0, 0, uintptr(ow), uintptr(oh), swpNoMove|swpNoZOrder|swpNoActivate)
	})
}

// Close destroys the window. With KeepAlive the last live window is released
// instead (hidden, controller closed) and Show brings it back.
func (w *Window) Close() {
	w.app.Dispatch(func() {
		if w.destroyed {
			return
		}
		if w.app.parkable(w) {
			w.release()
			return
		}
		_, _, _ = pDestroyWindow.Call(w.hwnd)
	})
}
