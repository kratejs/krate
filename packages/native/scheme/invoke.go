package scheme

import (
	"errors"
	"fmt"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/wire"
)

func notFound(kind, id string) error {
	return &krate.Error{Code: "not_found", Message: fmt.Sprintf("no such %s %q", kind, id), Data: nil}
}

// asError turns a panic captured by protect into a *krate.Error so callers
// outside the HTTP server see the same codes as the UI.
func asError(err error) error {
	var pe *panicError
	if errors.As(err, &pe) {
		return &krate.Error{Code: "panic", Message: fmt.Sprintf("panic in host function: %v", pe.val)}
	}
	return err
}

// Invoke calls the registered Func id in-process, without HTTP and without any
// permission gate: it is for trusted code that runs inside the app. An id that
// the UI reaches only behind a permission check has a Trusted twin that skips
// the check. args is a wire frame whose value is the argument array; the
// result is a wire frame holding the single result value.
func (r *Registry) Invoke(ctx krate.Ctx, id string, args []byte) ([]byte, error) {
	fn, _ := r.lookupTrusted(id)
	if fn == nil {
		return nil, notFound("function", id)
	}
	d, err := wire.NewDecoder(args)
	if err != nil {
		return nil, krate.Errorf("bad_request", "%s: arguments: %w", id, err)
	}
	e := wire.NewEncoder()
	if err := protect(func() error { return fn(ctx, d, e) }); err != nil {
		return nil, asError(err)
	}
	fr, err := e.Finish()
	if err != nil {
		return nil, krate.Errorf("internal", "%s: encoding the result: %w", id, err)
	}
	return fr, nil
}

// InvokeStream runs the registered stream id in-process until it ends, ctx is
// cancelled or emit fails. Each emitted value reaches emit as a wire frame.
// emit may block; the producer waits for it (backpressure).
func (r *Registry) InvokeStream(ctx krate.Ctx, id string, args []byte, emit func(frame []byte) error) error {
	_, sfn := r.lookupTrusted(id)
	if sfn == nil {
		return notFound("stream", id)
	}
	d, err := wire.NewDecoder(args)
	if err != nil {
		return krate.Errorf("bad_request", "%s: arguments: %w", id, err)
	}
	send := func(write func(*wire.Encoder)) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		e := wire.NewEncoder()
		if err := protect(func() error { write(e); return nil }); err != nil {
			return err
		}
		fr, err := e.Finish()
		if err != nil {
			return err
		}
		return emit(fr)
	}
	return asError(protect(func() error { return sfn(ctx, d, send) }))
}

// Subscribe attaches onChange to the registered signal id. Each change
// arrives as a wire frame holding the value. The returned cancel stops
// delivery.
func (r *Registry) Subscribe(id string, onChange func(frame []byte)) (cancel func(), err error) {
	sf := r.signal(id)
	if sf == nil {
		return nil, notFound("signal", id)
	}
	return sf(func(write func(*wire.Encoder)) {
		e := wire.NewEncoder()
		if protect(func() error { write(e); return nil }) != nil {
			return
		}
		if fr, err := e.Finish(); err == nil {
			onChange(fr)
		}
	}), nil
}
