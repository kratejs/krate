package theme

import (
	"fmt"
	"sync"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/sdk/internal/jsonwire"
	"github.com/kratejs/krate/packages/native/wire"
)

func errBadSource(s string) error {
	return krate.Errorf("bad_args", "theme source %q: want system, light or dark", s)
}

// platformProvider reads through a caps.Themer.
type platformProvider struct{ t caps.Themer }

func (p platformProvider) read() (caps.ThemeInfo, error) { return p.t.Theme() }
func (p platformProvider) watch(fn func()) func() {
	p.t.OnTheme(func(caps.ThemeInfo) { fn() })
	return func() {}
}

// Register adds the platform-side operations to t and starts following the
// system appearance.
func Register(t *bridge.Table, env *bridge.Env) {
	topic := env.Topic("theme")
	t.Stream(IDEvents, Permission, true, topic.Stream())

	var prov provider
	resolvedForApply := false
	if th, ok := env.App.(caps.Themer); ok {
		prov = platformProvider{th}
	} else if p := newDefaultProvider(); p != nil {
		prov = p
		resolvedForApply = true
	}
	var m *manager
	if prov != nil {
		m = &manager{prov: prov, publish: func(th Theme) { jsonwire.Publish(topic, th) }}
		if wt, ok := env.App.(caps.WindowThemer); ok {
			m.apply = func(src Source, resolved string) {
				mode := string(src)
				if resolvedForApply {
					mode = resolved
				}
				_ = wt.SetWindowTheme(mode) // best effort; a window theme is cosmetic
			}
		}
	}
	var once sync.Once
	start := func() {
		if m == nil {
			return
		}
		once.Do(func() {
			go func() {
				m.refresh(true)
				m.prov.watch(func() { m.refresh(false) })
			}()
		})
	}
	// Following the system needs no call from the app.
	start()

	unsupported := func() error {
		return fmt.Errorf("%w: the system appearance is not available on this platform", krate.ErrUnsupported)
	}
	t.Func(IDGet, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDGet).Done(); err != nil {
			return err
		}
		if m == nil {
			return unsupported()
		}
		return jsonwire.Result(out, m.current())
	})
	t.Func(IDSet, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSet)
		s := r.String()
		if err := r.Done(); err != nil {
			return err
		}
		if m == nil {
			return unsupported()
		}
		if err := m.setSource(s); err != nil {
			return err
		}
		out.Null()
		return nil
	})
}

// Available reports whether the appearance can be read.
func Available(env *bridge.Env) bool {
	if _, ok := env.App.(caps.Themer); ok {
		return true
	}
	return defaultAvailable()
}
