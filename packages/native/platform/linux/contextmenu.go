//go:build linux

package linux

import (
	"errors"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var (
	_ caps.ContextMenus    = (*App)(nil)
	_ caps.ContextCommands = (*App)(nil)
)

// WebKitHitTestResultContext bits.
const (
	hitLink      = 1 << 2
	hitImage     = 1 << 3
	hitMedia     = 1 << 4
	hitEditable  = 1 << 5
	hitSelection = 1 << 7
)

// WebKitContextMenuAction values (checked against the library's GEnum by
// TestContextMenuActionValues).
const (
	actCopyLink         = 4
	actSpellingGuess    = 21
	actNoGuessesFound   = 22
	actIgnoreSpelling   = 23
	actLearnSpelling    = 24
	actInspectElement   = 31
	gdkTwoButtonPress   = 5
	gdkThreeButtonPress = 6
)

// ctxAPI holds the bindings of the context menu support. A missing symbol
// leaves its field nil and SetContextMenu reports krate.ErrUnsupported.
type ctxAPI struct {
	menuItems       func(menu uintptr) uintptr // GList of WebKitContextMenuItem
	menuAppend      func(menu, item uintptr)
	menuNew         func() uintptr
	itemStock       func(item uintptr) int32
	itemTitle       func(item uintptr) uintptr
	itemSeparator   func() uintptr
	itemFromGAction func(action uintptr, label string, target uintptr) uintptr
	itemWithSubmenu func(label string, sub uintptr) uintptr
	itemGAction     func(item uintptr) uintptr
	hitContext      func(hit uintptr) uint32
	hitLinkURI      func(hit uintptr) uintptr
	hitImageURI     func(hit uintptr) uintptr
	hitMediaURI     func(hit uintptr) uintptr
	execEditing     func(view uintptr, cmd string)
	execEditingArg  func(view uintptr, cmd, arg string)
	getInspector    func(view uintptr) uintptr
	inspectorShow   func(insp uintptr)
	simpleActionNew func(name string, ptype uintptr) uintptr
	simpleStateful  func(name string, ptype, state uintptr) uintptr
	simpleEnabled   func(action uintptr, on int32)
	variantBool     func(b int32) uintptr
	actionActivate  func(action, param uintptr)
	gObjectRef      func(o uintptr) uintptr
	gObjectUnref    func(o uintptr)
	translateCoords func(src, dst uintptr, sx, sy int32, dx, dy *int32) int32
	gEnumNickValue  func(class uintptr, nick string) uintptr
	gTypeClassRef   func(t uintptr) uintptr
	menuActionType  func() uintptr
	signalConnect   func(inst uintptr, sig string, cb, data, destroy uintptr, flags int32) uint64
}

var (
	ctxOnce sync.Once
	ctxFns  *ctxAPI

	cbContextMenu, cbCtxAction uintptr
)

func loadCtxAPI() *ctxAPI {
	ctxOnce.Do(func() {
		c := &ctxAPI{}
		b := &binder{}
		open := func(name string) uintptr {
			h, err := purego.Dlopen(name, rtldNow|rtldGlobal)
			if err != nil {
				return 0
			}
			return h
		}
		wk, gobj, gio, gtk := open("libwebkit2gtk-4.1.so.0"), open("libgobject-2.0.so.0"), open("libgio-2.0.so.0"), open("libgtk-3.so.0")
		bind := func(lib uintptr, fn any, sym string) {
			if lib != 0 {
				b.bind(fn, lib, sym)
			}
		}
		bind(wk, &c.menuItems, "webkit_context_menu_get_items")
		bind(wk, &c.menuAppend, "webkit_context_menu_append")
		bind(wk, &c.menuNew, "webkit_context_menu_new")
		bind(wk, &c.itemStock, "webkit_context_menu_item_get_stock_action")
		bind(wk, &c.itemTitle, "webkit_context_menu_item_get_title")
		bind(wk, &c.itemSeparator, "webkit_context_menu_item_new_separator")
		bind(wk, &c.itemFromGAction, "webkit_context_menu_item_new_from_gaction")
		bind(wk, &c.itemWithSubmenu, "webkit_context_menu_item_new_with_submenu")
		bind(wk, &c.itemGAction, "webkit_context_menu_item_get_gaction")
		bind(wk, &c.hitContext, "webkit_hit_test_result_get_context")
		bind(wk, &c.hitLinkURI, "webkit_hit_test_result_get_link_uri")
		bind(wk, &c.hitImageURI, "webkit_hit_test_result_get_image_uri")
		bind(wk, &c.hitMediaURI, "webkit_hit_test_result_get_media_uri")
		bind(wk, &c.execEditing, "webkit_web_view_execute_editing_command")
		bind(wk, &c.execEditingArg, "webkit_web_view_execute_editing_command_with_argument")
		bind(wk, &c.getInspector, "webkit_web_view_get_inspector")
		bind(wk, &c.inspectorShow, "webkit_web_inspector_show")
		bind(wk, &c.menuActionType, "webkit_context_menu_action_get_type")
		bind(gio, &c.simpleActionNew, "g_simple_action_new")
		bind(gio, &c.simpleStateful, "g_simple_action_new_stateful")
		bind(gio, &c.simpleEnabled, "g_simple_action_set_enabled")
		bind(gio, &c.actionActivate, "g_action_activate")
		bind(gobj, &c.gObjectRef, "g_object_ref")
		bind(gobj, &c.gObjectUnref, "g_object_unref")
		bind(gobj, &c.gEnumNickValue, "g_enum_get_value_by_nick")
		bind(gobj, &c.gTypeClassRef, "g_type_class_ref")
		bind(gobj, &c.signalConnect, "g_signal_connect_data")
		bind(open("libglib-2.0.so.0"), &c.variantBool, "g_variant_new_boolean")
		bind(gtk, &c.translateCoords, "gtk_widget_translate_coordinates")
		if len(b.missing) > 0 {
			// Keep the ones that loaded; each use checks what it needs.
			logf("krate: context menu support is incomplete (missing: %v)", b.missing)
		}
		cbContextMenu = purego.NewCallback(contextMenuCallback)
		cbCtxAction = purego.NewCallback(ctxActionCallback)
		ctxFns = c
	})
	return ctxFns
}

// contextState is the context menu state of the App (main thread only).
type contextState struct {
	app     caps.ContextMenuPolicy
	appSet  bool
	byWin   map[caps.WindowRef]caps.ContextMenuPolicy
	byView  map[caps.ViewRef]caps.ContextMenuPolicy
	handler func(caps.ContextMenuInfo)
	learn   uintptr                  // the engine's "learn spelling" item of the last menu, ref'd
	lastVis map[uintptr]caps.ViewRef // window id -> view of its last context menu
	// handles are the menu targets of the extension items of the last menu,
	// dropped when the next menu is built so they do not pile up.
	handles []uintptr
	// Seen by tests: how many menus the policy suppressed, and the titles of
	// the last engine menu after extension.
	suppressed int
	lastTitles []string
}

func (a *App) ctxSt() *contextState {
	if a.cm == nil {
		a.cm = &contextState{byWin: map[caps.WindowRef]caps.ContextMenuPolicy{}, byView: map[caps.ViewRef]caps.ContextMenuPolicy{}, lastVis: map[uintptr]caps.ViewRef{}}
	}
	return a.cm
}

// SetContextMenu sets the policy of a view, a window or the whole app.
func (a *App) SetContextMenu(window caps.WindowRef, view caps.ViewRef, p caps.ContextMenuPolicy) error {
	c := loadCtxAPI()
	if c.menuAppend == nil || c.hitContext == nil {
		return krate.ErrUnsupported
	}
	switch p.Mode {
	case caps.ContextMenuDefault, caps.ContextMenuNone, caps.ContextMenuCustom, caps.ContextMenuExtend:
	default:
		return errors.New("krate: unknown context menu mode " + string(p.Mode))
	}
	p.Items = append([]caps.MenuItem(nil), p.Items...)
	return a.onMainErr(func() error {
		st := a.ctxSt()
		switch {
		case view != 0:
			st.byView[view] = p
		case window != 0:
			st.byWin[window] = p
		default:
			st.app, st.appSet = p, true
		}
		return nil
	})
}

// OnContextMenu sets the handler for right clicks in custom views.
func (a *App) OnContextMenu(fn func(caps.ContextMenuInfo)) {
	a.callMain(func() { a.ctxSt().handler = fn })
}

func (st *contextState) policy(v *view) caps.ContextMenuPolicy {
	if p, ok := st.byView[v.ref]; ok {
		return p
	}
	if p, ok := st.byWin[caps.WindowRef(v.w.id)]; ok {
		return p
	}
	if st.appSet {
		return st.app
	}
	return caps.ContextMenuPolicy{Mode: caps.ContextMenuDefault}
}

// connectContext hooks the context-menu signal of a view.
func (a *App) connectContext(v *view) {
	c := loadCtxAPI()
	if c.signalConnect == nil || c.hitContext == nil {
		return
	}
	c.signalConnect(v.widget, "context-menu", cbContextMenu, uintptr(v.ref), 0, 0)
}

// contextMenuCallback is the WebKitWebView "context-menu" handler. Returning
// true keeps the engine from showing its menu.
func contextMenuCallback(view, menu, event, hit, ref uintptr) uintptr {
	a := theApp
	c := loadCtxAPI()
	v := a.viewByRef(ref)
	if v == nil {
		return 0
	}
	st := a.ctxSt()
	a.dropHandles(st.handles)
	st.handles = st.handles[:0]
	st.lastVis[v.w.id] = v.ref
	p := st.policy(v)
	switch p.Mode {
	case caps.ContextMenuNone:
		st.suppressed++
		return 1
	case caps.ContextMenuExtend:
		if len(p.Items) > 0 {
			a.appendExtra(menu, p.Items, caps.WindowRef(v.w.id))
		}
		st.lastTitles = st.lastTitles[:0]
		if c.menuItems != nil && c.itemTitle != nil {
			for n := c.menuItems(menu); n != 0; {
				node := *(*unsafe.Pointer)(unsafe.Pointer(&n))
				st.lastTitles = append(st.lastTitles, goString(c.itemTitle(*(*uintptr)(node))))
				n = *(*uintptr)(unsafe.Add(node, 8))
			}
		}
		return 0
	case caps.ContextMenuCustom:
		if st.handler == nil {
			return 0
		}
	default:
		return 0
	}
	info := caps.ContextMenuInfo{Window: caps.WindowRef(v.w.id), View: v.ref, Inspectable: a.debug, Kinds: []string{"page"}}
	ctxBits := c.hitContext(hit)
	if ctxBits&hitLink != 0 {
		info.Kinds = append(info.Kinds, "link")
		info.LinkURL = goString(c.hitLinkURI(hit))
	}
	if ctxBits&hitImage != 0 {
		info.Kinds = append(info.Kinds, "image")
		info.ImageURL = goString(c.hitImageURI(hit))
	}
	if ctxBits&hitMedia != 0 {
		info.Kinds = append(info.Kinds, "media")
		info.MediaURL = goString(c.hitMediaURI(hit))
	}
	if ctxBits&hitEditable != 0 {
		info.Kinds = append(info.Kinds, "editable")
	}
	hasSel := ctxBits&hitSelection != 0
	if hasSel {
		info.Kinds = append(info.Kinds, "selection")
	}
	info.X, info.Y, info.WindowX, info.WindowY = a.eventPosition(v, event)
	spell := false
	if st.learn != 0 {
		c.gObjectUnref(st.learn)
		st.learn = 0
	}
	if c.menuItems != nil && c.itemStock != nil {
		for n := c.menuItems(menu); n != 0; {
			node := *(*unsafe.Pointer)(unsafe.Pointer(&n))
			item := *(*uintptr)(node)
			n = *(*uintptr)(unsafe.Add(node, 8))
			switch c.itemStock(item) {
			case actSpellingGuess:
				spell = true
				if c.itemTitle != nil {
					if t := goString(c.itemTitle(item)); t != "" {
						info.Suggestions = append(info.Suggestions, t)
					}
				}
			case actNoGuessesFound, actIgnoreSpelling:
				spell = true
			case actLearnSpelling:
				spell = true
				st.learn = c.gObjectRef(item)
			}
		}
	}
	deliver := func(sel string) {
		info.SelectionText = sel
		if spell {
			// WebKit selects the misspelled word when it is right-clicked.
			info.Misspelled = sel
		}
		if fn := a.ctxSt().handler; fn != nil {
			fn(info)
		}
	}
	if !hasSel && !spell {
		deliver("")
		return 1
	}
	a.evalString(v.widget, "String(window.getSelection())", deliver)
	return 1
}

// eventPosition reads the pointer position from the GdkEvent: x and y in the
// view, then in the window. A key event has none, so it gives 0, 0.
func (a *App) eventPosition(v *view, event uintptr) (x, y, wx, wy int) {
	if event == 0 {
		return
	}
	base := *(*unsafe.Pointer)(unsafe.Pointer(&event))
	switch *(*int32)(base) {
	case gdkButtonPress, gdkTwoButtonPress, gdkThreeButtonPress:
	default:
		return
	}
	fx := *(*float64)(unsafe.Add(base, 24))
	fy := *(*float64)(unsafe.Add(base, 32))
	x, y, wx, wy = int(fx), int(fy), int(fx), int(fy)
	if c := loadCtxAPI(); c.translateCoords != nil && v.w.win != 0 {
		var dx, dy int32
		if c.translateCoords(v.widget, v.w.win, int32(x), int32(y), &dx, &dy) != 0 {
			wx, wy = int(dx), int(dy)
		}
	}
	return
}

// appendExtra adds items to the engine's menu, after a separator.
func (a *App) appendExtra(menu uintptr, items []caps.MenuItem, win caps.WindowRef) {
	c := loadCtxAPI()
	if c.itemSeparator != nil {
		c.menuAppend(menu, c.itemSeparator())
	}
	a.appendItems(menu, items, win)
}

func (a *App) appendItems(menu uintptr, items []caps.MenuItem, win caps.WindowRef) {
	c := loadCtxAPI()
	for _, it := range items {
		var wi uintptr
		switch {
		case it.Kind == caps.MenuItemSeparator:
			wi = c.itemSeparator()
		case len(it.Submenu) > 0 || it.Kind == caps.MenuItemSubmenu:
			if c.menuNew == nil || c.itemWithSubmenu == nil {
				continue
			}
			sub := c.menuNew()
			a.appendItems(sub, it.Submenu, win)
			wi = c.itemWithSubmenu(plainLabel(it.Label), sub)
		default:
			wi = a.actionItem(it, win)
		}
		if wi != 0 {
			c.menuAppend(menu, wi)
		}
	}
}

// plainLabel drops the mnemonic marker of a label.
func plainLabel(l string) string {
	out := make([]byte, 0, len(l))
	for i := 0; i < len(l); i++ {
		if l[i] == '&' {
			if i+1 < len(l) && l[i+1] == '&' {
				out = append(out, '&')
				i++
			}
			continue
		}
		out = append(out, l[i])
	}
	return string(out)
}

var ctxActionSeq uint64

func (a *App) actionItem(it caps.MenuItem, win caps.WindowRef) uintptr {
	c := loadCtxAPI()
	if c.simpleActionNew == nil || c.itemFromGAction == nil {
		return 0
	}
	ctxActionSeq++
	name := "krate-ctx-" + itoa(ctxActionSeq)
	check := it.Kind == caps.MenuItemCheck || it.Kind == caps.MenuItemRadio
	var action uintptr
	if check && c.simpleStateful != nil && c.variantBool != nil {
		action = c.simpleStateful(name, 0, c.variantBool(b2i(it.Checked)))
	} else {
		action = c.simpleActionNew(name, 0)
		check = false
	}
	if action == 0 {
		return 0
	}
	if it.Disabled && c.simpleEnabled != nil {
		c.simpleEnabled(action, 0)
	}
	menuMu.Lock()
	menuNext++
	h := menuNext
	menuTargets[h] = &menuTarget{id: it.ID, win: win, isCtrl: check, checked: it.Checked}
	menuMu.Unlock()
	st := a.ctxSt()
	st.handles = append(st.handles, h)
	c.signalConnect(action, "activate", cbCtxAction, h, 0, 0)
	return c.itemFromGAction(action, plainLabel(it.Label), 0)
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for ; n > 0; n /= 10 {
		i--
		b[i] = byte('0' + n%10)
	}
	return string(b[i:])
}

// ctxActionCallback handles the activation of an extension item.
func ctxActionCallback(action, param, handle uintptr) uintptr {
	menuMu.Lock()
	t := menuTargets[handle]
	fn := menuFn
	if t != nil && t.isCtrl {
		t.checked = !t.checked
	}
	menuMu.Unlock()
	if t == nil || fn == nil {
		return 0
	}
	fn(caps.MenuEvent{ID: t.id, Window: t.win, Checked: t.isCtrl && t.checked})
	return 0
}

// ContextCommand runs a built-in command on the view that last showed a
// context menu in the window.
func (a *App) ContextCommand(window caps.WindowRef, cmd, arg string) error {
	c := loadCtxAPI()
	return a.onMainErr(func() error {
		w := a.windowByID(uintptr(window))
		if w == nil || !w.alive {
			return errors.New("krate: no such window")
		}
		widget := w.view
		if ref, ok := a.ctxSt().lastVis[w.id]; ok {
			if v := a.viewByRef(uintptr(ref)); v != nil && v.widget != 0 {
				widget = v.widget
			}
		}
		if widget == 0 {
			return errors.New("krate: the window has no webview")
		}
		editing := map[string]string{
			caps.CommandUndo: "Undo", caps.CommandRedo: "Redo", caps.CommandCut: "Cut", caps.CommandCopy: "Copy",
			caps.CommandPaste: "Paste", caps.CommandSelectAll: "SelectAll", caps.CommandDelete: "Delete",
		}
		if name, ok := editing[cmd]; ok {
			if c.execEditing == nil {
				return krate.ErrUnsupported
			}
			c.execEditing(widget, name)
			return nil
		}
		switch cmd {
		case caps.CommandReplace:
			if c.execEditingArg == nil {
				return krate.ErrUnsupported
			}
			c.execEditingArg(widget, "InsertText", arg)
			return nil
		case caps.CommandInspect:
			if c.getInspector == nil || c.inspectorShow == nil || !a.debug {
				return krate.ErrUnsupported
			}
			if insp := c.getInspector(widget); insp != 0 {
				c.inspectorShow(insp)
			}
			return nil
		case caps.CommandAddToDict:
			st := a.ctxSt()
			if st.learn == 0 || c.itemGAction == nil || c.actionActivate == nil {
				return krate.ErrUnsupported
			}
			ga := c.itemGAction(st.learn)
			if ga == 0 {
				return krate.ErrUnsupported
			}
			c.actionActivate(ga, 0)
			return nil
		}
		return errors.New("krate: unknown context menu command " + cmd)
	})
}
