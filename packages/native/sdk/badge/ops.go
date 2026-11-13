package badge

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

// backend shows the badge somewhere.
type backend interface {
	setBadge(text string) error
	setProgress(p caps.Progress) error
	setUrgent(on bool) error
}

// Register adds the platform-side operations to t.
func Register(t *bridge.Table, env *bridge.Env) {
	var mu sync.Mutex
	var be backend
	var text string
	var prog caps.Progress
	prog.State = caps.ProgressNone

	get := func() (backend, error) {
		if be != nil {
			return be, nil
		}
		if b, ok := env.App.(caps.Badger); ok {
			be = platformBackend{b, env.App}
			return be, nil
		}
		b, err := newDefaultBackend(env)
		if err != nil {
			return nil, err
		}
		be = b
		return be, nil
	}

	t.Func(IDSet, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSet)
		s := r.String()
		if err := r.Done(); err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		b, err := get()
		if err != nil {
			return err
		}
		if err := b.setBadge(s); err != nil {
			return err
		}
		text = s
		out.Null()
		return nil
	})
	t.Func(IDGet, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDGet).Done(); err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		return jsonwire.Result(out, text)
	})
	t.Func(IDSetProgress, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSetProgress)
		state := r.String()
		var value float64
		if r.More() {
			value = r.Float()
		}
		if err := r.Done(); err != nil {
			return err
		}
		p, err := caps.Progress{State: caps.ProgressState(state), Value: value}.Validate()
		if err != nil {
			return krate.Errorf("bad_args", "%s: %w", IDSetProgress, err)
		}
		mu.Lock()
		defer mu.Unlock()
		b, err := get()
		if err != nil {
			return err
		}
		if err := b.setProgress(p); err != nil {
			return err
		}
		prog = p
		out.Null()
		return nil
	})
	t.Func(IDProgress, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDProgress).Done(); err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		return jsonwire.Result(out, Progress{State: prog.State, Value: prog.Value})
	})
	t.Func(IDSetUrgent, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSetUrgent)
		on := r.Bool()
		if err := r.Done(); err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		b, err := get()
		if err != nil {
			return err
		}
		if err := b.setUrgent(on); err != nil {
			return err
		}
		out.Null()
		return nil
	})
}

// platformBackend adapts a caps.Badger.
type platformBackend struct {
	b   caps.Badger
	app any
}

func (p platformBackend) setBadge(text string) error { return p.b.SetBadge(text) }
func (p platformBackend) setProgress(pr caps.Progress) error {
	return p.b.SetProgress(pr)
}
func (p platformBackend) setUrgent(on bool) error {
	if a, ok := p.app.(caps.Attention); ok {
		return a.RequestAttention(on)
	}
	return fmt.Errorf("%w: requesting attention is not available on this platform", krate.ErrUnsupported)
}

// Available reports whether badges can be shown.
func Available(env *bridge.Env) bool {
	if _, ok := env.App.(caps.Badger); ok {
		return true
	}
	return defaultAvailable()
}
