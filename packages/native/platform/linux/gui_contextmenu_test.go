//go:build linux

package linux

import (
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

const ctxPage = `<!doctype html><html><head><meta charset="utf-8"><title>ctx</title><style>
body{margin:0} .a{position:absolute}
</style></head><body>
<a id="l" class="a" href="https://example.com/x" style="left:10px;top:10px;width:150px;height:30px;display:block">a link</a>
<img id="i" class="a" style="left:10px;top:60px;width:50px;height:50px" src="data:image/gif;base64,R0lGODlhAQABAIAAAAUEBAAAACwAAAAAAQABAAACAkQBADs=">
<textarea id="t" class="a" spellcheck="false" style="left:10px;top:130px;width:200px;height:40px">hello</textarea>
<p id="s" class="a" style="left:10px;top:200px;margin:0">select me</p>
<script>
const r = document.createRange(); r.selectNodeContents(document.getElementById('s'));
document.addEventListener('DOMContentLoaded', () => {});
window.selectP = () => { document.activeElement.blur(); const s = getSelection(); s.removeAllRanges(); s.addRange(r); };
</script></body></html>`

var ctxPageOnce sync.Once

func ctxWindow(t *testing.T, name string) *Window {
	t.Helper()
	ctxPageOnce.Do(func() {
		guiSrv.mux.HandleFunc("/ctx", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, ctxPage)
		})
	})
	loaded := make(chan struct{}, 4)
	w, err := guiApp.NewWindowNotify(WindowOptions{Title: name, Width: 400, Height: 300, Resizable: true, Path: "/ctx?" + name}, func() { loaded <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-loaded:
	case <-time.After(15 * time.Second):
		t.Fatal("window did not load")
	}
	t.Cleanup(func() { w.Close() })
	return w
}

// viewOrigin is the screen position of the window's webview.
func viewOrigin(w *Window) (x, y int) {
	a := w.app
	a.callMain(func() {
		var ox, oy int32
		a.x.gdkWindowGetOrigin(a.x.gtkWidgetGetWindow(w.view), &ox, &oy)
		x, y = int(ox), int(oy)
	})
	return
}

// fakeClick presses mouse button b at screen x, y through XTEST.
func fakeClick(t *testing.T, button uint32, sx, sy int) {
	t.Helper()
	skipOnWayland(t, "XTEST input does not reach Wayland windows")
	xt, err := purego.Dlopen("libXtst.so.6", rtldNow|rtldGlobal)
	if err != nil {
		t.Skip("libXtst.so.6 is not available: ", err)
	}
	x, err := loadX11()
	if err != nil {
		t.Skip(err)
	}
	var motion func(d uintptr, screen int32, x, y int32, delay uint64) int32
	var btn func(d uintptr, button uint32, press int32, delay uint64) int32
	purego.RegisterLibFunc(&motion, xt, "XTestFakeMotionEvent")
	purego.RegisterLibFunc(&btn, xt, "XTestFakeButtonEvent")
	x.mu.Lock()
	defer x.mu.Unlock()
	motion(x.dpy, -1, int32(sx), int32(sy), 0)
	x.flush(x.dpy)
	time.Sleep(150 * time.Millisecond)
	btn(x.dpy, button, 1, 0)
	x.flush(x.dpy)
	time.Sleep(50 * time.Millisecond)
	btn(x.dpy, button, 0, 0)
	x.flush(x.dpy)
}

func rightClickAt(t *testing.T, w *Window, vx, vy int) {
	t.Helper()
	ox, oy := viewOrigin(w)
	fakeClick(t, 3, ox+vx, oy+vy)
}

func waitInfo(t *testing.T, ch chan caps.ContextMenuInfo) caps.ContextMenuInfo {
	t.Helper()
	select {
	case i := <-ch:
		return i
	case <-time.After(8 * time.Second):
		t.Fatal("no context menu event")
	}
	return caps.ContextMenuInfo{}
}

func TestGUIContextMenuCustom(t *testing.T) {
	needGUI(t)
	w := ctxWindow(t, "ctxcustom")
	a := guiApp
	events := make(chan caps.ContextMenuInfo, 4)
	a.OnContextMenu(func(i caps.ContextMenuInfo) { events <- i })
	t.Cleanup(func() { a.OnContextMenu(nil) })
	if err := a.SetContextMenu(w.Ref(), 0, caps.ContextMenuPolicy{Mode: caps.ContextMenuCustom}); err != nil {
		t.Fatal(err)
	}

	rightClickAt(t, w, 40, 25)
	i := waitInfo(t, events)
	if !i.Has("link") || i.LinkURL != "https://example.com/x" || i.Window != w.Ref() || i.View != w.PrimaryView() {
		t.Fatalf("link: %+v", i)
	}
	if i.X < 30 || i.X > 50 || i.Y < 15 || i.Y > 35 {
		t.Fatalf("view position %d,%d, want about 40,25", i.X, i.Y)
	}
	if i.WindowY < i.Y {
		t.Fatalf("window position %d,%d is above the view position %d,%d", i.WindowX, i.WindowY, i.X, i.Y)
	}
	if !i.Inspectable {
		t.Fatal("a debug app reports inspectable")
	}

	rightClickAt(t, w, 30, 80)
	if i = waitInfo(t, events); !i.Has("image") || !strings.HasPrefix(i.ImageURL, "data:image/gif") {
		t.Fatalf("image: %+v", i)
	}

	rightClickAt(t, w, 100, 150)
	if i = waitInfo(t, events); !i.Has("editable") || i.Has("link") {
		t.Fatalf("editable: %+v", i)
	}

	w.Eval("selectP()")
	time.Sleep(300 * time.Millisecond)
	selected := make(chan string, 1)
	a.callMain(func() { a.evalString(w.view, "String(getSelection())", func(s string) { selected <- s }) })
	if s := <-selected; s != "select me" {
		t.Fatalf("test page selection is %q", s)
	}
	rightClickAt(t, w, 20, 205)
	if i = waitInfo(t, events); !i.Has("selection") || i.SelectionText != "select me" {
		t.Fatalf("selection: %+v", i)
	}

	// The host answers with its own menu, drawn by the native menu code.
	items := []caps.MenuItem{{ID: "mine", Label: "Mine"}, {Kind: caps.MenuItemSeparator}, {ID: "krate:copy", Label: "Copy"}}
	clicks := make(chan caps.MenuEvent, 2)
	a.OnMenu(func(e caps.MenuEvent) { clicks <- e })
	if err := a.PopupMenu(i.Window, items, i.WindowX, i.WindowY); err != nil {
		t.Fatal(err)
	}
	if !a.ActivateMenuItem(w.Ref(), "mine") {
		t.Fatal("popup item not found")
	}
	select {
	case e := <-clicks:
		if e.ID != "mine" || e.Window != w.Ref() {
			t.Fatalf("click %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no click")
	}
}

// skipOnWayland skips a test that needs X11: input injected through XTEST and
// window placement only work there (the CI also runs this package under
// weston with GDK_BACKEND=wayland).
func skipOnWayland(t *testing.T, why string) {
	t.Helper()
	if os.Getenv("GDK_BACKEND") == "wayland" || (os.Getenv("WAYLAND_DISPLAY") != "" && os.Getenv("DISPLAY") == "") {
		t.Skip("Wayland: " + why)
	}
}
