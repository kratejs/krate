package clipboard

import (
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// Register adds the platform-side operations to t.
func Register(t *bridge.Table, env *bridge.Env) {
	cb := func() (caps.Clipboard, error) { return bridge.Cap[caps.Clipboard](env.App, "the clipboard") }
	noArgs := func(name string, f func(c caps.Clipboard, out *wire.Encoder) error) func(krate.Ctx, *wire.Decoder, *wire.Encoder) error {
		return func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
			if err := codec.Args(args, name).Done(); err != nil {
				return err
			}
			c, err := cb()
			if err != nil {
				return err
			}
			return f(c, out)
		}
	}
	t.Func(IDReadText, ReadPermission, true, noArgs(IDReadText, func(c caps.Clipboard, out *wire.Encoder) error {
		s, err := c.ReadText()
		if err != nil {
			return err
		}
		out.String(s)
		return nil
	}))
	t.Func(IDReadImage, ReadPermission, true, noArgs(IDReadImage, func(c caps.Clipboard, out *wire.Encoder) error {
		b, err := c.ReadImage()
		if err != nil {
			return err
		}
		if b == nil {
			out.Null()
		} else {
			out.Bytes(b)
		}
		return nil
	}))
	t.Func(IDReadFiles, ReadPermission, true, noArgs(IDReadFiles, func(c caps.Clipboard, out *wire.Encoder) error {
		p, err := c.ReadFiles()
		if err != nil {
			return err
		}
		codec.WriteStrings(out, p)
		return nil
	}))
	t.Func(IDClear, Permission, true, noArgs(IDClear, func(c caps.Clipboard, out *wire.Encoder) error {
		out.Null()
		return c.Clear()
	}))
	t.Func(IDWriteText, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDWriteText)
		s := r.String()
		if err := r.Done(); err != nil {
			return err
		}
		c, err := cb()
		if err != nil {
			return err
		}
		out.Null()
		return c.WriteText(s)
	})
	t.Func(IDWriteImage, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDWriteImage)
		b := r.Bytes()
		if err := r.Done(); err != nil {
			return err
		}
		c, err := cb()
		if err != nil {
			return err
		}
		out.Null()
		return c.WriteImage(b)
	})
	t.Func(IDWriteFiles, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDWriteFiles)
		p := r.Strings()
		if err := r.Done(); err != nil {
			return err
		}
		c, err := cb()
		if err != nil {
			return err
		}
		out.Null()
		return c.WriteFiles(p)
	})
}
