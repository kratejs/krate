//go:build windows

package windows

// System tray icon through Shell_NotifyIconW with NOTIFYICON_VERSION_4. The
// icon is owned by the app's hidden utility window (see utilWnd) and all work
// happens on the main thread. UNTESTED beyond compilation and the pure logic
// in tray.go; see MANUAL_TEST.md.

import (
	"errors"
	"fmt"
	"sync/atomic"
	"unsafe"

	xw "golang.org/x/sys/windows"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

var (
	pShellNotifyIconW                      = shell32.NewProc("Shell_NotifyIconW")
	pCreateIconFromResourceEx              = user32.NewProc("CreateIconFromResourceEx")
	pCreateIconIndirect                    = user32.NewProc("CreateIconIndirect")
	pDestroyIcon                           = user32.NewProc("DestroyIcon")
	pLoadIconW                             = user32.NewProc("LoadIconW")
	pGetSystemMetrics                      = user32.NewProc("GetSystemMetrics")
	pGetSystemMetricsForDpi                = user32.NewProc("GetSystemMetricsForDpi")
	pRegisterWindowMessageW                = user32.NewProc("RegisterWindowMessageW")
	pChangeWindowMessageFilterEx           = user32.NewProc("ChangeWindowMessageFilterEx")
	gdi32                                  = xw.NewLazySystemDLL("gdi32.dll")
	pCreateBitmap                          = gdi32.NewProc("CreateBitmap")
	pDeleteObject                          = gdi32.NewProc("DeleteObject")
	_                            caps.Tray = (*App)(nil)
)

const (
	smCxSmIcon     = 49
	idiApplication = 32512
	tpmRightButton = 0x0002
	tpmNoNotify    = 0x0080
	wmNull         = 0x0000
)

// iconInfo is ICONINFO.
type iconInfo struct {
	icon     uint32
	xHotspot uint32
	yHotspot uint32
	mask     uintptr
	color    uintptr
}

type trayState struct {
	nid   notifyIconData
	hicon uintptr
	owned bool // hicon must be destroyed (not the stock icon)
	added bool
	menu  []caps.MenuItem
}

var trayHandler func(caps.TrayEvent)

// OnTray sets the handler for tray events.
func (a *App) OnTray(fn func(caps.TrayEvent)) {
	extMu.Lock()
	trayHandler = fn
	extMu.Unlock()
}

func emitTray(ev caps.TrayEvent) {
	extMu.Lock()
	fn := trayHandler
	extMu.Unlock()
	if fn != nil {
		fn(ev)
	}
}

// SetTray shows, updates or (with nil) removes the tray icon.
func (a *App) SetTray(icon *caps.TrayIcon) (err error) {
	a.callMain(func() { err = a.setTray(icon) })
	return err
}

// trayIconSize is the small-icon size of the primary monitor.
func trayIconSize() int {
	var r uintptr
	if pGetSystemMetricsForDpi.Find() == nil {
		r, _, _ = pGetSystemMetricsForDpi.Call(smCxSmIcon, uintptr(systemDPI()))
	}
	if r == 0 {
		r, _, _ = pGetSystemMetrics.Call(smCxSmIcon)
	}
	if r == 0 {
		return 16
	}
	return int(r)
}

// createHIcon makes an HICON from PNG bytes. It first lets Windows decode the
// PNG (CreateIconFromResourceEx accepts PNG data since Windows Vista); when
// that fails it decodes and scales in Go and builds the icon from a 32-bit
// bitmap. With no image the stock application icon is returned (not owned).
func createHIcon(png []byte) (h uintptr, owned bool, err error) {
	if len(png) == 0 {
		h, _, _ = pLoadIconW.Call(0, idiApplication)
		return h, false, nil
	}
	size := uintptr(trayIconSize())
	h, _, _ = pCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&png[0])), uintptr(len(png)),
		1, 0x00030000, size, size, 0)
	if h != 0 {
		return h, true, nil
	}
	bits, derr := pngToIconBits(png, int(size))
	if derr != nil {
		return 0, false, derr
	}
	stride := ((bits.size + 15) / 16) * 2
	maskBits := make([]byte, stride*bits.size)
	color, _, _ := pCreateBitmap.Call(uintptr(bits.size), uintptr(bits.size), 1, 32, uintptr(unsafe.Pointer(&bits.bgra[0])))
	mask, _, _ := pCreateBitmap.Call(uintptr(bits.size), uintptr(bits.size), 1, 1, uintptr(unsafe.Pointer(&maskBits[0])))
	if color == 0 || mask == 0 {
		for _, o := range []uintptr{color, mask} {
			if o != 0 {
				_, _, _ = pDeleteObject.Call(o)
			}
		}
		return 0, false, errors.New("windows: creating the tray icon bitmap failed")
	}
	ii := iconInfo{icon: 1, mask: mask, color: color}
	h, _, _ = pCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	_, _, _ = pDeleteObject.Call(color)
	_, _, _ = pDeleteObject.Call(mask)
	if h == 0 {
		return 0, false, errors.New("windows: CreateIconIndirect failed")
	}
	return h, true, nil
}

func (a *App) setTray(icon *caps.TrayIcon) error {
	t := a.tray
	if icon == nil {
		a.removeTray()
		return nil
	}
	h, owned, err := createHIcon(icon.PNG)
	if err != nil {
		return err
	}
	nid := notifyIconData{
		hwnd:            a.utilWnd,
		id:              trayIconID,
		flags:           nifMessage | nifIcon | nifTip | nifShowTip,
		callbackMessage: wmTrayCallback,
		icon:            h,
		version:         notifyIconVersion4,
	}
	nid.size = uint32(unsafe.Sizeof(nid))
	copy(nid.tip[:], tipUnits(trayTooltip(icon.Tooltip, icon.Title), trayTipUnits))

	msg := uintptr(nimModify)
	if !t.added {
		msg = nimAdd
	}
	if r, _, _ := pShellNotifyIconW.Call(msg, uintptr(unsafe.Pointer(&nid))); r == 0 {
		if owned {
			_, _, _ = pDestroyIcon.Call(h)
		}
		return errors.New("windows: Shell_NotifyIcon failed (is the taskbar running?)")
	}
	if !t.added {
		_, _, _ = pShellNotifyIconW.Call(nimSetVersion, uintptr(unsafe.Pointer(&nid)))
		t.added = true
	}
	old := t.hicon
	oldOwned := t.owned
	t.nid, t.hicon, t.owned = nid, h, owned
	if old != 0 && oldOwned {
		_, _, _ = pDestroyIcon.Call(old)
	}
	t.menu = append([]caps.MenuItem(nil), icon.Menu...)
	return nil
}

func (a *App) removeTray() {
	t := a.tray
	if t.added {
		_, _, _ = pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&t.nid)))
		t.added = false
	}
	if t.hicon != 0 && t.owned {
		_, _, _ = pDestroyIcon.Call(t.hicon)
	}
	t.hicon, t.owned = 0, false
	t.menu = nil
}

// trayReadd puts the icon back after the taskbar was recreated (explorer.exe
// restarted): the shell forgot every icon and broadcasts "TaskbarCreated".
func (a *App) trayReadd() {
	t := a.tray
	if !t.added {
		return
	}
	if r, _, _ := pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&t.nid))); r == 0 {
		logf("could not re-add the tray icon after the taskbar restarted")
		return
	}
	_, _, _ = pShellNotifyIconW.Call(nimSetVersion, uintptr(unsafe.Pointer(&t.nid)))
	trayReadds.Add(1)
}

// trayReadds counts the successful re-adds after a TaskbarCreated broadcast
// (the GUI test waits on it).
var trayReadds atomic.Int32

// allowTaskbarCreated lets the utility window receive TaskbarCreated when the
// process runs elevated: explorer.exe is not, and User Interface Privilege
// Isolation drops its broadcast otherwise.
func allowTaskbarCreated(hwnd uintptr, msg uint32) {
	if pChangeWindowMessageFilterEx.Find() != nil {
		return
	}
	const msgfltAllow = 1
	_, _, _ = pChangeWindowMessageFilterEx.Call(hwnd, uintptr(msg), msgfltAllow, 0)
}

// trayMessage handles the callback message of the tray icon.
func (a *App) trayMessage(wParam, lParam uintptr) {
	ev, id, x, y := decodeTrayMessage(wParam, lParam)
	if id != trayIconID {
		return
	}
	kind, ok := trayEventKind(ev)
	if !ok {
		return
	}
	emitTray(caps.TrayEvent{Kind: kind})
	if kind == "right-click" && len(a.tray.menu) > 0 {
		a.trayPopup(x, y)
	}
}

// trayPopup shows the tray menu at (x, y). The window must be the foreground
// window while the menu is up or the menu does not close when the user clicks
// elsewhere; the WM_NULL afterwards is the documented companion fix.
func (a *App) trayPopup(x, y int32) {
	if x == 0 && y == 0 {
		var pt point
		_, _, _ = pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
		x, y = pt.x, pt.y
	}
	cmds := map[uint16]menuCmd{}
	var next uint16 = 100
	hm := buildHMenu(true, a.tray.menu, cmds, &next)
	if hm == 0 {
		return
	}
	defer func() { _, _, _ = pDestroyMenu.Call(hm) }()
	_, _, _ = pSetForegroundWindow.Call(a.utilWnd)
	cmd, _, _ := pTrackPopupMenuEx.Call(hm, tpmReturnCmd|tpmNoNotify|tpmRightButton, uintptr(int64(x)), uintptr(int64(y)), a.utilWnd, 0)
	_, _, _ = pPostMessageW.Call(a.utilWnd, wmNull, 0, 0)
	if mc, ok := cmds[uint16(cmd)]; ok && cmd != 0 {
		emitTray(caps.TrayEvent{Kind: "menu", ID: mc.id})
	}
}

func (t *trayState) String() string { return fmt.Sprintf("tray(added=%v)", t.added) }
