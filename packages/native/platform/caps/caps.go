// Package caps defines the optional capability interfaces of the platform
// layer. Package platform re-exports them; the per-OS packages implement them.
package caps

// Optional capabilities. The core App and Window interfaces stay small and
// stable; an implementation adds the interfaces below as it gains the
// feature, and the SDK type-asserts for them and answers krate.ErrUnsupported
// when one is missing. Like App and Window, every method may be called from
// any goroutine and is marshalled onto the main thread by the implementation.
//
// Windows are named by the opaque WindowRef an implementation hands out in
// WindowControls.Ref; a zero WindowRef means "the application as a whole" or
// "the focused window", depending on the call.

// WindowRef identifies one window to the capability interfaces.
type WindowRef uint64

// WindowState is a snapshot of a window.
type WindowState struct {
	X, Y          int
	Width, Height int
	Maximized     bool
	Minimized     bool
	Fullscreen    bool
	AlwaysOnTop   bool
	Decorated     bool
	Visible       bool
	Focused       bool
	Title         string
}

// WindowEventKind names a window event.
type WindowEventKind string

// Window events. CloseRequested is only delivered while the window prevents
// close (WindowControls.SetPreventClose); the window then stays open until
// Close is called.
const (
	WindowCloseRequested WindowEventKind = "close-requested"
	WindowClosed         WindowEventKind = "closed"
	WindowFocused        WindowEventKind = "focus"
	WindowBlurred        WindowEventKind = "blur"
)

// WindowEvent is delivered to the handler set with WindowControls.OnEvent.
type WindowEvent struct {
	Window WindowRef
	Kind   WindowEventKind
}

// WindowControls is implemented by Window values that can be driven beyond
// the core Window interface.
type WindowControls interface {
	Ref() WindowRef
	SetAlwaysOnTop(on bool)
	SetFullscreen(on bool)
	// SetDecorated(false) removes the title bar and border (frameless).
	SetDecorated(on bool)
	SetPosition(x, y int)
	Minimize()
	Maximize()
	// Restore undoes Minimize, Maximize and Fullscreen.
	Restore()
	Focus()
	Show()
	Hide()
	// SetPreventClose makes a close request (title-bar button, Alt+F4)
	// deliver WindowCloseRequested instead of closing the window.
	SetPreventClose(on bool)
	State() WindowState
}

// WindowEvents is implemented by Apps that report window events.
type WindowEvents interface {
	// OnWindowEvent sets the single handler for every window's events. fn
	// runs on the main thread and must not block.
	OnWindowEvent(fn func(WindowEvent))
}

// FileDialogMode selects what a file dialog chooses.
type FileDialogMode int

// File dialog modes.
const (
	OpenFile FileDialogMode = iota
	SaveFile
	OpenFolder
)

// FileFilter limits a file dialog to some files. Patterns are glob patterns
// such as "*.png" (matched case-insensitively where the toolkit allows).
type FileFilter struct {
	Name     string
	Patterns []string
}

// FileDialogOptions configures a file dialog.
type FileDialogOptions struct {
	Mode         FileDialogMode
	Title        string
	Dir          string // initial folder
	Name         string // suggested file name (SaveFile)
	Filters      []FileFilter
	Multiple     bool // OpenFile and OpenFolder only
	AcceptLabel  string
	ShowHidden   bool
	ConfirmWrite bool // SaveFile: ask before overwriting
}

// MessageKind is the icon of a message dialog.
type MessageKind int

// Message dialog kinds.
const (
	MessageInfo MessageKind = iota
	MessageWarning
	MessageError
	MessageQuestion
)

// MessageDialogOptions configures a message dialog.
type MessageDialogOptions struct {
	Kind    MessageKind
	Title   string
	Message string
	Detail  string
	// Buttons are the button labels, left to right; empty means one "OK".
	Buttons []string
	// Default is the index of the default button.
	Default int
}

// Dialogs is implemented by Apps that can show native dialogs. Both calls
// block until the user answers. A cancelled file dialog returns no paths and
// a nil error; a message dialog closed without a button returns -1.
type Dialogs interface {
	FileDialog(parent WindowRef, o FileDialogOptions) (paths []string, err error)
	MessageDialog(parent WindowRef, o MessageDialogOptions) (button int, err error)
}

// Clipboard is implemented by Apps with access to the system clipboard.
// Read methods return a nil slice or an empty string when the clipboard holds
// no data of that kind.
type Clipboard interface {
	ReadText() (string, error)
	WriteText(s string) error
	// ReadImage returns PNG bytes.
	ReadImage() ([]byte, error)
	WriteImage(png []byte) error
	// ReadFiles returns absolute paths.
	ReadFiles() ([]string, error)
	WriteFiles(paths []string) error
	Clear() error
}

// MenuKind is the kind of a menu item.
type MenuKind int

// Menu item kinds.
const (
	MenuItemNormal MenuKind = iota
	MenuItemCheck
	MenuItemRadio
	MenuItemSeparator
	MenuItemSubmenu
)

// MenuItem is one entry of a menu tree.
type MenuItem struct {
	ID          string
	Label       string
	Kind        MenuKind
	Accelerator string // "CmdOrCtrl+Shift+S"
	Disabled    bool
	Checked     bool
	Submenu     []MenuItem
}

// MenuEvent reports a click on a menu item.
type MenuEvent struct {
	ID      string
	Window  WindowRef
	Checked bool
}

// Menus is implemented by Apps that can show menus.
type Menus interface {
	// SetAppMenu installs the menu bar on every window (the global menu bar
	// on macOS). A nil or empty slice removes it.
	SetAppMenu(items []MenuItem) error
	// PopupMenu shows a context menu at window coordinates (x, y); negative
	// coordinates mean the pointer position. It returns without waiting.
	PopupMenu(parent WindowRef, items []MenuItem, x, y int) error
	// OnMenu sets the handler for item clicks. fn must not block.
	OnMenu(fn func(MenuEvent))
}

// TrayIcon describes a system tray (notification area) icon.
type TrayIcon struct {
	Tooltip string
	Title   string
	PNG     []byte // icon image
	Menu    []MenuItem
}

// TrayEvent reports a click on the tray icon or one of its menu items.
type TrayEvent struct {
	// Kind is "click", "double-click", "right-click" or "menu".
	Kind string
	// ID is the menu item id for Kind "menu".
	ID string
}

// Tray is implemented by Apps that can show a tray icon on their own. On
// Linux the SDK drives the StatusNotifier D-Bus protocol itself and does not
// need this.
type Tray interface {
	SetTray(icon *TrayIcon) error // nil removes the icon
	OnTray(fn func(TrayEvent))
}

// Notification is a desktop notification.
type Notification struct {
	ID      string
	Title   string
	Body    string
	Icon    string // icon name or file path
	Actions []NotificationAction
	// Urgency is 0 low, 1 normal, 2 critical.
	Urgency int
	// TimeoutMS is the display time; 0 means the server default, -1 never.
	TimeoutMS int
	Silent    bool
}

// NotificationAction is a button on a notification.
type NotificationAction struct {
	ID    string
	Label string
}

// NotifyEvent reports a click on a notification or one of its actions.
type NotifyEvent struct {
	ID     string // the Notification.ID
	Action string // action id, or "" for the notification body
	Closed bool   // true when the notification was dismissed
}

// Notifier is implemented by Apps that can post notifications on their own.
// On Linux the SDK talks to org.freedesktop.Notifications itself.
type Notifier interface {
	Notify(n Notification) error
	OnNotify(fn func(NotifyEvent))
}

// Accelerator is a parsed global shortcut such as "Ctrl+Shift+K".
type Accelerator struct {
	Ctrl, Shift, Alt, Super bool
	// Key is a key name such as "K", "F5", "Space", "Escape".
	Key string
}

// Shortcuts is implemented by Apps that can register global (system-wide)
// shortcuts. Handle is chosen by the caller.
type Shortcuts interface {
	RegisterShortcut(handle string, a Accelerator) error
	UnregisterShortcut(handle string) error
	// OnShortcut sets the handler for presses. fn must not block.
	OnShortcut(fn func(handle string))
}

// ProcessRef is an operating system process that serves the app's webviews.
// Role is one of "webview:web", "webview:network", "webview:gpu" and
// "webview:other" (the memstat.RoleWebview* values).
type ProcessRef struct {
	PID  int
	Role string
}

// ProcessInfo is implemented by Apps that can name the processes behind their
// webviews, for memory measurement. The notes say what could not be
// attributed (for example "webview processes not attributable"); an empty
// list with notes is a normal answer. On Linux the processes are descendants
// of the shell; on macOS they are XPC services found through private WKWebView
// selectors; on Windows they come from WebView2's GetProcessInfos.
type ProcessInfo interface {
	WebviewProcesses() (procs []ProcessRef, notes []string)
}
