package fs

import (
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/internal/glue"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/wire"
)

func init() {
	krate.RegisterCapability("fs", func() bool { return true })
}

// Register exposes the fs area to the UI as krate/fs.* ids on Default.
func Register(r *scheme.Registry, set permission.Set) { RegisterScope(r, set, Default) }

// RegisterScope is Register for a specific Scope (used by tests).
func RegisterScope(r *scheme.Registry, set permission.Set, sc *Scope) {
	// one registers a function of one path argument.
	one := func(id string, f func(path string, out *wire.Encoder) error) {
		permission.Register(r, set, id, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
			a, err := glue.Open(id, args)
			if err != nil {
				return err
			}
			path, err := a.String("path")
			if err != nil {
				return err
			}
			if err := a.Done(); err != nil {
				return err
			}
			return f(path, out)
		})
	}
	one("krate/fs.readFile", func(p string, out *wire.Encoder) error {
		b, err := sc.ReadFile(p)
		if err != nil {
			return err
		}
		out.Bytes(b)
		return nil
	})
	one("krate/fs.readText", func(p string, out *wire.Encoder) error {
		s, err := sc.ReadText(p)
		if err != nil {
			return err
		}
		out.String(s)
		return nil
	})
	one("krate/fs.readDir", func(p string, out *wire.Encoder) error {
		list, err := sc.ReadDir(p)
		if err != nil {
			return err
		}
		out.BeginArray()
		for _, e := range list {
			out.BeginObject()
			out.Key("name")
			out.String(e.Name)
			out.Key("isDir")
			out.Bool(e.IsDir)
			out.Key("size")
			out.Int(e.Size)
			out.Key("modTime")
			out.Time(e.ModTime)
			out.EndObject()
		}
		out.EndArray()
		return nil
	})
	one("krate/fs.stat", func(p string, out *wire.Encoder) error {
		i, err := sc.Stat(p)
		if err != nil {
			return err
		}
		out.BeginObject()
		out.Key("name")
		out.String(i.Name)
		out.Key("size")
		out.Int(i.Size)
		out.Key("isDir")
		out.Bool(i.IsDir)
		out.Key("modTime")
		out.Time(i.ModTime)
		out.Key("mode")
		out.Int(int64(i.Mode))
		out.EndObject()
		return nil
	})
	one("krate/fs.exists", func(p string, out *wire.Encoder) error {
		ok, err := sc.Exists(p)
		if err != nil {
			return err
		}
		out.Bool(ok)
		return nil
	})
	one("krate/fs.mkdir", func(p string, out *wire.Encoder) error {
		if err := sc.Mkdir(p); err != nil {
			return err
		}
		out.Null()
		return nil
	})

	permission.Register(r, set, "krate/fs.writeFile", func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		const id = "krate/fs.writeFile"
		a, err := glue.Open(id, args)
		if err != nil {
			return err
		}
		path, err := a.String("path")
		if err != nil {
			return err
		}
		data, err := a.Bytes("data")
		if err != nil {
			return err
		}
		if err := a.Done(); err != nil {
			return err
		}
		if err := sc.WriteFile(path, data); err != nil {
			return err
		}
		out.Null()
		return nil
	})
	permission.Register(r, set, "krate/fs.writeText", func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		const id = "krate/fs.writeText"
		a, err := glue.Open(id, args)
		if err != nil {
			return err
		}
		path, err := a.String("path")
		if err != nil {
			return err
		}
		text, err := a.String("text")
		if err != nil {
			return err
		}
		if err := a.Done(); err != nil {
			return err
		}
		if err := sc.WriteText(path, text); err != nil {
			return err
		}
		out.Null()
		return nil
	})
	permission.Register(r, set, "krate/fs.remove", func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		const id = "krate/fs.remove"
		a, err := glue.Open(id, args)
		if err != nil {
			return err
		}
		path, err := a.String("path")
		if err != nil {
			return err
		}
		rec, err := a.OptBool("recursive")
		if err != nil {
			return err
		}
		if err := a.Done(); err != nil {
			return err
		}
		if err := sc.Remove(path, rec); err != nil {
			return err
		}
		out.Null()
		return nil
	})
	permission.Register(r, set, "krate/fs.rename", func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		const id = "krate/fs.rename"
		a, err := glue.Open(id, args)
		if err != nil {
			return err
		}
		from, err := a.String("from")
		if err != nil {
			return err
		}
		to, err := a.String("to")
		if err != nil {
			return err
		}
		if err := a.Done(); err != nil {
			return err
		}
		if err := sc.Rename(from, to); err != nil {
			return err
		}
		out.Null()
		return nil
	})
}
