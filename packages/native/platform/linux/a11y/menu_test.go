//go:build linux

package a11y

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/internal/atspi"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/platform/linux"
)

func fileMenu() []caps.MenuItem {
	return []caps.MenuItem{
		{Label: "&File", Kind: caps.MenuItemSubmenu, Submenu: []caps.MenuItem{
			{ID: "new", Label: "&New", Accelerator: "CmdOrCtrl+N"},
			{ID: "save", Label: "&Save", Accelerator: "CmdOrCtrl+Shift+S", Disabled: true},
			{Kind: caps.MenuItemSeparator},
			{ID: "wrap", Label: "&Word wrap", Kind: caps.MenuItemCheck, Checked: true},
			{ID: "dark", Label: "&Dark", Kind: caps.MenuItemRadio, Checked: true},
			{ID: "light", Label: "&Light", Kind: caps.MenuItemRadio},
		}},
		{Label: "&Help", Kind: caps.MenuItemSubmenu, Submenu: []caps.MenuItem{{ID: "about", Label: "&About"}}},
	}
}

func named(role int, name string) func(atspi.Node) bool {
	return func(n atspi.Node) bool { return n.Role() == role && n.Name() == name }
}

func TestAppMenuTree(t *testing.T) {
	need(t)
	w := open(t, linux.WindowOptions{Title: "Menu tree"})
	if err := app.SetAppMenu(fileMenu()); err != nil {
		t.Fatal(err)
	}
	_ = w
	fr := waitTopLevel(t, "Menu tree")
	var bar atspi.Node
	waitFor(t, "the menu bar", func() bool {
		fr, _ = topLevel("Menu tree")
		var ok bool
		bar, ok = fr.Find(func(n atspi.Node) bool { return n.Role() == atspi.RoleMenuBar })
		return ok
	})
	// Mnemonic markers are not part of the names a screen reader speaks.
	for _, m := range []string{"File", "Help"} {
		if _, ok := bar.Find(named(atspi.RoleMenu, m)); !ok {
			t.Errorf("menu %q missing from the menu bar:\n%s", m, bar.Dump())
		}
	}
	for _, c := range []struct {
		role int
		name string
	}{
		{atspi.RoleMenuItem, "New"}, {atspi.RoleMenuItem, "Save"}, {atspi.RoleCheckMenuItem, "Word wrap"},
		{atspi.RoleRadioMenuItem, "Dark"}, {atspi.RoleRadioMenuItem, "Light"}, {atspi.RoleMenuItem, "About"},
	} {
		if _, ok := bar.Find(named(c.role, c.name)); !ok {
			t.Errorf("no %d item named %q:\n%s", c.role, c.name, bar.Dump())
		}
	}
	if _, ok := bar.Find(func(n atspi.Node) bool { return n.Role() == atspi.RoleSeparator }); !ok {
		t.Error("the separator is missing")
	}
	// State: disabled, checked.
	save, _ := bar.Find(named(atspi.RoleMenuItem, "Save"))
	if save.HasState(atspi.StateEnabled) || save.HasState(atspi.StateSensitive) {
		t.Errorf("disabled item Save has states %v", save.States())
	}
	nw, _ := bar.Find(named(atspi.RoleMenuItem, "New"))
	if !nw.HasState(atspi.StateEnabled) {
		t.Errorf("New is not enabled: %v", nw.States())
	}
	wrap, _ := bar.Find(named(atspi.RoleCheckMenuItem, "Word wrap"))
	if !wrap.HasState(atspi.StateChecked) {
		t.Errorf("Word wrap is not checked: %v", wrap.States())
	}
	dark, _ := bar.Find(named(atspi.RoleRadioMenuItem, "Dark"))
	light, _ := bar.Find(named(atspi.RoleRadioMenuItem, "Light"))
	if !dark.HasState(atspi.StateChecked) || light.HasState(atspi.StateChecked) {
		t.Errorf("radio states: Dark %v, Light %v", dark.States(), light.States())
	}
	// Accelerators and mnemonics are announced through the Action key binding.
	acts := nw.Actions()
	// GTK writes "mnemonic;path mnemonic;accelerator": n;<Alt>f:n;<Primary>n.
	if len(acts) == 0 || !strings.Contains(acts[0].KeyBinding, "<Primary>n") || !strings.HasPrefix(acts[0].KeyBinding, "n;") {
		t.Errorf("New key binding %+v lacks the n mnemonic or the Ctrl+N accelerator", acts)
	}
}

func TestAppMenuKeyboardAccess(t *testing.T) {
	needWM(t)
	var mu sync.Mutex
	var got []string
	app.OnMenu(func(e caps.MenuEvent) { mu.Lock(); got = append(got, e.ID); mu.Unlock() })
	w := open(t, linux.WindowOptions{Title: "Menu keys"})
	if err := app.SetAppMenu(fileMenu()); err != nil {
		t.Fatal(err)
	}
	focusWindow(t, w, "Menu keys")
	waitFor(t, "the page", func() bool { fr, _ := topLevel("Menu keys"); return len(documents(fr)) > 0 })

	state := func(name string) (atspi.Node, bool) {
		fr, _ := topLevel("Menu keys")
		return fr.Find(func(n atspi.Node) bool {
			return (n.Role() == atspi.RoleMenu || n.Role() == atspi.RoleMenuItem) && n.Name() == name
		})
	}
	// F10, with the focus in the page, moves into the menu bar.
	if err := client.KeySym("F10"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "File to be selected after F10", func() bool {
		n, ok := state("File")
		return ok && n.HasState(atspi.StateSelected)
	})
	// Down opens it onto the first item; Down again moves on; Escape closes.
	_ = client.KeySym("Down")
	waitFor(t, "New to show", func() bool { n, ok := state("New"); return ok && n.HasState(atspi.StateShowing) })
	_ = client.KeySym("Escape")
	_ = client.KeySym("Escape")
	waitFor(t, "the menu to close", func() bool { n, ok := state("New"); return ok && !n.HasState(atspi.StateShowing) })

	// The mnemonic: Alt+F opens File, N activates New.
	if err := client.KeyCombo([]int32{atspi.KeycodeAlt}, "f"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "New to show after Alt+F", func() bool { n, ok := state("New"); return ok && n.HasState(atspi.StateShowing) })
	_ = client.KeySym("n")
	waitFor(t, "the New menu event", func() bool { mu.Lock(); defer mu.Unlock(); return len(got) > 0 })
	mu.Lock()
	if got[0] != "new" {
		t.Errorf("mnemonic activated %q, want new", got[0])
	}
	mu.Unlock()
	// The accelerator works with the focus in the page too.
	mu.Lock()
	got = nil
	mu.Unlock()
	time.Sleep(300 * time.Millisecond)
	if err := client.KeyCombo([]int32{atspi.KeycodeCtrl}, "n"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the accelerator event", func() bool { mu.Lock(); defer mu.Unlock(); return len(got) > 0 })
}

func TestContextMenuTreeAndEscape(t *testing.T) {
	needWM(t)
	w := open(t, linux.WindowOptions{Title: "Context menu"})
	focusWindow(t, w, "Context menu")
	items := []caps.MenuItem{
		{ID: "a", Label: "Alpha", Accelerator: "CmdOrCtrl+A"},
		{Kind: caps.MenuItemSeparator},
		{ID: "b", Label: "Beta", Kind: caps.MenuItemCheck, Checked: true},
		{ID: "c", Label: "Gamma", Disabled: true},
	}
	if err := app.PopupMenu(w.Ref(), items, 20, 20); err != nil {
		t.Fatal(err)
	}
	find := func(role int, name string) (atspi.Node, bool) {
		a, _ := appNode()
		return a.Find(named(role, name))
	}
	waitFor(t, "the context menu items", func() bool { _, ok := find(atspi.RoleMenuItem, "Alpha"); return ok })
	if n, ok := find(atspi.RoleCheckMenuItem, "Beta"); !ok || !n.HasState(atspi.StateChecked) {
		t.Errorf("Beta missing or not checked (%v)", ok)
	}
	if n, ok := find(atspi.RoleMenuItem, "Gamma"); !ok || n.HasState(atspi.StateEnabled) {
		t.Errorf("Gamma missing or enabled (%v)", ok)
	}
	// A popup menu is not a keyboard trap: Escape closes it.
	if err := client.KeySym("Escape"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the popup to close", func() bool {
		n, ok := find(atspi.RoleMenuItem, "Alpha")
		return !ok || !n.HasState(atspi.StateShowing)
	})
}
