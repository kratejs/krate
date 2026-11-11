//go:build linux

package linux

import (
	"errors"
	"net/url"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// GTK and WebKit enum values used below.
const (
	gtkWindowToplevel = 0

	loadFinished = 3 // WEBKIT_LOAD_FINISHED

	policyNavigation = 0 // WEBKIT_POLICY_DECISION_TYPE_NAVIGATION_ACTION
	policyNewWindow  = 1 // WEBKIT_POLICY_DECISION_TYPE_NEW_WINDOW_ACTION
)

// Window is one top-level window with one webview.
type Window struct {
	app  *App
	id   uintptr
	win  uintptr // GtkWindow
	view uintptr // WebKitWebView
	box  uintptr // GtkBox holding the menu bar and the view

	mu         sync.Mutex
	alive      bool
	resizable  bool
	onLoadDone func()

	// Touched on the main thread only.
	released     bool // the webview was destroyed; see release.go
	releasing    bool
	abort        bool // Show arrived while the release waited for the page
	route        string
	state        caps.ViewState
	scrollJS     string
	fixedW       int
	fixedH       int
	preventClose bool
	wantAbove    bool
	menubar      uintptr
	accel        uintptr
	lastState    uint32
}

func (a *App) newWindow(opts WindowOptions, onLoad func()) (*Window, error) {
	api := a.api
	w := &Window{app: a, alive: true, resizable: opts.Resizable, onLoadDone: onLoad}
	if opts.Width <= 0 {
		opts.Width = 1024
	}
	if opts.Height <= 0 {
		opts.Height = 768
	}
	w.win = api.gtkWindowNew(gtkWindowToplevel)
	if w.win == 0 {
		return nil, errors.New("krate: gtk_window_new failed")
	}
	w.view = a.newView()
	if w.view == 0 {
		api.gtkWidgetDestroy(w.win)
		return nil, errors.New("krate: webkit_web_view_new failed")
	}
	a.mu.Lock()
	a.nextWin++
	w.id = a.nextWin
	a.windows[w.id] = w
	a.mu.Unlock()

	api.gtkWindowSetTitle(w.win, opts.Title)
	api.gtkWindowSetDefSize(w.win, int32(opts.Width), int32(opts.Height))
	api.gtkWindowSetResize(w.win, b2i(opts.Resizable))
	w.fixedW, w.fixedH = opts.Width, opts.Height
	if !opts.Resizable {
		api.gtkWidgetSetSizeReq(w.view, int32(opts.Width), int32(opts.Height))
	}
	a.applyViewSettings(w.view)
	api.gSignalConnectData(w.win, "destroy", cbDestroy, w.id, 0, 0)
	if x := a.x; x.gtkBoxNew != nil && x.gtkWindowPresent != nil {
		api.gSignalConnectData(w.win, "delete-event", cbDelete, w.id, 0, 0)
		api.gSignalConnectData(w.win, "window-state-event", cbWinState, w.id, 0, 0)
	}
	w.connectView()

	if x := a.x; x.gtkBoxNew != nil {
		w.box = x.gtkBoxNew(1, 0)
		api.gtkContainerAdd(w.win, w.box)
	}
	w.packView()
	w.route = cleanPath(opts.Path)
	api.wkViewLoadURI(w.view, Origin+w.route)
	api.gtkWidgetShowAll(w.win)
	a.applyAppMenu(w)
	return w, nil
}

func b2i(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// cleanPath makes p an absolute path on the app origin.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// OnLoadFinished sets a hook that runs on the main thread each time a page
// finishes loading.
func (w *Window) OnLoadFinished(fn func()) {
	w.mu.Lock()
	w.onLoadDone = fn
	w.mu.Unlock()
}

// Navigate loads path on the app origin.
func (w *Window) Navigate(path string) {
	w.app.Dispatch(func() {
		if !w.alive {
			return
		}
		w.route = cleanPath(path)
		w.state = caps.ViewState{Route: w.route}
		if w.view != 0 {
			w.app.api.wkViewLoadURI(w.view, Origin+w.route)
		}
	})
}

// Eval runs JavaScript in the page without waiting for a result.
func (w *Window) Eval(js string) {
	w.app.Dispatch(func() {
		if w.alive && w.view != 0 {
			w.app.api.wkViewEval(w.view, js, -1, 0, 0, 0, 0, 0)
		}
	})
}

// SetTitle sets the window title.
func (w *Window) SetTitle(title string) {
	w.app.Dispatch(func() {
		if w.alive {
			w.app.api.gtkWindowSetTitle(w.win, title)
		}
	})
}

// SetSize resizes the window.
func (w *Window) SetSize(width, height int) {
	w.app.Dispatch(func() {
		if !w.alive {
			return
		}
		api := w.app.api
		w.fixedW, w.fixedH = width, height
		if !w.resizable && w.view != 0 {
			api.gtkWidgetSetSizeReq(w.view, int32(width), int32(height))
		}
		api.gtkWindowResize(w.win, int32(width), int32(height))
	})
}

// Close destroys the window. With KeepAlive the last live window is released
// instead (hidden, webview destroyed) and Show brings it back.
func (w *Window) Close() {
	w.app.Dispatch(func() {
		if !w.alive {
			return
		}
		if w.app.parkable(w) {
			w.release()
			return
		}
		w.app.api.gtkWidgetDestroy(w.win)
	})
}

// Title returns the current window title.
func (w *Window) Title() string {
	var s string
	w.app.callMain(func() {
		if w.alive {
			s = goString(w.app.api.gtkWindowGetTitle(w.win))
		}
	})
	return s
}

// Size returns the current window size.
func (w *Window) Size() (width, height int) {
	w.app.callMain(func() {
		if w.alive {
			var ww, hh int32
			w.app.api.gtkWindowGetSize(w.win, &ww, &hh)
			width, height = int(ww), int(hh)
		}
	})
	return
}

func destroyCallback(widget, id uintptr) uintptr {
	a := theApp
	a.mu.Lock()
	w := a.windows[id]
	delete(a.windows, id)
	last := len(a.windows) == 0
	keepAlive := a.keepAlive
	hook := a.onAllClosed
	a.mu.Unlock()
	if w != nil {
		w.alive = false
		a.emitWindow(w, caps.WindowClosed)
	}
	if last && w != nil {
		if hook != nil {
			hook()
		} else if caps.QuitWhenEmpty(keepAlive, 0) {
			a.api.gtkMainQuit()
		}
	}
	return 0
}

// closeCallback handles window.close() from the page.
func closeCallback(view, id uintptr) uintptr {
	if w := theApp.windowByID(id); w != nil {
		w.Close()
	}
	return 0
}

func loadCallback(view, event, id uintptr) uintptr {
	if event != loadFinished {
		return 0
	}
	w := theApp.windowByID(id)
	if w == nil {
		return 0
	}
	if js := w.scrollJS; js != "" && w.view != 0 {
		w.scrollJS = ""
		theApp.api.wkViewEval(w.view, js, -1, 0, 0, 0, 0, 0)
	}
	w.mu.Lock()
	fn := w.onLoadDone
	w.mu.Unlock()
	if fn != nil {
		fn()
	}
	return 0
}

// policyCallback implements the navigation lock: only the app origin (and
// in-page about:/blob: documents) may load.
func policyCallback(view, decision, kind, id uintptr) uintptr {
	if kind != policyNavigation && kind != policyNewWindow {
		return 0 // not handled; default behavior
	}
	api := theApp.api
	action := api.wkNavDecisionAction(decision)
	var uri string
	if action != 0 {
		if req := api.wkNavActionRequest(action); req != 0 {
			uri = goString(api.wkURIRequestURI(req))
		}
	}
	if kind == policyNavigation && allowedNavigation(uri) {
		return 0
	}
	logf("krate: blocked navigation to %q", uri)
	api.wkPolicyIgnore(decision)
	return 1
}

func allowedNavigation(uri string) bool {
	if uri == "about:blank" {
		return true
	}
	u, err := url.Parse(uri)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "krate":
		return u.Host == "app"
	case "blob":
		return strings.HasPrefix(u.Opaque, Origin) || strings.HasPrefix(u.Path, "//app")
	}
	return false
}
