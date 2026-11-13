// Package bridge carries SDK operations to the process that owns the
// platform. Every operation is a host function id plus a wire-encoded
// arguments array, so the same message works in three places:
//
//   - in the shell process (or a release build, where Go host code is linked
//     into the shell) the call is dispatched to the local Table;
//   - in the Go host child process of a dev run there is no platform, so the
//     call is forwarded over HTTP to the shell, which serves its Table on a
//     private unix socket named by KRATE_GOHOST_PARENT;
//   - the UI reaches a permission-filtered view of the same Table through the
//     app's scheme.Registry.
//
// Events travel the other way as host streams (Subscribe).
package bridge

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// ParentEnv names the environment variable that tells a Go host child where
// the shell serves its SDK operations: "unix:<socket path>", "tcp:127.0.0.1:<port>?token=<token>" or an http URL.
const ParentEnv = "KRATE_GOHOST_PARENT"

type entry struct {
	f    scheme.Func
	s    scheme.StreamFunc
	perm string // permission a UI call needs; "" with ui=false means Go only
	ui   bool
}

// Table holds the platform-side implementation of the SDK operations.
type Table struct {
	mu sync.RWMutex
	m  map[string]entry
}

// NewTable returns an empty Table.
func NewTable() *Table { return &Table{m: map[string]entry{}} }

// Func adds a request/response operation. perm names the permission a UI call
// needs; ui=false keeps it away from the UI entirely.
func (t *Table) Func(id, perm string, ui bool, f scheme.Func) {
	t.add(id, entry{f: f, perm: perm, ui: ui})
}

// Stream adds an event stream.
func (t *Table) Stream(id, perm string, ui bool, f scheme.StreamFunc) {
	t.add(id, entry{s: f, perm: perm, ui: ui})
}

func (t *Table) add(id string, e entry) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, dup := t.m[id]; dup {
		panic(fmt.Sprintf("bridge: %q registered twice", id))
	}
	t.m[id] = e
}

// IDs lists the ids in the table with their UI permission ("" = Go only).
func (t *Table) IDs() map[string]string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make(map[string]string, len(t.m))
	for id, e := range t.m {
		if e.ui {
			out[id] = e.perm
		} else {
			out[id] = ""
		}
	}
	return out
}

// RegisterUI registers on reg the operations the UI may call: those marked ui.
// One whose permission allowed accepts is registered as is; any other is
// registered as a stub that fails with denied(permission), so the UI can tell
// a missing permission from a mistyped id.
func (t *Table) RegisterUI(reg *scheme.Registry, allowed func(permission string) bool, denied func(permission string) error) {
	var af func(window, permission string) bool
	if allowed != nil {
		af = func(_, p string) bool { return allowed(p) }
	}
	t.RegisterUIFor(reg, af, denied)
}

// RegisterUIFor is RegisterUI with a per-window check. allowed("", p) decides
// whether the app grants p at all (stub or not); for a granted id the check
// runs again on every call with the window that made it, so a window with a
// narrower scope is denied.
func (t *Table) RegisterUIFor(reg *scheme.Registry, allowed func(window, permission string) bool, denied func(permission string) error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for id, e := range t.m {
		if !e.ui {
			continue
		}
		perm := e.perm
		// Trusted in-process callers (host code) are never gated, so every UI
		// id has a Trusted twin whether or not the app grants its permission.
		if e.f != nil {
			reg.Trusted(id, e.f)
		} else {
			reg.TrustedStream(id, e.s)
		}
		if allowed == nil || !allowed("", perm) {
			if e.f != nil {
				reg.Func(id, func(krate.Ctx, *wire.Decoder, *wire.Encoder) error { return denied(perm) })
			} else {
				reg.Stream(id, func(krate.Ctx, *wire.Decoder, func(func(*wire.Encoder)) error) error { return denied(perm) })
			}
			continue
		}
		if e.f != nil {
			f := e.f
			reg.Func(id, func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
				if !allowed(ctx.Window(), perm) {
					return denied(perm)
				}
				return f(ctx, a, out)
			})
		} else {
			s := e.s
			reg.Stream(id, func(ctx krate.Ctx, a *wire.Decoder, emit func(func(*wire.Encoder)) error) error {
				if !allowed(ctx.Window(), perm) {
					return denied(perm)
				}
				return s(ctx, a, emit)
			})
		}
	}
}

// RegisterAll registers every operation, for the trusted Go host child.
func (t *Table) RegisterAll(reg *scheme.Registry) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for id, e := range t.m {
		t.registerOne(reg, id, e)
	}
}

func (t *Table) registerOne(reg *scheme.Registry, id string, e entry) {
	if e.f != nil {
		reg.Func(id, e.f)
	} else {
		reg.Stream(id, e.s)
	}
}

func (t *Table) lookup(id string) (entry, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	e, ok := t.m[id]
	return e, ok
}

var (
	local atomic.Pointer[Table]
	epoch atomic.Int64
)

// SetLocal installs the Table of the process that owns the platform. Pass nil
// to remove it (tests).
func SetLocal(t *Table) {
	local.Store(t)
	epoch.Add(1)
}

// Epoch changes whenever the dispatch target changes (SetLocal, ResetRemote).
// Packages that keep one long-lived Subscribe use it to resubscribe.
func Epoch() int64 { return epoch.Load() }

// Local returns the installed Table, or nil.
func Local() *Table { return local.Load() }

// Available reports whether SDK operations can be dispatched at all: a local
// platform exists or a parent address is set.
func Available() bool { return local.Load() != nil || os.Getenv(ParentEnv) != "" }

func asCtx(ctx context.Context) krate.Ctx {
	if c, ok := ctx.(krate.Ctx); ok {
		return c
	}
	return krate.WithWindow(ctx, "")
}

// ErrNoPlatform is returned when there is neither a local platform nor a
// parent to forward to, for example Go code run outside a Krate app.
var ErrNoPlatform = fmt.Errorf("%w: the SDK needs a running Krate app (no platform in this process and no %s)", krate.ErrUnsupported, ParentEnv)

// Call runs operation id with the arguments written by args and returns a
// decoder positioned at the result value.
func Call(ctx context.Context, id string, args func(e *wire.Encoder)) (*wire.Decoder, error) {
	if t := local.Load(); t != nil {
		e, ok := t.lookup(id)
		if !ok || e.f == nil {
			return nil, krate.Errorf("not_found", "no SDK function %s", id)
		}
		ad, err := codec.EncodeArgs(args)
		if err != nil {
			return nil, krate.Errorf("bad_args", "%s: %w", id, err)
		}
		return codec.Result(func(out *wire.Encoder) error { return e.f(asCtx(ctx), ad, out) })
	}
	r, err := parentRemote()
	if err != nil {
		return nil, err
	}
	return r.call(asCtx(ctx), id, args)
}

// CallDo is Call for operations without a result.
func CallDo(ctx context.Context, id string, args func(e *wire.Encoder)) error {
	_, err := Call(ctx, id, args)
	return err
}

// Subscribe opens the event stream id and calls onValue with a decoder over
// each value, on its own goroutine, until cancel is called or ctx ends. In a
// child process it reconnects if the stream drops.
func Subscribe(ctx context.Context, id string, onValue func(d *wire.Decoder)) (cancel func(), err error) {
	return SubscribeArgs(ctx, id, nil, onValue)
}

// SubscribeArgs is Subscribe for a stream that takes arguments.
func SubscribeArgs(ctx context.Context, id string, args func(e *wire.Encoder), onValue func(d *wire.Decoder)) (cancel func(), err error) {
	ctx, stop := context.WithCancel(ctx)
	if t := local.Load(); t != nil {
		e, ok := t.lookup(id)
		if !ok || e.s == nil {
			stop()
			return nil, krate.Errorf("not_found", "no SDK stream %s", id)
		}
		ad, err := codec.EncodeArgs(args)
		if err != nil {
			stop()
			return nil, err
		}
		go func() {
			emit := func(write func(*wire.Encoder)) error {
				enc := wire.NewEncoder()
				write(enc)
				b, err := enc.Finish()
				if err != nil {
					return err
				}
				d, err := wire.NewDecoder(b)
				if err != nil {
					return err
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				onValue(d)
				return nil
			}
			_ = e.s(asCtx(ctx), ad, emit)
		}()
		return stop, nil
	}
	r, err := parentRemote()
	if err != nil {
		stop()
		return nil, err
	}
	go r.stream(asCtx(ctx), id, args, onValue)
	return stop, nil
}

var (
	remoteMu sync.Mutex
	remoteR  *remote
)

func parentRemote() (*remote, error) {
	remoteMu.Lock()
	defer remoteMu.Unlock()
	if remoteR != nil {
		return remoteR, nil
	}
	addr := os.Getenv(ParentEnv)
	if addr == "" {
		return nil, ErrNoPlatform
	}
	r, err := newRemote(addr)
	if err != nil {
		return nil, err
	}
	remoteR = r
	return r, nil
}

// ResetRemote forgets the cached parent connection; tests that change
// ParentEnv call it.
func ResetRemote() {
	epoch.Add(1)
	remoteMu.Lock()
	defer remoteMu.Unlock()
	if remoteR != nil {
		remoteR.close()
		remoteR = nil
	}
}
