package app

import "github.com/kratejs/krate/packages/native/desktopentry"

// DesktopOptions describes the Linux launcher entry of an app.
type DesktopOptions = desktopentry.Options

// DesktopEntry returns the text of a freedesktop .desktop file that launches
// the app and registers it as the handler of its URL schemes
// (MimeType=x-scheme-handler/<scheme>). Install it as
// ~/.local/share/applications/<app id>.desktop (or /usr/share/applications)
// and run update-desktop-database. When schemes are set Exec gets a %u
// placeholder so the URL arrives as the last argument.
func DesktopEntry(o DesktopOptions) string { return desktopentry.Entry(o) }
