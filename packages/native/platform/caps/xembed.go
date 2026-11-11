package caps

// XEmbedTray is implemented by Linux Apps that can fall back to the legacy
// XEmbed system tray (a _NET_SYSTEM_TRAY_S<n> manager, drawn with
// GtkStatusIcon) when no StatusNotifier host is running. The SDK tries the
// StatusNotifier protocol first and only then asks this interface.
type XEmbedTray interface {
	// XEmbedTrayAvailable reports whether an XEmbed tray manager owns the
	// tray selection of the default screen.
	XEmbedTrayAvailable() bool
	// SetXEmbedTray shows, updates or (nil) removes the icon.
	SetXEmbedTray(icon *TrayIcon) error
	// OnXEmbedTray sets the handler for clicks and menu selections. fn must
	// not block.
	OnXEmbedTray(fn func(TrayEvent))
}
