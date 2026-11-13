package window

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// Views: a window can hold several webviews, laid out as split panes. Every
// window has a primary view whose id is the window's id; AddView adds more.
// Each view loads its own route and can have its own permission scope. A
// request from a view is bound to that view by the platform, so the scope of a
// view cannot be claimed by its page.
//
// All views of an app share one web process where the platform allows it.

// Host function ids of the view operations.
const (
	IDAddView      = "krate/window.addView"
	IDRemoveView   = "krate/window.removeView"
	IDViews        = "krate/window.views"
	IDSetLayout    = "krate/window.setLayout"
	IDLayout       = "krate/window.layout"
	IDFocusView    = "krate/window.focusView"
	IDNavigateView = "krate/window.navigateView"
	IDPostMessage  = "krate/window.postMessage"
	IDViewEvents   = "krate/window.viewEvents"
)

// MaxMessage is the largest message PostMessage accepts, in bytes of JSON.
const MaxMessage = 1 << 20

// Sides of the view a new view is added to.
const (
	Right  = "right"
	Left   = "left"
	Top    = "top"
	Bottom = "bottom"
)

// ViewOptions configures AddView.
type ViewOptions struct {
	// ID names the view; empty picks "v1", "v2", ... It must be unique among
	// window and view ids.
	ID string
	// Path is the route the view opens. Default "/".
	Path string
	// Beside is the view to split; empty means the window's primary view.
	Beside string
	// Side is where the new view goes relative to Beside: Right (default),
	// Left, Top or Bottom.
	Side string
	// Size is the share of the split the new view gets, between 0 and 1.
	// Default one half.
	Size float64
	// MinWidth and MinHeight are the smallest size of the new pane in pixels.
	MinWidth, MinHeight int
	// Permissions, when HasPermissions is set, narrows the SDK permissions
	// the view's page may use to this list, which must be a subset of the
	// calling view's. Without it the view gets the caller's permissions.
	Permissions    []string
	HasPermissions bool
}

// ViewInfo describes a view.
type ViewInfo struct {
	ID      string
	Window  string
	Path    string
	Primary bool
}

// ViewEventKind names a view event.
type ViewEventKind string

// View events. Closed is a view that went away because its page called
// window.close() or its window closed; Removed is one taken out with Remove.
const (
	ViewAdded     ViewEventKind = "added"
	ViewRemoved   ViewEventKind = "removed"
	ViewNavigated ViewEventKind = "navigated"
	ViewClosed    ViewEventKind = "closed"
)

// ViewEvent is a view event.
type ViewEvent struct {
	View   string
	Window string
	Kind   ViewEventKind
	// Path is the route (added and navigated).
	Path string
}

// Layout is a layout tree: a pane that shows a view, or a split of two
// layouts. Direction "horizontal" puts First left of Second, "vertical" puts
// First above Second. Ratio is the share of the space First gets (default
// one half).
type Layout struct {
	View                string
	MinWidth, MinHeight int

	Direction     string
	Ratio         float64
	First, Second *Layout
}

// Pane returns a layout that shows a view.
func Pane(view string) *Layout { return &Layout{View: view} }

// Split returns a layout that splits two layouts.
func Split(direction string, ratio float64, first, second *Layout) *Layout {
	return &Layout{Direction: direction, Ratio: ratio, First: first, Second: second}
}

// View is a handle to one view.
type View struct{ ID string }

// AddView adds a view to the window.
func (w *Window) AddView(o ViewOptions) (*View, error) {
	d, err := bridge.Call(context.Background(), IDAddView, func(e *wire.Encoder) {
		e.String(w.ID)
		encodeViewOptions(e, o)
	})
	if err != nil {
		return nil, err
	}
	id, err := d.String()
	if err != nil {
		return nil, err
	}
	return &View{ID: id}, nil
}

// Views lists the window's views.
func (w *Window) Views() ([]ViewInfo, error) { return listViews(w.ID) }

// Views lists the views of every window.
func Views() ([]ViewInfo, error) { return listViews("") }

func listViews(window string) ([]ViewInfo, error) {
	d, err := bridge.Call(context.Background(), IDViews, func(e *wire.Encoder) { e.String(window) })
	if err != nil {
		return nil, err
	}
	r := &codec.R{D: d, Name: "views"}
	var out []ViewInfo
	r.Array(func() {
		var v ViewInfo
		r.Object(func(k string) bool {
			switch k {
			case "id":
				v.ID = r.String()
			case "window":
				v.Window = r.String()
			case "path":
				v.Path = r.String()
			case "primary":
				v.Primary = r.Bool()
			default:
				return false
			}
			return true
		})
		out = append(out, v)
	})
	return out, r.Err()
}

// SetLayout replaces the window's layout. It must name every view of the
// window exactly once.
func (w *Window) SetLayout(l *Layout) error {
	return bridge.CallDo(context.Background(), IDSetLayout, func(e *wire.Encoder) {
		e.String(w.ID)
		encodeLayout(e, l)
	})
}

// Layout returns the window's layout with the ratios the user dragged the
// dividers to.
func (w *Window) Layout() (*Layout, error) {
	d, err := bridge.Call(context.Background(), IDLayout, func(e *wire.Encoder) { e.String(w.ID) })
	if err != nil {
		return nil, err
	}
	r := &codec.R{D: d, Name: "layout"}
	l := decodeLayout(r)
	return l, r.Err()
}

// Remove removes the view; the pane next to it takes its space. A window's
// primary view cannot be removed.
func (v *View) Remove() error {
	return bridge.CallDo(context.Background(), IDRemoveView, func(e *wire.Encoder) { e.String(v.ID) })
}

// Focus gives the view keyboard focus.
func (v *View) Focus() error {
	return bridge.CallDo(context.Background(), IDFocusView, func(e *wire.Encoder) { e.String(v.ID) })
}

// Navigate loads another route in the view.
func (v *View) Navigate(path string) error {
	return bridge.CallDo(context.Background(), IDNavigateView, func(e *wire.Encoder) { e.String(v.ID); e.String(path) })
}

// Post sends a message to the view: its page gets a "krate:view-message" DOM
// event whose detail is {from, data}, where from is the id of the view that
// sent it (set by the platform, not by the sender's page) and data is msg as
// JSON. Host code sends with an empty from.
func (v *View) Post(msg any) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("window: message is not JSON: %w", err)
	}
	return bridge.CallDo(context.Background(), IDPostMessage, func(e *wire.Encoder) {
		e.String(v.ID)
		e.Raw(b)
	})
}

// OnViewEvent calls fn for every view event until cancel is called.
func OnViewEvent(fn func(ViewEvent)) (cancel func(), err error) {
	return bridge.Subscribe(context.Background(), IDViewEvents, func(d *wire.Decoder) {
		r := &codec.R{D: d, Name: "view event"}
		var ev ViewEvent
		r.Object(func(k string) bool {
			switch k {
			case "view":
				ev.View = r.String()
			case "window":
				ev.Window = r.String()
			case "kind":
				ev.Kind = ViewEventKind(r.String())
			case "path":
				ev.Path = r.String()
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
