//go:build windows

package windows

import (
	"encoding/json"
	"errors"
	"sync"
	"unsafe"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var (
	_ caps.ContextMenus    = (*App)(nil)
	_ caps.ContextCommands = (*App)(nil)
)

// Target kinds of ICoreWebView2ContextMenuTarget.
const (
	cmTargetImage = 1
	cmTargetAudio = 3
	cmTargetVideo = 4

	cmKindCommand = 0
	cmKindCheck   = 1
	cmKindRadio   = 2
	cmKindSep     = 3
	cmKindSubmenu = 4
)

// ctxState is the context menu configuration; the handlers run on the main
// thread, the setters on any goroutine.
type ctxState struct {
	mu      sync.Mutex
	app     caps.ContextMenuPolicy
	appSet  bool
	byWin   map[caps.WindowRef]caps.ContextMenuPolicy
	byView  map[caps.ViewRef]caps.ContextMenuPolicy
	handler func(caps.ContextMenuInfo)
	last    map[caps.WindowRef]caps.ViewRef // view of the last menu per window
}

var cmState = &ctxState{
	byWin: map[caps.WindowRef]caps.ContextMenuPolicy{}, byView: map[caps.ViewRef]caps.ContextMenuPolicy{},
	last: map[caps.WindowRef]caps.ViewRef{},
}

// SetContextMenu sets the policy of a view, a window or the whole app.
func (a *App) SetContextMenu(window caps.WindowRef, view caps.ViewRef, p caps.ContextMenuPolicy) error {
	switch p.Mode {
	case caps.ContextMenuDefault, caps.ContextMenuNone, caps.ContextMenuCustom, caps.ContextMenuExtend:
	default:
		return errors.New("windows: unknown context menu mode " + string(p.Mode))
	}
	p.Items = append([]caps.MenuItem(nil), p.Items...)
	cmState.mu.Lock()
	defer cmState.mu.Unlock()
	switch {
	case view != 0:
		cmState.byView[view] = p
	case window != 0:
		cmState.byWin[window] = p
	default:
		cmState.app, cmState.appSet = p, true
	}
	return nil
}

// OnContextMenu sets the handler for right clicks in custom views.
func (a *App) OnContextMenu(fn func(caps.ContextMenuInfo)) {
	cmState.mu.Lock()
	cmState.handler = fn
	cmState.mu.Unlock()
}

func (st *ctxState) policy(v *view) caps.ContextMenuPolicy {
	st.mu.Lock()
	defer st.mu.Unlock()
	if p, ok := st.byView[v.ref]; ok {
		return p
	}
	if p, ok := st.byWin[v.w.Ref()]; ok {
		return p
	}
	if st.appSet {
		return st.app
	}
	return caps.ContextMenuPolicy{Mode: caps.ContextMenuDefault}
}

// hookContextMenu registers the ContextMenuRequested handler of a view. A
// runtime too old for ICoreWebView2_11 keeps the default menu.
func (a *App) hookContextMenu(v *view) {
	wv11 := queryInterface(v.webview, iidWebView2_11)
	if wv11 == nil {
		logOnce("ctxmenu", "context menus: ICoreWebView2_11 is not available; the default menu stays")
		return
	}
	defer comRelease(wv11)
	v.hCtx = newHandler(iidContextMenuRequestedHandler, false, func(sender, args uintptr) uintptr {
		a.onContextMenuRequested(v, ptr(args))
		return sOK
	})
	var token int64
	if failed(vcall(wv11, wvAddContextMenuRequested, uintptr(v.hCtx), uintptr(unsafe.Pointer(&token)))) {
		logOnce("ctxmenu", "context menus: add_ContextMenuRequested failed; the default menu stays")
	}
}

func getBool(obj unsafe.Pointer, slot int) bool {
	var b int32
	vcall(obj, slot, uintptr(unsafe.Pointer(&b)))
	return b != 0
}

func (a *App) onContextMenuRequested(v *view, args unsafe.Pointer) {
	if args == nil {
		return
	}
	p := cmState.policy(v)
	cmState.mu.Lock()
	cmState.last[v.w.Ref()] = v.ref
	handler := cmState.handler
	cmState.mu.Unlock()
	switch p.Mode {
	case caps.ContextMenuNone:
		vcall(args, cmArgsPutHandled, 1)
		return
	case caps.ContextMenuExtend:
		if len(p.Items) > 0 {
			a.extendMenu(v, args, p.Items)
		}
		return
	case caps.ContextMenuCustom:
		if handler == nil {
			return
		}
	default:
		return
	}
	vcall(args, cmArgsPutHandled, 1)
	info := caps.ContextMenuInfo{Window: v.w.Ref(), View: v.ref, Kinds: []string{"page"}, Inspectable: a.cfg.Debug}
	var loc point
	vcall(args, cmArgsGetLocation, uintptr(unsafe.Pointer(&loc)))
	scale := float64(windowDPI(v.w.hwnd)) / 96
	if scale <= 0 {
		scale = 1
	}
	ox, oy := 0, 0
	for _, pane := range v.w.sp.arrange().Panes {
		if pane.View == v.ref {
			ox, oy = pane.Rect.X, pane.Rect.Y
		}
	}
	info.X, info.Y = int(float64(loc.x)/scale), int(float64(loc.y)/scale)
	info.WindowX, info.WindowY = int(float64(int(loc.x)+ox)/scale), int(float64(int(loc.y)+oy)/scale)

	var target unsafe.Pointer
	if !failed(vcall(args, cmArgsGetTarget, uintptr(unsafe.Pointer(&target)))) && target != nil {
		defer comRelease(target)
		var kind int32
		vcall(target, cmTargetGetKind, uintptr(unsafe.Pointer(&kind)))
		if getBool(target, cmTargetGetIsEditable) {
			info.Kinds = append(info.Kinds, "editable")
		}
		if getBool(target, cmTargetGetHasLinkUri) {
			info.Kinds = append(info.Kinds, "link")
			info.LinkURL = comString(target, cmTargetGetLinkUri)
		}
		if getBool(target, cmTargetGetHasSourceUri) {
			src := comString(target, cmTargetGetSourceUri)
			switch kind {
			case cmTargetImage:
				info.Kinds = append(info.Kinds, "image")
				info.ImageURL = src
			case cmTargetAudio, cmTargetVideo:
				info.Kinds = append(info.Kinds, "media")
				info.MediaURL = src
			}
		}
		if getBool(target, cmTargetGetHasSelection) {
			info.Kinds = append(info.Kinds, "selection")
			info.SelectionText = comString(target, cmTargetGetSelectionText)
		}
	}
	a.spellFromMenu(args, &info)
	handler(info)
}

// spellFromMenu reads spelling suggestions from the engine's menu items: the
// items Chromium names "spellCheck" ahead of the editing commands.
func (a *App) spellFromMenu(args unsafe.Pointer, info *caps.ContextMenuInfo) {
	if !info.Has("editable") {
		return
	}
	var items unsafe.Pointer
	if failed(vcall(args, cmArgsGetMenuItems, uintptr(unsafe.Pointer(&items)))) || items == nil {
		return
	}
	defer comRelease(items)
	var n uint32
	vcall(items, cmItemsGetCount, uintptr(unsafe.Pointer(&n)))
	for i := uint32(0); i < n; i++ {
		var it unsafe.Pointer
		if failed(vcall(items, cmItemsGetAt, uintptr(i), uintptr(unsafe.Pointer(&it)))) || it == nil {
			continue
		}
		if comString(it, cmItemGetName) == "spellCheck" {
			if l := comString(it, cmItemGetLabel); l != "" {
				info.Suggestions = append(info.Suggestions, l)
			}
		}
		comRelease(it)
	}
	if len(info.Suggestions) > 0 {
		info.Misspelled = info.SelectionText
	}
}

// extendMenu appends custom items, after a separator, to the engine's menu.
func (a *App) extendMenu(v *view, args unsafe.Pointer, items []caps.MenuItem) {
	env9 := queryInterface(a.env, iidEnvironment9)
	if env9 == nil {
		logOnce("ctxmenu-env9", "context menus: ICoreWebView2Environment9 is not available; items cannot be added")
		return
	}
	defer comRelease(env9)
	var col unsafe.Pointer
	if failed(vcall(args, cmArgsGetMenuItems, uintptr(unsafe.Pointer(&col)))) || col == nil {
		return
	}
	defer comRelease(col)
	sep := a.newCtxItem(env9, caps.MenuItem{Kind: caps.MenuItemSeparator}, v)
	var n uint32
	vcall(col, cmItemsGetCount, uintptr(unsafe.Pointer(&n)))
	if sep != nil {
		vcall(col, cmItemsInsertAt, uintptr(n), uintptr(sep))
		n++
		comRelease(sep)
	}
	for _, it := range items {
		if ni := a.newCtxItem(env9, it, v); ni != nil {
			vcall(col, cmItemsInsertAt, uintptr(n), uintptr(ni))
			n++
			comRelease(ni)
		}
	}
}

func (a *App) newCtxItem(env9 unsafe.Pointer, it caps.MenuItem, v *view) unsafe.Pointer {
	kind := uintptr(cmKindCommand)
	switch {
	case it.Kind == caps.MenuItemSeparator:
		kind = cmKindSep
	case len(it.Submenu) > 0 || it.Kind == caps.MenuItemSubmenu:
		kind = cmKindSubmenu
	case it.Kind == caps.MenuItemCheck:
		kind = cmKindCheck
	case it.Kind == caps.MenuItemRadio:
		kind = cmKindRadio
	}
	var item unsafe.Pointer
	label := plainMenuLabel(it.Label)
	if failed(vcall(env9, env9CreateContextMenuItem, uintptr(unsafe.Pointer(wstr(label))), 0, kind, uintptr(unsafe.Pointer(&item)))) || item == nil {
		return nil
	}
	if it.Disabled {
		vcall(item, cmItemPutIsEnabled, 0)
	}
	switch kind {
	case cmKindCheck, cmKindRadio:
		if it.Checked {
			vcall(item, cmItemPutIsChecked, 1)
		}
	case cmKindSubmenu:
		var kids unsafe.Pointer
		if !failed(vcall(item, cmItemGetChildren, uintptr(unsafe.Pointer(&kids)))) && kids != nil {
			for i, sub := range it.Submenu {
				if c := a.newCtxItem(env9, sub, v); c != nil {
					vcall(kids, cmItemsInsertAt, uintptr(i), uintptr(c))
					comRelease(c)
				}
			}
			comRelease(kids)
		}
	}
	if kind != cmKindSep && kind != cmKindSubmenu {
		id, win := it.ID, v.w.Ref()
		h := newHandler(iidCustomItemSelectedHandler, false, func(sender, _ uintptr) uintptr {
			ev := caps.MenuEvent{ID: id, Window: win}
			if kind == cmKindCheck || kind == cmKindRadio {
				ev.Checked = getBool(ptr(sender), cmItemGetIsChecked)
			}
			extMu.Lock()
			fn := menuFn
			extMu.Unlock()
			if fn != nil {
				fn(ev)
			}
			return sOK
		})
		var token int64
		vcall(item, cmItemAddCustomSelected, uintptr(h), uintptr(unsafe.Pointer(&token)))
	}
	return item
}

// plainMenuLabel drops the "&" mnemonic marker.
func plainMenuLabel(l string) string {
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

// ContextCommand runs a built-in command on the view that last showed a
// context menu in the window. Editing commands go through the DevTools
// protocol (Input.dispatchKeyEvent with editing commands), which acts on the
// focused element like the engine's own menu does.
func (a *App) ContextCommand(window caps.WindowRef, cmd, arg string) error {
	var err error
	a.callMain(func() {
		w := a.windows[a.parentHwnd(window)]
		if w == nil || w.destroyed {
			err = errors.New("windows: no such window")
			return
		}
		v := w.pv
		cmState.mu.Lock()
		if ref, ok := cmState.last[window]; ok {
			if lv := w.viewByRef(ref); lv != nil {
				v = lv
			}
		}
		cmState.mu.Unlock()
		if v.webview == nil {
			err = errors.New("windows: the window has no webview")
			return
		}
		cdp := func(method string, params any) {
			b, _ := json.Marshal(params)
			vcall(v.webview, wvCallDevToolsProtocol, uintptr(unsafe.Pointer(wstr(method))), uintptr(unsafe.Pointer(wstr(string(b)))), 0)
		}
		switch cmd {
		case caps.CommandUndo, caps.CommandRedo, caps.CommandCut, caps.CommandCopy, caps.CommandPaste, caps.CommandSelectAll, caps.CommandDelete:
			cdp("Input.dispatchKeyEvent", map[string]any{"type": "rawKeyDown", "commands": []string{cmd}})
		case caps.CommandReplace:
			cdp("Input.insertText", map[string]any{"text": arg})
		case caps.CommandInspect:
			if !a.cfg.Debug {
				err = krate.ErrUnsupported
				return
			}
			vcall(v.webview, wvOpenDevToolsWindow)
		case caps.CommandAddToDict:
			err = krate.ErrUnsupported
		default:
			err = errors.New("windows: unknown context menu command " + cmd)
		}
	})
	return err
}
