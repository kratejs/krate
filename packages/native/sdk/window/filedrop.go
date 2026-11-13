package window

import (
	"context"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/sdk/internal/picked"
	"github.com/kratejs/krate/packages/native/wire"
)

// Host function ids of file drops.
const (
	IDFileDrops = "krate/window.fileDrops"
	IDThisView  = "krate/window.thisView"
)

// FileDropEvent reports files the user dropped from the operating system onto
// a view. The page also gets its normal HTML5 drop event; this adds the real
// paths, which a page cannot see. The app may read the files through the fs
// SDK, like files chosen in a dialog.
type FileDropEvent struct {
	// View is the id of the view the files were dropped on; the first view of a
	// window has the window's id.
	View   string
	Window string
	// Paths are absolute paths of the dropped files and folders.
	Paths []string
	// X and Y are the drop position in the view in CSS pixels, -1 when the
	// platform cannot say.
	X, Y int
}

func encodeFileDrop(e *wire.Encoder, ev FileDropEvent) {
	e.BeginObject()
	e.Key("view")
	e.String(ev.View)
	e.Key("window")
	e.String(ev.Window)
	e.Key("paths")
	codec.WriteStrings(e, ev.Paths)
	e.Key("x")
	e.Int(int64(ev.X))
	e.Key("y")
	e.Int(int64(ev.Y))
	e.EndObject()
}

// OnFileDrop calls fn for every drop of files onto a view of the app until
// cancel is called. Host code can use it from Go; a page uses
// window.onFileDrop in @krate/app.
func OnFileDrop(fn func(FileDropEvent)) (cancel func(), err error) {
	return bridge.Subscribe(context.Background(), IDFileDrops, func(d *wire.Decoder) {
		r := &codec.R{D: d, Name: "file drop"}
		var ev FileDropEvent
		r.Object(func(k string) bool {
			switch k {
			case "view":
				ev.View = r.String()
			case "window":
				ev.Window = r.String()
			case "paths":
				ev.Paths = r.Strings()
			case "x":
				ev.X = int(r.Int())
			case "y":
				ev.Y = int(r.Int())
			default:
				return false
			}
			return true
		})
		if r.Err() == nil {
			fn(ev)
		}
	})
}

// registerFileDrops routes the platform's file drops to the "windowdrops"
// topic and grants the app access to the dropped paths.
func registerFileDrops(t *bridge.Table, env *bridge.Env) {
	topic := env.Topic("windowdrops")
	t.Stream(IDFileDrops, Permission, true, topic.Stream())
	// The id of the view that makes the call, so a page can pick its own
	// drops out of the stream.
	t.Func(IDThisView, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDThisView).Done(); err != nil {
			return err
		}
		out.String(ctx.Window())
		return nil
	})
	handle := func(d caps.FileDrop) {
		ev := FileDropEvent{Paths: d.Paths, X: d.X, Y: d.Y}
		if rec, ok := env.Windows.ViewByRef(d.View); ok {
			ev.View, ev.Window = rec.ID, rec.Window
		} else if id := env.Windows.ID(d.Window); id != "" {
			ev.View, ev.Window = id, id
		} else {
			return
		}
		if len(ev.Paths) == 0 {
			return
		}
		// What the user drops on the app is theirs to hand to it.
		for _, p := range ev.Paths {
			picked.Notify(p)
		}
		topic.Publish(func(e *wire.Encoder) { encodeFileDrop(e, ev) })
	}
	env.FileDrop = handle
	if fd, ok := env.App.(caps.FileDrops); ok {
		fd.OnFileDrop(handle)
	}
}
