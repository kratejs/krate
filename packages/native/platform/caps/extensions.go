package caps

// ExtensionInfo describes one loaded browser extension.
type ExtensionInfo struct {
	ID      string
	Name    string
	Enabled bool
}

// Extensions is implemented by Apps whose engine can load unpacked browser
// extensions: WebView2 (ICoreWebView2Profile7 AddBrowserExtension) and
// WKWebView on macOS 15.4 and later (WKWebExtensionController). WebKitGTK has
// no extension API. Every method may be called from any goroutine.
type Extensions interface {
	// ExtensionsSupported reports whether this engine can load extensions here
	// (the runtime or OS version may be too old).
	ExtensionsSupported() bool
	// LoadExtension loads an unpacked extension from a directory holding its
	// manifest.json.
	LoadExtension(dir string) (ExtensionInfo, error)
	// ListExtensions returns the loaded extensions.
	ListExtensions() ([]ExtensionInfo, error)
	// RemoveExtension unloads an extension by id.
	RemoveExtension(id string) error
}
