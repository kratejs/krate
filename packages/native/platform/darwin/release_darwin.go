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
// plain pool (one WebContent process per window). This path has not been run
// on macOS hardware yet; see the memory docs page.
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

// Release saves the route and scroll position, hides the window and removes
// its WKWebView. Show or Focus creates a new one on the saved route.
func (w *Window) Release() { w.withWin(w.release) }

// Released reports whether the window holds no webview.
func (w *Window) Released() (r bool) {
	runOnMain(func() { r = w.released })
	return
}

// release runs on the main thread. The page is asked for its route and scroll
// position first; one that does not answer in time is released with the route
// it was last told to load.
func (w *Window) release() {
	if w.closed || w.web == 0 || w.releasing {
		return
	}
	w.releasing, w.abort = true, false
	fallback := w.route
	w.win.Send(sel("orderOut:"), objc.ID(0))
	done := false
	finish := func(raw string) {
		if done {
			return
		}
		done = true
		w.releasing = false
		if w.closed || w.web == 0 || w.abort {
			return
		}
		w.state = caps.ParseViewState(raw, fallback)
		w.route = w.state.Route
		web := w.web
		w.web = 0
		web.Send(sel("stopLoading"))
		web.Send(sel("setNavigationDelegate:"), objc.ID(0))
		w.win.Send(sel("setContentView:"), objc.ID(0))
		release(web)
		w.released = true
		emitWindow(w, caps.WindowReleased)
		shrinkHeapSoon()
	}
	// void (^)(id result, NSError *error); result is the script's string.
	blk := objc.NewBlock(func(_ objc.Block, result, err objc.ID) {
		raw := ""
		if err == 0 && result != 0 {
			raw = goString(result)
		}
		finish(raw)
	})
	w.web.Send(sel("evaluateJavaScript:completionHandler:"), nsString(caps.ViewStateScript), objc.ID(blk))
	blk.Release() // WebKit holds its own copy
	time.AfterFunc(releaseWait, func() { dispatch(func() { finish("") }) })
}

// revive creates a new WKWebView for a released window on its saved route, or
// cancels a release that is still waiting for the page. Main thread.
func (w *Window) revive() {
	if w.releasing {
		w.abort = true
		return
	}
	if !w.released || w.closed {
		return
	}
	web := w.app.newWebView()
	if web == 0 {
		return
	}
	w.web = web
	w.released = false
	w.scrollJS = caps.ScrollScript(w.state.X, w.state.Y)
	w.win.Send(sel("setContentView:"), web)
	w.load(w.route)
	emitWindow(w, caps.WindowRestored)
}

// navigationFinished runs webView:didFinishNavigation:. A revived window
// restores its scroll position once its page has loaded.
func (a *App) navigationFinished(web objc.ID) {
	for _, w := range a.windows {
		if w.web == web && w.scrollJS != "" {
			js := w.scrollJS
			w.scrollJS = ""
			web.Send(sel("evaluateJavaScript:completionHandler:"), nsString(js), objc.ID(0))
			return
		}
	}
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
	if w.web != 0 {
		w.web.Send(sel("evaluateJavaScript:completionHandler:"), nsString(caps.MemoryPressureScript), objc.ID(0))
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
