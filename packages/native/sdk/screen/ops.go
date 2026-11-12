package screen

import (
	"fmt"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/sdk/internal/jsonwire"
	"github.com/kratejs/krate/packages/native/wire"
)

// Register adds the platform-side operations to t.
func Register(t *bridge.Table, env *bridge.Env) {
	topic := env.Topic("screen")
	t.Stream(IDEvents, Permission, true, topic.Stream())

	ds, hasDisplays := env.App.(caps.Displays)
	if hasDisplays {
		ds.OnDisplays(func() {
			list, err := ds.Displays()
			if err != nil {
				return
			}
			jsonwire.Publish(topic, Event{Displays: fromCapsList(list)})
		})
	}
	list := func() ([]caps.Display, error) {
		if !hasDisplays {
			return nil, fmt.Errorf("%w: listing displays is not available on this platform", krate.ErrUnsupported)
		}
		l, err := ds.Displays()
		if err != nil {
			return nil, err
		}
		if len(l) == 0 {
			return nil, krate.Errorf("unavailable", "the system reports no displays")
		}
		return l, nil
	}

	t.Func(IDList, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDList).Done(); err != nil {
			return err
		}
		l, err := list()
		if err != nil {
			return err
		}
		return jsonwire.Result(out, fromCapsList(l))
	})
	t.Func(IDPrimary, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDPrimary).Done(); err != nil {
			return err
		}
		l, err := list()
		if err != nil {
			return err
		}
		d, _ := caps.FindDisplay(l, "")
		return jsonwire.Result(out, fromCaps(d))
	})
	t.Func(IDOfWindow, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDOfWindow)
		win := ""
		if r.More() {
			win = r.String()
		}
		if err := r.Done(); err != nil {
			return err
		}
		l, err := list()
		if err != nil {
			return err
		}
		c, _, _, err := env.Controls(win)
		if err != nil {
			return err
		}
		d, _ := windowDisplay(l, c.State())
		return jsonwire.Result(out, fromCaps(d))
	})
	t.Func(IDCenterWindow, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDCenterWindow)
		win := r.String()
		disp := ""
		if r.More() {
			disp = r.String()
		}
		if err := r.Done(); err != nil {
			return err
		}
		l, err := list()
		if err != nil {
			return err
		}
		c, _, _, err := env.Controls(win)
		if err != nil {
			return err
		}
		st := c.State()
		var d caps.Display
		if disp == "" {
			d, _ = windowDisplay(l, st)
		} else {
			var ok bool
			if d, ok = caps.FindDisplay(l, disp); !ok {
				return krate.Errorf("not_found", "no display %q", disp)
			}
		}
		x, y := caps.CenterIn(d, st.Width, st.Height)
		c.SetPosition(x, y)
		out.Null()
		return nil
	})
}

func windowDisplay(l []caps.Display, st caps.WindowState) (caps.Display, bool) {
	return caps.DisplayForRect(l, caps.Rect{X: st.X, Y: st.Y, W: st.Width, H: st.Height})
}

// Available reports whether displays can be listed.
func Available(env *bridge.Env) bool {
	_, ok := env.App.(caps.Displays)
	return ok
}
