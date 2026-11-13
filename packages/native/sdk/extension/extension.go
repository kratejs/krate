// Package extension loads the browser extensions an app packages
// (native.extensions) into its webviews.
//
// What the engines offer:
//
//   - WebView2 (Windows): ICoreWebView2Profile7 AddBrowserExtension with the
//     environment option AreBrowserExtensionsEnabled. Unpacked Chromium
//     extensions load into the app's profile.
//   - WKWebView (macOS 15.4 and later): WKWebExtension and
//     WKWebExtensionController. Older systems answer krate.ErrUnsupported.
//   - WebKitGTK (Linux): no extension API; every call answers
//     krate.ErrUnsupported.
//
// Extensions are named, not addressed by path: the app declares directories in
// native.extensions, packaging copies them next to the app, and the ids take
// the declared name, so a page cannot load code from an arbitrary place.
// Declared extensions load when the app starts.
//
// Permission: "extension".
package extension

import (
	"sync"

	"github.com/kratejs/krate/packages/native/sdk/internal/jsonwire"
)

// Permission is the app permission the UI needs.
const Permission = "extension"

// Host function ids.
const (
	IDList     = "krate/extension.list"
	IDLoad     = "krate/extension.load"
	IDRemove   = "krate/extension.remove"
	IDDeclared = "krate/extension.declared"
)

// Info describes a loaded extension.
type Info struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// Declared is one extension the app packages.
type Declared struct {
	// Name is the directory's base name.
	Name string `json:"name"`
	// Dir is the absolute directory (set by the shell; not sent to pages).
	Dir string `json:"-"`
}

var (
	mu       sync.Mutex
	declared []Declared
)

// Configure sets the declared extensions. The shell calls it before the SDK
// attaches.
func Configure(d []Declared) {
	mu.Lock()
	declared = append([]Declared(nil), d...)
	mu.Unlock()
}

func lookup(name string) (Declared, bool) {
	mu.Lock()
	defer mu.Unlock()
	for _, d := range declared {
		if d.Name == name {
			return d, true
		}
	}
	return Declared{}, false
}

func all() []Declared {
	mu.Lock()
	defer mu.Unlock()
	return append([]Declared(nil), declared...)
}

// List returns the extensions loaded now.
func List() ([]Info, error) { return jsonwire.Query[[]Info](IDList) }

// Load loads a declared extension by name.
func Load(name string) (Info, error) { return jsonwire.Query[Info](IDLoad, name) }

// Remove unloads an extension by id.
func Remove(id string) error { return jsonwire.Do(IDRemove, id) }

// Names lists the declared extensions.
func Names() ([]string, error) { return jsonwire.Query[[]string](IDDeclared) }
