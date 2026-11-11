//go:build windows

package windows

import (
	"encoding/json"
	"sync/atomic"
	"time"
	"unsafe"

	xw "golang.org/x/sys/windows"

	"github.com/kratejs/krate/packages/native/internal/memory"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var _ caps.WindowRelease = (*Window)(nil)

const releaseWait = 1500 * time.Millisecond

// Release saves the route and scroll position of every view, hides the window
// and closes all its WebView2 controllers. Show or Focus creates new ones
// (from the same shared environment) on the saved routes and layout.
func (w *Window) Release() { w.onMain(w.release) }

// Released reports whether the window holds no webview.
func (w *Window) Released() (r bool) {
	w.app.callMain(func() { r = w.released })
	return
}

// release runs on the main thread. Every page is asked for its route and
// scroll position first; one that does not answer in time is released with
// the route it was last told to load.
func (w *Window) release() {
	if w.pv.webview == nil || w.releasing || w.released || w.destroyed {
		return
	}
	w.releasing, w.abort = true, false
	var live []*view
	for _, v := range w.allViews() {
		if v.webview != nil {
			live = append(live, v)
		}
	}
	fallback := map[*view]string{}
	for _, v := range live {
		fallback[v] = v.path
	}
	pShowWindow.Call(w.hwnd, swHide)
	answered := map[*view]bool{}
	finish := func() {
		w.releasing = false
		if w.destroyed || w.pv.webview == nil || w.abort {
			return
		}
		// The divider ratios live in w.layout, so they survive as is.
		for _, v := range live {
			v.path = v.state.Route
			v.closeController()
		}
		w.released = true
		emit(w, caps.WindowReleased)
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
		h := newHandler(iidExecuteScriptHandler, true, func(hr, result uintptr) uintptr {
			raw := ""
			if !failed(hr) && result != 0 {
				// The result is the script's value as JSON; the script returns a
				// string, so it is a quoted string holding the JSON of the state.
				var s string
				if json.Unmarshal([]byte(xw.UTF16PtrToString((*uint16)(ptr(result)))), &s) == nil {
					raw = s
				}
			}
			settle(v, raw)
			return sOK
		})
		if failed(vcall(v.webview, wvExecuteScript, uintptr(unsafe.Pointer(wstr(caps.ViewStateScript))), uintptr(h))) {
			settle(v, "")
		}
	}
	time.AfterFunc(releaseWait, func() {
		w.app.Dispatch(func() {
			for _, v := range live {
				settle(v, "")
			}
		})
	})
}

// revive recreates the controllers of a released window, or cancels a release
// that is still waiting for the pages. Main thread.
func (w *Window) revive() {
	if w.releasing {
		w.abort = true
		return
	}
	if !w.released || w.destroyed {
		return
	}
	w.released = false
	emit(w, caps.WindowRestored)
	for _, v := range w.allViews() {
		v.path = v.state.Route
		v.scrollJS = caps.ScrollScript(v.state.X, v.state.Y)
		v.ready = make(chan struct{})
		v.createController()
	}
}

// addScrollRestore registers the scroll restore of a revived view as a
// script that runs when the next document is created. It guards itself: only
// documents of the app origin run it, and only the first one.
func (v *view) addScrollRestore() {
	js := v.scrollJS
	v.scrollJS = ""
	if js == "" || v.webview == nil {
		return
	}
	guarded := `(function(){if(location.origin!==` + jsString(Origin) + `)return;` +
		`try{if(sessionStorage.getItem("__krate_scroll"))return;sessionStorage.setItem("__krate_scroll","1")}catch(e){}` +
		js + `})()`
	vcall(v.webview, wvAddScriptOnDocCreated, uintptr(unsafe.Pointer(wstr(guarded))), 0)
}

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// parkable reports whether closing w releases it instead of destroying it
// (KeepAlive, and w is the last window with a webview). Main thread.
func (a *App) parkable(w *Window) bool {
	if !a.cfg.KeepAlive || w.destroyed || w.pv.webview == nil || w.releasing {
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
	return caps.ParkOnClose(a.cfg.KeepAlive, live, parked)
}

// setViewHidden makes the controllers invisible (hidden or minimized window)
// or visible again, and sets the browser's memory target for the pages: LOW
// while hidden, the profile's visible level otherwise. A page that becomes
// hidden also gets the krate:memory-pressure event. Main thread.
func (w *Window) setViewHidden(hidden bool) {
	for _, v := range w.allViews() {
		v.setHidden(hidden)
	}
	if hidden {
		shrinkHeapSoon()
	}
}

func (v *view) setHidden(hidden bool) {
	if v.controller == nil || v.viewHid == hidden {
		return
	}
	v.viewHid = hidden
	if hidden {
		vcall(v.controller, ctlPutIsVisible, 0)
		setMemoryTarget(v.webview, memTargetLow)
		if v.webview != nil {
			vcall(v.webview, wvExecuteScript, uintptr(unsafe.Pointer(wstr(caps.MemoryPressureScript))), 0)
		}
		return
	}
	// Back to the level a visible view has in this profile (LOW for minimal).
	setMemoryTarget(v.webview, visibleMemoryTarget(v.w.app.cfg.Memory))
	vcall(v.controller, ctlPutIsVisible, 1)
}

var trimPending atomic.Bool

// shrinkHeapSoon returns free memory of this process to the system once,
// shortly after a window was hidden.
func shrinkHeapSoon() {
	if !trimPending.CompareAndSwap(false, true) {
		return
	}
	time.AfterFunc(400*time.Millisecond, func() {
		trimPending.Store(false)
		memory.Trim()
	})
}
