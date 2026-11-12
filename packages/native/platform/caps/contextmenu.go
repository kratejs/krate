package caps

// ContextMenuMode says what a right click in a webview does.
type ContextMenuMode string

// Context menu modes.
const (
	// ContextMenuDefault shows the engine's own menu.
	ContextMenuDefault ContextMenuMode = "default"
	// ContextMenuNone shows nothing.
	ContextMenuNone ContextMenuMode = "none"
	// ContextMenuCustom shows nothing itself and reports the click with
	// OnContextMenu; the host answers by popping up a menu of its own.
	ContextMenuCustom ContextMenuMode = "custom"
	// ContextMenuExtend shows the engine's menu with Items added at the end.
	ContextMenuExtend ContextMenuMode = "extend"
)

// ContextMenuPolicy is the context menu behavior of a window or view.
type ContextMenuPolicy struct {
	Mode ContextMenuMode
	// Items are appended to the engine's menu in ContextMenuExtend. Clicks
	// arrive as ordinary menu events.
	Items []MenuItem
}

// ContextMenuInfo describes what was right-clicked.
type ContextMenuInfo struct {
	Window WindowRef
	View   ViewRef
	// X and Y are in the view's client coordinates, like a DOM MouseEvent's
	// clientX and clientY. WindowX and WindowY are in the window's, which is
	// what PopupMenu takes.
	X, Y             int
	WindowX, WindowY int
	// Kinds lists what is under the pointer: "page" always, plus "link",
	// "image", "media", "editable" and "selection" as they apply.
	Kinds         []string
	LinkURL       string
	ImageURL      string
	MediaURL      string
	SelectionText string
	// Misspelled is the misspelled word under the pointer and Suggestions its
	// corrections. They are empty unless spell checking is on and the engine
	// reports them.
	Misspelled  string
	Suggestions []string
	// Inspectable is true when the webview has developer tools (dev builds).
	Inspectable bool
}

// Has reports whether kind is among Kinds.
func (i ContextMenuInfo) Has(kind string) bool {
	for _, k := range i.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// ContextMenus is implemented by Apps that can take over the webview's
// context menu.
type ContextMenus interface {
	// SetContextMenu sets the policy of one view, of a whole window (view 0),
	// or, with window 0, of every window that has none of its own. A view's
	// policy beats its window's, which beats the app's.
	SetContextMenu(window WindowRef, view ViewRef, p ContextMenuPolicy) error
	// OnContextMenu sets the handler for right clicks in ContextMenuCustom
	// views. fn runs on the main thread and must not block.
	OnContextMenu(fn func(ContextMenuInfo))
}

// Built-in context menu commands. A menu item whose id is "krate:" plus one
// of these (and ":" plus an argument for the last three) runs the command on
// the view that showed the context menu, instead of being reported.
const (
	CommandPrefix = "krate:"

	CommandUndo       = "undo"
	CommandRedo       = "redo"
	CommandCut        = "cut"
	CommandCopy       = "copy"
	CommandPaste      = "paste"
	CommandSelectAll  = "selectAll"
	CommandDelete     = "delete"
	CommandInspect    = "inspectElement"
	CommandReplace    = "replaceMisspelling" // argument: the replacement
	CommandAddToDict  = "addToDictionary"
	CommandCopyLink   = "copyLink"     // argument: the URL
	CommandCopyImgURL = "copyImageUrl" // argument: the URL
)

// ContextCommands is implemented by Apps that can run the built-in context
// menu commands.
type ContextCommands interface {
	// ContextCommand runs a command on the view of the window that last
	// showed a context menu (the primary view when none did).
	ContextCommand(window WindowRef, cmd, arg string) error
}
