package crash

import (
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/kratejs/krate/packages/native/crashreport"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/sdk/internal/jsonwire"
	"github.com/kratejs/krate/packages/native/wire"
)

func mapErr(err error) error {
	switch {
	case errors.Is(err, crashreport.ErrNoDir):
		return fmt.Errorf("%w: no crash report directory is configured", krate.ErrUnsupported)
	case errors.Is(err, crashreport.ErrNotFound):
		return krate.Errorf("not_found", "%v", err)
	}
	return err
}

// Register adds the platform-side operations to t.
func Register(t *bridge.Table, env *bridge.Env) {
	topic := env.Topic("crash")
	t.Stream(IDEvents, Permission, true, topic.Stream())

	var auto atomic.Bool
	lim := newLimiter(3, 30*time.Second)
	if vc, ok := env.App.(caps.ViewCrashes); ok {
		vc.OnViewCrash(func(c caps.ViewCrash) {
			// The handler runs on the UI thread; do the rest elsewhere.
			go func() {
				ev := Event{Kind: "webview", Reason: c.Reason}
				if rec, ok := env.Windows.ViewByRef(c.View); ok {
					ev.View, ev.Window = rec.ID, rec.Window
				}
				reason := c.Reason
				if c.ExitCode != 0 {
					reason = fmt.Sprintf("%s (exit code %d)", reason, c.ExitCode)
				}
				ev.ReportID, _ = crashreport.Write(crashreport.Report{Kind: "webview", Process: "webview", Reason: reason, Window: ev.Window, View: ev.View})
				if auto.Load() && lim.allow(uint64(c.View)) {
					ev.Reloaded = vc.ReloadView(c.View) == nil
				}
				jsonwire.Publish(topic, ev)
			}()
		})
	}

	// A Go crash found in the log of a dead process (the Go host child) is an
	// event too.
	crashreport.Subscribe(func(s crashreport.Summary, process string) {
		jsonwire.Publish(topic, Event{Kind: s.Kind, Reason: s.Reason, ReportID: s.ID})
	})

	t.Func(IDList, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDList).Done(); err != nil {
			return err
		}
		l, err := crashreport.List()
		if err != nil {
			return mapErr(err)
		}
		if l == nil {
			l = []Summary{}
		}
		return jsonwire.Result(out, l)
	})
	t.Func(IDRead, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDRead)
		id := r.String()
		if err := r.Done(); err != nil {
			return err
		}
		rep, err := crashreport.Read(id)
		if err != nil {
			return mapErr(err)
		}
		return jsonwire.Result(out, rep)
	})
	t.Func(IDDelete, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDDelete)
		id := r.String()
		if err := r.Done(); err != nil {
			return err
		}
		if err := crashreport.Delete(id); err != nil {
			return mapErr(err)
		}
		out.Null()
		return nil
	})
	t.Func(IDSetAutoReload, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSetAutoReload)
		on := r.Bool()
		if err := r.Done(); err != nil {
			return err
		}
		if _, ok := env.App.(caps.ViewCrashes); !ok {
			return fmt.Errorf("%w: this platform does not report webview crashes", krate.ErrUnsupported)
		}
		auto.Store(on)
		out.Null()
		return nil
	})
}

// Available reports whether webview crashes are reported.
func Available(env *bridge.Env) bool {
	_, ok := env.App.(caps.ViewCrashes)
	return ok
}
