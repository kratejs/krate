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

// Release saves the route and scroll position, hides the window and closes
// its WebView2 controller. Show or Focus creates a new controller (from the
// same shared environment) on the saved route.
func (w *Window) Release() { w.onMain(w.release) }

// Released reports whether the window holds no webview.
func (w *Window) Released() (r bool) {
	w.app.callMain(func() { r = w.released })
	return
}

// release runs on the main thread. The page is asked for its route and scroll
// position first; one that does not answer in time is released with the
// route it was last told to load.
func (w *Window) release() {
	if w.webview == nil || w.releasing || w.released || w.destroyed {
		return
	}
	w.releasing, w.abort = true, false
	fallback := w.path
	pShowWindow.Call(w.hwnd, swHide)
	done := false
	finish := func(raw string) {
		if done {
			return
		}
		done = true
		w.releasing = false
		if w.destroyed || w.webview == nil || w.abort {
			return
		}
		w.state = caps.ParseViewState(raw, fallback)
		w.path = w.state.Route
		w.closeController()
		w.released = true
		emit(w, caps.WindowReleased)
		shrinkHeapSoon()
	}
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
		finish(raw)
		return sOK
	})
	if failed(vcall(w.webview, wvExecuteScript, uintptr(unsafe.Pointer(wstr(caps.ViewStateScript))), uintptr(h))) {
		finish("")
		return
	}
	time.AfterFunc(releaseWait, func() { w.app.Dispatch(func() { finish("") }) })
}

// revive recreates the controller of a released window, or cancels a release
// that is still waiting for the page. Main thread.
func (w *Window) revive() {
	if w.releasing {
		w.abort = true
		return
	}
	if !w.released || w.destroyed {
		return
	}
	w.released = false
	w.path = w.state.Route
	w.scrollJS = caps.ScrollScript(w.state.X, w.state.Y)
	emit(w, caps.WindowRestored)
	w.createController()
}

// addScrollRestore registers the scroll restore of a revived window as a
// script that runs when the next document is created. It guards itself: only
// documents of the app origin run it, and only the first one.
func (w *Window) addScrollRestore() {
	js := w.scrollJS
	w.scrollJS = ""
	if js == "" || w.webview == nil {
		return
	}
	guarded := `(function(){if(location.origin!==` + jsString(Origin) + `)return;` +
		`try{if(sessionStorage.getItem("__krate_scroll"))return;sessionStorage.setItem("__krate_scroll","1")}catch(e){}` +
		js + `})()`
	vcall(w.webview, wvAddScriptOnDocCreated, uintptr(unsafe.Pointer(wstr(guarded))), 0)
}

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// parkable reports whether closing w releases it instead of destroying it
// (KeepAlive, and w is the last window with a webview). Main thread.
func (a *App) parkable(w *Window) bool {
	if !a.cfg.KeepAlive || w.destroyed || w.webview == nil || w.releasing {
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

// setViewHidden makes the controller invisible (hidden or minimized window) or
// visible again, and sets the browser's memory target for the page: LOW while
// hidden, the profile's visible level otherwise. A page that becomes hidden also gets the
// krate:memory-pressure event. Main thread.
func (w *Window) setViewHidden(hidden bool) {
	if w.controller == nil || w.viewHid == hidden {
		return
	}
	w.viewHid = hidden
	if hidden {
		vcall(w.controller, ctlPutIsVisible, 0)
		setMemoryTarget(w.webview, memTargetLow)
		if w.webview != nil {
			vcall(w.webview, wvExecuteScript, uintptr(unsafe.Pointer(wstr(caps.MemoryPressureScript))), 0)
		}
		shrinkHeapSoon()
		return
	}
	// Back to the level a visible view has in this profile (LOW for minimal).
	setMemoryTarget(w.webview, visibleMemoryTarget(w.app.cfg.Memory))
	vcall(w.controller, ctlPutIsVisible, 1)
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
