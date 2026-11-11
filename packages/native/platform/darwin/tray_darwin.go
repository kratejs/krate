//go:build darwin

package darwin

import (
	"errors"

	"github.com/ebitengine/purego/objc"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// System tray: one NSStatusItem with an image, a title, a tooltip and an
// NSMenu built from the same MenuItem model the Linux tray uses. All AppKit
// calls run on the main thread.
//
// Clicks: without a menu the button action reports "click" (left mouse up),
// "double-click" (left, click count 2) and "right-click". With a menu AppKit
// opens the menu on any click and never calls the button action, so opening
// the menu is reported as "click" (menuWillOpen:). Menu items report "menu"
// with their id.

var _ caps.Tray = (*App)(nil)

const (
	nsVariableStatusItemLength      = -1.0
	nsEventMaskLeftMouseUp     uint = 1 << 2
	nsEventMaskRightMouseUp    uint = 1 << 4
	nsEventTypeLeftMouseUp          = 2
	nsEventTypeRightMouseUp         = 4
	nsCellImagePosNone              = 0
	nsCellImagePosOnly              = 1
	nsCellImagePosLeft              = 2
)

type trayEntry struct {
	id   string
	kind caps.MenuKind
}

// trayState is touched only on the main thread.
var trayState struct {
	item   objc.ID // NSStatusItem, retained
	menu   objc.ID // the NSMenu set on the item, or 0
	target objc.ID // KrateTrayTarget, retained
	tags   map[int]trayEntry
	tag    int
}

var trayFn func(caps.TrayEvent) // guarded by extMu

// OnTray sets the handler for tray events.
func (a *App) OnTray(fn func(caps.TrayEvent)) {
	extMu.Lock()
	trayFn = fn
	extMu.Unlock()
}

func emitTray(ev caps.TrayEvent) {
	extMu.Lock()
	fn := trayFn
	extMu.Unlock()
	if fn != nil {
		fn(ev)
	}
}

// registerTrayClass registers the action target class.
func registerTrayClass(a *App) error {
	nsObject := objc.GetClass("NSObject")
	_, err := objc.RegisterClass("KrateTrayTarget", nsObject, nil, nil, []objc.MethodDef{
		{Cmd: sel("krateTrayClick:"), Fn: func(self objc.ID, _ objc.SEL, sender objc.ID) {
			a.trayClicked()
		}},
		{Cmd: sel("krateTrayMenu:"), Fn: func(self objc.ID, _ objc.SEL, item objc.ID) {
			a.trayMenuClicked(item)
		}},
		{Cmd: sel("menuWillOpen:"), Fn: func(self objc.ID, _ objc.SEL, menu objc.ID) {
			emitTray(caps.TrayEvent{Kind: "click"})
		}},
	})
	return err
}

// trayClicked is the button action (only called while the item has no menu).
func (a *App) trayClicked() {
	kind := "click"
	if ev := a.nsApp.Send(sel("currentEvent")); ev != 0 {
		switch objc.Send[uint](ev, sel("type")) {
		case nsEventTypeRightMouseUp:
			kind = "right-click"
		case nsEventTypeLeftMouseUp:
			if objc.Send[int](ev, sel("clickCount")) >= 2 {
				kind = "double-click"
			}
		}
	}
	emitTray(caps.TrayEvent{Kind: kind})
}

// trayMenuClicked is the action of every tray menu item. Check items toggle
// themselves; a radio item turns its radio siblings off.
func (a *App) trayMenuClicked(item objc.ID) {
	e, ok := trayState.tags[objc.Send[int](item, sel("tag"))]
	if !ok {
		return
	}
	switch e.kind {
	case caps.MenuItemCheck:
		st := nsControlStateOn
		if objc.Send[int](item, sel("state")) == nsControlStateOn {
			st = nsControlStateOff
		}
		item.Send(sel("setState:"), st)
	case caps.MenuItemRadio:
		if m := item.Send(sel("menu")); m != 0 {
			n := objc.Send[int](m, sel("numberOfItems"))
			for i := 0; i < n; i++ {
				sib := m.Send(sel("itemAtIndex:"), i)
				if r, ok := trayState.tags[objc.Send[int](sib, sel("tag"))]; ok && r.kind == caps.MenuItemRadio {
					sib.Send(sel("setState:"), nsControlStateOff)
				}
			}
		}
		item.Send(sel("setState:"), nsControlStateOn)
	}
	emitTray(caps.TrayEvent{Kind: "menu", ID: e.id})
}

// SetTray shows, updates or (nil) removes the status item.
func (a *App) SetTray(icon *caps.TrayIcon) error {
	return a.mainCall(func() error { return a.setTray(icon) })
}

func (a *App) setTray(icon *caps.TrayIcon) error {
	if icon == nil {
		removeTray()
		return nil
	}
	hasIcon := len(icon.PNG) > 0
	if !hasIcon && icon.Title == "" {
		return errors.New("darwin: a tray icon needs a PNG icon or a title; a status item with neither has no width")
	}
	var img objc.ID
	if hasIcon {
		info, err := inspectTrayIcon(icon.PNG)
		if err != nil {
			return err
		}
		data := cls("NSData").Send(sel("dataWithBytes:length:"), &icon.PNG[0], uint(len(icon.PNG)))
		img = cls("NSImage").Send(sel("alloc")).Send(sel("initWithData:"), data)
		if img == 0 {
			return errors.New("darwin: AppKit could not decode the tray icon")
		}
		defer release(img)
		img.Send(sel("setSize:"), nsSize{Width: info.W, Height: info.H})
		img.Send(sel("setTemplate:"), info.Template)
	}

	if trayState.target == 0 {
		trayState.target = cls("KrateTrayTarget").Send(sel("new"))
	}
	created := false
	if trayState.item == 0 {
		bar := cls("NSStatusBar").Send(sel("systemStatusBar"))
		item := bar.Send(sel("statusItemWithLength:"), float64(nsVariableStatusItemLength))
		if item == 0 {
			return errors.New("darwin: NSStatusBar did not create a status item")
		}
		retain(item)
		trayState.item = item
		created = true
	}
	item := trayState.item
	button := item.Send(sel("button"))
	if button == 0 {
		if created {
			removeTray()
		}
		return errors.New("darwin: the status item has no button (macOS 10.10 or newer is required)")
	}
	if created {
		button.Send(sel("setTarget:"), trayState.target)
		button.Send(sel("setAction:"), sel("krateTrayClick:"))
		button.Send(sel("sendActionOn:"), nsEventMaskLeftMouseUp|nsEventMaskRightMouseUp)
	}
	button.Send(sel("setImage:"), img)
	button.Send(sel("setTitle:"), nsString(icon.Title))
	pos := nsCellImagePosNone
	switch {
	case hasIcon && icon.Title != "":
		pos = nsCellImagePosLeft
	case hasIcon:
		pos = nsCellImagePosOnly
	}
	button.Send(sel("setImagePosition:"), pos)
	tip := objc.ID(0)
	if icon.Tooltip != "" {
		tip = nsString(icon.Tooltip)
	}
	button.Send(sel("setToolTip:"), tip)

	// Rebuild the menu. The old tags go with the old menu.
	trayState.tags = map[int]trayEntry{}
	trayState.tag = 0
	trayState.menu = 0
	if len(icon.Menu) > 0 {
		m := buildTrayMenu(icon.Menu)
		m.Send(sel("setDelegate:"), trayState.target)
		trayState.menu = m
	}
	item.Send(sel("setMenu:"), trayState.menu)
	return nil
}

// removeTray takes the status item out of the menu bar.
func removeTray() {
	if trayState.item == 0 {
		return
	}
	bar := cls("NSStatusBar").Send(sel("systemStatusBar"))
	bar.Send(sel("removeStatusItem:"), trayState.item)
	release(trayState.item)
	trayState.item, trayState.menu = 0, 0
	trayState.tags = nil
}

// buildTrayMenu builds the NSMenu of a tray from the portable model.
func buildTrayMenu(items []caps.MenuItem) objc.ID {
	m := newMenu("")
	m.Send(sel("setAutoenablesItems:"), false)
	for _, it := range items {
		if it.Kind == caps.MenuItemSeparator {
			m.Send(sel("addItem:"), cls("NSMenuItem").Send(sel("separatorItem")))
			continue
		}
		label := menuLabel(it.Label)
		key, mask := accelKeyEquivalent(it.Accelerator)
		item := menuItem(label, sel("krateTrayMenu:"), key, mask)
		if len(it.Submenu) > 0 || it.Kind == caps.MenuItemSubmenu {
			sub := buildTrayMenu(it.Submenu)
			sub.Send(sel("setTitle:"), nsString(label))
			item.Send(sel("setSubmenu:"), sub)
			item.Send(sel("setAction:"), objc.SEL(0))
		} else {
			trayState.tag++
			trayState.tags[trayState.tag] = trayEntry{id: it.ID, kind: it.Kind}
			item.Send(sel("setTag:"), trayState.tag)
			item.Send(sel("setTarget:"), trayState.target)
			if it.Checked && (it.Kind == caps.MenuItemCheck || it.Kind == caps.MenuItemRadio) {
				item.Send(sel("setState:"), nsControlStateOn)
			}
		}
		if it.Disabled {
			item.Send(sel("setEnabled:"), false)
		}
		m.Send(sel("addItem:"), item)
	}
	return m
}
