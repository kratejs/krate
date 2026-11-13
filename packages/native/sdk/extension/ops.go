package extension

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

func unsupported() error {
	why := "this webview has no extension API"
	switch runtime.GOOS {
	case "linux":
		why = "WebKitGTK has no browser extension API"
	case "darwin":
		why = "browser extensions need macOS 15.4 or later (WKWebExtension)"
	case "windows":
		why = "this WebView2 runtime has no extension support (ICoreWebView2Profile7)"
	}
	return fmt.Errorf("%w: %s", krate.ErrUnsupported, why)
}

func toInfo(i caps.ExtensionInfo) Info { return Info{ID: i.ID, Name: i.Name, Enabled: i.Enabled} }

// Register adds the platform-side operations to t.
func Register(t *bridge.Table, env *bridge.Env) {
	ext, has := env.App.(caps.Extensions)
	usable := func() (caps.Extensions, error) {
		if !has || !ext.ExtensionsSupported() {
			return nil, unsupported()
		}
		return ext, nil
	}
	t.Func(IDDeclared, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDDeclared).Done(); err != nil {
			return err
		}
		names := []string{}
		for _, d := range all() {
			names = append(names, d.Name)
		}
		return jsonwire.Result(out, names)
	})
	t.Func(IDList, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDList).Done(); err != nil {
			return err
		}
		e, err := usable()
		if err != nil {
			return err
		}
		l, err := e.ListExtensions()
		if err != nil {
			return err
		}
		res := make([]Info, 0, len(l))
		for _, i := range l {
			res = append(res, toInfo(i))
		}
		return jsonwire.Result(out, res)
	})
	t.Func(IDLoad, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDLoad)
		name := r.String()
		if err := r.Done(); err != nil {
			return err
		}
		d, ok := lookup(name)
		if !ok {
			return krate.Errorf("not_found", "no extension %q is declared (native.extensions)", name)
		}
		e, err := usable()
		if err != nil {
			return err
		}
		info, err := e.LoadExtension(d.Dir)
		if err != nil {
			return err
		}
		return jsonwire.Result(out, toInfo(info))
	})
	t.Func(IDRemove, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDRemove)
		id := r.String()
		if err := r.Done(); err != nil {
			return err
		}
		e, err := usable()
		if err != nil {
			return err
		}
		if err := e.RemoveExtension(id); err != nil {
			return err
		}
		out.Null()
		return nil
	})
}

// LoadDeclared loads every declared extension; the shell calls it once the
// platform runs. Errors are returned per extension name.
func LoadDeclared(app any) map[string]error {
	res := map[string]error{}
	ext, ok := app.(caps.Extensions)
	for _, d := range all() {
		switch {
		case !ok || !ext.ExtensionsSupported():
			res[d.Name] = unsupported()
		default:
			if _, err := ext.LoadExtension(d.Dir); err != nil {
				res[d.Name] = err
			}
		}
	}
	return res
}

// Available reports whether extensions can load here.
func Available(env *bridge.Env) bool {
	e, ok := env.App.(caps.Extensions)
	return ok && e.ExtensionsSupported()
}
