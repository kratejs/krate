// Package tray puts an icon in the system tray (notification area).
//
// Linux: the StatusNotifierItem protocol with a com.canonical.dbusmenu menu,
// implemented in pure Go over the session bus. It needs a StatusNotifier host
// (KDE Plasma, most panels, GNOME with the AppIndicator extension); without
// one New returns krate.ErrUnsupported. One tray icon per app.
//
// Windows: Shell_NotifyIconW (version 4) owned by a hidden window, with the
// menu shown on right click. The icon returns when explorer restarts. The
// Windows backend is compile-only in this release.
package tray

import (
	"context"

	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/sdk/menu"
	"github.com/kratejs/krate/packages/native/wire"
)

// Permission is the app permission the UI needs to use the tray.
const Permission = "tray"

// Host function ids.
const (
	IDSet    = "krate/tray.set"
	IDRemove = "krate/tray.remove"
	IDEvents = "krate/tray.events"
)

// Options describes the tray icon.
type Options struct {
	Tooltip string
	Title   string
	// Icon is a PNG image.
	Icon []byte
	// Menu is shown on right click (and left click where the host has no
	// separate activate action). Item clicks arrive as menu events, like any
	// other menu item (menu.OnClick), and as tray events of kind "menu".
	Menu *menu.Menu
}

// Event is a tray interaction.
type Event struct {
	// Kind is "click", "middle-click", "right-click" or "menu".
	Kind string
	// ID is the menu item id when Kind is "menu".
	ID string
}

// Tray is the app's tray icon.
type Tray struct{ o Options }

// New shows the tray icon.
func New(o Options) (*Tray, error) {
	t := &Tray{o: o}
	return t, t.apply()
}

func (t *Tray) apply() error {
	return bridge.CallDo(context.Background(), IDSet, func(e *wire.Encoder) { encodeOptions(e, t.o) })
}

// SetTooltip changes the tooltip.
func (t *Tray) SetTooltip(s string) error { t.o.Tooltip = s; return t.apply() }

// SetIcon changes the icon (PNG).
func (t *Tray) SetIcon(png []byte) error { t.o.Icon = png; return t.apply() }

// SetMenu changes the menu.
func (t *Tray) SetMenu(m *menu.Menu) error { t.o.Menu = m; return t.apply() }

// Remove takes the icon out of the tray.
func (t *Tray) Remove() error { return Remove() }

// OnEvent calls fn for clicks and menu selections until cancel is called.
func (t *Tray) OnEvent(fn func(Event)) (cancel func(), err error) { return OnEvent(fn) }

// Remove removes the app's tray icon.
func Remove() error { return bridge.CallDo(context.Background(), IDRemove, nil) }

// OnEvent calls fn for tray events until cancel is called.
func OnEvent(fn func(Event)) (cancel func(), err error) {
	return bridge.Subscribe(context.Background(), IDEvents, func(d *wire.Decoder) {
		if ev, err := decodeEvent(d); err == nil {
			fn(ev)
		}
	})
}

func encodeOptions(e *wire.Encoder, o Options) {
	e.BeginObject()
	e.Key("tooltip")
	e.String(o.Tooltip)
	e.Key("title")
	e.String(o.Title)
	e.Key("icon")
	e.Bytes(o.Icon)
	e.Key("menu")
	if o.Menu == nil {
		e.BeginArray()
		e.EndArray()
	} else {
		menu.EncodeItems(e, o.Menu.Items)
	}
	e.EndObject()
}

func decodeOptions(r *codec.R) (o Options, items []menu.Item) {
	r.Object(func(k string) bool {
		switch k {
		case "tooltip":
			o.Tooltip = r.String()
		case "title":
			o.Title = r.String()
		case "icon":
			o.Icon = r.Bytes()
		case "menu":
			items = menu.DecodeItems(r)
		default:
			return false
		}
		return true
	})
	return
}

func encodeEvent(e *wire.Encoder, ev Event) {
	e.BeginObject()
	e.Key("kind")
	e.String(ev.Kind)
	e.Key("id")
	e.String(ev.ID)
	e.EndObject()
}

func decodeEvent(d *wire.Decoder) (ev Event, err error) {
	r := &codec.R{D: d, Name: "tray event"}
	r.Object(func(k string) bool {
		switch k {
		case "kind":
			ev.Kind = r.String()
		case "id":
			ev.ID = r.String()
		default:
			return false
		}
		return true
	})
	return ev, r.Err()
}
