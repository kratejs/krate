package window

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

var viewIDRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,31}$`)

// MessageEvent is the DOM event a page receives for a posted message.
const MessageEvent = "krate:view-message"

func encodeViewEvent(e *wire.Encoder, ev ViewEvent) {
	e.BeginObject()
	e.Key("view")
	e.String(ev.View)
	e.Key("window")
	e.String(ev.Window)
	e.Key("kind")
	e.String(string(ev.Kind))
	e.Key("path")
	e.String(ev.Path)
	e.EndObject()
}

// messageScript builds the script that delivers a message to a page. The
// payload goes through JSON.parse of a quoted string, so nothing in it can
// become code.
func messageScript(from string, data []byte) string {
	detail, _ := json.Marshal(map[string]json.RawMessage{
		"from": mustJSON(from),
		"data": data,
	})
	return `try{window.dispatchEvent(new CustomEvent(` + string(mustJSON(MessageEvent)) +
		`,{detail:JSON.parse(` + string(mustJSON(string(detail))) + `)}))}catch(e){}`
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func registerViews(t *bridge.Table, env *bridge.Env) {
	topic := env.Topic("windowviews")
	t.Stream(IDViewEvents, Permission, true, topic.Stream())

	publish := func(rec bridge.ViewRec, e caps.ViewEvent) {
		topic.Publish(func(enc *wire.Encoder) {
			encodeViewEvent(enc, ViewEvent{View: rec.ID, Window: rec.Window, Kind: ViewEventKind(e.Kind), Path: e.Path})
		})
	}
	forget := func(rec bridge.ViewRec) {
		env.Windows.RemoveView(rec.ID)
		if !rec.Primary && env.Unscope != nil {
			env.Unscope(rec.ID)
		}
	}
	// Events of a view the SDK has not registered yet wait until it is.
	env.Windows.OnView(func(rec bridge.ViewRec, held []caps.ViewEvent) {
		for _, e := range held {
			publish(rec, e)
			if e.Kind == caps.ViewClosed || e.Kind == caps.ViewRemoved {
				forget(rec)
			}
		}
	})
	if ve, ok := env.App.(caps.ViewEvents); ok {
		ve.OnViewEvent(func(e caps.ViewEvent) {
			rec, ok := env.Windows.ViewByRef(e.View)
			if !ok {
				env.Windows.Hold(e)
				return
			}
			publish(rec, e)
			if e.Kind == caps.ViewClosed || e.Kind == caps.ViewRemoved {
				forget(rec)
			}
		})
	}

	views := func(window string) (caps.WindowViews, string, error) {
		win, id, ok := env.Windows.Get(window)
		if !ok {
			return nil, "", krate.Errorf("not_found", "no window %q", window)
		}
		wv, ok := win.(caps.WindowViews)
		if !ok {
			return nil, "", fmt.Errorf("%w: views are not available on this platform", krate.ErrUnsupported)
		}
		return wv, id, nil
	}
	// viewOf finds a registered view and the views interface of its window.
	viewOf := func(id string) (bridge.ViewRec, caps.WindowViews, error) {
		rec, ok := env.Windows.View(id)
		if !ok {
			return rec, nil, krate.Errorf("not_found", "no view %q", id)
		}
		wv, _, err := views(rec.Window)
		return rec, wv, err
	}

	t.Func(IDAddView, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDAddView)
		winID := r.String()
		o := decodeViewOptions(r)
		if err := r.Done(); err != nil {
			return err
		}
		wv, winID, err := views(winID)
		if err != nil {
			return err
		}
		if o.Size < 0 || o.Size >= 1 {
			return krate.Errorf("bad_args", "%s: size %v must be between 0 and 1", IDAddView, o.Size)
		}
		if o.MinWidth < 0 || o.MinHeight < 0 {
			return krate.Errorf("bad_args", "%s: minimum sizes cannot be negative", IDAddView)
		}
		co := caps.ViewOptions{Path: o.Path, Axis: caps.Horizontal, Share: o.Size, MinWidth: o.MinWidth, MinHeight: o.MinHeight}
		switch o.Side {
		case "", Right:
		case Left:
			co.NewFirst = true
		case Bottom:
			co.Axis = caps.Vertical
		case Top:
			co.Axis, co.NewFirst = caps.Vertical, true
		default:
			return krate.Errorf("bad_args", "%s: side %q must be right, left, top or bottom", IDAddView, o.Side)
		}
		if o.Beside != "" {
			rec, ok := env.Windows.View(o.Beside)
			if !ok || rec.Window != winID {
				return krate.Errorf("not_found", "no view %q in window %q", o.Beside, winID)
			}
			co.Beside = rec.Ref
		}
		id := o.ID
		if id == "" {
			id = env.Windows.ReserveView()
		} else if !viewIDRe.MatchString(id) {
			return krate.Errorf("bad_args", "%s: view id %q must match %s", IDAddView, id, viewIDRe)
		} else if env.Windows.IDTaken(id) {
			return krate.Errorf("already_exists", "an id %q is already in use by a window or view", id)
		}
		path := o.Path
		if env.Scope != nil {
			scoped, err := env.Scope(ctx.Window(), id, o.Permissions, o.HasPermissions)
			if err != nil {
				return err
			}
			if scoped {
				path = krate.ScopedPath(path, id)
			}
		}
		co.Path = path
		ref, err := wv.AddView(co)
		if err != nil {
			if env.Unscope != nil {
				env.Unscope(id)
			}
			return err
		}
		if err := env.Windows.AddView(id, winID, ref, false); err != nil {
			_ = wv.RemoveView(ref) // best effort; the AddView error is returned
			if env.Unscope != nil {
				env.Unscope(id)
			}
			return krate.Errorf("already_exists", "%v", err)
		}
		out.String(id)
		return nil
	})

	t.Func(IDRemoveView, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDRemoveView)
		id := r.String()
		if err := r.Done(); err != nil {
			return err
		}
		rec, wv, err := viewOf(id)
		if err != nil {
			return err
		}
		if rec.Primary {
			return krate.Errorf("bad_args", "view %q is the primary view of window %q; close the window instead", id, rec.Window)
		}
		if err := wv.RemoveView(rec.Ref); err != nil {
			return err
		}
		out.Null()
		return nil
	})

	t.Func(IDViews, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDViews)
		window := r.String()
		if err := r.Done(); err != nil {
			return err
		}
		if window != "" {
			if _, id, ok := env.Windows.Get(window); !ok {
				return krate.Errorf("not_found", "no window %q", window)
			} else {
				window = id
			}
		}
		out.BeginArray()
		for _, rec := range env.Windows.ViewsOf(window) {
			path := ""
			if wv, _, err := views(rec.Window); err == nil {
				for _, vi := range wv.Views() {
					if vi.Ref == rec.Ref {
						path = vi.Path
					}
				}
			}
			out.BeginObject()
			out.Key("id")
			out.String(rec.ID)
			out.Key("window")
			out.String(rec.Window)
			out.Key("path")
			out.String(path)
			out.Key("primary")
			out.Bool(rec.Primary)
			out.EndObject()
		}
		out.EndArray()
		return nil
	})

	t.Func(IDSetLayout, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSetLayout)
		winID := r.String()
		l := decodeLayout(r)
		if err := r.Done(); err != nil {
			return err
		}
		wv, winID, err := views(winID)
		if err != nil {
			return err
		}
		node, err := layoutToCaps(env, winID, l)
		if err != nil {
			return krate.Errorf("bad_args", "%s: %v", IDSetLayout, err)
		}
		if err := wv.SetLayout(node); err != nil {
			return krate.Errorf("bad_args", "%s: %v", IDSetLayout, err)
		}
		out.Null()
		return nil
	})

	t.Func(IDLayout, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDLayout)
		winID := r.String()
		if err := r.Done(); err != nil {
			return err
		}
		wv, _, err := views(winID)
		if err != nil {
			return err
		}
		encodeLayout(out, layoutFromCaps(env, wv.Layout()))
		return nil
	})

	t.Func(IDFocusView, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDFocusView)
		id := r.String()
		if err := r.Done(); err != nil {
			return err
		}
		rec, wv, err := viewOf(id)
		if err != nil {
			return err
		}
		if err := wv.FocusView(rec.Ref); err != nil {
			return err
		}
		out.Null()
		return nil
	})

	t.Func(IDNavigateView, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDNavigateView)
		id, path := r.String(), r.String()
		if err := r.Done(); err != nil {
			return err
		}
		rec, wv, err := viewOf(id)
		if err != nil {
			return err
		}
		if err := wv.NavigateView(rec.Ref, path); err != nil {
			return err
		}
		out.Null()
		return nil
	})

	t.Func(IDPostMessage, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDPostMessage)
		id := r.String()
		var raw []byte
		if r.Err() == nil {
			b, err := args.RawValue()
			if err != nil {
				r.Failf("%v", err)
			}
			raw = b
		}
		if err := r.Done(); err != nil {
			return err
		}
		if len(raw) > MaxMessage {
			return krate.Errorf("bad_args", "%s: message is %d bytes; the limit is %d", IDPostMessage, len(raw), MaxMessage)
		}
		if !json.Valid(raw) {
			return krate.Errorf("bad_args", "%s: message must be JSON data", IDPostMessage)
		}
		rec, wv, err := viewOf(id)
		if err != nil {
			return err
		}
		// ctx.Window() is the id of the sending view when the call came from a
		// page (the platform binds it), and empty for host code.
		if err := wv.EvalView(rec.Ref, messageScript(ctx.Window(), raw)); err != nil {
			return err
		}
		out.Null()
		return nil
	})
}

func layoutToCaps(env *bridge.Env, window string, l *Layout) (*caps.LayoutNode, error) {
	if l == nil {
		return nil, fmt.Errorf("layout is missing")
	}
	if l.First != nil || l.Second != nil {
		if l.First == nil || l.Second == nil {
			return nil, fmt.Errorf("a split needs both first and second")
		}
		first, err := layoutToCaps(env, window, l.First)
		if err != nil {
			return nil, err
		}
		second, err := layoutToCaps(env, window, l.Second)
		if err != nil {
			return nil, err
		}
		return &caps.LayoutNode{Axis: caps.Axis(l.Direction), Ratio: l.Ratio, First: first, Second: second}, nil
	}
	rec, ok := env.Windows.View(l.View)
	if !ok || rec.Window != window {
		return nil, fmt.Errorf("no view %q in window %q", l.View, window)
	}
	return &caps.LayoutNode{View: rec.Ref, MinWidth: l.MinWidth, MinHeight: l.MinHeight}, nil
}

func layoutFromCaps(env *bridge.Env, n *caps.LayoutNode) *Layout {
	if n == nil {
		return nil
	}
	if n.IsLeaf() {
		id := strconv.FormatUint(uint64(n.View), 10)
		if rec, ok := env.Windows.ViewByRef(n.View); ok {
			id = rec.ID
		}
		return &Layout{View: id, MinWidth: n.MinWidth, MinHeight: n.MinHeight}
	}
	return &Layout{Direction: string(n.Axis), Ratio: n.Ratio, First: layoutFromCaps(env, n.First), Second: layoutFromCaps(env, n.Second)}
}
