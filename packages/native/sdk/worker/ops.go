package worker

import (
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/sdk/internal/jsonwire"
	"github.com/kratejs/krate/packages/native/sdk/port"
	"github.com/kratejs/krate/packages/native/wire"
)

// Config is what the shell gives the worker area.
type Config struct {
	Supervisor *Supervisor
	// CanReceive reports whether a worker may be handed ports (its narrowed
	// permissions include port:receive).
	CanReceive func(Spec) bool
}

var (
	cfgMu sync.Mutex
	cfg   Config
)

// Configure installs the supervisor the ops use. The shell calls it before
// the SDK attaches.
func Configure(c Config) {
	cfgMu.Lock()
	cfg = c
	cfgMu.Unlock()
}

func current() Config {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	return cfg
}

func unsupported() error {
	return krate.Errorf("unsupported", "this app declares no workers (native.workers)")
}

// workerID returns the instance id when the caller is a worker.
func workerID(ctx krate.Ctx) (string, bool) {
	w := ctx.Window()
	if strings.HasPrefix(w, port.WorkerOwnerPrefix) {
		return strings.TrimPrefix(w, port.WorkerOwnerPrefix), true
	}
	return "", false
}

// Register adds the platform-side operations to t.
func Register(t *bridge.Table, env *bridge.Env) {
	c := current()
	sup := c.Supervisor
	if sup != nil {
		b := port.For(env)
		b.Worker = func(owner string) (bool, bool) {
			id := strings.TrimPrefix(owner, port.WorkerOwnerPrefix)
			sp, ok := sup.SpecOf(id)
			if !ok || !sup.Alive(id) {
				return false, false
			}
			return true, c.CanReceive != nil && c.CanReceive(sp)
		}
		sup.OnEnd = func(in Instance) { b.DropOwner(in.Owner()) }
	}

	t.Func(IDSpecs, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDSpecs).Done(); err != nil {
			return err
		}
		if sup == nil {
			return jsonwire.Result(out, []Spec{})
		}
		return jsonwire.Result(out, sup.Specs())
	})
	t.Func(IDStart, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDStart)
		name := r.String()
		var opts StartOptions
		if r.More() {
			opts = jsonwire.Arg[StartOptions](r)
		}
		if err := r.Done(); err != nil {
			return err
		}
		if sup == nil {
			return unsupported()
		}
		id, err := sup.Start(ctx, name, opts.Args, opts.Env)
		if err != nil {
			return err
		}
		return jsonwire.Result(out, id)
	})
	t.Func(IDStop, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDStop)
		id := r.String()
		if err := r.Done(); err != nil {
			return err
		}
		if sup == nil {
			return unsupported()
		}
		if err := sup.Stop(id); err != nil {
			return err
		}
		out.Null()
		return nil
	})
	t.Func(IDList, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDList).Done(); err != nil {
			return err
		}
		if sup == nil {
			return jsonwire.Result(out, []Info{})
		}
		return jsonwire.Result(out, sup.List())
	})
	t.Func(IDPost, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDPost)
		id := r.String()
		if err := r.Err(); err != nil {
			return err
		}
		frame, err := messageFrame(r, args)
		if err != nil {
			return err
		}
		if err := r.Done(); err != nil {
			return err
		}
		if sup == nil {
			return unsupported()
		}
		a := wire.NewEncoder()
		a.BeginArray()
		src, err := wire.NewDecoder(frame)
		if err != nil {
			return krate.Errorf("bad_args", "%s: %w", IDPost, err)
		}
		if err := wire.Copy(a, src); err != nil {
			return krate.Errorf("bad_args", "%s: %w", IDPost, err)
		}
		a.EndArray()
		af, err := a.Finish()
		if err != nil {
			return krate.Errorf("bad_args", "%s: %w", IDPost, err)
		}
		if err := sup.Post(ctx, id, af); err != nil {
			return err
		}
		out.Null()
		return nil
	})
	t.Func(IDEmit, "", true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDEmit)
		id, ok := workerID(ctx)
		if !ok {
			return krate.Errorf("not_a_worker", "worker.emit is for worker processes")
		}
		frame, err := messageFrame(r, args)
		if err != nil {
			return err
		}
		if err := r.Done(); err != nil {
			return err
		}
		if sup == nil {
			return unsupported()
		}
		if err := sup.Emit(id, frame); err != nil {
			return err
		}
		out.Null()
		return nil
	})
	t.Stream(IDEvents, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, emit func(func(*wire.Encoder)) error) error {
		r := codec.Args(args, IDEvents)
		id := ""
		if r.More() {
			id = r.String()
		}
		if err := r.Done(); err != nil {
			return err
		}
		if sup == nil {
			return nil
		}
		ch, cancel := sup.Subscribe(id)
		defer cancel()
		for {
			select {
			case ev := <-ch:
				if err := emit(func(e *wire.Encoder) { encodeEvent(e, ev) }); err != nil {
					return err
				}
			case <-ctx.Done():
				return nil
			}
		}
	})
}

// messageFrame copies the next argument into a self-contained frame.
func messageFrame(r *codec.R, args *wire.Decoder) ([]byte, error) {
	if !r.More() {
		return nil, krate.Errorf("bad_args", "a message is missing")
	}
	sub, err := args.SubDecoder()
	if err != nil {
		return nil, krate.Errorf("bad_args", "%w", err)
	}
	enc := wire.NewEncoder()
	if err := wire.Copy(enc, sub); err != nil {
		return nil, krate.Errorf("bad_args", "%w", err)
	}
	frame, err := enc.Finish()
	if err != nil {
		return nil, krate.Errorf("bad_args", "%w", err)
	}
	return frame, nil
}

func encodeEvent(e *wire.Encoder, ev Event) {
	e.BeginObject()
	str := func(k, v string) {
		if v != "" {
			e.Key(k)
			e.String(v)
		}
	}
	num := func(k string, v int) {
		if v != 0 {
			e.Key(k)
			e.Int(int64(v))
		}
	}
	str("kind", ev.Kind)
	str("id", ev.ID)
	str("name", ev.Name)
	num("pid", ev.Pid)
	if ev.ExitCode != nil {
		e.Key("exitCode")
		e.Int(int64(*ev.ExitCode))
	}
	str("error", ev.Error)
	num("attempt", ev.Attempt)
	num("delayMs", ev.DelayMs)
	if ev.Data != nil {
		e.Key("data")
		if src, err := wire.NewDecoder(ev.Data); err != nil || wire.Copy(e, src) != nil {
			e.Null()
		}
	}
	e.EndObject()
}

// Available reports whether workers are declared.
func Available(env *bridge.Env) bool { return current().Supervisor != nil }
