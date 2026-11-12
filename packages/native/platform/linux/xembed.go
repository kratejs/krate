//go:build linux

package linux

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var _ caps.XEmbedTray = (*App)(nil)

// statusIconAPI holds the GtkStatusIcon symbols. GtkStatusIcon is deprecated
// but still part of GTK 3; it is the only way to get an XEmbed tray icon
// without writing the XEmbed protocol by hand.
type statusIconAPI struct {
	newFromPixbuf   func(pb uintptr) uintptr
	newFromName     func(name string) uintptr
	setFromPixbuf   func(si, pb uintptr)
	setTooltip      func(si uintptr, text string)
	setTitle        func(si uintptr, title string)
	setVisible      func(si uintptr, on int32)
	isEmbedded      func(si uintptr) int32
	menuPopup       func(menu, parentShell, parentItem, posFn, data uintptr, button, time uint32)
	positionMenuFn  uintptr
	xOpenDisplay    func(name uintptr) uintptr
	xCloseDisplay   func(d uintptr) int32
	xDefaultScreen  func(d uintptr) int32
	xInternAtom     func(d uintptr, name string, onlyIfExists int32) uintptr
	xSelectionOwner func(d, atom uintptr) uintptr
}

var (
	siOnce sync.Once
	siAPI  *statusIconAPI

	cbSIActivate, cbSIPopup, cbSIButton, cbSIMenu uintptr
)

func loadStatusIcon() *statusIconAPI {
	siOnce.Do(func() {
		gtk, err := purego.Dlopen("libgtk-3.so.0", rtldNow|rtldGlobal)
		if err != nil {
			return
		}
		x11, err := purego.Dlopen("libX11.so.6", rtldNow|rtldGlobal)
		if err != nil {
			return
		}
		s := &statusIconAPI{}
		b := &binder{}
		b.bind(&s.newFromPixbuf, gtk, "gtk_status_icon_new_from_pixbuf")
		b.bind(&s.newFromName, gtk, "gtk_status_icon_new_from_icon_name")
		b.bind(&s.setFromPixbuf, gtk, "gtk_status_icon_set_from_pixbuf")
		b.bind(&s.setTooltip, gtk, "gtk_status_icon_set_tooltip_text")
		b.bind(&s.setTitle, gtk, "gtk_status_icon_set_title")
		b.bind(&s.setVisible, gtk, "gtk_status_icon_set_visible")
		b.bind(&s.isEmbedded, gtk, "gtk_status_icon_is_embedded")
		b.bind(&s.menuPopup, gtk, "gtk_menu_popup")
		b.bind(&s.xOpenDisplay, x11, "XOpenDisplay")
		b.bind(&s.xCloseDisplay, x11, "XCloseDisplay")
		b.bind(&s.xDefaultScreen, x11, "XDefaultScreen")
		b.bind(&s.xInternAtom, x11, "XInternAtom")
		b.bind(&s.xSelectionOwner, x11, "XGetSelectionOwner")
		if len(b.missing) > 0 {
			return
		}
		if fn, err := purego.Dlsym(gtk, "gtk_status_icon_position_menu"); err == nil {
			s.positionMenuFn = fn
		}
		siAPI = s
	})
	return siAPI
}

// trayState is the one tray icon of the app (main thread, except fn).
var trayState struct {
	sync.Mutex
	icon    uintptr
	menu    uintptr
	handles []uintptr
	fn      func(caps.TrayEvent)
	targets map[uintptr]string
	next    uintptr
}

func trayEmit(ev caps.TrayEvent) {
	trayState.Lock()
	fn := trayState.fn
	trayState.Unlock()
	if fn != nil {
		fn(ev)
	}
}

func siActivateCallback(icon, data uintptr) uintptr {
	trayEmit(caps.TrayEvent{Kind: "click"})
	return 0
}

func siPopupCallback(icon, button, activateTime, data uintptr) uintptr {
	trayEmit(caps.TrayEvent{Kind: "right-click"})
	s := siAPI
	trayState.Lock()
	menu := trayState.menu
	trayState.Unlock()
	if s != nil && menu != 0 {
		s.menuPopup(menu, 0, 0, s.positionMenuFn, icon, uint32(button), uint32(activateTime))
	}
	return 0
}

// siButtonCallback reports middle clicks; GdkEventButton.button is at offset 52.
func siButtonCallback(icon, ev, data uintptr) uintptr {
	if ev != 0 {
		if *(*uint32)(unsafe.Add(*(*unsafe.Pointer)(unsafe.Pointer(&ev)), 52)) == 2 {
			trayEmit(caps.TrayEvent{Kind: "middle-click"})
		}
	}
	return 0
}

func siMenuCallback(item, handle uintptr) uintptr {
	trayState.Lock()
	id := trayState.targets[handle]
	trayState.Unlock()
	if id != "" {
		trayEmit(caps.TrayEvent{Kind: "menu", ID: id})
	}
	return 0
}

func init() {
	cbSIActivate = purego.NewCallback(siActivateCallback)
	cbSIPopup = purego.NewCallback(siPopupCallback)
	cbSIButton = purego.NewCallback(siButtonCallback)
	cbSIMenu = purego.NewCallback(siMenuCallback)
}

// gdkIsX11 is a cheap check that GTK will use the X11 backend: GtkStatusIcon
// is never embedded on Wayland.
func gdkIsX11() bool {
	switch b := os.Getenv("GDK_BACKEND"); {
	case strings.HasPrefix(b, "wayland"):
		return false
	case strings.HasPrefix(b, "x11"):
		return os.Getenv("DISPLAY") != ""
	}
	return os.Getenv("WAYLAND_DISPLAY") == "" && os.Getenv("DISPLAY") != ""
}

// XEmbedTrayAvailable reports whether a tray manager owns
// _NET_SYSTEM_TRAY_S<screen> on the default X screen.
func (a *App) XEmbedTrayAvailable() bool {
	s := loadStatusIcon()
	if s == nil || !gdkIsX11() {
		return false
	}
	d := s.xOpenDisplay(0)
	if d == 0 {
		return false
	}
	defer s.xCloseDisplay(d)
	atom := s.xInternAtom(d, "_NET_SYSTEM_TRAY_S"+strconv.Itoa(int(s.xDefaultScreen(d))), 1)
	return atom != 0 && s.xSelectionOwner(d, atom) != 0
}

// OnXEmbedTray sets the handler for tray events.
func (a *App) OnXEmbedTray(fn func(caps.TrayEvent)) {
	trayState.Lock()
	trayState.fn = fn
	trayState.Unlock()
}

// XEmbedTrayEmbedded reports whether the status icon of a platform.App made
// by this package is currently docked in a tray. For tests.
func XEmbedTrayEmbedded(app any) bool {
	a := appOf(app)
	return a != nil && a.xembedTrayEmbedded()
}

func (a *App) xembedTrayEmbedded() bool {
	s := loadStatusIcon()
	if s == nil {
		return false
	}
	embedded := false
	a.callMain(func() {
		trayState.Lock()
		icon := trayState.icon
		trayState.Unlock()
		embedded = icon != 0 && s.isEmbedded(icon) != 0
	})
	return embedded
}

// SetXEmbedTray shows, updates or removes the XEmbed tray icon.
func (a *App) SetXEmbedTray(ic *caps.TrayIcon) error {
	s := loadStatusIcon()
	if s == nil {
		return krate.ErrUnsupported
	}
	return a.onMainErr(func() error {
		if ic == nil {
			a.dropStatusIcon(s)
			return nil
		}
		var pb uintptr
		if len(ic.PNG) > 0 {
			var err error
			if pb, err = a.pixbufFromPNG(ic.PNG); err != nil {
				return err
			}
			defer a.api.gObjectUnref(pb)
		}
		trayState.Lock()
		icon := trayState.icon
		trayState.Unlock()
		if icon == 0 {
			if pb != 0 {
				icon = s.newFromPixbuf(pb)
			} else {
				icon = s.newFromName("image-missing")
			}
			if icon == 0 {
				return errors.New("krate: cannot create the tray icon")
			}
			a.api.gSignalConnectData(icon, "activate", cbSIActivate, 0, 0, 0)
			a.api.gSignalConnectData(icon, "popup-menu", cbSIPopup, 0, 0, 0)
			a.api.gSignalConnectData(icon, "button-press-event", cbSIButton, 0, 0, 0)
			trayState.Lock()
			trayState.icon = icon
			trayState.Unlock()
		} else if pb != 0 {
			s.setFromPixbuf(icon, pb)
		}
		title, tip := caps.TrayLabel(ic, caps.AppDisplayName())
		s.setTooltip(icon, tip)
		s.setTitle(icon, title)
		s.setVisible(icon, 1)
		a.replaceTrayMenu(ic.Menu)
		return nil
	})
}

func (a *App) dropStatusIcon(s *statusIconAPI) {
	trayState.Lock()
	icon, menu := trayState.icon, trayState.menu
	trayState.icon, trayState.menu = 0, 0
	trayState.targets = nil
	trayState.Unlock()
	if icon != 0 {
		s.setVisible(icon, 0)
		a.api.gObjectUnref(icon)
	}
	if menu != 0 {
		a.api.gtkWidgetDestroy(menu)
	}
}

func (a *App) replaceTrayMenu(items []caps.MenuItem) {
	x := a.x
	trayState.Lock()
	old := trayState.menu
	trayState.menu = 0
	trayState.targets = map[uintptr]string{}
	trayState.Unlock()
	if old != 0 {
		a.api.gtkWidgetDestroy(old)
	}
	if len(items) == 0 || x.gtkMenuNew == nil {
		return
	}
	m := x.gtkMenuNew()
	a.buildTrayMenu(m, items)
	a.api.gtkWidgetShowAll(m)
	trayState.Lock()
	trayState.menu = m
	trayState.Unlock()
}

func (a *App) buildTrayMenu(shell uintptr, items []caps.MenuItem) {
	x := a.x
	var radioGroup uintptr
	for _, it := range items {
		var w uintptr
		switch it.Kind {
		case caps.MenuItemSeparator:
			w = x.gtkSeparatorItem()
			radioGroup = 0
		case caps.MenuItemCheck:
			w = x.gtkCheckMenuItem(mnemonic(it.Label))
			x.gtkCheckSetActive(w, b2i(it.Checked))
			radioGroup = 0
		case caps.MenuItemRadio:
			w = x.gtkRadioMenuItem(radioGroup, mnemonic(it.Label))
			radioGroup = x.gtkRadioGetGroup(w)
			x.gtkCheckSetActive(w, b2i(it.Checked))
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
				a.buildTrayMenu(sub, it.Submenu)
				x.gtkMenuItemSubmenu(w, sub)
			} else {
				trayState.Lock()
				trayState.next++
				h := trayState.next
				trayState.targets[h] = it.ID
				trayState.Unlock()
				a.api.gSignalConnectData(w, "activate", cbSIMenu, h, 0, 0)
			}
			x.gtkWidgetSensitive(w, b2i(!it.Disabled))
		}
		x.gtkMenuShellAppend(shell, w)
	}
}

// pixbufFromPNG decodes PNG bytes; the caller owns the returned reference.
func (a *App) pixbufFromPNG(png []byte) (uintptr, error) {
	x := a.x
	if x.pixbufLoaderNew == nil {
		return 0, krate.ErrUnsupported
	}
	var gerr uintptr
	l := x.pixbufLoaderNew("png", &gerr)
	if l == 0 {
		return 0, errors.New("krate: no PNG loader: " + gerrMessage(gerr))
	}
	defer a.api.gObjectUnref(l)
	if x.pixbufLoaderWrite(l, unsafe.Pointer(&png[0]), uint64(len(png)), &gerr) == 0 {
		x.pixbufLoaderClose(l, nil)
		return 0, errors.New("krate: the tray icon is not a valid PNG")
	}
	if x.pixbufLoaderClose(l, &gerr) == 0 {
		return 0, errors.New("krate: the tray icon is not a valid PNG: " + gerrMessage(gerr))
	}
	pb := x.pixbufLoaderGet(l)
	if pb == 0 {
		return 0, errors.New("krate: the tray icon is not a valid PNG")
	}
	return a.api.gObjectRef(pb), nil
}
