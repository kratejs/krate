//go:build darwin

package darwin

import (
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// The context menu hook is exercised by calling the willOpenMenu:withEvent:
// path with a synthesized right-mouse event and a menu holding a stand-in for
// WebKit's items, because a real right click would open a menu that blocks
// until a human dismisses it.

func rightMouseEvent(win objc.ID, x, y float64) objc.ID {
	// NSEventTypeRightMouseDown is 3; the location is in window coordinates.
	return cls("NSEvent").Send(sel("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
		3, nsPoint{X: x, Y: y}, uint(0), float64(0), objc.Send[int](win, sel("windowNumber")), objc.ID(0), 0, 1, float32(1))
}

func fakeEngineMenu() objc.ID {
	m := newMenu("")
	m.Send(sel("addItem:"), menuItem("Copy", 0, "", 0))
	m.Send(sel("addItem:"), menuItem("Reload", 0, "", 0))
	return m
}

func menuTitles(m objc.ID) (out []string) {
	n := objc.Send[int](m, sel("numberOfItems"))
	for i := 0; i < n; i++ {
		out = append(out, goString(m.Send(sel("itemAtIndex:"), i).Send(sel("title"))))
	}
	return
}

func TestGUIContextMenu(t *testing.T) {
	requireGUI(t)
	a := guiApp
	w, err := a.NewWindow(WindowOptions{Title: "ctx", Width: 400, Height: 300, Resizable: true, Path: "/?w=ctx"})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	waitReport(t)
	w.Eval(`document.body.innerHTML = '<a id=l href="https://example.com/x" style="position:absolute;left:10px;top:10px;width:150px;height:30px;display:block">link</a>'`)
	time.Sleep(500 * time.Millisecond)

	infos := make(chan caps.ContextMenuInfo, 2)
	a.OnContextMenu(func(i caps.ContextMenuInfo) { infos <- i })
	defer a.OnContextMenu(nil)

	var web, nsWin objc.ID
	var contentH float64
	runOnMain(func() {
		web, nsWin = w.web, w.win
		contentH = objc.Send[nsRect](web, sel("bounds")).Size.Height
	})
	open := func() (titles []string) {
		runOnMain(func() {
			withPool(func() {
				m := fakeEngineMenu()
				a.webViewWillOpenMenu(web, m, rightMouseEvent(nsWin, 40, contentH-25))
				titles = menuTitles(m)
			})
		})
		return
	}

	set := func(p caps.ContextMenuPolicy) {
		t.Helper()
		if err := a.SetContextMenu(w.Ref(), 0, p); err != nil {
			t.Fatal(err)
		}
	}

	set(caps.ContextMenuPolicy{Mode: caps.ContextMenuDefault})
	if got := open(); len(got) != 2 {
		t.Fatalf("default mode changed the menu: %q", got)
	}

	set(caps.ContextMenuPolicy{Mode: caps.ContextMenuNone})
	if got := open(); len(got) != 0 {
		t.Fatalf("none mode left items: %q", got)
	}

	set(caps.ContextMenuPolicy{Mode: caps.ContextMenuExtend, Items: []caps.MenuItem{{ID: "x", Label: "Extra"}, {ID: "y", Label: "More", Kind: caps.MenuItemSubmenu, Submenu: []caps.MenuItem{{ID: "z", Label: "Sub"}}}}})
	got := open()
	if len(got) != 5 || got[0] != "Copy" || got[3] != "Extra" || got[4] != "More" {
		t.Fatalf("extend mode: %q", got)
	}

	set(caps.ContextMenuPolicy{Mode: caps.ContextMenuCustom})
	if got := open(); len(got) != 0 {
		t.Fatalf("custom mode left items: %q", got)
	}
	select {
	case i := <-infos:
		if !i.Has("link") || i.LinkURL != "https://example.com/x" || i.Window != w.Ref() || i.View != w.PrimaryView() {
			t.Fatalf("info %+v", i)
		}
		if i.X < 30 || i.X > 50 || i.Y < 15 || i.Y > 35 {
			t.Fatalf("position %d,%d, want about 40,25", i.X, i.Y)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no context menu event")
	}
}
