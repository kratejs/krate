package window

import (
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// Register adds the platform-side operations to t and routes platform window
// events to the "window" topic.
func Register(t *bridge.Table, env *bridge.Env) {
	topic := env.Topic("window")
	t.Stream(IDEvents, Permission, true, topic.Stream())

	if ev, ok := env.App.(caps.WindowEvents); ok {
		ev.OnWindowEvent(func(e caps.WindowEvent) {
			id := env.Windows.ID(e.Window)
			if id == "" {
				return
			}
			if e.Kind == caps.WindowClosed {
				if env.Unscope != nil {
					for _, v := range env.Windows.ViewsOf(id) {
						if !v.Primary {
							env.Unscope(v.ID)
						}
					}
					env.Unscope(id)
				}
				env.Windows.Remove(id)
			}
			topic.Publish(func(enc *wire.Encoder) { encodeEvent(enc, Event{Window: id, Kind: EventKind(e.Kind)}) })
		})
	}

	registerViews(t, env)

	t.Func(IDOpen, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDOpen)
		o := decodeOptions(r)
		if err := r.Done(); err != nil {
			return err
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
			scoped, err := env.Scope(ctx.Window(), id, o.Permissions, o.HasPermissions)
			if err != nil {
				return err
			}
			if scoped {
				o.Path = krate.ScopedPath(o.Path, id)
			}
		}
		if o.Frameless && (o.Chrome.TitleBar == "" || o.Chrome.TitleBar == caps.TitleBarNative) {
			o.Chrome.TitleBar = caps.TitleBarNone
		}
		chromeOpts, err := o.Chrome.Validate()
		if err != nil {
			return krate.Errorf("bad_args", "window chrome: %w", err)
		}
		w, err := env.App.NewWindow(platform.WindowOptions{Title: o.Title, Width: o.Width, Height: o.Height, Resizable: !o.Fixed, Path: o.Path, Chrome: chromeOpts})
		if err != nil {
			return err
		}
		id = env.Windows.Add(id, w)
		if c, ok := w.(caps.WindowControls); ok {
			if o.Frameless && o.Chrome.TitleBar == caps.TitleBarNone {
				if _, ok := w.(caps.WindowChrome); !ok {
					c.SetDecorated(false)
				}
			}
			if o.AlwaysOnTop {
				c.SetAlwaysOnTop(true)
			}
			if o.HasPosition {
				c.SetPosition(o.X, o.Y)
			}
			if o.Maximized {
				c.Maximize()
			}
			if o.Fullscreen {
				c.SetFullscreen(true)
			}
			if o.PreventClose {
				c.SetPreventClose(true)
			}
		}
		out.String(id)
		return nil
	})

	t.Func(IDList, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDList)
		if err := r.Done(); err != nil {
			return err
		}
		codec.WriteStrings(out, env.Windows.List())
		return nil
	})

	t.Func(IDState, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDState)
		id := r.String()
		if err := r.Done(); err != nil {
			return err
		}
		c, _, _, err := env.Controls(id)
		if err != nil {
			return err
		}
		s := c.State()
		encodeState(out, State(s))
		return nil
	})

	// Simple commands: arguments are the window id plus an optional value.
	// parse reads the value and returns the action to run once the whole
	// call decoded cleanly.
	type act func(c caps.WindowControls, w platform.Window)
	type cmd struct {
		id    string
		core  bool // works on the core Window interface; the rest need controls
		parse func(r *codec.R) act
	}
	cmds := []cmd{
		{IDClose, true, func(r *codec.R) act { return func(c caps.WindowControls, w platform.Window) { w.Close() } }},
		{IDSetTitle, true, func(r *codec.R) act {
			s := r.String()
			return func(c caps.WindowControls, w platform.Window) { w.SetTitle(s) }
		}},
		{IDSetSize, true, func(r *codec.R) act {
			wd, ht := int(r.Int()), int(r.Int())
			return func(c caps.WindowControls, w platform.Window) { w.SetSize(wd, ht) }
		}},
		{IDNavigate, true, func(r *codec.R) act {
			s := r.String()
			return func(c caps.WindowControls, w platform.Window) { w.Navigate(s) }
		}},
		{IDSetPosition, false, func(r *codec.R) act {
			x, y := int(r.Int()), int(r.Int())
			return func(c caps.WindowControls, w platform.Window) { c.SetPosition(x, y) }
		}},
		{IDSetAlwaysOnTop, false, func(r *codec.R) act {
			b := r.Bool()
			return func(c caps.WindowControls, w platform.Window) { c.SetAlwaysOnTop(b) }
		}},
		{IDSetFullscreen, false, func(r *codec.R) act {
			b := r.Bool()
			return func(c caps.WindowControls, w platform.Window) { c.SetFullscreen(b) }
		}},
		{IDSetFrameless, false, func(r *codec.R) act {
			b := r.Bool()
			return func(c caps.WindowControls, w platform.Window) { c.SetDecorated(!b) }
		}},
		{IDMinimize, false, func(r *codec.R) act { return func(c caps.WindowControls, w platform.Window) { c.Minimize() } }},
		{IDMaximize, false, func(r *codec.R) act { return func(c caps.WindowControls, w platform.Window) { c.Maximize() } }},
		{IDRestore, false, func(r *codec.R) act { return func(c caps.WindowControls, w platform.Window) { c.Restore() } }},
		{IDFocus, false, func(r *codec.R) act { return func(c caps.WindowControls, w platform.Window) { c.Focus() } }},
		{IDShow, false, func(r *codec.R) act { return func(c caps.WindowControls, w platform.Window) { c.Show() } }},
		{IDHide, false, func(r *codec.R) act {
			release := r.More() && r.Bool()
			return func(c caps.WindowControls, w platform.Window) {
				if rel, ok := w.(caps.WindowRelease); ok && release {
					rel.Release()
					return
				}
				c.Hide()
			}
		}},
		{IDPreventClose, false, func(r *codec.R) act {
			b := r.Bool()
			return func(c caps.WindowControls, w platform.Window) { c.SetPreventClose(b) }
		}},
	}
	for _, cm := range cmds {
		cm := cm
		t.Func(cm.id, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
			r := codec.Args(args, cm.id)
			id := r.String()
			run := cm.parse(r)
			if err := r.Done(); err != nil {
				return err
			}
			var c caps.WindowControls
			var win platform.Window
			if cm.core {
				w, wid, ok := env.Windows.Get(id)
				if !ok {
					return krate.Errorf("not_found", "no window %q", wid)
				}
				win = w
				c, _ = w.(caps.WindowControls)
			} else {
				var err error
				if c, win, _, err = env.Controls(id); err != nil {
					return err
				}
			}
			run(c, win)
			out.Null()
			return nil
		})
	}
}
