//go:build linux

package linux

import (
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
	// reqView is webkit_uri_scheme_request_get_web_view: the view a scheme
	// request came from.
	reqView func(request uintptr) uintptr
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
	bind(wk, &v.reqView, "webkit_uri_scheme_request_get_web_view")
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

// Release saves the route and scroll position of every view of the window,
// hides the window and destroys all its webviews. Show or Focus recreates
// them on the routes and layout they had.
func (w *Window) Release() { w.app.Dispatch(w.release) }

// Released reports whether the window holds no webview.
func (w *Window) Released() (r bool) {
	w.app.callMain(func() { r = w.released })
	return
}

const releaseWait = 1500 * time.Millisecond

// release runs on the main thread. Every page is asked for its route and
// scroll position first; a page that does not answer in time is released with
// the route WebKit reports.
func (w *Window) release() {
	a := w.app
	if !w.alive || w.view == 0 || w.releasing {
		return
	}
	w.releasing, w.abort = true, false
	live := w.liveViews()
	fallback := map[*view]string{}
	for _, v := range live {
		fallback[v] = v.currentRoute()
	}
	if x := a.x; x.gtkWidgetHide != nil {
		x.gtkWidgetHide(w.win)
	}
	answered := map[*view]bool{}
	finish := func() {
		w.releasing = false
		if !w.alive || w.view == 0 || w.abort {
			return
		}
		w.snapshotRatios()
		// Destroying the root takes the pane tree and every webview in it.
		if w.root != 0 {
			a.api.gtkWidgetDestroy(w.root)
			w.root = 0
		}
		for _, p := range w.panes {
			delete(a.panes, p.id)
		}
		w.panes = nil
		for _, v := range live {
			v.route = v.state.Route
			a.setWidget(v, 0)
		}
		w.released = true
		a.emitWindow(w, caps.WindowReleased)
		shrinkHeapSoon()
	}
	settle := func(v *view, raw string) {
		if answered[v] {
			return
		}
		answered[v] = true
		v.state = caps.ParseViewState(raw, fallback[v])
		if len(answered) == len(live) {
			finish()
		}
	}
	var toks []uintptr
	for _, v := range live {
		v := v
		toks = append(toks, a.evalString(v.widget, caps.ViewStateScript, func(raw string) { settle(v, raw) }))
	}
	time.AfterFunc(releaseWait, func() {
		a.Dispatch(func() {
			for _, t := range toks {
				delete(a.evalCbs, t)
			}
			for _, v := range live {
				settle(v, "")
			}
		})
	})
}

// revive recreates the webviews of a released window on their saved routes and
// layout, or cancels a release that is still waiting for the pages. Main
// thread.
func (w *Window) revive() {
	if w.releasing {
		w.abort = true
		return
	}
	if !w.released || !w.alive {
		return
	}
	a := w.app
	all := w.allViews()
	widgets := make([]uintptr, 0, len(all))
	for range all {
		// The first view is created first, so the others are related to it
		// and share its web process.
		widget := a.newView()
		if widget == 0 {
			logf("krate: cannot recreate the webviews of window %d", w.id)
			for _, d := range widgets {
				a.api.gtkWidgetDestroy(d)
			}
			return
		}
		widgets = append(widgets, widget)
		a.setWidget(all[len(widgets)-1], widget)
	}
	w.released = false
	for _, v := range all {
		a.applyViewSettings(v.widget)
		a.connectView(v)
		if v.zoom != 0 && a.pg.setZoom != nil {
			a.pg.setZoom(v.widget, v.zoom)
		}
		v.find = findState{}
		if v.minW > 0 || v.minH > 0 {
			a.api.gtkWidgetSetSizeReq(v.widget, int32(v.minW), int32(v.minH))
		}
	}
	w.applyLayout()
	a.emitWindow(w, caps.WindowRestored)
	for _, v := range all {
		if js := caps.ScrollScript(v.state.X, v.state.Y); js != "" {
			v.scrollJS = js
		}
		a.api.wkViewLoadURI(v.widget, Origin+caps.CleanRoute(v.route))
	}
}

// memoryPressure tells the pages of a window that it was hidden or minimized,
// and returns freed memory of this process to the system shortly after.
func (w *Window) memoryPressure() {
	for _, v := range w.liveViews() {
		w.app.api.wkViewEval(v.widget, caps.MemoryPressureScript, -1, 0, 0, 0, 0, 0)
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

// WebViewCount returns the number of live webviews of the app, for tests.
func (a *App) WebViewCount() (n int) {
	a.callMain(func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		for _, v := range a.views {
			if v.widget != 0 {
				n++
			}
		}
	})
	return
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
