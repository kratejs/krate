// Package theme reads the system appearance (light or dark, high contrast),
// reports changes, and makes the app's native title bars follow it or a fixed
// choice.
//
// Linux: the desktop portal org.freedesktop.portal.Settings (color-scheme and
// contrast in org.freedesktop.appearance), falling back to GTK_THEME and
// ~/.config/gtk-3.0/settings.ini. GTK 3 does not read the portal itself, so
// the SDK resolves the scheme and sets gtk-application-prefer-dark-theme.
//
// macOS: NSApp effectiveAppearance and the increase-contrast accessibility
// option; changes come from the AppleInterfaceThemeChanged distributed
// notification and the accessibility display-options notification.
//
// Windows: the Personalize registry key (AppsUseLightTheme), the high
// contrast flag of SystemParametersInfo, WM_SETTINGCHANGE for changes, and
// DwmSetWindowAttribute DWMWA_USE_IMMERSIVE_DARK_MODE for title bars.
package theme

import (
	"github.com/kratejs/krate/packages/native/sdk/internal/jsonwire"
)

// Permission is the app permission the UI needs.
const Permission = "theme"

// Host function ids.
const (
	IDGet    = "krate/theme.get"
	IDSet    = "krate/theme.set"
	IDEvents = "krate/theme.events"
)

// Source says what the app's chrome follows.
type Source string

// Sources.
const (
	System Source = "system"
	Light  Source = "light"
	Dark   Source = "dark"
)

// Theme is the appearance.
type Theme struct {
	// Scheme is what the app's native chrome uses now.
	Scheme string `json:"scheme"`
	// HighContrast is the system's high contrast mode.
	HighContrast bool `json:"highContrast"`
	// System is the system's own scheme, whatever the override.
	System string `json:"system"`
	Source Source `json:"source"`
}

// Get returns the current appearance.
func Get() (Theme, error) { return jsonwire.Query[Theme](IDGet) }

// Set chooses what native title bars follow.
func Set(s Source) error { return jsonwire.Do(IDSet, string(s)) }

// OnChange calls fn when the appearance changes, until cancel is called.
func OnChange(fn func(Theme)) (cancel func(), err error) { return jsonwire.Events(IDEvents, fn) }
