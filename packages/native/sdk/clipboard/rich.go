package clipboard

import (
	"context"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// Host function ids for rich formats.
const (
	IDWrite       = "krate/clipboard.write"
	IDReadHTML    = "krate/clipboard.readHtml"
	IDReadRTF     = "krate/clipboard.readRtf"
	IDFormats     = "krate/clipboard.formats"
	IDReadFormat  = "krate/clipboard.readFormat"
	IDWriteFormat = "krate/clipboard.writeFormat"
)

// Data is a set of representations of one clipboard value. Write puts every
// non-empty field on the clipboard together, so a paste target takes the
// richest one it understands and a plain text field takes Text.
type Data struct {
	Text string `json:"text,omitempty"`
	HTML string `json:"html,omitempty"`
	RTF  string `json:"rtf,omitempty"`
}

// Common format names for ReadFormat, WriteFormat and Formats.
const (
	FormatText = caps.FormatText
	FormatHTML = caps.FormatHTML
	FormatRTF  = caps.FormatRTF
	FormatPNG  = caps.FormatPNG
)

// Write replaces the clipboard with d, all representations on one entry.
func Write(d Data) error {
	return bridge.CallDo(context.Background(), IDWrite, func(e *wire.Encoder) {
		e.BeginObject()
		e.Key("text")
		e.String(d.Text)
		e.Key("html")
		e.String(d.HTML)
		e.Key("rtf")
		e.String(d.RTF)
		e.EndObject()
	})
}

func readString(id string) (string, error) {
	d, err := bridge.Call(context.Background(), id, nil)
	if err != nil {
		return "", err
	}
	if d.IsNull() {
		return "", nil
	}
	return d.String()
}

// ReadHTML returns the HTML fragment on the clipboard, or "".
func ReadHTML() (string, error) { return readString(IDReadHTML) }

// ReadRTF returns the RTF document on the clipboard, or "".
func ReadRTF() (string, error) { return readString(IDReadRTF) }

// Formats lists the formats on the clipboard by MIME type where the platform
// has one ("text/plain", "text/html", "text/rtf", "image/png"), otherwise by
// its native name.
func Formats() ([]string, error) {
	d, err := bridge.Call(context.Background(), IDFormats, nil)
	if err != nil {
		return nil, err
	}
	return codec.ReadStrings(d)
}

// ReadFormat returns the bytes of one format, or nil when it is not on the
// clipboard. A MIME name is mapped to the platform's own name.
func ReadFormat(name string) ([]byte, error) {
	d, err := bridge.Call(context.Background(), IDReadFormat, func(e *wire.Encoder) { e.String(name) })
	if err != nil {
		return nil, err
	}
	if d.IsNull() {
		return nil, nil
	}
	return d.Bytes()
}

// WriteFormat replaces the clipboard with one format.
func WriteFormat(name string, data []byte) error {
	return bridge.CallDo(context.Background(), IDWriteFormat, func(e *wire.Encoder) {
		e.String(name)
		e.Bytes(data)
	})
}

func registerRich(t *bridge.Table, env *bridge.Env) {
	rich := func() (caps.RichClipboard, error) {
		return bridge.Cap[caps.RichClipboard](env.App, "rich clipboard formats")
	}
	t.Func(IDWrite, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDWrite)
		var d caps.ClipboardData
		r.Object(func(k string) bool {
			switch k {
			case "text":
				d.Text = r.String()
			case "html":
				d.HTML = r.String()
			case "rtf":
				d.RTF = r.String()
			default:
				return false
			}
			return true
		})
		if err := r.Done(); err != nil {
			return err
		}
		c, err := rich()
		if err != nil {
			return err
		}
		out.Null()
		return c.WriteRich(d)
	})
	str := func(id string, f func(c caps.RichClipboard) (string, error)) {
		t.Func(id, ReadPermission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
			if err := codec.Args(args, id).Done(); err != nil {
				return err
			}
			c, err := rich()
			if err != nil {
				return err
			}
			s, err := f(c)
			if err != nil {
				return err
			}
			out.String(s)
			return nil
		})
	}
	str(IDReadHTML, caps.RichClipboard.ReadHTML)
	str(IDReadRTF, caps.RichClipboard.ReadRTF)
	t.Func(IDFormats, ReadPermission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDFormats).Done(); err != nil {
			return err
		}
		c, err := rich()
		if err != nil {
			return err
		}
		list, err := c.ClipboardFormats()
		if err != nil {
			return err
		}
		codec.WriteStrings(out, list)
		return nil
	})
	t.Func(IDReadFormat, ReadPermission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDReadFormat)
		name := r.String()
		if err := r.Done(); err != nil {
			return err
		}
		c, err := rich()
		if err != nil {
			return err
		}
		b, err := c.ReadFormat(name)
		if err != nil {
			return err
		}
		if b == nil {
			out.Null()
		} else {
			out.Bytes(b)
		}
		return nil
	})
	t.Func(IDWriteFormat, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDWriteFormat)
		name := r.String()
		data := r.Bytes()
		if err := r.Done(); err != nil {
			return err
		}
		c, err := rich()
		if err != nil {
			return err
		}
		out.Null()
		return c.WriteFormat(name, data)
	})
}
