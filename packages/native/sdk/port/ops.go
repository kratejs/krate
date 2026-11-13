package port

import (
	"context"
	"sync"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/sdk/internal/jsonwire"
	"github.com/kratejs/krate/packages/native/wire"
)

var (
	forMu    sync.Mutex
	brokerOf = map[*bridge.Env]*Broker{}
)

// For returns the broker of an SDK environment, creating it on first use.
// The worker area uses it to hand ends to workers.
func For(env *bridge.Env) *Broker {
	forMu.Lock()
	defer forMu.Unlock()
	b := brokerOf[env]
	if b == nil {
		b = NewBroker()
		brokerOf[env] = b
		b.Alive = func(owner string) bool {
			_, ok := env.Windows.View(owner)
			return ok
		}
		b.CanReceive = func(owner string) bool { return env.Allows != nil && env.Allows(owner, PermissionReceive) }
		env.Windows.Subscribe(nil, b.DropOwner)
	}
	return b
}

// Register adds the platform-side operations to t.
func Register(t *bridge.Table, env *bridge.Env) {
	b := For(env)

	t.Func(IDChannel, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDChannel).Done(); err != nil {
			return err
		}
		a, c := b.Channel(ctx.Window())
		return jsonwire.Result(out, Ends{A: a, B: c})
	})
	t.Func(IDTransfer, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDTransfer)
		end, to := r.String(), r.String()
		if err := r.Done(); err != nil {
			return err
		}
		if err := b.Transfer(ctx.Window(), end, to); err != nil {
			return err
		}
		out.Null()
		return nil
	})
	t.Func(IDPost, PermissionReceive, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDPost)
		end := r.String()
		if err := r.Err(); err != nil {
			return err
		}
		if !r.More() {
			return krate.Errorf("bad_args", "%s: a message is missing", IDPost)
		}
		sub, err := args.SubDecoder()
		if err != nil {
			return krate.Errorf("bad_args", "%s: %w", IDPost, err)
		}
		enc := wire.NewEncoder()
		if err := wire.Copy(enc, sub); err != nil {
			return krate.Errorf("bad_args", "%s: %w", IDPost, err)
		}
		frame, err := enc.Finish()
		if err != nil {
			return krate.Errorf("bad_args", "%s: %w", IDPost, err)
		}
		if err := r.Done(); err != nil {
			return err
		}
		if err := b.Post(ctx.Window(), end, frame); err != nil {
			return err
		}
		out.Null()
		return nil
	})
	t.Func(IDClose, PermissionReceive, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDClose)
		end := r.String()
		if err := r.Done(); err != nil {
			return err
		}
		if err := b.Close(ctx.Window(), end); err != nil {
			return err
		}
		out.Null()
		return nil
	})
	t.Stream(IDListen, PermissionReceive, true, func(ctx krate.Ctx, args *wire.Decoder, emit func(func(*wire.Encoder)) error) error {
		r := codec.Args(args, IDListen)
		end := r.String()
		if err := r.Done(); err != nil {
			return err
		}
		closed, err := b.Listen(ctx, ctx.Window(), end, func(frame []byte) error {
			src, err := wire.NewDecoder(frame)
			if err != nil {
				return err
			}
			return emit(func(e *wire.Encoder) {
				e.BeginObject()
				e.Key("data")
				if wire.Copy(e, src) != nil {
					e.Null()
				}
				e.EndObject()
			})
		})
		if closed {
			return emit(func(e *wire.Encoder) {
				e.BeginObject()
				e.Key("closed")
				e.Bool(true)
				e.EndObject()
			})
		}
		if err != nil && err != context.Canceled && ctx.Err() == nil {
			return err
		}
		return nil
	})
}

// Available reports whether channels can be used (always: the broker is pure
// Go).
func Available(env *bridge.Env) bool { return true }
