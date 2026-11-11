// Package menu builds the application menu bar and context menus.
//
// Menu items are plain data with string ids. A click is delivered as an event
// (OnClick) to Go host code, and as the host stream "krate/menu.events" to the
// UI. On Linux the application menu is a GTK menu bar at the top of every
// window; on macOS it is the global menu bar.
package menu

import (
	"context"

	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// Permission is the app permission the UI needs to set menus.
const Permission = "menu"

// Host function ids.
const (
	IDSetApp = "krate/menu.SetApp"
	IDPopup  = "krate/menu.Popup"
	IDEvents = "krate/menu.events"
)

// Kind is the kind of a menu item.
type Kind int

// Item kinds.
const (
	KindNormal Kind = iota
	KindCheck
	KindRadio
	KindSeparator
	KindSubmenu
)

// Item is one menu entry.
type Item struct {
	// ID identifies the item in click events.
	ID    string
	Label string // "&File" marks the mnemonic
	Kind  Kind
	// Accelerator such as "CmdOrCtrl+Shift+S"; shown in the menu and active
	// while the window has focus.
	Accelerator string
	Disabled    bool
	Checked     bool
	Submenu     []Item
}

// Menu is a list of items.
type Menu struct{ Items []Item }

// New returns a Menu of items.
func New(items ...Item) *Menu { return &Menu{Items: items} }

// Action returns a normal item.
func Action(id, label, accelerator string) Item {
	return Item{ID: id, Label: label, Accelerator: accelerator}
}

// Check returns a checkbox item.
func Check(id, label string, checked bool) Item {
	return Item{ID: id, Label: label, Kind: KindCheck, Checked: checked}
}

// Separator returns a separator line.
func Separator() Item { return Item{Kind: KindSeparator} }

// Sub returns a submenu.
func Sub(label string, items ...Item) Item {
	return Item{Label: label, Kind: KindSubmenu, Submenu: items}
}

// Event is a menu click.
type Event struct {
	ID string
	// Window is the SDK id of the window whose menu was used.
	Window string
	// Checked is the new state of a check or radio item.
	Checked bool
}

// SetApp installs m as the menu bar of every window. A nil menu removes it.
func SetApp(m *Menu) error {
	var items []Item
	if m != nil {
		items = m.Items
	}
	return bridge.CallDo(context.Background(), IDSetApp, func(e *wire.Encoder) { encodeItems(e, items) })
}

// Popup shows m as a context menu in a window ("" = the main window) at
// window coordinates x, y; negative coordinates use the pointer position
// (Linux always uses the pointer).
func Popup(m *Menu, window string, x, y int) error {
	return bridge.CallDo(context.Background(), IDPopup, func(e *wire.Encoder) {
		e.String(window)
		e.Int(int64(x))
		e.Int(int64(y))
		encodeItems(e, m.Items)
	})
}

// OnClick calls fn for every click on any item until cancel is called.
func OnClick(fn func(Event)) (cancel func(), err error) {
	return bridge.Subscribe(context.Background(), IDEvents, func(d *wire.Decoder) {
		if ev, err := decodeEvent(d); err == nil {
			fn(ev)
		}
	})
}

// OnItem calls fn when the item with this id is clicked.
func OnItem(id string, fn func(Event)) (cancel func(), err error) {
	return OnClick(func(ev Event) {
		if ev.ID == id {
			fn(ev)
		}
	})
}

func encodeItems(e *wire.Encoder, items []Item) {
	e.BeginArray()
	for _, it := range items {
		e.BeginObject()
		e.Key("id")
		e.String(it.ID)
		e.Key("label")
		e.String(it.Label)
		e.Key("kind")
		e.Int(int64(it.Kind))
		e.Key("accelerator")
		e.String(it.Accelerator)
		e.Key("disabled")
		e.Bool(it.Disabled)
		e.Key("checked")
		e.Bool(it.Checked)
		e.Key("submenu")
		encodeItems(e, it.Submenu)
		e.EndObject()
	}
	e.EndArray()
}

const maxDepth = 16

func decodeItems(r *codec.R, depth int) []Item {
	if depth > maxDepth {
		r.Failf("menu is nested deeper than %d levels", maxDepth)
		return nil
	}
	var out []Item
	r.Array(func() {
		var it Item
		r.Object(func(k string) bool {
			switch k {
			case "id":
				it.ID = r.String()
			case "label":
				it.Label = r.String()
			case "kind":
				it.Kind = Kind(r.Int())
			case "accelerator":
				it.Accelerator = r.String()
			case "disabled":
				it.Disabled = r.Bool()
			case "checked":
				it.Checked = r.Bool()
			case "submenu":
				it.Submenu = decodeItems(r, depth+1)
			default:
				return false
			}
			return true
		})
		out = append(out, it)
	})
	return out
}

func encodeEvent(e *wire.Encoder, ev Event) {
	e.BeginObject()
	e.Key("id")
	e.String(ev.ID)
	e.Key("window")
	e.String(ev.Window)
	e.Key("checked")
	e.Bool(ev.Checked)
	e.EndObject()
}

func decodeEvent(d *wire.Decoder) (ev Event, err error) {
	r := &codec.R{D: d, Name: "menu event"}
	r.Object(func(k string) bool {
		switch k {
		case "id":
			ev.ID = r.String()
		case "window":
			ev.Window = r.String()
		case "checked":
			ev.Checked = r.Bool()
		default:
			return false
		}
		return true
	})
	return ev, r.Err()
}

// EncodeItems and DecodeItems expose the item codec to the other SDK
// packages (the tray menu uses it).

// EncodeItems writes items as a wire array.
func EncodeItems(e *wire.Encoder, items []Item) { encodeItems(e, items) }

// DecodeItems reads an array written by EncodeItems.
func DecodeItems(r *codec.R) []Item { return decodeItems(r, 0) }

// ToCaps converts items to the platform representation.
func ToCaps(items []Item) []caps.MenuItem { return toCaps(items) }
