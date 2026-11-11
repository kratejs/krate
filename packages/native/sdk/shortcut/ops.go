package shortcut

import (
	"errors"
	"fmt"
	"sync"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// backend grabs shortcuts.
type backend interface {
	register(handle string, a caps.Accelerator) error
	unregister(handle string) error
}

// RegisterOps adds the platform-side operations to t. (The name Register is
// taken by the Go API of this package.)
func RegisterOps(t *bridge.Table, env *bridge.Env) {
	topic := env.Topic("shortcut")
	t.Stream(IDEvents, Permission, true, topic.Stream())
	publish := func(handle string) { topic.Publish(func(e *wire.Encoder) { encodeEvent(e, handle) }) }

	var mu sync.Mutex
	var primary backend
	var portal backend
	var portalTried bool
	owner := map[string]backend{}
	if s, ok := env.App.(caps.Shortcuts); ok {
		s.OnShortcut(publish)
		primary = platformBackend{s}
	}
	getPortal := func() (backend, error) {
		if !portalTried {
			portalTried = true
			portal, _ = newPortalBackend(publish)
		}
		if portal == nil {
			return nil, fmt.Errorf("%w: global shortcuts are not available in this session (Wayland without the GlobalShortcuts portal)", krate.ErrUnsupported)
		}
		return portal, nil
	}

	t.Func(IDRegister, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDRegister)
		handle, accel := r.String(), r.String()
		if err := r.Done(); err != nil {
			return err
		}
		a, err := Parse(accel)
		if err != nil {
			return krate.Errorf("bad_args", "%w", err)
		}
		mu.Lock()
		defer mu.Unlock()
		if _, dup := owner[handle]; dup {
			return krate.Errorf("bad_args", "shortcut handle %q is already registered", handle)
		}
		var err2 error
		if primary != nil {
			if err2 = primary.register(handle, a); err2 == nil {
				owner[handle] = primary
				out.Null()
				return nil
			}
			if !krate.IsUnsupported(err2) {
				return err2
			}
		}
		p, perr := getPortal()
		if perr != nil {
			if err2 != nil && !errors.Is(err2, krate.ErrUnsupported) {
				return err2
			}
			return perr
		}
		if err := p.register(handle, a); err != nil {
			return err
		}
		owner[handle] = p
		out.Null()
		return nil
	})
	t.Func(IDUnregister, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDUnregister)
		handle := r.String()
		if err := r.Done(); err != nil {
			return err
		}
		mu.Lock()
		b := owner[handle]
		delete(owner, handle)
		mu.Unlock()
		out.Null()
		if b == nil {
			return nil
		}
		return b.unregister(handle)
	})
}

type platformBackend struct{ s caps.Shortcuts }

func (p platformBackend) register(h string, a caps.Accelerator) error {
	return p.s.RegisterShortcut(h, a)
}
func (p platformBackend) unregister(h string) error { return p.s.UnregisterShortcut(h) }
