// Package login registers the app to start when the user logs in.
//
// Linux: an XDG autostart entry, ~/.config/autostart/<appId>.desktop
// (honouring XDG_CONFIG_HOME).
//
// macOS: SMAppService.mainApp (macOS 13 and later, for an app that runs from
// a signed .app bundle), with a LaunchAgent plist in ~/Library/LaunchAgents
// as the fallback when the app is not a bundle or the service refuses.
//
// Windows: a value named after the app id under
// HKCU\Software\Microsoft\Windows\CurrentVersion\Run. A packaged (MSIX) app
// cannot use that key: it declares a windows.startupTask extension in its
// package manifest, and these calls answer krate.ErrUnsupported saying so;
// toggling a StartupTask from code is not implemented.
//
// The entry starts the running executable with the given arguments only (not
// the arguments of the current run, which may carry a deep link). In
// development the executable is the shell, not your app; use a release build
// to try it.
package login

import (
	"github.com/kratejs/krate/packages/native/sdk/internal/jsonwire"
)

// Permission is the app permission the UI needs.
const Permission = "login"

// Host function ids.
const (
	IDGet = "krate/login.get"
	IDSet = "krate/login.set"
)

// Item says whether the app starts at login.
type Item struct {
	Enabled bool `json:"enabled"`
	// Method is how it is registered: "smappservice", "launchagent",
	// "registry" or "autostart"; "" when not enabled.
	Method string `json:"method"`
}

// Options configures Set.
type Options struct {
	// Args are passed to the app at login.
	Args []string `json:"args"`
}

// Get reports whether the app starts at login.
func Get() (Item, error) { return jsonwire.Query[Item](IDGet) }

// Set registers (enabled) or removes the app's login entry.
func Set(enabled bool, o Options) error { return jsonwire.Do(IDSet, enabled, o) }
