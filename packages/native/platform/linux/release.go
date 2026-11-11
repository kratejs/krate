//go:build linux

package linux

import (
	"strings"
	"sync/atomic"
	"time"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/internal/memory"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var _ caps.WindowRelease = (*Window)(nil)

// viewAPI holds the bindings behind shared web processes, window release and
// heap trimming. A missing symbol leaves its field nil and the feature falls
// back (separate web processes, no release state capture, no trim).
type viewAPI struct {
	newRelated func(related uintptr) uintptr
	getURI     func(v uintptr) uintptr
	evalFinish func(v, res uintptr, gerr *uintptr) uintptr
	jscString  func(val uintptr) uintptr
}

func loadView() *viewAPI {
	v := &viewAPI{}
	b := &binder{}
	open := func(names ...string) uintptr {
		for _, n := range names {
			if h, err := purego.Dlopen(n, rtldNow|rtldGlobal); err == nil {
				return h
			}
		}
		return 0
	}
	bind := func(lib uintptr, fn any, sym string) {
		if lib != 0 {
			b.bind(fn, lib, sym)
		}
	}
	wk := open("libwebkit2gtk-4.1.so.0")
	jsc := open("libjavascriptcoregtk-4.1.so.0")
	bind(wk, &v.newRelated, "webkit_web_view_new_with_related_view")
	bind(wk, &v.getURI, "webkit_web_view_get_uri")
	bind(wk, &v.evalFinish, "webkit_web_view_evaluate_javascript_finish")
	bind(jsc, &v.jscString, "jsc_value_to_string")
	return v
}

// newView creates a web view. Every view after the first is created related
// to a live one, so all windows of the app share one web process. When the
// last view goes away the next one starts a fresh process. Main thread only.
func (a *App) newView() uintptr {
	if rel := a.relatedView(); rel != 0 && a.v.newRelated != nil {
		if v := a.v.newRelated(rel); v != 0 {
			return v
		}
	}
	return a.api.wkViewNewWithContext(a.ctx)
}

// relatedView returns a live view (the oldest window's), or 0.
func (a *App) relatedView() uintptr {
	a.mu.Lock()
	defer a.mu.Unlock()
	var best *Window
	for _, w := range a.windows {
		if w.view != 0 && (best == nil || w.id < best.id) {
			best = w
		}
	}
	if best == nil {
		return 0
	}
	return best.view
}

// connectView installs the per-view signal handlers.
func (w *Window) connectView() {
	api := w.app.api
	api.gSignalConnectData(w.view, "decide-policy", cbPolicy, w.id, 0, 0)
	api.gSignalConnectData(w.view, "load-changed", cbLoad, w.id, 0, 0)
	api.gSignalConnectData(w.view, "close", cbClose, w.id, 0, 0)
}

// packView puts the view into the window and shows it.
func (w *Window) packView() {
	a := w.app
	if w.box != 0 {
		a.x.gtkBoxPackStart(w.box, w.view, 1, 1, 0)
	} else {
		a.api.gtkContainerAdd(w.win, w.view)
	}
}

// evalString runs js in view and calls fn with the string it evaluates to.
// fn runs on the main thread, once, with "" when the script failed. The caller
// guards against a page that never answers.
func (a *App) evalString(view uintptr, js string, fn func(string)) uintptr {
	a.nextTok++
	tok := a.nextTok
	a.evalCbs[tok] = fn
	a.api.wkViewEval(view, js, -1, 0, 0, 0, cbEvalDone, tok)
	return tok
}

func evalDoneCallback(src, res, tok uintptr) uintptr {
	a := theApp
	var s string
	if a.v.evalFinish != nil {
		var gerr uintptr
		if val := a.v.evalFinish(src, res, &gerr); val != 0 {
			if p := a.v.jscString(val); p != 0 {
				s = goString(p)
				a.api.gFree(p)
			}
			a.api.gObjectUnref(val)
		}
		if gerr != 0 {
			a.api.gErrorFree(gerr)
		}
	}
	if fn := a.evalCbs[tok]; fn != nil {
		delete(a.evalCbs, tok)
		fn(s)
	}
	return 0
}

// currentRoute is the route the view shows according to WebKit, or the last
// route the window was told to load.
func (w *Window) currentRoute() string {
	if w.view != 0 && w.app.v.getURI != nil {
		if uri := goString(w.app.v.getURI(w.view)); strings.HasPrefix(uri, Origin+"/") {
			return uri[len(Origin):]
		}
	}
	return w.route
}

// Release saves the route and scroll position, hides the window and destroys
// its webview. Show or Focus recreates it.
func (w *Window) Release() { w.app.Dispatch(w.release) }

// Released reports whether the window holds no webview.
func (w *Window) Released() (r bool) {
	w.app.callMain(func() { r = w.released })
	return
}

const releaseWait = 1500 * time.Millisecond

// release runs on the main thread. The page is asked for its route and scroll
// position first; a page that does not answer in time is released with the
// route WebKit reports.
func (w *Window) release() {
	a := w.app
	if !w.alive || w.view == 0 || w.releasing {
		return
	}
	w.releasing, w.abort = true, false
	fallback := w.currentRoute()
	if x := a.x; x.gtkWidgetHide != nil {
		x.gtkWidgetHide(w.win)
	}
	done := false
	finish := func(raw string) {
		if done {
			return
		}
		done = true
		w.releasing = false
		if !w.alive || w.view == 0 || w.abort {
			return
		}
		w.state = caps.ParseViewState(raw, fallback)
		w.route = w.state.Route
		v := w.view
		w.view = 0
		a.api.gtkWidgetDestroy(v)
		w.released = true
		a.emitWindow(w, caps.WindowReleased)
		shrinkHeapSoon()
	}
	tok := a.evalString(w.view, caps.ViewStateScript, finish)
	time.AfterFunc(releaseWait, func() {
		a.Dispatch(func() {
			delete(a.evalCbs, tok)
			finish("")
		})
	})
}

// revive recreates the webview of a released window on its saved route, or
// cancels a release that is still waiting for the page. Main thread.
func (w *Window) revive() {
	if w.releasing {
		w.abort = true
		return
	}
	if !w.released || !w.alive {
		return
	}
	a := w.app
	view := a.newView()
	if view == 0 {
		logf("krate: cannot recreate the webview of window %d", w.id)
		return
	}
	w.view = view
	w.released = false
	a.applyViewSettings(view)
	w.connectView()
	if !w.resizable && w.fixedW > 0 {
		a.api.gtkWidgetSetSizeReq(view, int32(w.fixedW), int32(w.fixedH))
	}
	w.packView()
	a.api.gtkWidgetShowAll(w.win)
	a.emitWindow(w, caps.WindowRestored)
	if js := caps.ScrollScript(w.state.X, w.state.Y); js != "" {
		w.scrollJS = js
	}
	a.api.wkViewLoadURI(view, Origin+caps.CleanRoute(w.route))
}

// memoryPressure tells a page that its window was hidden or minimized, and
// returns freed memory of this process to the system shortly after.
func (w *Window) memoryPressure() {
	if w.view != 0 {
		w.app.api.wkViewEval(w.view, caps.MemoryPressureScript, -1, 0, 0, 0, 0, 0)
	}
	shrinkHeapSoon()
}

var trimPending atomic.Bool

// shrinkHeapSoon runs shrinkHeap once, a moment from now, so that handlers of
// the page and WebKit's own cleanup have run first.
func shrinkHeapSoon() {
	if !trimPending.CompareAndSwap(false, true) {
		return
	}
	time.AfterFunc(400*time.Millisecond, func() {
		trimPending.Store(false)
		shrinkHeap()
	})
}

// shrinkHeap returns free memory of the shell process to the system: a Go
// collection with scavenge, then glibc's malloc_trim (GTK, WebKit's shared
// libraries and QuickJS allocate from the glibc heap). The reaper in
// internal/memory owns the trim and its hooks.
func shrinkHeap() { memory.Trim() }

// parkable reports whether closing w releases it instead of destroying it
// (keepAlive and w is the last live window). Main thread.
func (a *App) parkable(w *Window) bool {
	if !a.keepAlive || !w.alive || w.view == 0 || w.releasing {
		return false
	}
	live, parked := 0, 0
	a.mu.Lock()
	for _, o := range a.windows {
		switch {
		case o == w:
		case o.released:
			parked++
		default:
			live++
		}
	}
	a.mu.Unlock()
	return caps.ParkOnClose(a.keepAlive, live, parked)
}

// ViewCount returns the number of windows that hold a webview, for tests.
func (a *App) ViewCount() (n int) {
	a.callMain(func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		for _, w := range a.windows {
			if w.view != 0 {
				n++
			}
		}
	})
	return
}
