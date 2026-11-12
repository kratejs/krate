package chrome

import (
	"fmt"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// unsupported builds the error for a window or app that lacks a capability.
func unsupported(what string) error {
	return fmt.Errorf("%w: %s", krate.ErrUnsupported, what)
}

// target looks up the platform window with this SDK id.
func target(env *bridge.Env, id string) (platform.Window, string, error) {
	win, wid, ok := env.Windows.Get(id)
	if !ok {
		return nil, "", krate.Errorf("not_found", "no window %q", id)
	}
	return win, wid, nil
}

func viaChrome(env *bridge.Env, id string) (caps.WindowChrome, string, error) {
	win, wid, err := target(env, id)
	if err != nil {
		return nil, "", err
	}
	c, ok := win.(caps.WindowChrome)
	if !ok {
		return nil, "", unsupported("window chrome is not available on this platform")
	}
	return c, wid, nil
}

// need requires the optional capability T of the window, with a message that
// names the platform feature.
func need[T any](env *bridge.Env, id, what string) (T, string, error) {
	var zero T
	win, wid, err := target(env, id)
	if err != nil {
		return zero, "", err
	}
	v, ok := win.(T)
	if !ok {
		return zero, "", unsupported(what)
	}
	return v, wid, nil
}

// Register adds the platform-side operations to t and routes platform chrome
// events to the "chrome" topic.
func Register(t *bridge.Table, env *bridge.Env) {
	topic := env.Topic("chrome")
	t.Stream(IDEvents, PermissionWindow, true, topic.Stream())
	if ev, ok := env.App.(caps.ChromeEvents); ok {
		ev.OnChromeEvent(func(e caps.ChromeEvent) {
			id := env.Windows.ID(e.Window)
			if id == "" {
				return
			}
			topic.Publish(func(enc *wire.Encoder) {
				encodeEvent(enc, Event{Window: id, Kind: e.Kind, ID: e.ID})
			})
		})
	}

	// win registers an operation whose first argument is a window id; run
	// reads the rest and returns the action to perform once decoding is clean.
	win := func(id, perm string, run func(r *codec.R, wid string) (func(out *wire.Encoder) error, error)) {
		t.Func(id, perm, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
			r := codec.Args(args, id)
			wid := r.String()
			act, err := run(r, wid)
			if err != nil {
				return err
			}
			if err := r.Done(); err != nil {
				return err
			}
			return act(out)
		})
	}
	void := func(f func() error) (func(*wire.Encoder) error, error) {
		return func(out *wire.Encoder) error {
			if err := f(); err != nil {
				return err
			}
			out.Null()
			return nil
		}, nil
	}

	win(IDState, PermissionWindow, func(r *codec.R, wid string) (func(*wire.Encoder) error, error) {
		return func(out *wire.Encoder) error {
			c, _, err := viaChrome(env, wid)
			if err != nil {
				return err
			}
			encodeState(out, stateFromCaps(c.ChromeState()))
			return nil
		}, nil
	})
	win(IDSetTitleBar, PermissionWindow, func(r *codec.R, wid string) (func(*wire.Encoder) error, error) {
		style := r.String()
		return void(func() error {
			if _, ok := caps.NormalizeTitleBar(style); !ok || style == "" {
				return krate.Errorf("bad_args", "titleBar must be native, hidden or none, got %q", style)
			}
			c, _, err := viaChrome(env, wid)
			if err != nil {
				return err
			}
			return c.SetTitleBar(style)
		})
	})
	win(IDSetBackdrop, PermissionWindow, func(r *codec.R, wid string) (func(*wire.Encoder) error, error) {
		name := r.String()
		return void(func() error {
			if _, ok := caps.BackdropType(name); !ok {
				return krate.Errorf("bad_args", "backdrop must be one of %v, got %q", caps.BackdropNames(), name)
			}
			c, _, err := viaChrome(env, wid)
			if err != nil {
				return err
			}
			return c.SetBackdrop(name)
		})
	})
	win(IDSetVibrancy, PermissionWindow, func(r *codec.R, wid string) (func(*wire.Encoder) error, error) {
		m := r.String()
		return void(func() error {
			if _, ok := caps.VibrancyMaterial(m); !ok && m != "" {
				return krate.Errorf("bad_args", "vibrancy must be one of %v or empty, got %q", caps.VibrancyNames(), m)
			}
			c, _, err := viaChrome(env, wid)
			if err != nil {
				return err
			}
			return c.SetVibrancy(m)
		})
	})
	win(IDSetTitleBarOverlay, PermissionWindow, func(r *codec.R, wid string) (func(*wire.Encoder) error, error) {
		var o *caps.TitleBarOverlay
		if r.More() && r.D.Kind() == wire.KindNull {
			r.Skip()
		} else if r.More() {
			ov := decodeOverlay(r).toCaps()
			o = &ov
		}
		return void(func() error {
			if o != nil {
				n, err := o.Normalize()
				if err != nil {
					return krate.Errorf("bad_args", "%w", err)
				}
				o = &n
			}
			c, _, err := viaChrome(env, wid)
			if err != nil {
				return err
			}
			return c.SetTitleBarOverlay(o)
		})
	})
	win(IDSetTrafficLightPos, PermissionWindow, func(r *codec.R, wid string) (func(*wire.Encoder) error, error) {
		x, y := int(r.Int()), int(r.Int())
		return void(func() error {
			if x < 0 || y < 0 || x > 1000 || y > 1000 {
				return krate.Errorf("bad_args", "traffic light position (%d, %d) is out of range 0..1000", x, y)
			}
			c, _, err := viaChrome(env, wid)
			if err != nil {
				return err
			}
			return c.SetTrafficLightPosition(caps.Point{X: x, Y: y})
		})
	})
	win(IDStartDrag, PermissionWindow, func(r *codec.R, wid string) (func(*wire.Encoder) error, error) {
		return void(func() error {
			c, _, err := viaChrome(env, wid)
			if err != nil {
				return err
			}
			return c.StartDrag()
		})
	})
	win(IDToggleMaximize, PermissionWindow, func(r *codec.R, wid string) (func(*wire.Encoder) error, error) {
		return void(func() error {
			c, _, err := viaChrome(env, wid)
			if err != nil {
				return err
			}
			return c.ToggleMaximize()
		})
	})
	win(IDSetDocumentEdited, PermissionWindow, func(r *codec.R, wid string) (func(*wire.Encoder) error, error) {
		on := r.Bool()
		return void(func() error {
			d, _, err := need[caps.WindowDocument](env, wid, "the document-edited state is a macOS feature")
			if err != nil {
				return err
			}
			return d.SetDocumentEdited(on)
		})
	})
	win(IDSetRepresentedFile, PermissionWindow, func(r *codec.R, wid string) (func(*wire.Encoder) error, error) {
		path := r.String()
		return void(func() error {
			d, _, err := need[caps.WindowDocument](env, wid, "the represented file (proxy icon) is a macOS feature")
			if err != nil {
				return err
			}
			if path != "" && !isAbs(path) {
				return krate.Errorf("bad_args", "represented file %q must be an absolute path", path)
			}
			return d.SetRepresentedFile(path)
		})
	})
	win(IDAttachSheet, PermissionWindow, func(r *codec.R, wid string) (func(*wire.Encoder) error, error) {
		child := r.String()
		return void(func() error {
			s, _, err := need[caps.WindowSheets](env, wid, "sheets are a macOS feature")
			if err != nil {
				return err
			}
			if child == wid {
				return krate.Errorf("bad_args", "a window cannot be a sheet of itself")
			}
			ref := env.Windows.Ref(child)
			if ref == 0 {
				return krate.Errorf("not_found", "no window %q", child)
			}
			return s.AttachSheet(ref)
		})
	})
	win(IDEndSheet, PermissionWindow, func(r *codec.R, wid string) (func(*wire.Encoder) error, error) {
		return void(func() error {
			s, _, err := need[caps.WindowSheets](env, wid, "sheets are a macOS feature")
			if err != nil {
				return err
			}
			return s.EndSheet()
		})
	})
	win(IDSetTabbing, PermissionWindow, func(r *codec.R, wid string) (func(*wire.Encoder) error, error) {
		ident, mode := r.String(), r.String()
		return void(func() error {
			if _, ok := caps.TabbingModeValue(mode); !ok {
				return krate.Errorf("bad_args", "tabbing mode must be auto, preferred or disallowed, got %q", mode)
			}
			if len(ident) > 128 {
				return krate.Errorf("bad_args", "tabbing identifier is longer than 128 characters")
			}
			tb, _, err := need[caps.WindowTabs](env, wid, "native window tabs are a macOS feature")
			if err != nil {
				return err
			}
			return tb.SetTabbing(ident, mode)
		})
	})
	win(IDMergeAllWindows, PermissionWindow, func(r *codec.R, wid string) (func(*wire.Encoder) error, error) {
		return void(func() error {
			tb, _, err := need[caps.WindowTabs](env, wid, "native window tabs are a macOS feature")
			if err != nil {
				return err
			}
			return tb.MergeAllWindows()
		})
	})
	win(IDMoveTabToNewWindow, PermissionWindow, func(r *codec.R, wid string) (func(*wire.Encoder) error, error) {
		return void(func() error {
			tb, _, err := need[caps.WindowTabs](env, wid, "native window tabs are a macOS feature")
			if err != nil {
				return err
			}
			return tb.MoveTabToNewWindow()
		})
	})
	win(IDTabs, PermissionWindow, func(r *codec.R, wid string) (func(*wire.Encoder) error, error) {
		return func(out *wire.Encoder) error {
			tb, _, err := need[caps.WindowTabs](env, wid, "native window tabs are a macOS feature")
			if err != nil {
				return err
			}
			var ids []string
			for _, ref := range tb.Tabs() {
				if id := env.Windows.ID(ref); id != "" {
					ids = append(ids, id)
				}
			}
			codec.WriteStrings(out, ids)
			return nil
		}, nil
	})
	win(IDSetThumbButtons, PermissionWindow, func(r *codec.R, wid string) (func(*wire.Encoder) error, error) {
		bs := decodeThumbs(r)
		return void(func() error {
			cb := make([]caps.ThumbButton, len(bs))
			for i, b := range bs {
				cb[i] = caps.ThumbButton{ID: b.ID, Tooltip: b.Tooltip, Icon: b.Icon, Disabled: b.Disabled, Hidden: b.Hidden, DismissOnClick: b.DismissOnClick}
			}
			if err := caps.ValidateThumbButtons(cb); err != nil {
				return krate.Errorf("bad_args", "%w", err)
			}
			tb, _, err := need[caps.WindowThumbBar](env, wid, "taskbar thumbnail buttons are a Windows feature")
			if err != nil {
				return err
			}
			return tb.SetThumbButtons(cb)
		})
	})
	win(IDShare, PermissionShare, func(r *codec.R, wid string) (func(*wire.Encoder) error, error) {
		items := decodeShare(r)
		return void(func() error {
			ci := caps.ShareItems{Text: items.Text, URLs: items.URLs, Files: items.Files}
			if err := ci.Validate(); err != nil {
				return krate.Errorf("bad_args", "%w", err)
			}
			sh, err := bridge.Cap[caps.Sharer](env.App, "the share menu")
			if err != nil {
				return unsupported("the system share menu is a macOS feature")
			}
			_, _, terr := target(env, wid)
			if terr != nil {
				return terr
			}
			return sh.Share(env.Windows.Ref(wid), ci)
		})
	})

	t.Func(IDSetJumpList, PermissionApp, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSetJumpList)
		ts := decodeTasks(r)
		if err := r.Done(); err != nil {
			return err
		}
		jl, err := bridge.Cap[caps.JumpLister](env.App, "jump lists")
		if err != nil {
			return unsupported("jump lists are a Windows feature")
		}
		ct := make([]caps.JumpTask, len(ts))
		for i, t := range ts {
			ct[i] = caps.JumpTask{Title: t.Title, Args: t.Args, Description: t.Description, IconPath: t.IconPath, IconIndex: t.IconIndex}
		}
		if err := caps.ValidateJumpTasks(ct); err != nil {
			return krate.Errorf("bad_args", "%w", err)
		}
		if err := jl.SetJumpList(ct); err != nil {
			return err
		}
		out.Null()
		return nil
	})
	t.Func(IDClearJumpList, PermissionApp, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDClearJumpList)
		if err := r.Done(); err != nil {
			return err
		}
		jl, err := bridge.Cap[caps.JumpLister](env.App, "jump lists")
		if err != nil {
			return unsupported("jump lists are a Windows feature")
		}
		if err := jl.ClearJumpList(); err != nil {
			return err
		}
		out.Null()
		return nil
	})
	t.Func(IDSetActivationPolicy, PermissionApp, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSetActivationPolicy)
		p := r.String()
		if err := r.Done(); err != nil {
			return err
		}
		if _, ok := caps.ActivationPolicyValue(p); !ok {
			return krate.Errorf("bad_args", "activation policy must be regular, accessory or prohibited, got %q", p)
		}
		ap, err := bridge.Cap[caps.AppPresence](env.App, "the activation policy")
		if err != nil {
			return unsupported("the activation policy (menu-bar-only apps) is a macOS feature")
		}
		if err := ap.SetActivationPolicy(p); err != nil {
			return err
		}
		out.Null()
		return nil
	})
	t.Func(IDSetDockVisible, PermissionApp, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSetDockVisible)
		on := r.Bool()
		if err := r.Done(); err != nil {
			return err
		}
		ap, err := bridge.Cap[caps.AppPresence](env.App, "the dock icon")
		if err != nil {
			return unsupported("the dock icon is a macOS feature")
		}
		if err := ap.SetDockVisible(on); err != nil {
			return err
		}
		out.Null()
		return nil
	})
}

func isAbs(p string) bool {
	return caps.ShareItems{Files: []string{p}}.Validate() == nil
}

func stateFromCaps(s caps.ChromeState) State {
	st := State{
		TitleBar: s.TitleBar, Transparent: s.Transparent, Backdrop: s.Backdrop, Vibrancy: s.Vibrancy,
		DocumentEdited: s.DocumentEdited, RepresentedFile: s.RepresentedFile,
		TabbingID: s.TabbingID, TabbingMode: s.TabbingMode, Tabs: s.Tabs, Sheet: s.Sheet,
		ThumbButtons: s.ThumbButtons, NativeRegions: s.NativeRegions, Features: s.Features,
	}
	if s.Overlay != nil {
		st.Overlay = &Overlay{Color: s.Overlay.Color, SymbolColor: s.Overlay.SymbolColor, Height: s.Overlay.Height}
	}
	if s.TrafficLight != nil {
		st.TrafficLight = &Point{X: s.TrafficLight.X, Y: s.TrafficLight.Y}
	}
	return st
}

// Supports answers app.Supports for the capabilities "chrome" and
// "chrome:<feature>".
func Supports(env *bridge.Env, capability string) (ok, handled bool) {
	switch capability {
	case "chrome":
		return true, true
	case "chrome:jumpList":
		_, ok := env.App.(caps.JumpLister)
		return ok, true
	case "chrome:share":
		_, ok := env.App.(caps.Sharer)
		return ok, true
	case "chrome:dock":
		_, ok := env.App.(caps.AppPresence)
		return ok, true
	}
	const prefix = "chrome:"
	if len(capability) <= len(prefix) || capability[:len(prefix)] != prefix {
		return false, false
	}
	win, _, ok2 := env.Windows.Get("")
	if !ok2 {
		return false, true
	}
	c, isChrome := win.(caps.WindowChrome)
	if !isChrome {
		return false, true
	}
	return c.ChromeState().HasFeature(capability[len(prefix):]), true
}
