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
	NotificationCloser   = caps.NotificationCloser
	Accelerator          = caps.Accelerator
	Shortcuts            = caps.Shortcuts
	ProcessRef           = caps.ProcessRef
	ProcessInfo          = caps.ProcessInfo
	ViewRef              = caps.ViewRef
	ViewEvent            = caps.ViewEvent
	ViewEvents           = caps.ViewEvents
	ViewLookup           = caps.ViewLookup
	ViewInfo             = caps.ViewInfo
	ViewOptions          = caps.ViewOptions
	WindowViews          = caps.WindowViews
	LayoutNode           = caps.LayoutNode
	Axis                 = caps.Axis
	PermissionKind       = caps.PermissionKind
	PermissionRequest    = caps.PermissionRequest
	PermissionHandler    = caps.PermissionHandler
	PermissionRequests   = caps.PermissionRequests
	PrivacyStatus        = caps.PrivacyStatus
	Privacy              = caps.Privacy
	CaptureSource        = caps.CaptureSource
	CaptureSources       = caps.CaptureSources
	CaptureChooser       = caps.CaptureChooser
	ChromeOptions        = caps.ChromeOptions
	ChromeState          = caps.ChromeState
	ChromeEvent          = caps.ChromeEvent
	TitleBarOverlay      = caps.TitleBarOverlay
	Point                = caps.Point
	WindowChrome         = caps.WindowChrome
	WindowDocument       = caps.WindowDocument
	WindowSheets         = caps.WindowSheets
	WindowTabs           = caps.WindowTabs
	WindowThumbBar       = caps.WindowThumbBar
	ThumbButton          = caps.ThumbButton
	JumpTask             = caps.JumpTask
	JumpLister           = caps.JumpLister
	AppPresence          = caps.AppPresence
	Sharer               = caps.Sharer
	ShareItems           = caps.ShareItems
	ChromeEvents         = caps.ChromeEvents
)

const (
	Horizontal           = caps.Horizontal
	Vertical             = caps.Vertical
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
