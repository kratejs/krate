// Package chrome controls window chrome beyond the basics of package window:
// title bar styles and drag regions, backdrops and vibrancy, the caption
// buttons over a hidden title bar, traffic lights, document state, sheets,
// native tabs, taskbar thumbnail buttons and jump lists, the share menu, and
// how the app appears in the dock.
//
// Most of it is specific to one platform. An operation the platform has
// nothing for returns an error that wraps krate.ErrUnsupported with a message
// that says so; use app.Supports("chrome:<feature>") to ask in advance.
// Features: titleBar, transparent, backdrop, vibrancy, titleBarOverlay,
// trafficLights, drag, resize, document, sheets, tabs, thumbButtons, jumpList,
// share and dock.
//
//	Linux    titleBar (decorations on or off), transparent, drag, resize
//	macOS    titleBar, transparent, vibrancy, trafficLights, drag, document,
//	         sheets, tabs, share, dock
//	Windows  titleBar, transparent, backdrop, titleBarOverlay, drag,
//	         thumbButtons, jumpList
//
// Drag regions are marked in the page, not here: add the data-krate-drag
// attribute to an element, or give it the CSS property -webkit-app-region (or
// app-region) with the value drag; no-drag marks a part of a drag region that
// stays clickable. Buttons, links and form controls inside a drag region stay
// clickable on their own. See the window chrome docs page.
package chrome

import (
	"context"

	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// Permissions. Window operations need "window", app-wide ones "app", and the
// share menu "share".
const (
	PermissionWindow = "window"
	PermissionApp    = "app"
	PermissionShare  = "share"
)

// Host function ids.
const (
	IDState               = "krate/chrome.state"
	IDSetTitleBar         = "krate/chrome.setTitleBar"
	IDSetBackdrop         = "krate/chrome.setBackdrop"
	IDSetVibrancy         = "krate/chrome.setVibrancy"
	IDSetTitleBarOverlay  = "krate/chrome.setTitleBarOverlay"
	IDSetTrafficLightPos  = "krate/chrome.setTrafficLightPosition"
	IDStartDrag           = "krate/chrome.startDrag"
	IDToggleMaximize      = "krate/chrome.toggleMaximize"
	IDSetDocumentEdited   = "krate/chrome.setDocumentEdited"
	IDSetRepresentedFile  = "krate/chrome.setRepresentedFile"
	IDAttachSheet         = "krate/chrome.attachSheet"
	IDEndSheet            = "krate/chrome.endSheet"
	IDSetTabbing          = "krate/chrome.setTabbing"
	IDMergeAllWindows     = "krate/chrome.mergeAllWindows"
	IDMoveTabToNewWindow  = "krate/chrome.moveTabToNewWindow"
	IDTabs                = "krate/chrome.tabs"
	IDSetThumbButtons     = "krate/chrome.setThumbButtons"
	IDShare               = "krate/chrome.share"
	IDSetJumpList         = "krate/chrome.setJumpList"
	IDClearJumpList       = "krate/chrome.clearJumpList"
	IDSetActivationPolicy = "krate/chrome.setActivationPolicy"
	IDSetDockVisible      = "krate/chrome.setDockVisible"
	IDEvents              = "krate/chrome.events"
)

// Overlay describes the caption buttons the OS draws over a hidden title bar
// (Windows). Colors are "#rgb", "#rrggbb" or "#rrggbbaa"; empty keeps the
// system's. Height is the height of the draggable strip in pixels (default 32).
type Overlay struct {
	Color       string
	SymbolColor string
	Height      int
}

// Point is a position in pixels.
type Point struct{ X, Y int }

// State is the chrome of a window.
type State struct {
	// TitleBar is "native", "hidden" or "none".
	TitleBar    string
	Transparent bool
	// Backdrop is the Windows system backdrop ("none", "mica", "micaAlt",
	// "acrylic") and Vibrancy the macOS material; empty when unset.
	Backdrop string
	Vibrancy string
	// Overlay is set while a caption button overlay is installed.
	Overlay      *Overlay
	TrafficLight *Point
	// DocumentEdited and RepresentedFile are the macOS document state.
	DocumentEdited  bool
	RepresentedFile string
	TabbingID       string
	TabbingMode     string
	// Tabs is the number of windows in the window's tab group (1 when alone).
	Tabs int
	// Sheet reports that the window is shown as a sheet.
	Sheet bool
	// ThumbButtons is the number of taskbar thumbnail buttons.
	ThumbButtons int
	// NativeRegions: the web engine itself honors the CSS app-region property.
	NativeRegions bool
	// Features lists what this window supports.
	Features []string
}

// ThumbButton is a button in the taskbar thumbnail of a window (Windows).
type ThumbButton struct {
	ID      string
	Tooltip string
	// Icon is a PNG image of about 16x16 pixels.
	Icon           []byte
	Disabled       bool
	Hidden         bool
	DismissOnClick bool
}

// JumpTask is a task in the taskbar jump list (Windows). Choosing it starts
// the app with Args; a running instance receives them as a second-instance
// event (see app.OnSecondInstance), a stopped app starts with them.
type JumpTask struct {
	Title       string
	Args        []string
	Description string
	// IconPath is an absolute .exe or .ico path; IconIndex picks the icon.
	IconPath  string
	IconIndex int
}

// ShareItems is what the share menu offers: any of text, http(s) and mailto
// URLs, and absolute file paths.
type ShareItems struct {
	Text  string
	URLs  []string
	Files []string
}

// Event kinds.
const (
	// EventNewTab: the user asked for a new tab in a tabbed window (the "+"
	// button or Cmd+T). Open a window with the same tabbing identifier.
	EventNewTab = "new-tab"
	// EventTabsChanged: a window joined or left a tab group.
	EventTabsChanged = "tabs-changed"
	// EventThumbClick: a taskbar thumbnail button was clicked; ID names it.
	EventThumbClick = "thumb-click"
	// EventSheetEnded: a sheet closed.
	EventSheetEnded = "sheet-ended"
)

// Event is a chrome event.
type Event struct {
	Window string
	Kind   string
	// ID is the thumbnail button id for EventThumbClick.
	ID string
}

// Window is a handle to one window, by its SDK id (see package window).
type Window struct{ ID string }

// Main is the handle of the first window.
func Main() *Window { return &Window{ID: "main"} }

// Get returns the handle for an id.
func Get(id string) *Window { return &Window{ID: id} }

func (w *Window) do(id string, args func(*wire.Encoder)) error {
	return bridge.CallDo(context.Background(), id, func(e *wire.Encoder) {
		e.String(w.ID)
		if args != nil {
			args(e)
		}
	})
}

// State reads the window's chrome.
func (w *Window) State() (State, error) {
	d, err := bridge.Call(context.Background(), IDState, func(e *wire.Encoder) { e.String(w.ID) })
	if err != nil {
		return State{}, err
	}
	return decodeState(d)
}

// SetTitleBar sets the title bar style: "native", "hidden" or "none".
func (w *Window) SetTitleBar(style string) error {
	return w.do(IDSetTitleBar, func(e *wire.Encoder) { e.String(style) })
}

// SetBackdrop sets a Windows 11 system backdrop ("none", "mica", "micaAlt",
// "acrylic"). The page must leave its background transparent for the
// backdrop to show.
func (w *Window) SetBackdrop(name string) error {
	return w.do(IDSetBackdrop, func(e *wire.Encoder) { e.String(name) })
}

// SetVibrancy puts a macOS vibrancy material ("sidebar", "hudWindow", ...)
// behind the page; "" removes it.
func (w *Window) SetVibrancy(material string) error {
	return w.do(IDSetVibrancy, func(e *wire.Encoder) { e.String(material) })
}

// SetTitleBarOverlay installs the caption button overlay on a window with a
// hidden title bar (Windows); nil removes it.
func (w *Window) SetTitleBarOverlay(o *Overlay) error {
	return w.do(IDSetTitleBarOverlay, func(e *wire.Encoder) {
		if o == nil {
			e.Null()
			return
		}
		encodeOverlay(e, *o)
	})
}

// SetTrafficLightPosition moves the macOS window buttons.
func (w *Window) SetTrafficLightPosition(x, y int) error {
	return w.do(IDSetTrafficLightPos, func(e *wire.Encoder) { e.Int(int64(x)); e.Int(int64(y)) })
}

// StartDrag starts a native window move from the pointer press that is
// happening. Pages mark drag regions in markup instead; this is for a page
// that decides in code.
func (w *Window) StartDrag() error { return w.do(IDStartDrag, nil) }

// ToggleMaximize maximizes the window or restores it.
func (w *Window) ToggleMaximize() error { return w.do(IDToggleMaximize, nil) }

// SetDocumentEdited marks the window's document as having unsaved changes
// (the dot in the close button; macOS).
func (w *Window) SetDocumentEdited(on bool) error {
	return w.do(IDSetDocumentEdited, func(e *wire.Encoder) { e.Bool(on) })
}

// SetRepresentedFile shows the proxy icon of a file in the title bar (macOS);
// "" clears it.
func (w *Window) SetRepresentedFile(path string) error {
	return w.do(IDSetRepresentedFile, func(e *wire.Encoder) { e.String(path) })
}

// AttachSheet shows child, another window of the app, as a sheet attached to
// w (macOS). EventSheetEnded arrives when it closes.
func (w *Window) AttachSheet(child *Window) error {
	return w.do(IDAttachSheet, func(e *wire.Encoder) { e.String(child.ID) })
}

// EndSheet ends the sheet w is showing as (macOS).
func (w *Window) EndSheet() error { return w.do(IDEndSheet, nil) }

// SetTabbing puts the window in a tab group: windows with the same
// identifier tab together. Mode is "auto", "preferred" or "disallowed"
// (macOS).
func (w *Window) SetTabbing(identifier, mode string) error {
	return w.do(IDSetTabbing, func(e *wire.Encoder) { e.String(identifier); e.String(mode) })
}

// MergeAllWindows moves all windows with the same tabbing identifier into
// one tab group (macOS).
func (w *Window) MergeAllWindows() error { return w.do(IDMergeAllWindows, nil) }

// MoveTabToNewWindow takes the window out of its tab group (macOS).
func (w *Window) MoveTabToNewWindow() error { return w.do(IDMoveTabToNewWindow, nil) }

// Tabs lists the ids of the windows in the window's tab group, in tab order.
func (w *Window) Tabs() ([]string, error) {
	d, err := bridge.Call(context.Background(), IDTabs, func(e *wire.Encoder) { e.String(w.ID) })
	if err != nil {
		return nil, err
	}
	return codec.ReadStrings(d)
}

// SetThumbButtons replaces the buttons in the window's taskbar thumbnail
// (Windows; at most 7). Clicks arrive as EventThumbClick.
func (w *Window) SetThumbButtons(bs []ThumbButton) error {
	return w.do(IDSetThumbButtons, func(e *wire.Encoder) { encodeThumbs(e, bs) })
}

// Share shows the system share menu for items, anchored on the window
// (macOS). It returns when the menu is shown, not when the user chooses.
func (w *Window) Share(items ShareItems) error {
	return w.do(IDShare, func(e *wire.Encoder) { encodeShare(e, items) })
}

// SetJumpList replaces the tasks in the taskbar jump list (Windows; at most
// 20). The list is committed before the call returns.
func SetJumpList(tasks []JumpTask) error {
	return bridge.CallDo(context.Background(), IDSetJumpList, func(e *wire.Encoder) { encodeTasks(e, tasks) })
}

// ClearJumpList removes the app's custom jump list entries.
func ClearJumpList() error { return bridge.CallDo(context.Background(), IDClearJumpList, nil) }

// SetActivationPolicy sets how the app appears (macOS): "regular", "accessory"
// (a menu-bar-only app: no dock icon, no application menu) or "prohibited".
func SetActivationPolicy(policy string) error {
	return bridge.CallDo(context.Background(), IDSetActivationPolicy, func(e *wire.Encoder) { e.String(policy) })
}

// SetDockVisible shows or hides the dock icon (macOS): "regular" or
// "accessory" policy.
func SetDockVisible(on bool) error {
	return bridge.CallDo(context.Background(), IDSetDockVisible, func(e *wire.Encoder) { e.Bool(on) })
}

// OnEvent calls fn for every chrome event until cancel is called.
func OnEvent(fn func(Event)) (cancel func(), err error) {
	return bridge.Subscribe(context.Background(), IDEvents, func(d *wire.Decoder) {
		if ev, err := decodeEvent(d); err == nil {
			fn(ev)
		}
	})
}

// OnNewTab calls fn when the user asks for a new tab in this window's tab
// group.
func (w *Window) OnNewTab(fn func()) (cancel func(), err error) {
	return OnEvent(func(ev Event) {
		if ev.Window == w.ID && ev.Kind == EventNewTab {
			fn()
		}
	})
}

// OnThumbClick calls fn with the button id when one of this window's
// thumbnail buttons is clicked.
func (w *Window) OnThumbClick(fn func(id string)) (cancel func(), err error) {
	return OnEvent(func(ev Event) {
		if ev.Window == w.ID && ev.Kind == EventThumbClick {
			fn(ev.ID)
		}
	})
}

// toCaps converts the SDK types to the platform's.
func (o Overlay) toCaps() caps.TitleBarOverlay {
	return caps.TitleBarOverlay{Color: o.Color, SymbolColor: o.SymbolColor, Height: o.Height}
}
