package menu

import (
	"context"
	"log"
	"strings"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// Host function ids of the webview context menu.
const (
	IDSetContext    = "krate/menu.setContextMenu"
	IDContextEvents = "krate/menu.contextEvents"
)

// ContextMode says what a right click in a webview does.
type ContextMode string

// Context menu modes.
const (
	// ContextDefault shows the engine's own menu (the default).
	ContextDefault ContextMode = "default"
	// ContextNone shows no menu.
	ContextNone ContextMode = "none"
	// ContextCustom shows no menu and reports the click (OnContext); the
	// handler answers with items, which are drawn by the native menu code.
	ContextCustom ContextMode = "custom"
	// ContextExtend shows the engine's menu with Items added at the end.
	ContextExtend ContextMode = "extend"
)

// ContextOptions is the context menu behavior of a window or view.
type ContextOptions struct {
	Mode ContextMode
	// Items are added to the engine's menu in ContextExtend. Their clicks are
	// delivered like any other menu click (OnClick).
	Items []Item
}

// ContextInfo describes what was right-clicked.
type ContextInfo struct {
	// Window and View are SDK ids; the primary view has its window's id.
	Window, View string
	// X and Y are in the view's client coordinates (like a DOM MouseEvent's
	// clientX and clientY); WindowX and WindowY are in the window's, which is
	// what Popup takes.
	X, Y             int
	WindowX, WindowY int
	// Kinds lists what is under the pointer: "page", "link", "image",
	// "media", "editable", "selection".
	Kinds         []string
	LinkURL       string
	ImageURL      string
	MediaURL      string
	SelectionText string
	// Misspelled and Suggestions are set when spell checking is on
	// (native.features.spellcheck) and the engine found a misspelled word.
	Misspelled  string
	Suggestions []string
	// Inspectable is true in dev builds, where the webview has developer tools.
	Inspectable bool
}

// Has reports whether kind is among Kinds.
func (i ContextInfo) Has(kind string) bool {
	for _, k := range i.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// Ids of the built-in actions a menu item can name. The shell runs them on
// the view that showed the context menu instead of reporting the click.
const (
	ActionUndo       = caps.CommandPrefix + caps.CommandUndo
	ActionRedo       = caps.CommandPrefix + caps.CommandRedo
	ActionCut        = caps.CommandPrefix + caps.CommandCut
	ActionCopy       = caps.CommandPrefix + caps.CommandCopy
	ActionPaste      = caps.CommandPrefix + caps.CommandPaste
	ActionSelectAll  = caps.CommandPrefix + caps.CommandSelectAll
	ActionDelete     = caps.CommandPrefix + caps.CommandDelete
	ActionInspect    = caps.CommandPrefix + caps.CommandInspect
	ActionAddToDict  = caps.CommandPrefix + caps.CommandAddToDict
	actionReplace    = caps.CommandPrefix + caps.CommandReplace + ":"
	actionCopyLink   = caps.CommandPrefix + caps.CommandCopyLink + ":"
	actionCopyImgURL = caps.CommandPrefix + caps.CommandCopyImgURL + ":"
)

// ActionReplace is the id of the item that replaces the misspelled word with
// word. ActionCopyLink and ActionCopyImageURL copy a URL to the clipboard.
func ActionReplace(word string) string     { return actionReplace + word }
func ActionCopyLink(url string) string     { return actionCopyLink + url }
func ActionCopyImageURL(url string) string { return actionCopyImgURL + url }

// DefaultContextItems is the menu a browser would show for info, built from
// the built-in actions, so a custom menu can start from it and add its own
// items. Labels are English. Editable fields get undo, redo, cut, copy, paste
// and select all, with spelling suggestions and "Add to Dictionary" above them
// when the engine reported a misspelling; a selection gets copy; a link gets
// "Copy Link Address"; dev builds end with "Inspect Element".
func DefaultContextItems(info ContextInfo) []Item {
	var items []Item
	sep := func() {
		if len(items) > 0 && items[len(items)-1].Kind != KindSeparator {
			items = append(items, Separator())
		}
	}
	editable := info.Has("editable")
	if editable && info.Misspelled != "" {
		for _, s := range info.Suggestions {
			items = append(items, Action(ActionReplace(s), s, ""))
		}
		if len(info.Suggestions) == 0 {
			items = append(items, Item{Label: "No Guesses Found", Disabled: true})
		}
		items = append(items, Action(ActionAddToDict, "Add to Dictionary", ""))
		sep()
	}
	if info.Has("link") && info.LinkURL != "" {
		items = append(items, Action(ActionCopyLink(info.LinkURL), "Copy Link Address", ""))
		sep()
	}
	if info.Has("image") && info.ImageURL != "" {
		items = append(items, Action(ActionCopyImageURL(info.ImageURL), "Copy Image Address", ""))
		sep()
	}
	if editable {
		items = append(items,
			Action(ActionUndo, "Undo", "CmdOrCtrl+Z"),
			Action(ActionRedo, "Redo", "CmdOrCtrl+Shift+Z"),
			Separator(),
			Action(ActionCut, "Cut", "CmdOrCtrl+X"),
			Action(ActionCopy, "Copy", "CmdOrCtrl+C"),
			Action(ActionPaste, "Paste", "CmdOrCtrl+V"),
			Action(ActionSelectAll, "Select All", "CmdOrCtrl+A"),
		)
	} else if info.Has("selection") {
		items = append(items, Action(ActionCopy, "Copy", "CmdOrCtrl+C"), Action(ActionSelectAll, "Select All", "CmdOrCtrl+A"))
	}
	if info.Inspectable {
		sep()
		items = append(items, Action(ActionInspect, "Inspect Element", ""))
	}
	for len(items) > 0 && items[len(items)-1].Kind == KindSeparator {
		items = items[:len(items)-1]
	}
	return items
}

// SetContextMenu sets how right clicks behave in a window ("" = the main
// window, "*" = every window that has no setting of its own) or, when view is
// not empty, in one view.
func SetContextMenu(window, view string, o ContextOptions) error {
	return bridge.CallDo(context.Background(), IDSetContext, func(e *wire.Encoder) {
		e.String(window)
		e.String(view)
		e.BeginObject()
		e.Key("mode")
		e.String(string(o.Mode))
		e.Key("items")
		encodeItems(e, o.Items)
		e.EndObject()
	})
}

// OnContext calls fn for every right click in a view whose mode is
// ContextCustom, until cancel is called.
func OnContext(fn func(ContextInfo)) (cancel func(), err error) {
	return bridge.Subscribe(context.Background(), IDContextEvents, func(d *wire.Decoder) {
		if info, err := decodeContextInfo(d); err == nil {
			fn(info)
		}
	})
}

// OnContextMenu is OnContext plus the answer: fn returns the items to show,
// and the menu pops up at the click. A nil or empty answer shows nothing.
// Start from DefaultContextItems to keep the usual entries.
func OnContextMenu(fn func(ContextInfo) []Item) (cancel func(), err error) {
	return OnContext(func(info ContextInfo) {
		items := fn(info)
		if len(items) == 0 {
			return
		}
		_ = Popup(New(items...), info.Window, info.WindowX, info.WindowY) // the window may have closed meanwhile
	})
}

func encodeContextInfo(e *wire.Encoder, i ContextInfo) {
	e.BeginObject()
	str := func(k, v string) { e.Key(k); e.String(v) }
	num := func(k string, v int) { e.Key(k); e.Int(int64(v)) }
	str("window", i.Window)
	str("view", i.View)
	num("x", i.X)
	num("y", i.Y)
	num("windowX", i.WindowX)
	num("windowY", i.WindowY)
	e.Key("kinds")
	codec.WriteStrings(e, i.Kinds)
	str("linkUrl", i.LinkURL)
	str("imageUrl", i.ImageURL)
	str("mediaUrl", i.MediaURL)
	str("selectionText", i.SelectionText)
	str("misspelled", i.Misspelled)
	e.Key("suggestions")
	codec.WriteStrings(e, i.Suggestions)
	e.Key("inspectable")
	e.Bool(i.Inspectable)
	e.EndObject()
}

func decodeContextInfo(d *wire.Decoder) (i ContextInfo, err error) {
	r := &codec.R{D: d, Name: "context menu event"}
	r.Object(func(k string) bool {
		switch k {
		case "window":
			i.Window = r.String()
		case "view":
			i.View = r.String()
		case "x":
			i.X = int(r.Int())
		case "y":
			i.Y = int(r.Int())
		case "windowX":
			i.WindowX = int(r.Int())
		case "windowY":
			i.WindowY = int(r.Int())
		case "kinds":
			i.Kinds = r.Strings()
		case "linkUrl":
			i.LinkURL = r.String()
		case "imageUrl":
			i.ImageURL = r.String()
		case "mediaUrl":
			i.MediaURL = r.String()
		case "selectionText":
			i.SelectionText = r.String()
		case "misspelled":
			i.Misspelled = r.String()
		case "suggestions":
			i.Suggestions = r.Strings()
		case "inspectable":
			i.Inspectable = r.Bool()
		default:
			return false
		}
		return true
	})
	return i, r.Err()
}

// runBuiltin runs a "krate:" menu item and reports whether id was one.
func runBuiltin(env *bridge.Env, ev caps.MenuEvent) bool {
	if !strings.HasPrefix(ev.ID, caps.CommandPrefix) {
		return false
	}
	// The menu handler runs on the platform's main thread; the commands go
	// back to it, so they run on their own goroutine.
	go runCommand(env, ev)
	return true
}

func runCommand(env *bridge.Env, ev caps.MenuEvent) {
	cmd, arg, _ := strings.Cut(strings.TrimPrefix(ev.ID, caps.CommandPrefix), ":")
	var err error
	switch cmd {
	case caps.CommandCopyLink, caps.CommandCopyImgURL:
		if cb, ok := env.App.(caps.Clipboard); ok {
			err = cb.WriteText(arg)
		} else {
			err = krate.ErrUnsupported
		}
	default:
		if cc, ok := env.App.(caps.ContextCommands); ok {
			err = cc.ContextCommand(ev.Window, cmd, arg)
		} else {
			err = krate.ErrUnsupported
		}
	}
	if err != nil {
		log.Printf("krate: menu: %s: %v", ev.ID, err)
	}
}

func registerContext(t *bridge.Table, env *bridge.Env) {
	topic := env.Topic("menu.context")
	t.Stream(IDContextEvents, Permission, true, topic.Stream())
	if cm, ok := env.App.(caps.ContextMenus); ok {
		cm.OnContextMenu(func(ci caps.ContextMenuInfo) {
			info := ContextInfo{
				Window: env.Windows.ID(ci.Window), X: ci.X, Y: ci.Y, WindowX: ci.WindowX, WindowY: ci.WindowY,
				Kinds: ci.Kinds, LinkURL: ci.LinkURL, ImageURL: ci.ImageURL, MediaURL: ci.MediaURL,
				SelectionText: ci.SelectionText, Misspelled: ci.Misspelled, Suggestions: ci.Suggestions, Inspectable: ci.Inspectable,
			}
			if rec, ok := env.Windows.ViewByRef(ci.View); ok {
				info.View = rec.ID
			} else {
				info.View = info.Window
			}
			topic.Publish(func(e *wire.Encoder) { encodeContextInfo(e, info) })
		})
	}
	t.Func(IDSetContext, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSetContext)
		window, view := r.String(), r.String()
		var o ContextOptions
		r.Object(func(k string) bool {
			switch k {
			case "mode":
				o.Mode = ContextMode(r.String())
			case "items":
				o.Items = decodeItems(r, 0)
			default:
				return false
			}
			return true
		})
		if err := r.Done(); err != nil {
			return err
		}
		if o.Mode == "" {
			o.Mode = ContextDefault
		}
		cm, err := bridge.Cap[caps.ContextMenus](env.App, "context menus")
		if err != nil {
			return err
		}
		var wref caps.WindowRef
		var vref caps.ViewRef
		switch {
		case window == "*" && view == "":
		case view != "":
			rec, ok := env.Windows.View(view)
			if !ok {
				return krate.Errorf("not_found", "no view %q", view)
			}
			vref = rec.Ref
		default:
			if wref = env.Windows.Ref(window); wref == 0 {
				return krate.Errorf("not_found", "no window %q", window)
			}
		}
		if o.Mode != ContextExtend && len(o.Items) > 0 {
			return krate.Errorf("bad_args", "items are only used in %q mode", ContextExtend)
		}
		out.Null()
		return cm.SetContextMenu(wref, vref, caps.ContextMenuPolicy{Mode: caps.ContextMenuMode(o.Mode), Items: toCaps(o.Items)})
	})
}
