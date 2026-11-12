//go:build darwin

package darwin

import (
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/ebitengine/purego/objc"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var _ caps.WindowRelease = (*Window)(nil)

const releaseWait = 1500 * time.Millisecond

// newProcessPool creates the one WKProcessPool of the app. WebKit starts a
// WebContent process per web view unless told otherwise; the private
// _WKProcessPoolConfiguration.usesSingleWebProcess switch makes every view of
// the pool share one, which is what an app with a single origin wants. Both
// the class and the selectors are checked, and a WebKit without them gets the
// plain pool (one WebContent process per window). This path runs when
// each macOS GUI test creates its first window; no test asserts that the
// views then share one process (see the memory docs page).
func newProcessPool() objc.ID {
	poolCls := cls("WKProcessPool")
	confCls := cls("_WKProcessPoolConfiguration")
	initSel, setSel := sel("_initWithConfiguration:"), sel("setUsesSingleWebProcess:")
	if confCls != 0 && objc.Send[bool](poolCls, sel("instancesRespondToSelector:"), initSel) &&
		objc.Send[bool](confCls, sel("instancesRespondToSelector:"), setSel) {
		conf := confCls.Send(sel("new"))
		if conf != 0 {
			defer release(conf)
			conf.Send(setSel, true)
			if pool := poolCls.Send(sel("alloc")).Send(initSel, conf); pool != 0 {
				return pool
			}
		}
	}
	return poolCls.Send(sel("new"))
}

// Release saves the route and scroll position of every view of the window,
// hides it and removes all its WKWebViews. Show or Focus creates new ones on
// the saved routes and layout.
func (w *Window) Release() { w.withWin(w.release) }

// Released reports whether the window holds no webview.
func (w *Window) Released() (r bool) {
	runOnMain(func() { r = w.released })
	return
}

// release runs on the main thread. Every page is asked for its route and
// scroll position first; one that does not answer in time is released with the
// route it was last told to load.
func (w *Window) release() {
	if w.closed || w.web == 0 || w.releasing {
		return
	}
	w.releasing, w.abort = true, false
	live := w.liveViews()
	fallback := map[*view]string{}
	for _, v := range live {
		fallback[v] = v.route
	}
	w.win.Send(sel("orderOut:"), objc.ID(0))
	answered := map[*view]bool{}
	finish := func() {
		w.releasing = false
		if w.closed || w.web == 0 || w.abort {
			return
		}
		w.snapshotRatios()
		w.splits = nil
		for _, v := range live {
			v.route = v.state.Route
			web := v.web
			w.app.setWeb(v, 0)
			web.Send(sel("stopLoading"))
			web.Send(sel("setNavigationDelegate:"), objc.ID(0))
			web.Send(sel("removeFromSuperview"))
			release(web)
		}
		w.win.Send(sel("setContentView:"), objc.ID(0))
		w.root = 0
		w.released = true
		emitWindow(w, caps.WindowReleased)
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
	for _, v := range live {
		v := v
		// void (^)(id result, NSError *error); result is the script's string.
		blk := objc.NewBlock(func(_ objc.Block, result, err objc.ID) {
			raw := ""
			if err == 0 && result != 0 {
				raw = goString(result)
			}
			settle(v, raw)
		})
		v.web.Send(sel("evaluateJavaScript:completionHandler:"), nsString(caps.ViewStateScript), objc.ID(blk))
		blk.Release() // WebKit holds its own copy
	}
	time.AfterFunc(releaseWait, func() {
		dispatch(func() {
			for _, v := range live {
				settle(v, "")
			}
		})
	})
}

// revive creates new WKWebViews for a released window on their saved routes
// and layout, or cancels a release that is still waiting for the pages. Main
// thread.
func (w *Window) revive() {
	if w.releasing {
		w.abort = true
		return
	}
	if !w.released || w.closed {
		return
	}
	all := w.allViews()
	var made []objc.ID
	for _, v := range all {
		web := w.app.newWebView()
		if web == 0 {
			for _, m := range made {
				release(m)
			}
			return
		}
		made = append(made, web)
		w.app.setWeb(v, web)
	}
	w.released = false
	for _, v := range all {
		v.scrollJS = caps.ScrollScript(v.state.X, v.state.Y)
		v.applyMinSize()
		v.find.Reset()
		if v.zoom != 0 && respondsTo(v.web, sel("setPageZoom:")) {
			v.web.Send(sel("setPageZoom:"), v.zoom)
		}
	}
	w.applyLayout()
	for _, v := range all {
		v.load(v.route)
	}
	emitWindow(w, caps.WindowRestored)
}

// navigationFinished runs webView:didFinishNavigation:. A revived view
// restores its scroll position once its page has loaded; every load is
// reported as a navigated event.
func (a *App) navigationFinished(web objc.ID) {
	v := a.byWeb[web]
	if v == nil {
		return
	}
	if v.scrollJS != "" {
		js := v.scrollJS
		v.scrollJS = ""
		web.Send(sel("evaluateJavaScript:completionHandler:"), nsString(js), objc.ID(0))
	}
	emitView(a, v, caps.ViewNavigated, v.currentRoute())
}

// parkable reports whether closing w releases it instead of destroying it
// (KeepAlive, and w is the last window with a webview). Main thread.
func (a *App) parkable(w *Window) bool {
	if !a.opts.KeepAlive || w.closed || w.web == 0 || w.releasing {
		return false
	}
	live, parked := 0, 0
	for _, o := range a.windows {
		switch {
		case o == w:
		case o.released:
			parked++
		default:
			live++
		}
	}
	return caps.ParkOnClose(a.opts.KeepAlive, live, parked)
}

// memoryPressure tells the page that its window was hidden or minimized and
// returns free Go heap to the system shortly after. WebKit throttles hidden
// and miniaturized web views on its own; nothing else is set. Main thread.
func (w *Window) memoryPressure() {
	for _, v := range w.liveViews() {
		v.web.Send(sel("evaluateJavaScript:completionHandler:"), nsString(caps.MemoryPressureScript), objc.ID(0))
	}
	shrinkHeapSoon()
}

var trimPending atomic.Bool

func shrinkHeapSoon() {
	if !trimPending.CompareAndSwap(false, true) {
		return
	}
	time.AfterFunc(400*time.Millisecond, func() {
		trimPending.Store(false)
		debug.FreeOSMemory()
	})
}
