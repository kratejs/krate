//go:build linux

package linux

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/internal/chrome"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// xtest drives the X server's pointer through the XTEST extension.
type xtest struct {
	dpy    uintptr
	motion func(d uintptr, screen, x, y int32, delay uint64) int32
	button func(d uintptr, button uint32, press int32, delay uint64) int32
	flush  func(d uintptr) int32
	sync   func(d uintptr, discard int32) int32
}

var (
	xtOnce sync.Once
	xtInst *xtest
)

func loadXTest(t *testing.T) *xtest {
	t.Helper()
	xtOnce.Do(func() {
		x11, err := purego.Dlopen("libX11.so.6", rtldNow|rtldGlobal)
		if err != nil {
			return
		}
		xt, err := purego.Dlopen("libXtst.so.6", rtldNow|rtldGlobal)
		if err != nil {
			return
		}
		var open func(name uintptr) uintptr
		x := &xtest{}
		purego.RegisterLibFunc(&open, x11, "XOpenDisplay")
		purego.RegisterLibFunc(&x.flush, x11, "XFlush")
		purego.RegisterLibFunc(&x.sync, x11, "XSync")
		purego.RegisterLibFunc(&x.motion, xt, "XTestFakeMotionEvent")
		purego.RegisterLibFunc(&x.button, xt, "XTestFakeButtonEvent")
		x.dpy = open(0)
		if x.dpy != 0 {
			xtInst = x
		}
	})
	if xtInst == nil {
		t.Skip("XTEST is not available")
	}
	return xtInst
}

func (x *xtest) moveTo(px, py int) {
	x.motion(x.dpy, -1, int32(px), int32(py), 0)
	x.sync(x.dpy, 0)
}

func (x *xtest) press(down bool) {
	v := int32(0)
	if down {
		v = 1
	}
	x.button(x.dpy, 1, v, 0)
	x.sync(x.dpy, 0)
}

const chromePageCSS = `<style>
body{margin:0;background:#fff}
#bar{height:60px;background:#ccc;-webkit-app-region:drag}
#btn{-webkit-app-region:no-drag;margin:4px}
#attr{height:40px;background:#9c9}
#plain{height:120px;background:#c99}
</style>`

const chromePageBody = `<div id="bar"><button id="btn">b</button></div>
<div id="attr" data-krate-drag></div>
<div id="plain"></div>`

type chromeReport struct {
	Edges   int    `json:"edges"`
	Version int    `json:"version"`
	Region  string `json:"region"`
	Btn     string `json:"btn"`
	Plain   string `json:"plain"`
	Attr    string `json:"attr"`
}

var (
	chromeSetup   sync.Once
	chromeReports = make(chan chromeReport, 8)
)

// registerChromePages serves the test page and the chrome endpoint on the
// shared test server, resolving the window from the request's webview exactly
// as the shell does.
func registerChromePages() {
	chromeSetup.Do(func() {
		h := chrome.Handler(func(r *http.Request) (any, bool) {
			ref, ok := caps.ViewOf(r)
			if !ok {
				return nil, false
			}
			wr, ok := guiApp.WindowOfView(ref)
			if !ok {
				return nil, false
			}
			w := guiApp.WindowByRef(wr)
			return w, w != nil
		})
		guiSrv.mux.HandleFunc(chrome.Path, func(w http.ResponseWriter, r *http.Request) { h(w, r) })
		guiSrv.mux.HandleFunc("/chrome-page", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprintf(w, "<!doctype html><meta charset=utf-8><title>chrome</title>%s%s%s", chromePageCSS, chromePageBody, chrome.Script)
			_, _ = io.WriteString(w, `<script>
setTimeout(async function () {
  var c = window.__krateChrome;
  await new Promise(function (r) { setTimeout(r, 400); });
  var r = {
    edges: document.querySelectorAll("[data-krate-edge]").length,
    version: c ? c.version : 0,
    region: c.regionOf(document.getElementById("bar")),
    btn: c.regionOf(document.getElementById("btn")),
    plain: c.regionOf(document.getElementById("plain")),
    attr: c.regionOf(document.getElementById("attr")),
  };
  fetch("/chrome-report", {method: "POST", body: JSON.stringify(r)});
}, 50);
</script>`)
		})
		guiSrv.mux.HandleFunc("/chrome-report", func(w http.ResponseWriter, r *http.Request) {
			var rep chromeReport
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &rep)
			select {
			case chromeReports <- rep:
			default:
			}
		})
	})
}

func chromeWindow(t *testing.T, o caps.ChromeOptions, resizable bool) (*Window, chromeReport) {
	t.Helper()
	registerChromePages()
	for len(chromeReports) > 0 {
		<-chromeReports
	}
	loaded := make(chan struct{}, 4)
	w, err := guiApp.NewWindowNotify(WindowOptions{Title: "chrome", Width: 500, Height: 400, Resizable: resizable, Path: "/chrome-page", Chrome: o},
		func() { loaded <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	select {
	case <-loaded:
	case <-time.After(15 * time.Second):
		t.Fatal("window did not load")
	}
	var rep chromeReport
	select {
	case rep = <-chromeReports:
	case <-time.After(10 * time.Second):
		t.Fatal("the page script reported nothing")
	}
	return w, rep
}

func TestGUIChromeTitleBarStyles(t *testing.T) {
	needGUI(t)
	w, _ := chromeWindow(t, caps.ChromeOptions{TitleBar: caps.TitleBarHidden}, true)
	if st := w.State(); st.Decorated {
		t.Fatal("a hidden title bar left the decorations")
	}
	cs := w.ChromeState()
	if cs.TitleBar != "hidden" || !cs.ScriptResize || !cs.Resizable || cs.Decorated {
		t.Fatalf("chrome state %+v", cs)
	}
	if err := w.SetTitleBar("native"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "decorated", func() bool { return w.State().Decorated })
	if cs := w.ChromeState(); cs.TitleBar != "native" {
		t.Fatalf("chrome state %+v", cs)
	}
	if err := w.SetTitleBar("none"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "frameless", func() bool { return !w.State().Decorated })
	if err := w.SetTitleBar("tabs"); err == nil {
		t.Fatal("unknown style accepted")
	}
}

func TestGUIChromeOptionsAreValidated(t *testing.T) {
	needGUI(t)
	_, err := guiApp.NewWindow(WindowOptions{Title: "x", Path: "/blank?bad", Chrome: caps.ChromeOptions{Backdrop: "glass"}})
	if err == nil {
		t.Fatal("an invalid backdrop opened a window")
	}
}

func TestGUIChromeUnsupportedOnLinux(t *testing.T) {
	needGUI(t)
	w := ext_newWin(t, "unsup")
	for name, err := range map[string]error{
		"backdrop":  w.SetBackdrop("mica"),
		"vibrancy":  w.SetVibrancy("sidebar"),
		"overlay":   w.SetTitleBarOverlay(&caps.TitleBarOverlay{}),
		"trafficLt": w.SetTrafficLightPosition(caps.Point{X: 10, Y: 10}),
	} {
		if !errors.Is(err, krate.ErrUnsupported) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := w.SetBackdrop("none"); err != nil {
		t.Errorf("clearing the backdrop failed: %v", err)
	}
	if err := w.SetVibrancy(""); err != nil {
		t.Errorf("clearing vibrancy failed: %v", err)
	}
}

func TestGUIChromeTransparent(t *testing.T) {
	needGUI(t)
	w, _ := chromeWindow(t, caps.ChromeOptions{TitleBar: caps.TitleBarNone, Transparent: true}, true)
	c := loadChromeAPI()
	var haveRGBA bool
	guiApp.callMain(func() {
		if c != nil && c.gtkWidgetScreen != nil && c.gdkRGBAVisual != nil {
			haveRGBA = c.gdkRGBAVisual(c.gtkWidgetScreen(w.win)) != 0
		}
	})
	got := w.ChromeState().Transparent
	if got != haveRGBA {
		t.Fatalf("transparent = %v but the screen has an RGBA visual: %v", got, haveRGBA)
	}
	if !haveRGBA {
		t.Log("this X server has no RGBA visual; transparency was refused cleanly")
	}
	if w.State().Decorated {
		t.Fatal("titleBar none left the decorations")
	}
}

func TestGUIChromeRegionsFromScript(t *testing.T) {
	needGUI(t)
	_, rep := chromeWindow(t, caps.ChromeOptions{TitleBar: caps.TitleBarNone}, true)
	if rep.Version != 1 {
		t.Fatalf("script version %d", rep.Version)
	}
	if rep.Region != "drag" || rep.Attr != "drag" {
		t.Errorf("drag regions: bar %q, attribute %q", rep.Region, rep.Attr)
	}
	if rep.Btn != "no-drag" {
		t.Errorf("button in the bar: %q", rep.Btn)
	}
	if rep.Plain != "" {
		t.Errorf("plain area: %q", rep.Plain)
	}
	if rep.Edges != 8 {
		t.Errorf("a frameless resizable window drew %d resize edges, want 8", rep.Edges)
	}
}

func TestGUIChromeDecoratedHasNoScriptEdges(t *testing.T) {
	needGUI(t)
	_, rep := chromeWindow(t, caps.ChromeOptions{}, true)
	if rep.Edges != 0 {
		t.Fatalf("a decorated window drew %d resize edges", rep.Edges)
	}
}

func TestGUIChromeDragMovesTheWindow(t *testing.T) {
	needGUI(t)
	if !getenvWM() {
		t.Skip("moving a window needs a window manager (KRATE_NATIVE_WM=1)")
	}
	xt := loadXTest(t)
	w, _ := chromeWindow(t, caps.ChromeOptions{TitleBar: caps.TitleBarNone}, true)
	w.SetPosition(100, 100)
	waitFor(t, "position", func() bool { s := w.State(); return s.X == 100 && s.Y == 100 })

	// A press on the plain area does not move the window.
	xt.moveTo(100+50, 100+60+40+60)
	time.Sleep(150 * time.Millisecond)
	xt.press(true)
	time.Sleep(300 * time.Millisecond)
	for i := 1; i <= 5; i++ {
		xt.moveTo(100+50+i*20, 100+60+40+60+i*10)
		time.Sleep(40 * time.Millisecond)
	}
	xt.press(false)
	time.Sleep(300 * time.Millisecond)
	if s := w.State(); s.X != 100 || s.Y != 100 {
		t.Fatalf("a press outside any drag region moved the window to %d,%d", s.X, s.Y)
	}

	// A press on the bar starts a window-manager move that follows the pointer.
	startX, startY := 100+50, 100+20
	xt.moveTo(startX, startY)
	time.Sleep(150 * time.Millisecond)
	xt.press(true)
	time.Sleep(400 * time.Millisecond)
	for i := 1; i <= 10; i++ {
		xt.moveTo(startX+i*12, startY+i*6)
		time.Sleep(40 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	xt.press(false)
	time.Sleep(300 * time.Millisecond)
	s := w.State()
	if s.X < 100+60 || s.Y < 100+20 {
		t.Fatalf("dragging the bar left the window at %d,%d (it started at 100,100)", s.X, s.Y)
	}
}

func TestGUIChromeDoublePressTogglesMaximize(t *testing.T) {
	needGUI(t)
	if !getenvWM() {
		t.Skip("maximizing needs a window manager (KRATE_NATIVE_WM=1)")
	}
	xt := loadXTest(t)
	w, _ := chromeWindow(t, caps.ChromeOptions{TitleBar: caps.TitleBarNone}, true)
	w.SetPosition(150, 120)
	waitFor(t, "position", func() bool { s := w.State(); return s.X == 150 && s.Y == 120 })
	xt.moveTo(150+80, 120+20)
	time.Sleep(200 * time.Millisecond)
	for i := 0; i < 2; i++ {
		xt.press(true)
		time.Sleep(60 * time.Millisecond)
		xt.press(false)
		time.Sleep(120 * time.Millisecond)
	}
	waitFor(t, "maximized by a double press", func() bool { return w.State().Maximized })
	if err := w.ToggleMaximize(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "restored", func() bool { return !w.State().Maximized })
}

func TestGUIChromeEdgeResize(t *testing.T) {
	needGUI(t)
	if !getenvWM() {
		t.Skip("resizing needs a window manager (KRATE_NATIVE_WM=1)")
	}
	xt := loadXTest(t)
	w, rep := chromeWindow(t, caps.ChromeOptions{TitleBar: caps.TitleBarNone}, true)
	if rep.Edges != 8 {
		t.Fatalf("%d resize edges", rep.Edges)
	}
	w.SetPosition(100, 100)
	waitFor(t, "position", func() bool { s := w.State(); return s.X == 100 && s.Y == 100 })
	before := w.State()
	// The bottom-right corner edge is 8 px square.
	cx, cy := 100+before.Width-3, 100+before.Height-3
	xt.moveTo(cx, cy)
	time.Sleep(200 * time.Millisecond)
	xt.press(true)
	time.Sleep(400 * time.Millisecond)
	for i := 1; i <= 10; i++ {
		xt.moveTo(cx+i*10, cy+i*8)
		time.Sleep(40 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	xt.press(false)
	waitFor(t, "grown", func() bool { s := w.State(); return s.Width > before.Width+50 && s.Height > before.Height+40 })
}

func getenvWM() bool { return osGetenv("KRATE_NATIVE_WM") == "1" }

var osGetenv = os.Getenv
