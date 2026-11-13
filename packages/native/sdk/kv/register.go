package kv

import (
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/internal/glue"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/wire"
)

// Register exposes the store to the UI as krate/kv.* ids on Default.
func Register(r *scheme.Registry, set permission.Set) { RegisterWith(r, set, Default) }

// RegisterWith is Register with a custom way to get the Store (tests).
func RegisterWith(r *scheme.Registry, set permission.Set, get func() (*Store, error)) {
	permission.Register(r, set, "krate/kv.get", func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		a, err := glue.Open("krate/kv.get", args)
		if err != nil {
			return err
		}
		key, err := a.String("key")
		if err != nil {
			return err
		}
		if err := a.Done(); err != nil {
			return err
		}
		s, err := get()
		if err != nil {
			return err
		}
		frame, ok, err := s.Get(ctx, key)
		if err != nil {
			return err
		}
		if !ok {
			out.Null()
			return nil
		}
		d, err := wire.NewDecoder(frame)
		if err != nil {
			return krate.Errorf("internal", "kv: stored value for %q is damaged: %w", key, err)
		}
		if err := wire.Copy(out, d); err != nil {
			return krate.Errorf("internal", "kv: stored value for %q is damaged: %w", key, err)
		}
		return nil
	})

	permission.Register(r, set, "krate/kv.set", func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		a, err := glue.Open("krate/kv.set", args)
		if err != nil {
			return err
		}
		key, err := a.String("key")
		if err != nil {
			return err
		}
		if err := a.Next("value"); err != nil {
			return err
		}
		e := wire.NewEncoder()
		if err := wire.Copy(e, a.Decoder()); err != nil {
			return a.Fail("value", err)
		}
		frame, err := e.Finish()
		if err != nil {
			return a.Fail("value", err)
		}
		if err := a.Done(); err != nil {
			return err
		}
		s, err := get()
		if err != nil {
			return err
		}
		if err := s.Set(ctx, key, frame); err != nil {
			return err
		}
		out.Null()
		return nil
	})

	permission.Register(r, set, "krate/kv.delete", func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		a, err := glue.Open("krate/kv.delete", args)
		if err != nil {
			return err
		}
		key, err := a.String("key")
		if err != nil {
			return err
		}
		if err := a.Done(); err != nil {
			return err
		}
		s, err := get()
		if err != nil {
			return err
		}
		if err := s.Delete(ctx, key); err != nil {
			return err
		}
		out.Null()
		return nil
	})

	permission.Register(r, set, "krate/kv.keys", func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		a, err := glue.Open("krate/kv.keys", args)
		if err != nil {
			return err
		}
		prefix, err := a.OptString("prefix", "")
		if err != nil {
			return err
		}
		if err := a.Done(); err != nil {
			return err
		}
		s, err := get()
		if err != nil {
			return err
		}
		keys, err := s.Keys(ctx, prefix)
		if err != nil {
			return err
		}
		out.BeginArray()
		for _, k := range keys {
			out.String(k)
		}
		out.EndArray()
		return nil
	})
}
