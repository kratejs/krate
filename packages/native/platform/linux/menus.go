//go:build linux

package linux

import (
	"errors"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var _ caps.Menus = (*App)(nil)

var cbMenuActivate uintptr

// menuTarget is what a GtkMenuItem's activate signal maps to.
type menuTarget struct {
	id     string
	win    caps.WindowRef
	item   uintptr
	isCtrl bool // check or radio item
}

var (
	menuMu      sync.Mutex
	menuTargets = map[uintptr]*menuTarget{}
	menuNext    uintptr
	menuFn      func(caps.MenuEvent)
	appItems    []caps.MenuItem
)

func menuActivateCallback(item, handle uintptr) uintptr {
	menuMu.Lock()
	t := menuTargets[handle]
	fn := menuFn
	menuMu.Unlock()
	if t == nil || fn == nil {
		return 0
	}
	ev := caps.MenuEvent{ID: t.id, Window: t.win}
	if t.isCtrl {
		ev.Checked = theApp.x.gtkCheckGetActive(item) != 0
	}
	fn(ev)
	return 0
}

// OnMenu sets the handler for menu clicks.
func (a *App) OnMenu(fn func(caps.MenuEvent)) {
	menuMu.Lock()
	menuFn = fn
	menuMu.Unlock()
}

// mnemonic converts "&File" to GTK's "_File" and protects literal underscores.
func mnemonic(label string) string {
	var b strings.Builder
	for i := 0; i < len(label); i++ {
		c := label[i]
		switch {
		case c == '&' && i+1 < len(label) && label[i+1] == '&':
			b.WriteByte('&')
			i++
		case c == '&':
			b.WriteByte('_')
		case c == '_':
			b.WriteString("__")
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// gtkAccelerator converts "CmdOrCtrl+Shift+S" to "<Control><Shift>s".
func gtkAccelerator(acc string) string {
	if acc == "" {
		return ""
	}
	parts := strings.Split(acc, "+")
	var out strings.Builder
	for i, p := range parts {
		if i == len(parts)-1 {
			out.WriteString(gtkKeyName(p))
			break
		}
		switch strings.ToLower(p) {
		case "cmdorctrl", "ctrl", "control", "cmd", "command":
			out.WriteString("<Control>")
		case "shift":
			out.WriteString("<Shift>")
		case "alt", "option":
			out.WriteString("<Alt>")
		case "super", "meta", "win":
			out.WriteString("<Super>")
		}
	}
	return out.String()
}

func gtkKeyName(k string) string {
	switch strings.ToLower(k) {
	case "plus":
		return "plus"
	case "space":
		return "space"
	case "esc", "escape":
		return "Escape"
	case "enter", "return":
		return "Return"
	case "tab":
		return "Tab"
	case "backspace":
		return "BackSpace"
	case "delete":
		return "Delete"
	case "up":
		return "Up"
	case "down":
		return "Down"
	case "left":
		return "Left"
	case "right":
		return "Right"
	}
	if len(k) == 1 {
		return strings.ToLower(k)
	}
	return k
}

// buildMenu builds a GtkMenuShell child list for items under shell.
func (a *App) buildMenu(shell uintptr, items []caps.MenuItem, win caps.WindowRef, accel uintptr, handles *[]uintptr) {
	x := a.x
	var radioGroup uintptr
	for _, it := range items {
		var w uintptr
		isCtrl := false
		switch it.Kind {
		case caps.MenuItemSeparator:
			w = x.gtkSeparatorItem()
			radioGroup = 0
		case caps.MenuItemCheck:
			w = x.gtkCheckMenuItem(mnemonic(it.Label))
			x.gtkCheckSetActive(w, b2i(it.Checked))
			isCtrl = true
			radioGroup = 0
		case caps.MenuItemRadio:
			w = x.gtkRadioMenuItem(radioGroup, mnemonic(it.Label))
			radioGroup = x.gtkRadioGetGroup(w)
			x.gtkCheckSetActive(w, b2i(it.Checked))
			isCtrl = true
		default:
			w = x.gtkMenuItemMnemonic(mnemonic(it.Label))
			radioGroup = 0
		}
		if w == 0 {
			continue
		}
		if it.Kind != caps.MenuItemSeparator {
			if len(it.Submenu) > 0 || it.Kind == caps.MenuItemSubmenu {
				sub := x.gtkMenuNew()
				a.buildMenu(sub, it.Submenu, win, accel, handles)
				x.gtkMenuItemSubmenu(w, sub)
			} else {
				menuMu.Lock()
				menuNext++
				h := menuNext
				menuTargets[h] = &menuTarget{id: it.ID, win: win, item: w, isCtrl: isCtrl}
				menuMu.Unlock()
				*handles = append(*handles, h)
				a.api.gSignalConnectData(w, "activate", cbMenuActivate, h, 0, 0)
			}
			x.gtkWidgetSensitive(w, b2i(!it.Disabled))
			if acc := gtkAccelerator(it.Accelerator); acc != "" && accel != 0 && x.gtkAccelParse != nil {
				var key, mods uint32
				x.gtkAccelParse(acc, &key, &mods)
				if key != 0 {
					x.gtkWidgetAddAccel(w, "activate", accel, key, mods, 1)
				}
			}
		}
		x.gtkMenuShellAppend(shell, w)
	}
}

func (a *App) dropHandles(hs []uintptr) {
	menuMu.Lock()
	for _, h := range hs {
		delete(menuTargets, h)
	}
	menuMu.Unlock()
}

type barState struct{ handles []uintptr }

var bars = map[uintptr]*barState{} // window id -> handles (main thread only)

func (a *App) installBar(w *Window, items []caps.MenuItem) {
	x := a.x
	if st := bars[w.id]; st != nil {
		a.dropHandles(st.handles)
		delete(bars, w.id)
	}
	if w.menubar != 0 {
		x.gtkContainerRemove(w.box, w.menubar)
		w.menubar = 0
	}
	if w.accel != 0 {
		x.gtkWindowRemoveAccel(w.win, w.accel)
		a.api.gObjectUnref(w.accel)
		w.accel = 0
	}
	if len(items) == 0 {
		return
	}
	w.accel = x.gtkAccelGroupNew()
	x.gtkWindowAddAccel(w.win, w.accel)
	bar := x.gtkMenuBarNew()
	st := &barState{}
	a.buildMenu(bar, items, caps.WindowRef(w.id), w.accel, &st.handles)
	bars[w.id] = st
	x.gtkBoxPackStart(w.box, bar, 0, 0, 0)
	x.gtkBoxReorderChild(w.box, bar, 0)
	a.api.gtkWidgetShowAll(bar)
	w.menubar = bar
}

// SetAppMenu installs the menu bar on every window, now and for windows
// opened later.
func (a *App) SetAppMenu(items []caps.MenuItem) error {
	x := a.x
	if x.gtkMenuBarNew == nil || x.gtkBoxNew == nil || x.gtkMenuItemMnemonic == nil {
		return krate.ErrUnsupported
	}
	return a.onMainErr(func() error {
		appItems = append([]caps.MenuItem(nil), items...)
		a.mu.Lock()
		wins := make([]*Window, 0, len(a.windows))
		for _, w := range a.windows {
			wins = append(wins, w)
		}
		a.mu.Unlock()
		for _, w := range wins {
			if w.alive && w.box != 0 {
				a.installBar(w, items)
			}
		}
		return nil
	})
}

// applyAppMenu gives a new window the current app menu (main thread).
func (a *App) applyAppMenu(w *Window) {
	if len(appItems) > 0 && w.box != 0 && !w.offscreen && a.x.gtkMenuBarNew != nil {
		a.installBar(w, appItems)
	}
}

// PopupMenu shows a context menu. Negative x or y means the pointer.
func (a *App) PopupMenu(parent caps.WindowRef, items []caps.MenuItem, px, py int) error {
	x := a.x
	if x.gtkMenuNew == nil || x.gtkMenuPopupPointer == nil {
		return krate.ErrUnsupported
	}
	if len(items) == 0 {
		return errors.New("krate: empty menu")
	}
	return a.onMainErr(func() error {
		menu := x.gtkMenuNew()
		var hs []uintptr
		a.buildMenu(menu, items, parent, 0, &hs)
		a.api.gtkWidgetShowAll(menu)
		if w := a.windowByID(uintptr(parent)); w != nil && w.alive && w.view != 0 && x.gtkMenuAttachWidget != nil {
			x.gtkMenuAttachWidget(menu, w.view)
		}
		gw := uintptr(0)
		if w := a.windowByID(uintptr(parent)); w != nil && w.alive {
			gw = x.gtkWidgetGetWindow(w.win)
		}
		if gw == 0 {
			return errors.New("krate: the window for the context menu is not mapped")
		}
		if px < 0 || py < 0 {
			px, py = 0, 0
			if x.gdkDevicePosition != nil {
				var ox, oy, rx, ry int32
				x.gdkWindowGetOrigin(gw, &ox, &oy)
				seat := x.gdkDisplaySeat(x.gdkDisplayDefault())
				x.gdkDevicePosition(x.gdkSeatPointer(seat), nil, &rx, &ry)
				px, py = int(rx-ox), int(ry-oy)
			}
		}
		// GdkRectangle { x, y, width, height }; anchors are GDK_GRAVITY_NORTH_WEST.
		rect := [4]int32{int32(px), int32(py), 1, 1}
		x.gtkMenuPopupRect(menu, gw, unsafe.Pointer(&rect[0]), 1, 1, 0)
		a.popup = menu
		// Handles stay registered until the process ends; context menus are
		// short-lived and small.
		return nil
	})
}

// ActivateMenuItem activates the first menu bar item with this id on window
// ref (0 means any), as a click would. For tests.
func (a *App) ActivateMenuItem(ref caps.WindowRef, id string) bool {
	var item uintptr
	menuMu.Lock()
	for _, t := range menuTargets {
		if t.id == id && (ref == 0 || t.win == ref) {
			item = t.item
			break
		}
	}
	menuMu.Unlock()
	if item == 0 {
		return false
	}
	a.callMain(func() {
		a.x.gtkMenuItemActivate(item)
		// A click closes the menu it hit; activating the item does not, and
		// the open menu would keep its keyboard grab.
		if a.popup != 0 && a.x.gtkMenuPopdown != nil {
			a.x.gtkMenuPopdown(a.popup)
			a.popup = 0
		}
	})
	return true
}

func init() {
	cbMenuActivate = purego.NewCallback(menuActivateCallback)
}

// ActivateMenuItem activates the first menu item with this id on any window
// of a platform.App made by this package (as a click would). For tests; it
// reports false for other Apps or an unknown id.
func ActivateMenuItem(app any, id string) bool {
	a := appOf(app)
	if a == nil {
		return false
	}
	return a.ActivateMenuItem(0, id)
}
