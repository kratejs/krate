package offscreen

import (
	"context"
	"fmt"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/sdk/internal/jsonwire"
	"github.com/kratejs/krate/packages/native/wire"
)

func unsupported(what string) error {
	return fmt.Errorf("%w: %s", krate.ErrUnsupported, what)
}

// viewOf resolves a window id or view id to the view to capture.
func viewOf(env *bridge.Env, id string) (caps.ViewRef, error) {
	if rec, ok := env.Windows.View(id); ok {
		return rec.Ref, nil
	}
	win, wid, ok := env.Windows.Get(id)
	if !ok {
		return 0, krate.Errorf("not_found", "no window or view %q", id)
	}
	v, ok := win.(caps.WindowViews)
	if !ok {
		return 0, unsupported("this platform cannot tell which webview a window holds")
	}
	_ = wid
	return v.PrimaryView(), nil
}

func normalize(o *CaptureOptions) error {
	switch o.Format {
	case "":
		o.Format = FormatPNG
	case FormatPNG, FormatBGRA:
	default:
		return krate.Errorf("bad_args", "format %q is not png or bgra", o.Format)
	}
	return nil
}

func frameOf(s caps.Snapshot, o CaptureOptions, seq, dropped int) (Frame, error) {
	f := Frame{Seq: seq, TimeMs: time.Now().UnixMilli(), Width: s.Width, Height: s.Height, Format: o.Format, Dropped: dropped}
	var err error
	if o.Format == FormatBGRA {
		f.Data = RawBGRA(s)
	} else {
		f.Data, err = EncodePNG(s)
	}
	return f, err
}

func writeFrame(e *wire.Encoder, f Frame) {
	e.BeginObject()
	e.Key("seq")
	e.Int(int64(f.Seq))
	e.Key("timeMs")
	e.Int(f.TimeMs)
	e.Key("width")
	e.Int(int64(f.Width))
	e.Key("height")
	e.Int(int64(f.Height))
	e.Key("format")
	e.String(f.Format)
	e.Key("dropped")
	e.Int(int64(f.Dropped))
	e.Key("data")
	e.Bytes(f.Data)
	e.EndObject()
}

// Register adds the platform-side operations to t.
func Register(t *bridge.Table, env *bridge.Env) {
	snaps, hasSnaps := env.App.(caps.ViewSnapshots)
	off, hasOff := env.App.(caps.OffscreenWindows)

	t.Func(IDOpen, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDOpen)
		var o OpenOptions
		if r.More() {
			o = jsonwire.Arg[OpenOptions](r)
		}
		if err := r.Done(); err != nil {
			return err
		}
		if !hasOff || !off.OffscreenSupported() {
			return unsupported("this platform cannot render a window out of sight")
		}
		if o.Width <= 0 {
			o.Width = 1024
		}
		if o.Height <= 0 {
			o.Height = 768
		}
		id := ""
		if env.Scope != nil {
			id = env.Windows.Reserve()
			scoped, err := env.Scope(ctx.Window(), id, nil, false)
			if err != nil {
				return err
			}
			if scoped && o.Path != "" {
				o.Path = krate.ScopedPath(o.Path, id)
			}
		}
		w, err := env.App.NewWindow(platform.WindowOptions{Title: "offscreen", Width: o.Width, Height: o.Height, Resizable: true, Path: o.Path, Offscreen: true})
		if err != nil {
			return err
		}
		id = env.Windows.Add(id, w)
		return jsonwire.Result(out, id)
	})

	t.Func(IDCapture, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDCapture)
		win := r.String()
		var o CaptureOptions
		if r.More() {
			o = jsonwire.Arg[CaptureOptions](r)
		}
		if err := r.Done(); err != nil {
			return err
		}
		if err := normalize(&o); err != nil {
			return err
		}
		if !hasSnaps {
			return unsupported("this platform cannot capture a page")
		}
		v, err := viewOf(env, win)
		if err != nil {
			return err
		}
		s, err := snaps.SnapshotView(v, caps.SnapshotOptions{FullDocument: o.FullDocument})
		if err != nil {
			return err
		}
		f, err := frameOf(s, o, 1, 0)
		if err != nil {
			return err
		}
		writeFrame(out, f)
		return nil
	})

	t.Stream(IDFrames, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, emit func(func(*wire.Encoder)) error) error {
		r := codec.Args(args, IDFrames)
		win := r.String()
		var o CaptureOptions
		if r.More() {
			o = jsonwire.Arg[CaptureOptions](r)
		}
		if err := r.Done(); err != nil {
			return err
		}
		if err := normalize(&o); err != nil {
			return err
		}
		if !hasSnaps {
			return unsupported("this platform cannot capture a page")
		}
		v, err := viewOf(env, win)
		if err != nil {
			return err
		}
		return stream(ctx, o, func() (caps.Snapshot, error) {
			return snaps.SnapshotView(v, caps.SnapshotOptions{FullDocument: o.FullDocument})
		}, func(f Frame) error { return emit(func(e *wire.Encoder) { writeFrame(e, f) }) })
	})
}

// stream is the pacing loop: capture, encode, deliver (blocking), then wait
// out what is left of the interval. Frames that were due while it was busy are
// skipped and counted.
func stream(ctx context.Context, o CaptureOptions, capture func() (caps.Snapshot, error), deliver func(Frame) error) error {
	interval := Interval(o.FPS)
	seq, dropped := 0, 0
	for ctx.Err() == nil {
		start := time.Now()
		s, err := capture()
		if err != nil {
			return err
		}
		seq++
		f, err := frameOf(s, o, seq, dropped)
		if err != nil {
			return err
		}
		if err := deliver(f); err != nil {
			return err
		}
		if o.MaxFrames > 0 && seq >= o.MaxFrames {
			return nil
		}
		busy := time.Since(start)
		dropped = Missed(busy, interval)
		if wait := interval - busy; wait > 0 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return nil
			}
		}
	}
	return nil
}

// Available reports whether pages can be captured.
func Available(env *bridge.Env) bool {
	_, ok := env.App.(caps.ViewSnapshots)
	return ok
}
