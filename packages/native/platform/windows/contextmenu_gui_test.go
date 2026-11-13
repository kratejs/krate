//go:build windows

package windows

import (
	"encoding/json"
	"testing"
	"time"
	"unsafe"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

var (
	pClientToScreen = user32.NewProc("ClientToScreen")
	pSetCursorPos   = user32.NewProc("SetCursorPos")
	pMouseEvent     = user32.NewProc("mouse_event")
)

// cdpRightClick right-clicks at page coordinates through the DevTools
// protocol, which goes through the same input path as a real mouse.
func cdpRightClick(w *Window, x, y int) {
	v := w.pv
	w.app.callMain(func() {
		for _, typ := range []string{"mousePressed", "mouseReleased"} {
			b, _ := json.Marshal(map[string]any{"type": typ, "x": x, "y": y, "button": "right", "buttons": 2, "clickCount": 1})
			vcall(v.webview, wvCallDevToolsProtocol, uintptr(unsafe.Pointer(wstr("Input.dispatchMouseEvent"))), uintptr(unsafe.Pointer(wstr(string(b)))), 0)
		}
	})
}

// osRightClick moves the real cursor over the window and right-clicks.
func osRightClick(w *Window, x, y int) {
	scale := float64(windowDPI(w.hwnd)) / 96
	pt := point{int32(float64(x) * scale), int32(float64(y) * scale)}
	_, _, _ = pClientToScreen.Call(w.hwnd, uintptr(unsafe.Pointer(&pt)))
	_, _, _ = pSetCursorPos.Call(uintptr(pt.x), uintptr(pt.y))
	time.Sleep(200 * time.Millisecond)
	const rightDown, rightUp = 0x0008, 0x0010
	_, _, _ = pMouseEvent.Call(rightDown, 0, 0, 0, 0)
	_, _, _ = pMouseEvent.Call(rightUp, 0, 0, 0, 0)
}

func TestGUIContextMenu(t *testing.T) {
	needGUI(t)
	a := guiApp
	w, err := a.NewWindow(WindowConfig{Title: "ctx", Width: 400, Height: 300, Resizable: true, Path: "/?w=ctx"})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	waitReport(t, "ctx")
	w.Eval(`document.body.innerHTML = '<a id=l href="https://example.com/x" style="position:absolute;left:10px;top:10px;width:150px;height:30px;display:block">link</a><p id=s style="position:absolute;left:10px;top:100px;margin:0">select me</p>'; var r = document.createRange(); r.selectNodeContents(document.getElementById('s')); getSelection().removeAllRanges(); getSelection().addRange(r);`)
	time.Sleep(500 * time.Millisecond)

	infos := make(chan caps.ContextMenuInfo, 4)
	a.OnContextMenu(func(i caps.ContextMenuInfo) { infos <- i })
	defer a.OnContextMenu(nil)
	if err := a.SetContextMenu(w.Ref(), 0, caps.ContextMenuPolicy{Mode: caps.ContextMenuCustom}); err != nil {
		t.Fatal(err)
	}

	var info caps.ContextMenuInfo
	got := false
	for _, click := range []func(){
		func() { cdpRightClick(w, 40, 25) },
		func() { osRightClick(w, 40, 25) },
	} {
		click()
		select {
		case info = <-infos:
			got = true
		case <-time.After(8 * time.Second):
		}
		if got {
			break
		}
	}
	if !got {
		t.Fatal("no context menu event from a DevTools right click or from a real one")
	}
	if !info.Has("link") || info.LinkURL != "https://example.com/x" || info.Window != w.Ref() || info.View != w.PrimaryView() {
		t.Fatalf("info %+v", info)
	}
	if info.X < 30 || info.X > 50 || info.Y < 15 || info.Y > 35 {
		t.Fatalf("position %d,%d, want about 40,25", info.X, info.Y)
	}

	// Selected text comes with the click.
	cdpRightClick(w, 20, 105)
	select {
	case info = <-infos:
		if !info.Has("selection") || info.SelectionText != "select me" {
			t.Fatalf("selection: %+v", info)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("no event for the selection click")
	}

	// None mode reports nothing.
	if err := a.SetContextMenu(w.Ref(), 0, caps.ContextMenuPolicy{Mode: caps.ContextMenuNone}); err != nil {
		t.Fatal(err)
	}
	cdpRightClick(w, 40, 25)
	select {
	case i := <-infos:
		t.Fatalf("none mode reported %+v", i)
	case <-time.After(1500 * time.Millisecond):
	}
}
