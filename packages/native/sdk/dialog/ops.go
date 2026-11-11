package dialog

import (
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// Register adds the platform-side operations to t. It is called by the
// package sdk when a platform is attached.
func Register(t *bridge.Table, env *bridge.Env) {
	open := func(mode caps.FileDialogMode, name string) func(krate.Ctx, *wire.Decoder, *wire.Encoder) error {
		return func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
			r := codec.Args(args, name)
			o := decodeOpen(r)
			if err := r.Done(); err != nil {
				return err
			}
			dl, err := bridge.Cap[caps.Dialogs](env.App, "dialogs")
			if err != nil {
				return err
			}
			paths, err := dl.FileDialog(env.Windows.Ref(o.Window), caps.FileDialogOptions{
				Mode: mode, Title: o.Title, Dir: o.Dir, Filters: toCaps(o.Filters), Multiple: o.Multiple, ShowHidden: o.ShowHidden,
			})
			if err != nil {
				return err
			}
			codec.WriteStrings(out, paths)
			return nil
		}
	}
	t.Func(IDOpenFile, Permission, true, open(caps.OpenFile, IDOpenFile))
	t.Func(IDOpenFolder, Permission, true, open(caps.OpenFolder, IDOpenFolder))
	t.Func(IDSaveFile, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSaveFile)
		o := decodeSave(r)
		if err := r.Done(); err != nil {
			return err
		}
		dl, err := bridge.Cap[caps.Dialogs](env.App, "dialogs")
		if err != nil {
			return err
		}
		paths, err := dl.FileDialog(env.Windows.Ref(o.Window), caps.FileDialogOptions{
			Mode: caps.SaveFile, Title: o.Title, Dir: o.Dir, Name: o.Name, Filters: toCaps(o.Filters), ConfirmWrite: o.Overwrite,
		})
		if err != nil {
			return err
		}
		if len(paths) == 0 {
			out.Null()
		} else {
			out.String(paths[0])
		}
		return nil
	})
	t.Func(IDMessage, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDMessage)
		o := decodeMessage(r)
		if err := r.Done(); err != nil {
			return err
		}
		dl, err := bridge.Cap[caps.Dialogs](env.App, "dialogs")
		if err != nil {
			return err
		}
		n, err := dl.MessageDialog(env.Windows.Ref(o.Window), caps.MessageDialogOptions{
			Kind: caps.MessageKind(o.Kind), Title: o.Title, Message: o.Message, Detail: o.Detail, Buttons: o.Buttons, Default: o.Default,
		})
		if err != nil {
			return err
		}
		out.Int(int64(n))
		return nil
	})
}

func toCaps(fs []Filter) []caps.FileFilter {
	var out []caps.FileFilter
	for _, f := range fs {
		out = append(out, caps.FileFilter{Name: f.Name, Patterns: f.Patterns})
	}
	return out
}

// Supported reports whether the platform can show dialogs.
func Supported(env *bridge.Env) bool {
	_, ok := env.App.(caps.Dialogs)
	return ok
}
