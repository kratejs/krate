//go:build darwin

package darwin

import (
	"fmt"
	"image/color"
	"sync"
	"testing"

	"github.com/ebitengine/purego/objc"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// GUI tests for the tray, global shortcuts and notifications. They run under
// KRATE_NATIVE_GUI=1 like gui_test.go and need a logged-in session (a status
// bar and a window server), which GitHub's macOS runners have. What needs a
// human (clicking the real status item, pressing the hotkey, seeing the
// banner) is listed in MANUAL_TEST.md.

// menuInfo is the readable state of one NSMenuItem.
type menuInfo struct {
	Title     string
	Enabled   bool
	State     int
	Separator bool
	Sub       []menuInfo
}

// readMenu snapshots an NSMenu (main thread).
func readMenu(m objc.ID) []menuInfo {
	var out []menuInfo
	n := objc.Send[int](m, sel("numberOfItems"))
	for i := 0; i < n; i++ {
		it := m.Send(sel("itemAtIndex:"), i)
		mi := menuInfo{
			Title:     goString(it.Send(sel("title"))),
			Enabled:   objc.Send[bool](it, sel("isEnabled")),
			State:     objc.Send[int](it, sel("state")),
			Separator: objc.Send[bool](it, sel("isSeparatorItem")),
		}
		if mi.Separator {
			mi.Enabled = true // AppKit's answer for separators is not specified
		}
		if sub := it.Send(sel("submenu")); sub != 0 {
			mi.Sub = readMenu(sub)
		}
		out = append(out, mi)
	}
	return out
}

type trayView struct {
	Present  bool
	Title    string
	Tooltip  string
	HasImage bool
	Template bool
	ImageW   float64
	Menu     []menuInfo
}

func readTray() (v trayView) {
	runOnMain(func() {
		withPool(func() {
			if trayState.item == 0 {
				return
			}
			v.Present = true
			b := trayState.item.Send(sel("button"))
			v.Title = goString(b.Send(sel("title")))
			v.Tooltip = goString(b.Send(sel("toolTip")))
			if img := b.Send(sel("image")); img != 0 {
				v.HasImage = true
				v.Template = objc.Send[bool](img, sel("isTemplate"))
				v.ImageW = objc.Send[nsSize](img, sel("size")).Width
			}
			if trayState.menu != 0 {
				v.Menu = readMenu(trayState.menu)
			}
		})
	})
	return
}

type trayRecorder struct {
	mu  sync.Mutex
	evs []caps.TrayEvent
}

func (r *trayRecorder) add(e caps.TrayEvent) { r.mu.Lock(); r.evs = append(r.evs, e); r.mu.Unlock() }
func (r *trayRecorder) take() []caps.TrayEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.evs
	r.evs = nil
	return out
}

func blackIcon(t *testing.T, size int) []byte {
	return encodePNG(t, filled(size, size, color.NRGBA{0, 0, 0, 255}))
}

func trayMenu() []caps.MenuItem {
	return []caps.MenuItem{
		{ID: "open", Label: "&Open", Accelerator: "CmdOrCtrl+O"},
		{ID: "auto", Label: "Autosave", Kind: caps.MenuItemCheck, Checked: true},
		{Kind: caps.MenuItemSeparator},
		{ID: "r1", Label: "Mode A", Kind: caps.MenuItemRadio, Checked: true},
		{ID: "r2", Label: "Mode B", Kind: caps.MenuItemRadio},
		{Label: "More", Kind: caps.MenuItemSubmenu, Submenu: []caps.MenuItem{
			{ID: "deep", Label: "Deep item"},
			{ID: "off", Label: "Disabled item", Disabled: true},
		}},
	}
}

func performItem(m objc.ID, i int) {
	runOnMain(func() { withPool(func() { m.Send(sel("performActionForItemAtIndex:"), i) }) })
}

func TestTrayItemAndMenu(t *testing.T) {
	requireGUI(t)
	var rec trayRecorder
	guiApp.OnTray(rec.add)
	t.Cleanup(func() { _ = guiApp.SetTray(nil) })

	if err := guiApp.SetTray(&caps.TrayIcon{Tooltip: "Krate test", Title: "KT", PNG: blackIcon(t, 36), Menu: trayMenu()}); err != nil {
		t.Fatal(err)
	}
	v := readTray()
	if !v.Present || v.Title != "KT" || v.Tooltip != "Krate test" || !v.HasImage {
		t.Fatalf("tray = %+v", v)
	}
	if !v.Template {
		t.Error("a black icon should be installed as a template image")
	}
	if v.ImageW != 18 {
		t.Errorf("36 px icon should be 18 pt wide, got %v", v.ImageW)
	}
	want := []menuInfo{
		{Title: "Open", Enabled: true},
		{Title: "Autosave", Enabled: true, State: nsControlStateOn},
		{Separator: true, Enabled: true},
		{Title: "Mode A", Enabled: true, State: nsControlStateOn},
		{Title: "Mode B", Enabled: true},
		{Title: "More", Enabled: true, Sub: []menuInfo{
			{Title: "Deep item", Enabled: true},
			{Title: "Disabled item", Enabled: false},
		}},
	}
	if fmt.Sprintf("%+v", v.Menu) != fmt.Sprintf("%+v", want) {
		t.Errorf("menu:\n got %+v\nwant %+v", v.Menu, want)
	}

	// Drive the real menu actions: item events, check toggling, radio group.
	menu := trayState.menu
	performItem(menu, 0)
	performItem(menu, 1)
	performItem(menu, 4)
	evs := rec.take()
	wantEvs := []caps.TrayEvent{{Kind: "menu", ID: "open"}, {Kind: "menu", ID: "auto"}, {Kind: "menu", ID: "r2"}}
	if fmt.Sprint(evs) != fmt.Sprint(wantEvs) {
		t.Fatalf("events = %+v, want %+v", evs, wantEvs)
	}
	v = readTray()
	if v.Menu[1].State != nsControlStateOff {
		t.Error("check item should have toggled off")
	}
	if v.Menu[3].State != nsControlStateOff || v.Menu[4].State != nsControlStateOn {
		t.Errorf("radio group: A=%d B=%d", v.Menu[3].State, v.Menu[4].State)
	}

	// An update replaces the menu and keeps one item.
	if err := guiApp.SetTray(&caps.TrayIcon{Tooltip: "second", PNG: encodePNG(t, filled(16, 16, color.NRGBA{200, 30, 30, 255})),
		Menu: []caps.MenuItem{{ID: "only", Label: "Only"}}}); err != nil {
		t.Fatal(err)
	}
	v = readTray()
	if v.Tooltip != "second" || v.Title != "" || v.Template || len(v.Menu) != 1 {
		t.Errorf("after update: %+v", v)
	}
	rec.take()
	performItem(trayState.menu, 0)
	if evs := rec.take(); len(evs) != 1 || evs[0].ID != "only" {
		t.Errorf("events after update = %+v", evs)
	}

	// Without a menu the button action reports a click.
	if err := guiApp.SetTray(&caps.TrayIcon{Title: "no menu"}); err != nil {
		t.Fatal(err)
	}
	rec.take()
	runOnMain(func() { withPool(func() { trayState.item.Send(sel("button")).Send(sel("performClick:"), objc.ID(0)) }) })
	if evs := rec.take(); len(evs) != 1 || evs[0].Kind != "click" {
		t.Errorf("click events = %+v", evs)
	}

	if err := guiApp.SetTray(nil); err != nil {
		t.Fatal(err)
	}
	if readTray().Present {
		t.Error("SetTray(nil) left the item")
	}
	if err := guiApp.SetTray(nil); err != nil {
		t.Errorf("removing twice: %v", err)
	}
}
