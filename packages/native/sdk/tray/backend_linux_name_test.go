//go:build linux

package tray

import (
	"testing"

	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/dbusiface"
)

func TestItemBusName(t *testing.T) {
	if got := itemBusName("", 42); got != "org.kde.StatusNotifierItem-42-1" {
		t.Errorf("direct = %s", got)
	}
	if got := itemBusName("com.acme.App", 2); got != "com.acme.App.StatusNotifierItem" {
		t.Errorf("flatpak = %s", got)
	}
}

// A tray icon is named for assistive technology by the StatusNotifierItem
// Title and ToolTip, and its menu by the dbusmenu label properties.

func propString(t *testing.T, props map[string]dbusiface.Variant, key string) string {
	t.Helper()
	s, _ := props[key].Value.(string)
	return s
}

func TestItemPropsAlwaysNameTheIcon(t *testing.T) {
	t.Setenv("KRATE_APP_NAME", "Krate Notes")
	for _, c := range []struct {
		name         string
		icon         caps.TrayIcon
		wantT, wantP string
	}{
		{"both", caps.TrayIcon{Title: "Notes", Tooltip: "2 unsynced"}, "Notes", "2 unsynced"},
		{"tooltip only", caps.TrayIcon{Tooltip: "2 unsynced"}, "2 unsynced", "2 unsynced"},
		{"icon only", caps.TrayIcon{}, "Krate Notes", "Krate Notes"},
	} {
		s := &sni{icon: c.icon}
		props := s.itemProps()
		if got := propString(t, props, "Title"); got != c.wantT {
			t.Errorf("%s: Title = %q, want %q", c.name, got, c.wantT)
		}
		tt, _ := props["ToolTip"].Value.(tooltip)
		if tt.Title != c.wantP {
			t.Errorf("%s: ToolTip title = %q, want %q", c.name, tt.Title, c.wantP)
		}
	}
}

func TestMenuItemsCarryLabelsStatesAndShortcuts(t *testing.T) {
	s := &sni{icon: caps.TrayIcon{Menu: []caps.MenuItem{
		{ID: "open", Label: "&Open", Accelerator: "CmdOrCtrl+O"},
		{Kind: caps.MenuItemSeparator},
		{ID: "mute", Label: "Mute", Kind: caps.MenuItemCheck, Checked: true},
		{ID: "off", Label: "Quit", Disabled: true},
		{Label: "More", Kind: caps.MenuItemSubmenu, Submenu: []caps.MenuItem{{ID: "deep", Label: "Deeper"}}},
	}}}
	s.mu.Lock()
	s.buildMenuLocked()
	s.mu.Unlock()
	byLabel := map[string]map[string]dbusiface.Variant{}
	seps := 0
	for _, it := range s.flat {
		if propString(t, it.props, "type") == "separator" {
			seps++
			continue
		}
		byLabel[propString(t, it.props, "label")] = it.props
	}
	if seps != 1 {
		t.Errorf("%d separators, want 1", seps)
	}
	for _, l := range []string{"_Open", "Mute", "Quit", "More", "Deeper"} {
		if byLabel[l] == nil {
			t.Fatalf("no menu item labelled %q (have %v)", l, byLabel)
		}
	}
	if en, _ := byLabel["Quit"]["enabled"].Value.(bool); en {
		t.Error("a disabled item is announced as enabled")
	}
	if st, _ := byLabel["Mute"]["toggle-state"].Value.(int32); st != 1 {
		t.Errorf("checked item toggle-state = %v", byLabel["Mute"]["toggle-state"].Value)
	}
	if ty := propString(t, byLabel["Mute"], "toggle-type"); ty != "checkmark" {
		t.Errorf("check item toggle-type = %q", ty)
	}
	if byLabel["_Open"]["shortcut"].Value == nil {
		t.Error("the accelerator is not announced as a shortcut")
	}
	if cd := propString(t, byLabel["More"], "children-display"); cd != "submenu" {
		t.Errorf("submenu children-display = %q", cd)
	}
}
