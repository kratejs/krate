package tray

import (
	"sync"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/sdk/menu"
	"github.com/kratejs/krate/packages/native/wire"
)

// backend shows one tray icon.
type backend interface {
	set(icon *caps.TrayIcon) error // nil removes
}

// Register adds the platform-side operations to t.
func Register(t *bridge.Table, env *bridge.Env) {
	topic := env.Topic("tray")
	t.Stream(IDEvents, Permission, true, topic.Stream())

	menuTopic := env.Topic("menu")
	publish := func(ev caps.TrayEvent) {
		topic.Publish(func(e *wire.Encoder) { encodeEvent(e, Event{Kind: ev.Kind, ID: ev.ID}) })
		if ev.Kind == "menu" {
			// Tray menu items are ordinary menu items for menu.OnClick.
			menuTopic.Publish(func(e *wire.Encoder) {
				e.BeginObject()
				e.Key("id")
				e.String(ev.ID)
				e.Key("window")
				e.String("")
				e.Key("checked")
				e.Bool(false)
				e.EndObject()
			})
		}
	}
	var mu sync.Mutex
	var be backend
	get := func() (backend, error) {
		mu.Lock()
		defer mu.Unlock()
		if be != nil {
			if c, ok := be.(interface{ current() bool }); !ok || c.current() {
				return be, nil
			}
			// The session bus connection died or moved; start over.
			if r, ok := be.(interface{ release() }); ok {
				r.release()
			}
			be = nil
		}
		if p, ok := env.App.(caps.Tray); ok {
			p.OnTray(publish)
			be = platformBackend{p}
			return be, nil
		}
		b, err := newDefaultBackend(env.App, publish)
		if err != nil {
			return nil, err
		}
		be = b
		return be, nil
	}

	t.Func(IDSet, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSet)
		o, items := decodeOptions(r)
		if err := r.Done(); err != nil {
			return err
		}
		b, err := get()
		if err != nil {
			return err
		}
		out.Null()
		return b.set(&caps.TrayIcon{Tooltip: o.Tooltip, Title: o.Title, PNG: o.Icon, Menu: menu.ToCaps(items)})
	})
	t.Func(IDRemove, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDRemove).Done(); err != nil {
			return err
		}
		mu.Lock()
		b := be
		mu.Unlock()
		out.Null()
		if b == nil {
			return nil
		}
		return b.set(nil)
	})
}

type platformBackend struct{ t caps.Tray }

func (p platformBackend) set(icon *caps.TrayIcon) error { return p.t.SetTray(icon) }
