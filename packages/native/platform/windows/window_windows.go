//go:build windows

package windows

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Window is one top-level window with one webview.
type Window struct {
	app  *App
	hwnd uintptr

	// Main-thread state.
	controller unsafe.Pointer
	webview    unsafe.Pointer
	pending    []func()
	destroyed  bool
	noQuit     bool
	path       string

	// Release state, see release_windows.go.
	released  bool
	releasing bool
	abort     bool // Show arrived while the release waited for the page
	viewHid   bool // the controller is invisible (hidden or minimized)
	state     caps.ViewState
	scrollJS  string

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
	w.path = cfg.Path
	a.windows[hwnd] = w
	a.everOpened = true
	pShowWindow.Call(hwnd, swShow)
	pUpdateWindow.Call(hwnd)

	w.createController()
	return nil
}

// createController asks the app's one shared environment for a controller of
// the window. Every window of the app uses the same environment (same user
// data folder), so all webviews run under one browser process; a released
// window gets a new controller here when it is shown again.
func (w *Window) createController() {
	a := w.app
	a.withEnv(func() {
		if w.destroyed {
			w.finish(errors.New("windows: window closed before the webview was created"))
			return
		}
		h := newHandler(iidControllerCompletedHandler, true, func(hr, controller uintptr) uintptr {
			w.controllerCompleted(hr, controller)
			return sOK
		})
		hr := vcall(a.env, envCreateController, w.hwnd, uintptr(h))
		if failed(hr) {
			w.finish(fmt.Errorf("windows: CreateCoreWebView2Controller: 0x%08x", uint32(hr)))
			pDestroyWindow.Call(w.hwnd)
		}
	})
}

// finish records the creation result and releases NewWindow waiters.
func (w *Window) finish(err error) {
	select {
	case <-w.ready:
	default:
		w.err = err
		close(w.ready)
	}
}

func (w *Window) controllerCompleted(hr, controller uintptr) {
	if w.destroyed {
		w.finish(errors.New("windows: window closed before the webview was created"))
		return
	}
	if failed(hr) || controller == 0 {
		w.finish(fmt.Errorf("windows: creating the WebView2 controller failed: 0x%08x", uint32(hr)))
		pDestroyWindow.Call(w.hwnd)
		return
	}
	a := w.app
	w.controller = ptr(controller)
	comAddRef(w.controller)

	var wv uintptr
	if failed(vcall(w.controller, ctlGetCoreWebView2, uintptr(unsafe.Pointer(&wv)))) || wv == 0 {
		w.finish(errors.New("windows: get_CoreWebView2 failed"))
		pDestroyWindow.Call(w.hwnd)
		return
	}
	w.webview = ptr(wv) // get_ returned an owned reference

	var settings uintptr
	if !failed(vcall(w.webview, wvGetSettings, uintptr(unsafe.Pointer(&settings)))) && settings != 0 {
		dev := uintptr(0)
		if a.cfg.Debug {
			dev = 1
		}
		vcall(ptr(settings), setPutAreDevToolsEnabled, dev)
		comRelease(ptr(settings))
	}

	var token int64
	if hr := vcall(w.webview, wvAddWebResourceRequested, uintptr(a.hResource), uintptr(unsafe.Pointer(&token))); failed(hr) {
		w.finish(fmt.Errorf("windows: add_WebResourceRequested: 0x%08x", uint32(hr)))
		pDestroyWindow.Call(w.hwnd)
		return
	}
	if hr := vcall(w.webview, wvAddWebResourceFilter,
		uintptr(unsafe.Pointer(wstr(resourceFilter))), resourceContextAll); failed(hr) {
		w.finish(fmt.Errorf("windows: AddWebResourceRequestedFilter: 0x%08x", uint32(hr)))
		pDestroyWindow.Call(w.hwnd)
		return
	}
	vcall(w.webview, wvAddNavigationStarting, uintptr(a.hNavigation), uintptr(unsafe.Pointer(&token)))
	vcall(w.webview, wvAddFrameNavigationStarting, uintptr(a.hFrameNav), uintptr(unsafe.Pointer(&token)))
	vcall(w.webview, wvAddNewWindowRequested, uintptr(a.hNewWindow), uintptr(unsafe.Pointer(&token)))

	a.tuneMemory(w)

	w.resize()
	vcall(w.controller, ctlPutIsVisible, 1)
	w.viewHid = false
	w.addScrollRestore()
	vcall(w.webview, wvNavigate, uintptr(unsafe.Pointer(wstr(navURL(w.path)))))
	w.focus()

	pending := w.pending
	w.pending = nil
	for _, f := range pending {
		f()
	}
	w.finish(nil)
}

// destroy runs on WM_DESTROY.
func (w *Window) destroy() {
	w.destroyed = true
	w.closeController()
	w.pending = nil
	w.finish(errors.New("windows: window destroyed"))
}

// closeController closes the WebView2 controller and drops the references
// to it and to its CoreWebView2.
func (w *Window) closeController() {
	if w.controller != nil {
		vcall(w.controller, ctlClose)
		comRelease(w.controller)
		w.controller = nil
	}
	if w.webview != nil {
		comRelease(w.webview)
		w.webview = nil
	}
}

func (w *Window) resize() {
	if w.controller == nil {
		return
	}
	var r rect
	pGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
	putBounds(w.controller, &r)
}

func (w *Window) focus() {
	if w.controller != nil {
		vcall(w.controller, ctlMoveFocus, webviewMoveProgram)
	}
}

// do runs f on the main thread against the webview, queueing it until the
// webview exists.
func (w *Window) do(f func()) {
	w.app.Dispatch(func() {
		switch {
		case w.destroyed:
		case w.webview == nil:
			w.pending = append(w.pending, f)
		default:
			f()
		}
	})
}

// Navigate loads path on the app origin.
func (w *Window) Navigate(path string) {
	w.do(func() {
		w.path = path
		vcall(w.webview, wvNavigate, uintptr(unsafe.Pointer(wstr(navURL(path)))))
	})
}

// Eval runs JavaScript in the page without waiting for a result.
func (w *Window) Eval(js string) {
	w.do(func() {
		vcall(w.webview, wvExecuteScript, uintptr(unsafe.Pointer(wstr(js))), 0)
	})
}

// SetTitle sets the window title.
func (w *Window) SetTitle(title string) {
	w.app.Dispatch(func() {
		if !w.destroyed {
			pSetWindowTextW.Call(w.hwnd, uintptr(unsafe.Pointer(wstr(title))))
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
		pSetWindowPos.Call(w.hwnd, 0, 0, 0, uintptr(ow), uintptr(oh), swpNoMove|swpNoZOrder|swpNoActivate)
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
		pDestroyWindow.Call(w.hwnd)
	})
}
