//go:build linux

package linux

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/native/internal/memory"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// GTK and WebKit enum values used below.
const (
	gtkWindowToplevel = 0

	loadFinished = 3 // WEBKIT_LOAD_FINISHED

	policyNavigation = 0 // WEBKIT_POLICY_DECISION_TYPE_NAVIGATION_ACTION
	policyNewWindow  = 1 // WEBKIT_POLICY_DECISION_TYPE_NEW_WINDOW_ACTION
	policyResponse   = 2 // WEBKIT_POLICY_DECISION_TYPE_RESPONSE
)

// Window is one top-level window with one webview, or several laid out as
// split panes (see views.go).
type Window struct {
	app  *App
	id   uintptr
	win  uintptr // GtkWindow
	view uintptr // the primary WebKitWebView; mirrors pv.widget
	box  uintptr // GtkBox holding the menu bar and the pane tree

	// Touched on the main thread only.
	pv     *view   // the primary view
	extra  []*view // views added with AddView, in creation order
	layout *caps.LayoutNode
	root   uintptr // the widget in box: the primary view or the top pane
	panes  []*paneState

	mu         sync.Mutex
	alive      bool
	resizable  bool
	onLoadDone func()

	// Touched on the main thread only.
	released     bool // the webview was destroyed; see release.go
	releasing    bool
	abort        bool // Show arrived while the release waited for the page
	fixedW       int
	fixedH       int
	preventClose bool
	wantAbove    bool
	chrome       chromeState
	lastPress    press
	menubar      uintptr
	accel        uintptr
	lastState    uint32
	offscreen    bool // a GtkOffscreenWindow: never shown on screen
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
	w.offscreen = opts.Offscreen && a.OffscreenSupported()
	w.win = a.newToplevel(w.offscreen)
	if w.win == 0 {
		return nil, errors.New("krate: gtk_window_new failed")
	}
	if _, err := opts.Chrome.Validate(); err != nil {
		api.gtkWidgetDestroy(w.win)
		return nil, fmt.Errorf("krate: window chrome: %w", err)
	}
	widget := a.newView()
	if widget == 0 {
		api.gtkWidgetDestroy(w.win)
		return nil, errors.New("krate: webkit_web_view_new failed")
	}
	a.mu.Lock()
	a.nextWin++
	w.id = a.nextWin
	a.windows[w.id] = w
	a.mu.Unlock()
	w.pv = a.registerView(w, widget, true)

	api.gtkWindowSetTitle(w.win, opts.Title)
	api.gtkWindowSetDefSize(w.win, int32(opts.Width), int32(opts.Height))
	api.gtkWindowSetResize(w.win, b2i(opts.Resizable))
	w.fixedW, w.fixedH = opts.Width, opts.Height
	a.applyViewSettings(w.view)
	api.gSignalConnectData(w.win, "destroy", cbDestroy, w.id, 0, 0)
	if x := a.x; x.gtkBoxNew != nil && x.gtkWindowPresent != nil {
		api.gSignalConnectData(w.win, "delete-event", cbDelete, w.id, 0, 0)
		api.gSignalConnectData(w.win, "window-state-event", cbWinState, w.id, 0, 0)
	}
	a.connectView(w.pv)

	if x := a.x; x.gtkBoxNew != nil {
		w.box = x.gtkBoxNew(1, 0)
		api.gtkContainerAdd(w.win, w.box)
	}
	if !opts.Resizable {
		api.gtkWidgetSetSizeReq(w.sizeTarget(), int32(opts.Width), int32(opts.Height))
	}
	_ = w.applyChrome(opts.Chrome)
	w.applyLayout()
	w.pv.load(opts.Path)
	api.gtkWidgetShowAll(w.win)
	a.applyAppMenu(w)
	return w, nil
}

// sizeTarget is the widget that carries the fixed size of a window that
// cannot be resized: the box around the pane tree, so that splits can divide
// the whole size.
func (w *Window) sizeTarget() uintptr {
	if w.box != 0 {
		return w.box
	}
	return w.view
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
		w.pv.load(path)
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
		if !w.resizable && w.sizeTarget() != 0 {
			api.gtkWidgetSetSizeReq(w.sizeTarget(), int32(width), int32(height))
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
		views := w.allViews()
		for _, v := range views {
			a.unregisterView(v)
			v.widget = 0
		}
		for _, p := range w.panes {
			delete(a.panes, p.id)
		}
		w.panes, w.root = nil, 0
		// Views first: the SDK forgets a window's views when it hears that
		// the window closed.
		for _, v := range views {
			a.emitView(v, caps.ViewClosed, "")
		}
		a.emitWindow(w, caps.WindowClosed)
	}
	if w != nil && !last {
		// A closed window frees its page memory; trim once things settle.
		memory.Request()
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

// closeCallback handles window.close() from a page. In the primary view it
// closes the window; in another view it removes the view.
func closeCallback(_, ref uintptr) uintptr {
	v := theApp.viewByRef(ref)
	if v == nil {
		return 0
	}
	if v.primary {
		v.w.Close()
		return 0
	}
	_ = v.w.removeView(v.ref, caps.ViewClosed) // already closing; nothing to report to the callback
	return 0
}

func loadCallback(_, event, ref uintptr) uintptr {
	if event != loadFinished {
		return 0
	}
	v := theApp.viewByRef(ref)
	if v == nil {
		return 0
	}
	w := v.w
	if js := v.scrollJS; js != "" && v.widget != 0 {
		v.scrollJS = ""
		theApp.api.wkViewEval(v.widget, js, -1, 0, 0, 0, 0, 0)
	}
	theApp.emitView(v, caps.ViewNavigated, v.currentRoute())
	if !v.primary {
		return 0
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
	if kind == policyResponse {
		return responsePolicy(id, decision)
	}
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

// applyViewSettings configures a new view's WebKitSettings: the developer
// extras, then the memory profile. Every view calls it, the first window's
// and the ones that share its web process, and a recreated one after release.
func (a *App) applyViewSettings(view uintptr) {
	settings := a.api.wkViewGetSettings(view)
	if settings == 0 {
		return
	}
	a.api.wkSettingsDevExtras(settings, b2i(a.debug))
	a.applyProfileSettings(settings)
	a.applyMediaSettings(settings)
}
