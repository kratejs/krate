package window

import (
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

func encodeViewOptions(e *wire.Encoder, o ViewOptions) {
	e.BeginObject()
	e.Key("id")
	e.String(o.ID)
	e.Key("path")
	e.String(o.Path)
	e.Key("beside")
	e.String(o.Beside)
	e.Key("side")
	e.String(o.Side)
	e.Key("size")
	e.Float(o.Size)
	e.Key("minWidth")
	e.Int(int64(o.MinWidth))
	e.Key("minHeight")
	e.Int(int64(o.MinHeight))
	if o.HasPermissions {
		e.Key("permissions")
		codec.WriteStrings(e, o.Permissions)
	}
	e.EndObject()
}

func decodeViewOptions(r *codec.R) (o ViewOptions) {
	r.Object(func(k string) bool {
		switch k {
		case "id":
			o.ID = r.String()
		case "path":
			o.Path = r.String()
		case "beside":
			o.Beside = r.String()
		case "side":
			o.Side = r.String()
		case "size":
			o.Size = r.Float()
		case "minWidth":
			o.MinWidth = int(r.Int())
		case "minHeight":
			o.MinHeight = int(r.Int())
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

func encodeLayout(e *wire.Encoder, l *Layout) {
	if l == nil {
		e.Null()
		return
	}
	e.BeginObject()
	if l.First != nil || l.Second != nil {
		e.Key("direction")
		e.String(l.Direction)
		e.Key("ratio")
		e.Float(l.Ratio)
		e.Key("first")
		encodeLayout(e, l.First)
		e.Key("second")
		encodeLayout(e, l.Second)
	} else {
		e.Key("view")
		e.String(l.View)
		e.Key("minWidth")
		e.Int(int64(l.MinWidth))
		e.Key("minHeight")
		e.Int(int64(l.MinHeight))
	}
	e.EndObject()
}

// maxLayoutDecode bounds the recursion of a layout read from the wire.
const maxLayoutDecode = 24

func decodeLayout(r *codec.R) *Layout { return decodeLayoutAt(r, 0) }

func decodeLayoutAt(r *codec.R, depth int) *Layout {
	if depth > maxLayoutDecode {
		r.Failf("layout is nested too deeply")
		return nil
	}
	if r.Err() == nil && r.D.IsNull() {
		r.Skip()
		return nil
	}
	l := &Layout{}
	r.Object(func(k string) bool {
		switch k {
		case "view":
			l.View = r.String()
		case "minWidth":
			l.MinWidth = int(r.Int())
		case "minHeight":
			l.MinHeight = int(r.Int())
		case "direction":
			l.Direction = r.String()
		case "ratio":
			l.Ratio = r.Float()
		case "first":
			l.First = decodeLayoutAt(r, depth+1)
		case "second":
			l.Second = decodeLayoutAt(r, depth+1)
		default:
			return false
		}
		return true
	})
	return l
}
