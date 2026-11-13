// Package screen lists displays, finds the display a window is on, reports
// display changes and centers windows.
//
// Linux: GDK monitors (X11 and Wayland). macOS: NSScreen, with the dock and
// menu bar excluded from the work area, and a screen-parameters notification
// for changes. Windows: EnumDisplayMonitors and GetDpiForMonitor, and
// WM_DISPLAYCHANGE for changes. Coordinates are logical pixels in the space
// window.state and window.setPosition use.
package screen

import (
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/jsonwire"
)

// Permission is the app permission the UI needs.
const Permission = "screen"

// Host function ids.
const (
	IDList         = "krate/screen.list"
	IDPrimary      = "krate/screen.primary"
	IDOfWindow     = "krate/screen.ofWindow"
	IDCenterWindow = "krate/screen.centerWindow"
	IDEvents       = "krate/screen.events"
)

// Display is one monitor.
type Display struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	X          int     `json:"x"`
	Y          int     `json:"y"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	WorkX      int     `json:"workX"`
	WorkY      int     `json:"workY"`
	WorkWidth  int     `json:"workWidth"`
	WorkHeight int     `json:"workHeight"`
	Scale      float64 `json:"scale"`
	Primary    bool    `json:"primary"`
	RefreshHz  float64 `json:"refreshHz"`
}

// Event reports that the display configuration changed.
type Event struct {
	Displays []Display `json:"displays"`
}

func fromCaps(d caps.Display) Display {
	return Display{ID: d.ID, Name: d.Name, X: d.X, Y: d.Y, Width: d.Width, Height: d.Height,
		WorkX: d.WorkX, WorkY: d.WorkY, WorkWidth: d.WorkWidth, WorkHeight: d.WorkHeight,
		Scale: d.Scale, Primary: d.Primary, RefreshHz: d.RefreshHz}
}

func fromCapsList(ds []caps.Display) []Display {
	out := make([]Display, len(ds))
	for i, d := range ds {
		out[i] = fromCaps(d)
	}
	return out
}

// List returns the displays, primary first.
func List() ([]Display, error) { return jsonwire.Query[[]Display](IDList) }

// Primary returns the primary display.
func Primary() (Display, error) { return jsonwire.Query[Display](IDPrimary) }

// OfWindow returns the display most of the window is on ("" is the main
// window).
func OfWindow(window string) (Display, error) { return jsonwire.Query[Display](IDOfWindow, window) }

// CenterWindow moves the window to the middle of the work area of the display
// with this id; "" means the display the window is on.
func CenterWindow(window, display string) error {
	return jsonwire.Do(IDCenterWindow, window, display)
}

// OnChange calls fn when displays are added, removed or change, until cancel
// is called.
func OnChange(fn func(Event)) (cancel func(), err error) {
	return jsonwire.Events(IDEvents, fn)
}
