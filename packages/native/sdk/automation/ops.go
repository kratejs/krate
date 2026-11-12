package automation

import (
	"fmt"
	"runtime"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/sdk/internal/jsonwire"
	"github.com/kratejs/krate/packages/native/wire"
)

func off() error {
	return fmt.Errorf("%w: automation is off (it runs in dev, or in a build with native.debug)", krate.ErrUnsupported)
}

func noCDP() error {
	why := "this webview does not speak the Chrome DevTools Protocol"
	switch runtime.GOOS {
	case "linux":
		why = "WebKitGTK speaks the WebKit Inspector protocol, not the Chrome DevTools Protocol"
	case "darwin":
		why = "WKWebView has no DevTools protocol endpoint; use Safari's Develop menu"
	}
	return fmt.Errorf("%w: %s", krate.ErrUnsupported, why)
}

func viewOf(env *bridge.Env, id string) (caps.ViewRef, error) {
	if rec, ok := env.Windows.View(id); ok {
		return rec.Ref, nil
	}
	win, _, ok := env.Windows.Get(id)
	if !ok {
		return 0, krate.Errorf("not_found", "no window or view %q", id)
	}
	v, ok := win.(caps.WindowViews)
	if !ok {
		return 0, fmt.Errorf("%w: this platform cannot tell which webview a window holds", krate.ErrUnsupported)
	}
	return v.PrimaryView(), nil
}

// Register adds the platform-side operations to t.
func Register(t *bridge.Table, env *bridge.Env) {
	auto, hasAuto := env.App.(caps.Automation)
	cdp, hasCDP := env.App.(caps.DevToolsProtocol)

	t.Func(IDEndpoint, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDEndpoint).Done(); err != nil {
			return err
		}
		ep := Endpoint{Enabled: enabled.Load(), CDP: hasCDP && enabled.Load()}
		if hasAuto && ep.Enabled {
			e := auto.AutomationEndpoint()
			ep.Kind, ep.URL, ep.Port = e.Kind, e.URL, e.Port
		}
		return jsonwire.Result(out, ep)
	})
	t.Func(IDCDP, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDCDP)
		method := r.String()
		if !r.More() {
			r.Failf("params are missing")
		}
		params, err := rawArg(r)
		view := ""
		if r.More() {
			view = r.String()
		}
		if e := r.Done(); e != nil {
			return e
		}
		if err != nil {
			return err
		}
		if !enabled.Load() {
			return off()
		}
		if !hasCDP {
			return noCDP()
		}
		v, err := viewOf(env, view)
		if err != nil {
			return err
		}
		res, err := cdp.CDP(v, method, string(params))
		if err != nil {
			return err
		}
		if res == "" {
			res = "{}"
		}
		out.Raw([]byte(res))
		return nil
	})
	t.Stream(IDCDPEvents, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, emit func(func(*wire.Encoder)) error) error {
		r := codec.Args(args, IDCDPEvents)
		event := r.String()
		view := ""
		if r.More() {
			view = r.String()
		}
		if err := r.Done(); err != nil {
			return err
		}
		if !enabled.Load() {
			return off()
		}
		if !hasCDP {
			return noCDP()
		}
		v, err := viewOf(env, view)
		if err != nil {
			return err
		}
		ch := make(chan string, 256)
		cancel, err := cdp.OnCDPEvent(v, event, func(p string) {
			select {
			case ch <- p:
			default:
			}
		})
		if err != nil {
			return err
		}
		defer cancel()
		for {
			select {
			case p := <-ch:
				if err := emit(func(e *wire.Encoder) { e.Raw([]byte(p)) }); err != nil {
					return err
				}
			case <-ctx.Done():
				return nil
			}
		}
	})
}

// rawArg reads the next argument as JSON text.
func rawArg(r *codec.R) ([]byte, error) {
	raw, err := r.D.RawValue()
	if err != nil {
		return nil, krate.Errorf("bad_args", "%w", err)
	}
	return append([]byte(nil), raw...), nil
}

// Available reports whether an endpoint can exist on this platform.
func Available(env *bridge.Env) bool {
	_, ok := env.App.(caps.Automation)
	return ok
}
