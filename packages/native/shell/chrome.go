package shell

import (
	"fmt"
	"net/http"

	"github.com/kratejs/krate/packages/native/internal/chrome"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// ChromeSpec is the window chrome of the main window (native.window in the
// config): title bar style, transparency, backdrops and the per-platform
// decorations. The page can change most of it later with the chrome SDK
// area.
type ChromeSpec struct {
	// TitleBar is "native" (default), "hidden" or "none".
	TitleBar string `json:"titleBar,omitempty"`
	// Transparent makes the window and webview background transparent.
	Transparent bool `json:"transparent,omitempty"`
	// Backdrop is "none", "mica", "micaAlt" or "acrylic" (Windows 11).
	Backdrop string `json:"backdrop,omitempty"`
	// Vibrancy is a macOS NSVisualEffectView material ("sidebar", "hudWindow"...).
	Vibrancy string `json:"vibrancy,omitempty"`
	// TitleBarOverlay draws the caption buttons over a hidden title bar
	// (Windows).
	TitleBarOverlay *OverlaySpec `json:"titleBarOverlay,omitempty"`
	// TrafficLightPosition moves the macOS window buttons.
	TrafficLightPosition *PointSpec `json:"trafficLightPosition,omitempty"`
	// TabbingIdentifier and TabbingMode set native window tabs (macOS).
	TabbingIdentifier string `json:"tabbingIdentifier,omitempty"`
	TabbingMode       string `json:"tabbingMode,omitempty"`
}

// OverlaySpec is the caption button overlay of a hidden title bar.
type OverlaySpec struct {
	Color       string `json:"color,omitempty"`
	SymbolColor string `json:"symbolColor,omitempty"`
	Height      int    `json:"height,omitempty"`
}

// PointSpec is a position in device-independent pixels.
type PointSpec struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Options converts the spec to the platform's options (without validating).
func (c *ChromeSpec) Options() caps.ChromeOptions {
	if c == nil {
		return caps.ChromeOptions{}
	}
	o := caps.ChromeOptions{
		TitleBar: c.TitleBar, Transparent: c.Transparent, Backdrop: c.Backdrop, Vibrancy: c.Vibrancy,
		TabbingID: c.TabbingIdentifier, TabbingMode: c.TabbingMode,
	}
	if c.TitleBarOverlay != nil {
		o.Overlay = &caps.TitleBarOverlay{Color: c.TitleBarOverlay.Color, SymbolColor: c.TitleBarOverlay.SymbolColor, Height: c.TitleBarOverlay.Height}
	}
	if c.TrafficLightPosition != nil {
		o.TrafficLight = &caps.Point{X: c.TrafficLightPosition.X, Y: c.TrafficLightPosition.Y}
	}
	return o
}

// validate checks the spec the way the platform will.
func (c *ChromeSpec) validate() error {
	if c == nil {
		return nil
	}
	if _, err := c.Options().Validate(); err != nil {
		return fmt.Errorf("window.chrome: %w", err)
	}
	return nil
}

// needsScript reports whether every page of the app gets the chrome script:
// a window without the native title bar or with a transparent background
// almost always draws its own bar.
func (c *ChromeSpec) needsScript() bool {
	if c == nil {
		return false
	}
	return (c.TitleBar != "" && c.TitleBar != caps.TitleBarNative) || c.Transparent
}

// newChromeHandler resolves a request to its platform window.
func (a *app) newChromeHandler() func(http.ResponseWriter, *http.Request) bool {
	return chrome.Handler(func(r *http.Request) (any, bool) {
		ref, ok := caps.ViewOf(r)
		if !ok {
			return nil, false
		}
		id, ok := a.sdk.ViewWindow(ref)
		if !ok {
			return nil, false
		}
		win, _, ok := a.sdk.Window(id)
		if !ok {
			return nil, false
		}
		return win, true
	})
}
