package window

import (
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

func encodeOptions(e *wire.Encoder, o Options) {
	e.BeginObject()
	e.Key("title")
	e.String(o.Title)
	e.Key("width")
	e.Int(int64(o.Width))
	e.Key("height")
	e.Int(int64(o.Height))
	e.Key("path")
	e.String(o.Path)
	e.Key("fixed")
	e.Bool(o.Fixed)
	e.Key("frameless")
	e.Bool(o.Frameless)
	e.Key("alwaysOnTop")
	e.Bool(o.AlwaysOnTop)
	e.Key("fullscreen")
	e.Bool(o.Fullscreen)
	e.Key("maximized")
	e.Bool(o.Maximized)
	e.Key("x")
	e.Int(int64(o.X))
	e.Key("y")
	e.Int(int64(o.Y))
	e.Key("hasPosition")
	e.Bool(o.HasPosition)
	e.Key("preventClose")
	e.Bool(o.PreventClose)
	if o.HasPermissions {
		e.Key("permissions")
		codec.WriteStrings(e, o.Permissions)
	}
	e.EndObject()
}

func decodeOptions(r *codec.R) (o Options) {
	r.Object(func(k string) bool {
		switch k {
		case "title":
			o.Title = r.String()
		case "width":
			o.Width = int(r.Int())
		case "height":
			o.Height = int(r.Int())
		case "path":
			o.Path = r.String()
		case "fixed":
			o.Fixed = r.Bool()
		case "frameless":
			o.Frameless = r.Bool()
		case "alwaysOnTop":
			o.AlwaysOnTop = r.Bool()
		case "fullscreen":
			o.Fullscreen = r.Bool()
		case "maximized":
			o.Maximized = r.Bool()
		case "x":
			o.X = int(r.Int())
		case "y":
			o.Y = int(r.Int())
		case "hasPosition":
			o.HasPosition = r.Bool()
		case "preventClose":
			o.PreventClose = r.Bool()
		case "permissions":
			o.Permissions = r.Strings()
			o.HasPermissions = true
		default:
			return false
		}
		return true
	})
	return
}

func encodeState(e *wire.Encoder, s State) {
	e.BeginObject()
	e.Key("x")
	e.Int(int64(s.X))
	e.Key("y")
	e.Int(int64(s.Y))
	e.Key("width")
	e.Int(int64(s.Width))
	e.Key("height")
	e.Int(int64(s.Height))
	e.Key("maximized")
	e.Bool(s.Maximized)
	e.Key("minimized")
	e.Bool(s.Minimized)
	e.Key("fullscreen")
	e.Bool(s.Fullscreen)
	e.Key("alwaysOnTop")
	e.Bool(s.AlwaysOnTop)
	e.Key("decorated")
	e.Bool(s.Decorated)
	e.Key("visible")
	e.Bool(s.Visible)
	e.Key("focused")
	e.Bool(s.Focused)
	e.Key("title")
	e.String(s.Title)
	e.EndObject()
}

func decodeState(d *wire.Decoder) (s State, err error) {
	r := &codec.R{D: d, Name: "window state"}
	r.Object(func(k string) bool {
		switch k {
		case "x":
			s.X = int(r.Int())
		case "y":
			s.Y = int(r.Int())
		case "width":
			s.Width = int(r.Int())
		case "height":
			s.Height = int(r.Int())
		case "maximized":
			s.Maximized = r.Bool()
		case "minimized":
			s.Minimized = r.Bool()
		case "fullscreen":
			s.Fullscreen = r.Bool()
		case "alwaysOnTop":
			s.AlwaysOnTop = r.Bool()
		case "decorated":
			s.Decorated = r.Bool()
		case "visible":
			s.Visible = r.Bool()
		case "focused":
			s.Focused = r.Bool()
		case "title":
			s.Title = r.String()
		default:
			return false
		}
		return true
	})
	return s, r.Err()
}

func encodeEvent(e *wire.Encoder, ev Event) {
	e.BeginObject()
	e.Key("window")
	e.String(ev.Window)
	e.Key("kind")
	e.String(string(ev.Kind))
	e.EndObject()
}

func decodeEvent(d *wire.Decoder) (ev Event, err error) {
	r := &codec.R{D: d, Name: "window event"}
	r.Object(func(k string) bool {
		switch k {
		case "window":
			ev.Window = r.String()
		case "kind":
			ev.Kind = EventKind(r.String())
		default:
			return false
		}
		return true
	})
	return ev, r.Err()
}
