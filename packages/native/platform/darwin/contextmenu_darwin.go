//go:build darwin

package darwin

import (
	"encoding/json"
	"errors"

	"github.com/ebitengine/purego/objc"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var (
	_ caps.ContextMenus    = (*App)(nil)
	_ caps.ContextCommands = (*App)(nil)
)

// ctxState is the context menu configuration (main thread only).
type ctxState struct {
	app     caps.ContextMenuPolicy
	appSet  bool
	byWin   map[caps.WindowRef]caps.ContextMenuPolicy
	byView  map[caps.ViewRef]caps.ContextMenuPolicy
	handler func(caps.ContextMenuInfo)
	last    map[caps.WindowRef]caps.ViewRef
	// Seen by tests.
	suppressed int
}

func (a *App) ctxSt() *ctxState {
	if a.cm == nil {
		a.cm = &ctxState{byWin: map[caps.WindowRef]caps.ContextMenuPolicy{}, byView: map[caps.ViewRef]caps.ContextMenuPolicy{}, last: map[caps.WindowRef]caps.ViewRef{}}
	}
	return a.cm
}

// SetContextMenu sets the policy of a view, a window or the whole app.
func (a *App) SetContextMenu(window caps.WindowRef, view caps.ViewRef, p caps.ContextMenuPolicy) error {
	switch p.Mode {
	case caps.ContextMenuDefault, caps.ContextMenuNone, caps.ContextMenuCustom, caps.ContextMenuExtend:
	default:
		return errors.New("darwin: unknown context menu mode " + string(p.Mode))
	}
	p.Items = append([]caps.MenuItem(nil), p.Items...)
	runOnMain(func() {
		st := a.ctxSt()
		switch {
		case view != 0:
			st.byView[view] = p
		case window != 0:
			st.byWin[window] = p
		default:
			st.app, st.appSet = p, true
		}
	})
	return nil
}

// OnContextMenu sets the handler for right clicks in custom views.
func (a *App) OnContextMenu(fn func(caps.ContextMenuInfo)) {
	runOnMain(func() { a.ctxSt().handler = fn })
}

func (st *ctxState) policy(v *view) caps.ContextMenuPolicy {
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

// WKMenuItemIdentifier values of the items WebKit puts in its menu.
const (
	idSpellingGuess = "WKMenuItemIdentifierSpellingGuess"
	idLearnSpelling = "WKMenuItemIdentifierLearnSpelling"
	idNoGuesses     = "WKMenuItemIdentifierNoGuessesFound"
)

// webViewWillOpenMenu applies the policy to the menu about to open. The menu
// already holds WebKit's items.
func (a *App) webViewWillOpenMenu(web, menu, event objc.ID) {
	v := a.byWeb[web]
	if v == nil {
		return
	}
	st := a.ctxSt()
	st.last[v.w.Ref()] = v.ref
	p := st.policy(v)
	switch p.Mode {
	case caps.ContextMenuNone:
		menu.Send(sel("removeAllItems"))
		st.suppressed++
		return
	case caps.ContextMenuExtend:
		if len(p.Items) > 0 {
			a.ensureMenuTarget()
			menu.Send(sel("addItem:"), cls("NSMenuItem").Send(sel("separatorItem")))
			for _, it := range buildNSMenuItems(p.Items) {
				menu.Send(sel("addItem:"), it)
			}
		}
		return
	case caps.ContextMenuCustom:
		if st.handler == nil {
			return
		}
	default:
		return
	}
	info := caps.ContextMenuInfo{Window: v.w.Ref(), View: v.ref, Kinds: []string{"page"}, Inspectable: a.opts.Debug}
	// Suggestions come from WebKit's own spelling items.
	n := objc.Send[int](menu, sel("numberOfItems"))
	for i := 0; i < n; i++ {
		it := menu.Send(sel("itemAtIndex:"), i)
		switch goString(it.Send(sel("identifier"))) {
		case idSpellingGuess:
			info.Suggestions = append(info.Suggestions, goString(it.Send(sel("title"))))
		case idLearnSpelling, idNoGuesses:
			info.Kinds = append(info.Kinds, "editable")
		}
	}
	menu.Send(sel("removeAllItems"))
	st.suppressed++

	loc := objc.Send[nsPoint](event, sel("locationInWindow"))
	local := objc.Send[nsPoint](web, sel("convertPoint:fromView:"), loc, objc.ID(0))
	if !objc.Send[bool](web, sel("isFlipped")) {
		h := objc.Send[nsRect](web, sel("bounds")).Size.Height
		local.Y = h - local.Y
	}
	info.X, info.Y = int(local.X), int(local.Y)
	info.WindowX, info.WindowY = info.X, info.Y
	a.evalView(v, caps.ContextHitScript(info.X, info.Y), func(raw string) {
		caps.ParseContextHit(raw, &info)
		if len(info.Suggestions) > 0 {
			info.Misspelled = info.SelectionText
		}
		if fn := a.ctxSt().handler; fn != nil {
			fn(info)
		}
	})
}

// evalView evaluates js in the view and calls fn with the string result
// ("" on failure) on the main thread.
func (a *App) evalView(v *view, js string, fn func(string)) {
	blk := objc.NewBlock(func(_ objc.Block, result, err objc.ID) {
		raw := ""
		if err == 0 && result != 0 {
			raw = goString(result)
		}
		fn(raw)
	})
	v.web.Send(sel("evaluateJavaScript:completionHandler:"), nsString(js), objc.ID(blk))
	blk.Release() // WebKit holds its own copy
}

// ContextCommand runs a built-in command on the view that last showed a
// context menu in the window.
func (a *App) ContextCommand(window caps.WindowRef, cmd, arg string) error {
	return a.mainCall(func() error {
		var w *Window
		for _, cand := range a.windows {
			if cand.Ref() == window {
				w = cand
			}
		}
		if w == nil {
			return errors.New("darwin: no such window")
		}
		web := w.web
		if ref, ok := a.ctxSt().last[window]; ok {
			for _, v := range w.allViews() {
				if v.ref == ref && v.web != 0 {
					web = v.web
				}
			}
		}
		if web == 0 {
			return errors.New("darwin: the window has no webview")
		}
		// The responder-chain selectors WKWebView implements.
		actions := map[string]string{
			caps.CommandUndo: "undo:", caps.CommandRedo: "redo:", caps.CommandCut: "cut:", caps.CommandCopy: "copy:",
			caps.CommandPaste: "paste:", caps.CommandSelectAll: "selectAll:", caps.CommandDelete: "delete:",
		}
		if s, ok := actions[cmd]; ok {
			if !respondsTo(web, sel(s)) {
				return krate.ErrUnsupported
			}
			web.Send(sel(s), objc.ID(0))
			return nil
		}
		switch cmd {
		case caps.CommandReplace:
			// WebKit's editing command; the misspelled word is selected.
			a.evalView(&view{web: web}, "document.execCommand('insertText',false,"+jsonString(arg)+");0", func(string) {})
			return nil
		case caps.CommandInspect:
			if !a.opts.Debug {
				return krate.ErrUnsupported
			}
			if s := sel("_inspector"); respondsTo(web, s) {
				if insp := web.Send(s); insp != 0 && respondsTo(insp, sel("show")) {
					insp.Send(sel("show"))
					return nil
				}
			}
			return krate.ErrUnsupported
		case caps.CommandAddToDict:
			if respondsTo(cls("NSSpellChecker").Send(sel("sharedSpellChecker")), sel("learnWord:")) {
				cls("NSSpellChecker").Send(sel("sharedSpellChecker")).Send(sel("learnWord:"), nsString(arg))
				return nil
			}
			return krate.ErrUnsupported
		}
		return errors.New("darwin: unknown context menu command " + cmd)
	})
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// buildNSMenuItems builds the items of a menu for adding to an existing one.
func buildNSMenuItems(items []caps.MenuItem) []objc.ID {
	m := buildNSMenu("", items)
	n := objc.Send[int](m, sel("numberOfItems"))
	out := make([]objc.ID, 0, n)
	for i := 0; i < n; i++ {
		it := m.Send(sel("itemAtIndex:"), i)
		retain(it)
		out = append(out, it)
	}
	m.Send(sel("removeAllItems"))
	for _, it := range out {
		it.Send(sel("autorelease"))
	}
	return out
}
