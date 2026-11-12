package chrome

import (
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

func encodeOverlay(e *wire.Encoder, o Overlay) {
	e.BeginObject()
	e.Key("color")
	e.String(o.Color)
	e.Key("symbolColor")
	e.String(o.SymbolColor)
	e.Key("height")
	e.Int(int64(o.Height))
	e.EndObject()
}

func decodeOverlay(r *codec.R) (o Overlay) {
	r.Object(func(k string) bool {
		switch k {
		case "color":
			o.Color = r.String()
		case "symbolColor":
			o.SymbolColor = r.String()
		case "height":
			o.Height = int(r.Int())
		default:
			return false
		}
		return true
	})
	return
}

func encodeState(e *wire.Encoder, s State) {
	e.BeginObject()
	e.Key("titleBar")
	e.String(s.TitleBar)
	e.Key("transparent")
	e.Bool(s.Transparent)
	e.Key("backdrop")
	e.String(s.Backdrop)
	e.Key("vibrancy")
	e.String(s.Vibrancy)
	e.Key("overlay")
	if s.Overlay != nil {
		encodeOverlay(e, *s.Overlay)
	} else {
		e.Null()
	}
	e.Key("trafficLight")
	if s.TrafficLight != nil {
		e.BeginObject()
		e.Key("x")
		e.Int(int64(s.TrafficLight.X))
		e.Key("y")
		e.Int(int64(s.TrafficLight.Y))
		e.EndObject()
	} else {
		e.Null()
	}
	e.Key("documentEdited")
	e.Bool(s.DocumentEdited)
	e.Key("representedFile")
	e.String(s.RepresentedFile)
	e.Key("tabbingId")
	e.String(s.TabbingID)
	e.Key("tabbingMode")
	e.String(s.TabbingMode)
	e.Key("tabs")
	e.Int(int64(s.Tabs))
	e.Key("sheet")
	e.Bool(s.Sheet)
	e.Key("thumbButtons")
	e.Int(int64(s.ThumbButtons))
	e.Key("nativeRegions")
	e.Bool(s.NativeRegions)
	e.Key("features")
	codec.WriteStrings(e, s.Features)
	e.EndObject()
}

func decodeState(d *wire.Decoder) (s State, err error) {
	r := &codec.R{D: d, Name: "chrome state"}
	r.Object(func(k string) bool {
		switch k {
		case "titleBar":
			s.TitleBar = r.String()
		case "transparent":
			s.Transparent = r.Bool()
		case "backdrop":
			s.Backdrop = r.String()
		case "vibrancy":
			s.Vibrancy = r.String()
		case "overlay":
			if d.Kind() == wire.KindNull {
				r.Skip()
			} else {
				o := decodeOverlay(r)
				s.Overlay = &o
			}
		case "trafficLight":
			if d.Kind() == wire.KindNull {
				r.Skip()
			} else {
				var p Point
				r.Object(func(k string) bool {
					switch k {
					case "x":
						p.X = int(r.Int())
					case "y":
						p.Y = int(r.Int())
					default:
						return false
					}
					return true
				})
				s.TrafficLight = &p
			}
		case "documentEdited":
			s.DocumentEdited = r.Bool()
		case "representedFile":
			s.RepresentedFile = r.String()
		case "tabbingId":
			s.TabbingID = r.String()
		case "tabbingMode":
			s.TabbingMode = r.String()
		case "tabs":
			s.Tabs = int(r.Int())
		case "sheet":
			s.Sheet = r.Bool()
		case "thumbButtons":
			s.ThumbButtons = int(r.Int())
		case "nativeRegions":
			s.NativeRegions = r.Bool()
		case "features":
			s.Features = r.Strings()
		default:
			return false
		}
		return true
	})
	return s, r.Err()
}

func encodeThumbs(e *wire.Encoder, bs []ThumbButton) {
	e.BeginArray()
	for _, b := range bs {
		e.BeginObject()
		e.Key("id")
		e.String(b.ID)
		e.Key("tooltip")
		e.String(b.Tooltip)
		e.Key("icon")
		e.Bytes(b.Icon)
		e.Key("disabled")
		e.Bool(b.Disabled)
		e.Key("hidden")
		e.Bool(b.Hidden)
		e.Key("dismissOnClick")
		e.Bool(b.DismissOnClick)
		e.EndObject()
	}
	e.EndArray()
}

func decodeThumbs(r *codec.R) (out []ThumbButton) {
	r.Array(func() {
		var b ThumbButton
		r.Object(func(k string) bool {
			switch k {
			case "id":
				b.ID = r.String()
			case "tooltip":
				b.Tooltip = r.String()
			case "icon":
				b.Icon = r.Bytes()
			case "disabled":
				b.Disabled = r.Bool()
			case "hidden":
				b.Hidden = r.Bool()
			case "dismissOnClick":
				b.DismissOnClick = r.Bool()
			default:
				return false
			}
			return true
		})
		out = append(out, b)
	})
	return
}

func encodeTasks(e *wire.Encoder, ts []JumpTask) {
	e.BeginArray()
	for _, t := range ts {
		e.BeginObject()
		e.Key("title")
		e.String(t.Title)
		e.Key("args")
		codec.WriteStrings(e, t.Args)
		e.Key("description")
		e.String(t.Description)
		e.Key("iconPath")
		e.String(t.IconPath)
		e.Key("iconIndex")
		e.Int(int64(t.IconIndex))
		e.EndObject()
	}
	e.EndArray()
}

func decodeTasks(r *codec.R) (out []JumpTask) {
	r.Array(func() {
		var t JumpTask
		r.Object(func(k string) bool {
			switch k {
			case "title":
				t.Title = r.String()
			case "args":
				t.Args = r.Strings()
			case "description":
				t.Description = r.String()
			case "iconPath":
				t.IconPath = r.String()
			case "iconIndex":
				t.IconIndex = int(r.Int())
			default:
				return false
			}
			return true
		})
		out = append(out, t)
	})
	return
}

func encodeShare(e *wire.Encoder, s ShareItems) {
	e.BeginObject()
	e.Key("text")
	e.String(s.Text)
	e.Key("urls")
	codec.WriteStrings(e, s.URLs)
	e.Key("files")
	codec.WriteStrings(e, s.Files)
	e.EndObject()
}

func decodeShare(r *codec.R) (s ShareItems) {
	r.Object(func(k string) bool {
		switch k {
		case "text":
			s.Text = r.String()
		case "urls":
			s.URLs = r.Strings()
		case "files":
			s.Files = r.Strings()
		default:
			return false
		}
		return true
	})
	return
}

func encodeEvent(e *wire.Encoder, ev Event) {
	e.BeginObject()
	e.Key("window")
	e.String(ev.Window)
	e.Key("kind")
	e.String(ev.Kind)
	e.Key("id")
	e.String(ev.ID)
	e.EndObject()
}

func decodeEvent(d *wire.Decoder) (ev Event, err error) {
	r := &codec.R{D: d, Name: "chrome event"}
	r.Object(func(k string) bool {
		switch k {
		case "window":
			ev.Window = r.String()
		case "kind":
			ev.Kind = r.String()
		case "id":
			ev.ID = r.String()
		default:
			return false
		}
		return true
	})
	return ev, r.Err()
}
