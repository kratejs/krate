//go:build linux

package linux

import (
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

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

func TestGUIContextMenuNone(t *testing.T) {
	needGUI(t)
	w := ctxWindow(t, "ctxnone")
	a := guiApp
	events := make(chan caps.ContextMenuInfo, 2)
	a.OnContextMenu(func(i caps.ContextMenuInfo) { events <- i })
	t.Cleanup(func() { a.OnContextMenu(nil) })
	if err := a.SetContextMenu(w.Ref(), 0, caps.ContextMenuPolicy{Mode: caps.ContextMenuNone}); err != nil {
		t.Fatal(err)
	}
	before := 0
	a.callMain(func() { before = a.ctxSt().suppressed })
	rightClickAt(t, w, 40, 25)
	waitFor(t, "the menu to be suppressed", func() (ok bool) {
		a.callMain(func() { ok = a.ctxSt().suppressed > before })
		return
	})
	select {
	case i := <-events:
		t.Fatalf("none mode reported an event: %+v", i)
	case <-time.After(300 * time.Millisecond):
	}

	// A view policy beats the window's, and the window's beats the app's.
	if err := a.SetContextMenu(0, w.PrimaryView(), caps.ContextMenuPolicy{Mode: caps.ContextMenuCustom}); err != nil {
		t.Fatal(err)
	}
	rightClickAt(t, w, 40, 25)
	if i := waitInfo(t, events); !i.Has("link") {
		t.Fatalf("view override: %+v", i)
	}
}

func TestGUIContextMenuExtend(t *testing.T) {
	needGUI(t)
	w := ctxWindow(t, "ctxextend")
	a := guiApp
	items := []caps.MenuItem{
		{ID: "ext-one", Label: "Extra &One"},
		{ID: "ext-chk", Label: "Check me", Kind: caps.MenuItemCheck},
		{Label: "More", Kind: caps.MenuItemSubmenu, Submenu: []caps.MenuItem{{ID: "ext-sub", Label: "Sub"}}},
	}
	if err := a.SetContextMenu(w.Ref(), 0, caps.ContextMenuPolicy{Mode: caps.ContextMenuExtend, Items: items}); err != nil {
		t.Fatal(err)
	}
	rightClickAt(t, w, 40, 25)
	var titles []string
	waitFor(t, "the engine menu", func() (ok bool) {
		a.callMain(func() { titles = append([]string(nil), a.ctxSt().lastTitles...); ok = len(titles) > 0 })
		return
	})
	has := func(s string) bool {
		for _, x := range titles {
			if x == s {
				return true
			}
		}
		return false
	}
	if !has("Extra One") || !has("Check me") || !has("More") {
		t.Fatalf("extension items missing from the engine menu: %q", titles)
	}
	// The engine's own items are still there (we only added to them).
	if len(titles) < 5 {
		t.Fatalf("engine menu looks replaced: %q", titles)
	}
	fakeKeys(t, "Escape")
	time.Sleep(200 * time.Millisecond)

	clicks := make(chan caps.MenuEvent, 4)
	a.OnMenu(func(e caps.MenuEvent) { clicks <- e })
	for _, id := range []string{"ext-one", "ext-chk", "ext-chk"} {
		if !ctxActivate(id) {
			t.Fatalf("no extension item %q", id)
		}
	}
	var got []string
	for i := 0; i < 3; i++ {
		select {
		case e := <-clicks:
			got = append(got, fmt.Sprintf("%s:%v", e.ID, e.Checked))
		case <-time.After(3 * time.Second):
			t.Fatalf("clicks so far: %v", got)
		}
	}
	if want := []string{"ext-one:false", "ext-chk:true", "ext-chk:false"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("clicks %v, want %v", got, want)
	}
}

// ctxActivate runs the activate handler of an extension item, as a click on
// the engine's menu would.
func ctxActivate(id string) bool {
	var h uintptr
	menuMu.Lock()
	for k, t := range menuTargets {
		if t.id == id && t.item == 0 {
			h = k
		}
	}
	menuMu.Unlock()
	if h == 0 {
		return false
	}
	ctxActionCallback(0, 0, h)
	return true
}

func TestGUIContextCommands(t *testing.T) {
	needGUI(t)
	w := ctxWindow(t, "ctxcmd")
	a := guiApp
	if err := a.SetContextMenu(w.Ref(), 0, caps.ContextMenuPolicy{Mode: caps.ContextMenuCustom}); err != nil {
		t.Fatal(err)
	}
	events := make(chan caps.ContextMenuInfo, 2)
	a.OnContextMenu(func(i caps.ContextMenuInfo) { events <- i })
	t.Cleanup(func() { a.OnContextMenu(nil) })
	rightClickAt(t, w, 100, 150)
	if i := waitInfo(t, events); !i.Has("editable") {
		t.Fatalf("not editable: %+v", i)
	}
	if err := a.WriteText(""); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{caps.CommandSelectAll, caps.CommandCopy} {
		if err := a.ContextCommand(w.Ref(), c, ""); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "copy", func() bool { s, _ := a.ReadText(); return s == "hello" })
	if err := a.ContextCommand(w.Ref(), caps.CommandReplace, "howdy"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{caps.CommandSelectAll, caps.CommandCut} {
		if err := a.ContextCommand(w.Ref(), c, ""); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "cut", func() bool { s, _ := a.ReadText(); return s == "howdy" })
	if err := a.ContextCommand(w.Ref(), caps.CommandPaste, ""); err != nil {
		t.Fatal(err)
	}
	if err := a.ContextCommand(w.Ref(), "nonsense", ""); err == nil {
		t.Fatal("an unknown command is an error")
	}
	if err := a.ContextCommand(w.Ref(), caps.CommandInspect, ""); err != nil {
		t.Fatalf("inspect in a debug app: %v", err)
	}
}

// The action numbers above are hard-coded; check them against the library.
func TestContextMenuActionValues(t *testing.T) {
	needGUI(t)
	c := loadCtxAPI()
	if c.menuActionType == nil || c.gTypeClassRef == nil || c.gEnumNickValue == nil {
		t.Skip("the GEnum lookups are not bound")
	}
	class := c.gTypeClassRef(c.menuActionType())
	for nick, want := range map[string]int32{
		"copy-link-to-clipboard": actCopyLink, "spelling-guess": actSpellingGuess, "no-guesses-found": actNoGuessesFound,
		"ignore-spelling": actIgnoreSpelling, "learn-spelling": actLearnSpelling, "inspect-element": actInspectElement,
	} {
		v := c.gEnumNickValue(class, nick)
		if v == 0 {
			t.Fatalf("no enum value with nick %q", nick)
		}
		if got := *(*int32)(*(*unsafe.Pointer)(unsafe.Pointer(&v))); got != want {
			t.Fatalf("%s = %d in WebKit, %d here", nick, got, want)
		}
	}
}
