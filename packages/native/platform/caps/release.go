package caps

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/native/krate"
)

// WindowRelease is implemented by Window values that can give up their
// webview while the native window stays registered. A released window is
// hidden and holds no web content, so the web process of an app whose windows
// are all released exits. Show or Focus (WindowControls) brings the webview
// back on the route and scroll position it had.
type WindowRelease interface {
	// Release saves the route and scroll position, hides the window and
	// destroys its webview. Releasing a released window does nothing.
	Release()
	// Released reports whether the webview is gone.
	Released() bool
}

// MemoryPressureEvent is the DOM event the shell dispatches on the window of a
// page that was hidden or minimized, so the app can drop caches.
const MemoryPressureEvent = "krate:memory-pressure"

// MemoryPressureScript dispatches MemoryPressureEvent in the page.
const MemoryPressureScript = `try{window.dispatchEvent(new Event("` + MemoryPressureEvent + `"))}catch(e){}`

// ViewStateScript evaluates to a JSON string with the page's route and scroll
// position; ParseViewState reads it.
const ViewStateScript = `(function(){var s="";try{s=sessionStorage.getItem("` + krate.WindowQuery + `")||""}catch(e){}` +
	`return JSON.stringify({r:location.pathname+location.search+location.hash,x:window.scrollX||0,y:window.scrollY||0,w:s})})()`

// ViewState is what a released window remembers about its page.
type ViewState struct {
	// Route is the path, query and fragment on the app origin.
	Route string
	// X and Y are the scroll offsets.
	X, Y float64
}

// ParseViewState reads the result of ViewStateScript. It never fails: an
// unreadable answer, or a route that is not a path on the app origin, yields
// fallback (the last known route) with no scroll offset. The window id the
// shell keeps in sessionStorage is put back into the route when navigation
// dropped it, because a new webview starts with empty session storage.
func ParseViewState(raw, fallback string) ViewState {
	st := ViewState{Route: CleanRoute(fallback)}
	var v struct {
		R string  `json:"r"`
		X float64 `json:"x"`
		Y float64 `json:"y"`
		W string  `json:"w"`
	}
	if json.Unmarshal([]byte(raw), &v) != nil || !strings.HasPrefix(v.R, "/") || strings.HasPrefix(v.R, "//") {
		return st
	}
	st.Route = v.R
	if v.W != "" {
		st.Route = withWindowID(st.Route, v.W)
	}
	if v.X > 0 && v.X < 1e7 {
		st.X = v.X
	}
	if v.Y > 0 && v.Y < 1e7 {
		st.Y = v.Y
	}
	return st
}

// withWindowID adds the window id parameter to route unless it has one.
func withWindowID(route, id string) string {
	frag := ""
	if i := strings.IndexByte(route, '#'); i >= 0 {
		route, frag = route[:i], route[i:]
	}
	path, query, _ := strings.Cut(route, "?")
	if q, err := url.ParseQuery(query); err != nil || q.Has(krate.WindowQuery) {
		return route + frag
	}
	if query != "" {
		query += "&"
	}
	return path + "?" + query + krate.WindowQuery + "=" + url.QueryEscape(id) + frag
}

// CleanRoute makes p an absolute path on the app origin ("/" when empty).
func CleanRoute(p string) string {
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// ScrollScript restores a scroll position. A freshly loaded page is often
// still laying out (hydration), so it retries for about a second.
func ScrollScript(x, y float64) string {
	if x == 0 && y == 0 {
		return ""
	}
	xs, ys := strconv.FormatFloat(x, 'f', -1, 64), strconv.FormatFloat(y, 'f', -1, 64)
	return fmt.Sprintf(`(function(x,y){var n=0;function t(){window.scrollTo(x,y);`+
		`if((Math.abs(window.scrollX-x)>1||Math.abs(window.scrollY-y)>1)&&n++<20)setTimeout(t,50)}t()})(%s,%s)`, xs, ys)
}

// QuitWhenEmpty reports whether the application quits after a window went
// away and remaining windows are left. With keepAlive the app runs on.
func QuitWhenEmpty(keepAlive bool, remaining int) bool {
	return !keepAlive && remaining == 0
}

// ParkOnClose reports whether closing a window releases it instead of
// destroying it. With keepAlive the last live window is parked so that Show
// can bring it back; liveOthers counts the other windows that still have a
// webview, parkedOthers the other released windows. At most one window is
// parked, so a long-running tray app does not collect hidden windows.
func ParkOnClose(keepAlive bool, liveOthers, parkedOthers int) bool {
	return keepAlive && liveOthers == 0 && parkedOthers == 0
}
