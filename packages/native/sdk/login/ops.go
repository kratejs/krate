package login

import (
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/sdk/internal/jsonwire"
	"github.com/kratejs/krate/packages/native/wire"
)

// store is the platform's login item.
type store interface {
	get() (Item, error)
	set(enabled bool, args []string) error
}

// Register adds the platform-side operations to t.
func Register(t *bridge.Table, env *bridge.Env) {
	st := newStore(env)
	t.Func(IDGet, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDGet).Done(); err != nil {
			return err
		}
		it, err := st.get()
		if err != nil {
			return err
		}
		return jsonwire.Result(out, it)
	})
	t.Func(IDSet, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSet)
		on := r.Bool()
		var o Options
		if r.More() {
			o = jsonwire.Arg[Options](r)
		}
		if err := r.Done(); err != nil {
			return err
		}
		if err := st.set(on, o.Args); err != nil {
			return err
		}
		out.Null()
		return nil
	})
}

// Available reports whether login items work.
func Available(env *bridge.Env) bool { return true }

// platformLogin returns the platform's own login item support, if any.
func platformLogin(env *bridge.Env) caps.LoginItems {
	l, _ := env.App.(caps.LoginItems)
	return l
}
