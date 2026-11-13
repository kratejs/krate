package window

import (
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// encodeChrome writes the chrome options of a window as keys of the options
// object, in the spelling of the @krate/app WindowOptions.
func encodeChrome(e *wire.Encoder, c caps.ChromeOptions) {
	if c.TitleBar != "" {
		e.Key("titleBar")
		e.String(c.TitleBar)
	}
	if c.Transparent {
		e.Key("transparent")
		e.Bool(true)
	}
	if c.Backdrop != "" {
		e.Key("backdrop")
		e.String(c.Backdrop)
	}
	if c.Vibrancy != "" {
		e.Key("vibrancy")
		e.String(c.Vibrancy)
	}
	if c.Overlay != nil {
		e.Key("titleBarOverlay")
		e.BeginObject()
		e.Key("color")
		e.String(c.Overlay.Color)
		e.Key("symbolColor")
		e.String(c.Overlay.SymbolColor)
		e.Key("height")
		e.Int(int64(c.Overlay.Height))
		e.EndObject()
	}
	if c.TrafficLight != nil {
		e.Key("trafficLightPosition")
		e.BeginObject()
		e.Key("x")
		e.Int(int64(c.TrafficLight.X))
		e.Key("y")
		e.Int(int64(c.TrafficLight.Y))
		e.EndObject()
	}
	if c.TabbingID != "" {
		e.Key("tabbingIdentifier")
		e.String(c.TabbingID)
	}
	if c.TabbingMode != "" {
		e.Key("tabbingMode")
		e.String(c.TabbingMode)
	}
}

// decodeChromeKey reads one chrome key of the options object; it reports
// false for any other key.
func decodeChromeKey(r *codec.R, k string, c *caps.ChromeOptions) bool {
	switch k {
	case "titleBar":
		c.TitleBar = r.String()
	case "transparent":
		c.Transparent = r.Bool()
	case "backdrop":
		c.Backdrop = r.String()
	case "vibrancy":
		c.Vibrancy = r.String()
	case "titleBarOverlay":
		switch r.D.Kind() {
		case wire.KindNull:
			r.Skip()
		case wire.KindBool:
			if r.Bool() {
				c.Overlay = &caps.TitleBarOverlay{}
			}
		default:
			var o caps.TitleBarOverlay
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
			c.Overlay = &o
		}
	case "trafficLightPosition":
		var p caps.Point
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
		c.TrafficLight = &p
	case "tabbingIdentifier":
		c.TabbingID = r.String()
	case "tabbingMode":
		c.TabbingMode = r.String()
	default:
		return false
	}
	return true
}
