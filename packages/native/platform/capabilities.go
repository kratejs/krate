package platform

import "github.com/kratejs/krate/packages/native/platform/caps"

// The optional capability interfaces and their types live in package caps so
// that the per-OS implementations can use them without importing this package.
// They are re-exported here.

type (
	WindowRef            = caps.WindowRef
	WindowState          = caps.WindowState
	WindowEventKind      = caps.WindowEventKind
	WindowEvent          = caps.WindowEvent
	WindowControls       = caps.WindowControls
	WindowEvents         = caps.WindowEvents
	FileDialogMode       = caps.FileDialogMode
	FileFilter           = caps.FileFilter
	FileDialogOptions    = caps.FileDialogOptions
	MessageKind          = caps.MessageKind
	MessageDialogOptions = caps.MessageDialogOptions
	Dialogs              = caps.Dialogs
	Clipboard            = caps.Clipboard
	MenuKind             = caps.MenuKind
	MenuItem             = caps.MenuItem
	MenuEvent            = caps.MenuEvent
	Menus                = caps.Menus
	TrayIcon             = caps.TrayIcon
	TrayEvent            = caps.TrayEvent
	Tray                 = caps.Tray
	XEmbedTray           = caps.XEmbedTray
	Notification         = caps.Notification
	NotificationAction   = caps.NotificationAction
	NotifyEvent          = caps.NotifyEvent
	Notifier             = caps.Notifier
	Accelerator          = caps.Accelerator
	Shortcuts            = caps.Shortcuts
	ProcessRef           = caps.ProcessRef
	ProcessInfo          = caps.ProcessInfo
)

const (
	WindowCloseRequested = caps.WindowCloseRequested
	WindowClosed         = caps.WindowClosed
	WindowFocused        = caps.WindowFocused
	WindowBlurred        = caps.WindowBlurred
	WindowReleased       = caps.WindowReleased
	WindowRestored       = caps.WindowRestored
	OpenFile             = caps.OpenFile
	SaveFile             = caps.SaveFile
	OpenFolder           = caps.OpenFolder
	MessageInfo          = caps.MessageInfo
	MessageWarning       = caps.MessageWarning
	MessageError         = caps.MessageError
	MessageQuestion      = caps.MessageQuestion
	MenuItemNormal       = caps.MenuItemNormal
	MenuItemCheck        = caps.MenuItemCheck
	MenuItemRadio        = caps.MenuItemRadio
	MenuItemSeparator    = caps.MenuItemSeparator
	MenuItemSubmenu      = caps.MenuItemSubmenu
)
