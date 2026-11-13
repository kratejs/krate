// Package page controls the web contents of one view: the print dialog and
// PDF output, find in page, zoom and capturing the view as an image. A view
// is named by its id, the window id for a window's primary view; an empty id
// means the view of the caller.
//
// Linux uses WebKitGTK (GtkPrintOperation, WebKitFindController, the zoom
// level and webkit_web_view_get_snapshot); macOS uses WKWebView and Windows
// WebView2. An engine that lacks a call gives krate.ErrUnsupported with the
// reason.
package page

import (
	"context"
	"fmt"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// Permission is the app permission the UI needs for these operations.
const Permission = "page"

// Host function ids.
const (
	IDPrint     = "krate/page.print"
	IDPrintPDF  = "krate/page.printToPdf"
	IDFind      = "krate/page.find"
	IDStopFind  = "krate/page.stopFind"
	IDFindEvent = "krate/page.findEvents"
	IDZoom      = "krate/page.zoom"
	IDSetZoom   = "krate/page.setZoom"
	IDCapture   = "krate/page.capture"
)

// Margins are page margins in inches.
type Margins struct{ Top, Right, Bottom, Left float64 }

// PDFOptions configures PrintToPDF. The zero value is A4, portrait, with
// 0.4 inch margins and no backgrounds.
type PDFOptions struct {
	Landscape       bool
	PrintBackground bool
	// PageSize names a paper size: "A3", "A4", "A5", "Letter", "Legal" or
	// "Tabloid". PageWidth and PageHeight (inches) override it.
	PageSize              string
	PageWidth, PageHeight float64
	// Margins in inches; nil means 0.4 inch on every side.
	Margins *Margins
}

// FindOptions configures Find.
type FindOptions struct {
	Backward  bool
	MatchCase bool
	// NoWrap stops at the end of the page.
	NoWrap bool
	// Continue moves to the next (or, with Backward, previous) match of the
	// search for the same text that is already running.
	Continue bool
}

// FindEvent reports the outcome of a Find.
type FindEvent struct {
	// View is the id of the view that was searched.
	View    string
	Text    string
	Matches int
	// Active is the 1-based index of the highlighted match, 0 when unknown
	// or when there is no match.
	Active int
	// Final is true when Matches will not change for this search.
	Final bool
}

// Print opens the print dialog of a view. It returns when the dialog has been
// requested, not when the user is done.
func Print(view string) error {
	return bridge.CallDo(context.Background(), IDPrint, func(e *wire.Encoder) { e.String(view) })
}

// PrintToPDF renders the view to PDF bytes.
func PrintToPDF(view string, o PDFOptions) ([]byte, error) {
	d, err := bridge.Call(context.Background(), IDPrintPDF, func(e *wire.Encoder) {
		e.String(view)
		encodePDFOptions(e, o)
	})
	if err != nil {
		return nil, err
	}
	return d.Bytes()
}

// Find searches for text in a view and highlights the first match.
func Find(view, text string, o FindOptions) error {
	return bridge.CallDo(context.Background(), IDFind, func(e *wire.Encoder) {
		e.String(view)
		e.String(text)
		e.BeginObject()
		e.Key("backward")
		e.Bool(o.Backward)
		e.Key("matchCase")
		e.Bool(o.MatchCase)
		e.Key("noWrap")
		e.Bool(o.NoWrap)
		e.Key("continue")
		e.Bool(o.Continue)
		e.EndObject()
	})
}

// StopFind clears the highlight of the search in a view.
func StopFind(view string) error {
	return bridge.CallDo(context.Background(), IDStopFind, func(e *wire.Encoder) { e.String(view) })
}

// OnFind calls fn for find results until cancel is called.
func OnFind(fn func(FindEvent)) (cancel func(), err error) {
	return bridge.Subscribe(context.Background(), IDFindEvent, func(d *wire.Decoder) {
		r := &codec.R{D: d, Name: "find event"}
		var ev FindEvent
		r.Object(func(k string) bool {
			switch k {
			case "view":
				ev.View = r.String()
			case "text":
				ev.Text = r.String()
			case "matches":
				ev.Matches = int(r.Int())
			case "active":
				ev.Active = int(r.Int())
			case "final":
				ev.Final = r.Bool()
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

// Zoom returns the zoom factor of a view; 1 is 100 percent.
func Zoom(view string) (float64, error) {
	d, err := bridge.Call(context.Background(), IDZoom, func(e *wire.Encoder) { e.String(view) })
	if err != nil {
		return 0, err
	}
	return d.Float()
}

// SetZoom sets the zoom factor of a view, between 0.25 and 5.
func SetZoom(view string, factor float64) error {
	return bridge.CallDo(context.Background(), IDSetZoom, func(e *wire.Encoder) {
		e.String(view)
		e.Float(factor)
	})
}

// Capture returns a PNG of the visible area of a view.
func Capture(view string) ([]byte, error) {
	d, err := bridge.Call(context.Background(), IDCapture, func(e *wire.Encoder) { e.String(view) })
	if err != nil {
		return nil, err
	}
	return d.Bytes()
}

func encodePDFOptions(e *wire.Encoder, o PDFOptions) {
	e.BeginObject()
	e.Key("landscape")
	e.Bool(o.Landscape)
	e.Key("printBackground")
	e.Bool(o.PrintBackground)
	e.Key("pageSize")
	e.String(o.PageSize)
	e.Key("pageWidth")
	e.Float(o.PageWidth)
	e.Key("pageHeight")
	e.Float(o.PageHeight)
	if o.Margins != nil {
		e.Key("margins")
		e.BeginObject()
		e.Key("top")
		e.Float(o.Margins.Top)
		e.Key("right")
		e.Float(o.Margins.Right)
		e.Key("bottom")
		e.Float(o.Margins.Bottom)
		e.Key("left")
		e.Float(o.Margins.Left)
		e.EndObject()
	}
	e.EndObject()
}

func decodePDFOptions(r *codec.R) (o caps.PDFOptions) {
	var size string
	r.Object(func(k string) bool {
		switch k {
		case "landscape":
			o.Landscape = r.Bool()
		case "printBackground":
			o.PrintBackground = r.Bool()
		case "pageSize":
			size = r.String()
		case "pageWidth":
			o.PageWidth = r.Float()
		case "pageHeight":
			o.PageHeight = r.Float()
		case "margins":
			m := caps.Margins{}
			r.Object(func(k string) bool {
				switch k {
				case "top":
					m.Top = r.Float()
				case "right":
					m.Right = r.Float()
				case "bottom":
					m.Bottom = r.Float()
				case "left":
					m.Left = r.Float()
				default:
					return false
				}
				return true
			})
			o.Margins = &m
		default:
			return false
		}
		return true
	})
	if size != "" && o.PageWidth == 0 && o.PageHeight == 0 {
		w, h, ok := caps.PaperSize(size)
		if !ok {
			r.Failf("pageSize %q must be A3, A4, A5, Letter, Legal or Tabloid", size)
		}
		o.PageWidth, o.PageHeight = w, h
	}
	return o
}

func decodeFindOptions(r *codec.R) (o caps.FindOptions) {
	r.Object(func(k string) bool {
		switch k {
		case "backward":
			o.Backward = r.Bool()
		case "matchCase":
			o.MatchCase = r.Bool()
		case "noWrap":
			o.NoWrap = r.Bool()
		case "continue":
			o.Continue = r.Bool()
		default:
			return false
		}
		return true
	})
	return o
}

// Register adds the operations to t.
func Register(t *bridge.Table, env *bridge.Env) {
	topic := env.Topic("page")
	t.Stream(IDFindEvent, Permission, true, topic.Stream())
	if pe, ok := env.App.(caps.PageEvents); ok {
		pe.OnFind(func(e caps.FindEvent) {
			rec, ok := env.Windows.ViewByRef(e.View)
			if !ok {
				return
			}
			topic.Publish(func(enc *wire.Encoder) {
				enc.BeginObject()
				enc.Key("view")
				enc.String(rec.ID)
				enc.Key("text")
				enc.String(e.Text)
				enc.Key("matches")
				enc.Int(int64(e.Matches))
				enc.Key("active")
				enc.Int(int64(e.Active))
				enc.Key("final")
				enc.Bool(e.Final)
				enc.EndObject()
			})
		})
	}

	// target resolves a view id ("" is the caller's view) to the controls of
	// its window.
	target := func(ctx krate.Ctx, id string) (caps.PageControls, caps.ViewRef, error) {
		if id == "" {
			id = ctx.Window()
		}
		rec, ok := env.Windows.View(id)
		if !ok {
			if id == "" {
				return nil, 0, krate.Errorf("bad_args", "no view given, and the call did not come from a page")
			}
			return nil, 0, krate.Errorf("not_found", "no view %q", id)
		}
		win, _, ok := env.Windows.Get(rec.Window)
		if !ok {
			return nil, 0, krate.Errorf("not_found", "no window %q", rec.Window)
		}
		pc, ok := win.(caps.PageControls)
		if !ok {
			return nil, 0, fmt.Errorf("%w: page controls are not available on this platform", krate.ErrUnsupported)
		}
		return pc, rec.Ref, nil
	}
	oneView := func(id string, fn func(pc caps.PageControls, v caps.ViewRef, out *wire.Encoder) error) bridgeFunc {
		return func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
			r := codec.Args(args, id)
			view := r.String()
			if err := r.Done(); err != nil {
				return err
			}
			pc, v, err := target(ctx, view)
			if err != nil {
				return err
			}
			return fn(pc, v, out)
		}
	}

	t.Func(IDPrint, Permission, true, oneView(IDPrint, func(pc caps.PageControls, v caps.ViewRef, out *wire.Encoder) error {
		if err := pc.PrintDialog(v); err != nil {
			return err
		}
		out.Null()
		return nil
	}))
	t.Func(IDStopFind, Permission, true, oneView(IDStopFind, func(pc caps.PageControls, v caps.ViewRef, out *wire.Encoder) error {
		if err := pc.StopFind(v); err != nil {
			return err
		}
		out.Null()
		return nil
	}))
	t.Func(IDZoom, Permission, true, oneView(IDZoom, func(pc caps.PageControls, v caps.ViewRef, out *wire.Encoder) error {
		z, err := pc.ZoomFactor(v)
		if err != nil {
			return err
		}
		out.Float(z)
		return nil
	}))
	t.Func(IDCapture, Permission, true, oneView(IDCapture, func(pc caps.PageControls, v caps.ViewRef, out *wire.Encoder) error {
		png, err := pc.Capture(v, caps.CaptureOptions{})
		if err != nil {
			return err
		}
		out.Bytes(png)
		return nil
	}))

	t.Func(IDSetZoom, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSetZoom)
		view := r.String()
		f := r.Float()
		if err := r.Done(); err != nil {
			return err
		}
		if f != f || f < caps.MinZoom || f > caps.MaxZoom {
			return krate.Errorf("bad_args", "%s: zoom factor %v must be between %v and %v", IDSetZoom, f, caps.MinZoom, caps.MaxZoom)
		}
		pc, v, err := target(ctx, view)
		if err != nil {
			return err
		}
		if err := pc.SetZoomFactor(v, f); err != nil {
			return err
		}
		out.Null()
		return nil
	})

	t.Func(IDFind, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDFind)
		view, text := r.String(), r.String()
		var o caps.FindOptions
		if r.More() {
			o = decodeFindOptions(r)
		}
		if err := r.Done(); err != nil {
			return err
		}
		if len(text) > maxFindText {
			return krate.Errorf("bad_args", "%s: the text is %d bytes; the limit is %d", IDFind, len(text), maxFindText)
		}
		pc, v, err := target(ctx, view)
		if err != nil {
			return err
		}
		if err := pc.Find(v, text, o); err != nil {
			return err
		}
		out.Null()
		return nil
	})

	t.Func(IDPrintPDF, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDPrintPDF)
		view := r.String()
		var o caps.PDFOptions
		if r.More() {
			o = decodePDFOptions(r)
		}
		if err := r.Done(); err != nil {
			return err
		}
		o, err := o.Resolve()
		if err != nil {
			return krate.Errorf("bad_args", "%s: %v", IDPrintPDF, err)
		}
		pc, v, err := target(ctx, view)
		if err != nil {
			return err
		}
		pdf, err := pc.PrintPDF(v, o)
		if err != nil {
			return err
		}
		out.Bytes(pdf)
		return nil
	})
}

// maxFindText is the longest search text accepted, in bytes.
const maxFindText = 4096

type bridgeFunc = func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error
