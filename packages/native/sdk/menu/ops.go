package menu

import (
	"encoding/json"
	"fmt"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

func toCaps(items []Item) []caps.MenuItem {
	out := make([]caps.MenuItem, 0, len(items))
	for _, it := range items {
		out = append(out, caps.MenuItem{
			ID: it.ID, Label: it.Label, Kind: caps.MenuKind(it.Kind), Accelerator: it.Accelerator,
			Disabled: it.Disabled, Checked: it.Checked, Submenu: toCaps(it.Submenu),
		})
	}
	return out
}

// Register adds the platform-side operations to t and routes menu clicks to
// the "menu" topic.
func Register(t *bridge.Table, env *bridge.Env) {
	topic := env.Topic("menu")
	t.Stream(IDEvents, Permission, true, topic.Stream())
	env.Providers[bridge.InjectKey("menu")] = func(payload []byte) error {
		var p struct {
			ID, Window string
			Checked    bool
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return fmt.Errorf("menu event: %w", err)
		}
		if p.ID == "" {
			return fmt.Errorf("menu event: id is required")
		}
		topic.Publish(func(e *wire.Encoder) { encodeEvent(e, Event{ID: p.ID, Window: p.Window, Checked: p.Checked}) })
		return nil
	}
	if m, ok := env.App.(caps.Menus); ok {
		m.OnMenu(func(ev caps.MenuEvent) {
			if runBuiltin(env, ev) {
				return
			}
			topic.Publish(func(e *wire.Encoder) {
				encodeEvent(e, Event{ID: ev.ID, Window: env.Windows.ID(ev.Window), Checked: ev.Checked})
			})
		})
	}
	registerContext(t, env)
	t.Func(IDSetApp, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSetApp)
		items := decodeItems(r, 0)
		if err := r.Done(); err != nil {
			return err
		}
		m, err := bridge.Cap[caps.Menus](env.App, "menus")
		if err != nil {
			return err
		}
		out.Null()
		return m.SetAppMenu(toCaps(items))
	})
	t.Func(IDPopup, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDPopup)
		win := r.String()
		x, y := int(r.Int()), int(r.Int())
		items := decodeItems(r, 0)
		if err := r.Done(); err != nil {
			return err
		}
		m, err := bridge.Cap[caps.Menus](env.App, "menus")
		if err != nil {
			return err
		}
		out.Null()
		return m.PopupMenu(env.Windows.Ref(win), toCaps(items), x, y)
	})
}
