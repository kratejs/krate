//go:build windows

package windows

// OS integration: taskbar badge and progress, displays, power, theme and
// WebView2 process failures. UNTESTED beyond compilation and the pure logic in
// sysmap.go; the CI GUI tests in system_gui_test.go exercise it on a real
// desktop.
//
//   - Badge and progress: ITaskbarList3 (SetProgressValue, SetProgressState,
//     SetOverlayIcon with an icon drawn by caps.BadgeImage). The state is kept
//     and applied again when the taskbar button is (re)created.
//   - Displays: EnumDisplayMonitors, GetMonitorInfo, GetDpiForMonitor;
//     WM_DISPLAYCHANGE on the utility window reports changes.
//   - Power: WM_POWERBROADCAST and WM_WTSSESSION_CHANGE on the utility window,
//     GetSystemPowerStatus, GetLastInputInfo, and SetThreadExecutionState on a
//     thread of its own (the state belongs to the calling thread).
//   - Theme: the Personalize registry key and DwmSetWindowAttribute for
//     title bars; WM_SETTINGCHANGE ("ImmersiveColorSet") reports changes.
//   - Crashes: ICoreWebView2.add_ProcessFailed.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"syscall"
	"unsafe"

	xw "golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

var (
	_ caps.Badger       = (*App)(nil)
	_ caps.Attention    = (*App)(nil)
	_ caps.Displays     = (*App)(nil)
	_ caps.Power        = (*App)(nil)
	_ caps.Themer       = (*App)(nil)
	_ caps.WindowThemer = (*App)(nil)
	_ caps.ViewCrashes  = (*App)(nil)
)

var (
	dwmapi   = xw.NewLazySystemDLL("dwmapi.dll")
	wtsapi32 = xw.NewLazySystemDLL("wtsapi32.dll")

	pDwmSetWindowAttribute          = dwmapi.NewProc("DwmSetWindowAttribute")
	pFlashWindowEx                  = user32.NewProc("FlashWindowEx")
	pEnumDisplayMonitors            = user32.NewProc("EnumDisplayMonitors")
	pEnumDisplaySettingsW           = user32.NewProc("EnumDisplaySettingsW")
	pGetDpiForMonitor               = shcore.NewProc("GetDpiForMonitor")
	pGetSystemPowerStatus           = kernel32.NewProc("GetSystemPowerStatus")
	pGetTickCount                   = kernel32.NewProc("GetTickCount")
	pGetLastInputInfo               = user32.NewProc("GetLastInputInfo")
	pSetThreadExecutionState        = kernel32.NewProc("SetThreadExecutionState")
	pWTSRegisterSessionNotification = wtsapi32.NewProc("WTSRegisterSessionNotification")
	pSystemParametersInfoW          = user32.NewProc("SystemParametersInfoW")
	pCoCreateInstance               = ole32.NewProc("CoCreateInstance")

	// ITaskbarList3 vtable slots (ITaskbarList, ITaskbarList2, then its own).
	tbHrInit           = 3
	tbSetProgressValue = 9
	tbSetProgressState = 10
	tbSetOverlayIcon   = 18

	wvAddProcessFailed = vtIndex("ICoreWebView2", "add_ProcessFailed")
)

const (
	clsidTaskbarList      = "56FDF344-FD6D-11d0-958A-006097C9A090"
	iidTaskbarList3       = "ea1afb91-9e28-4b86-90e9-9e9f8a5eefaf"
	iidProcessFailedEvent = "79e0aea4-990b-42d9-aa1d-0fcc2e5bc7f1"

	dwmUseImmersiveDarkMode    = 20
	dwmUseImmersiveDarkModeOld = 19
	swpFrameChanged            = 0x0020
	flashAllUntilFocus         = 0x0F // FLASHW_ALL | FLASHW_TIMERNOFG
	spiGetHighContrast         = 0x0042
	hcfHighContrastOn          = 0x0001
)

var sysSt struct {
	mu sync.Mutex

	displays func()
	power    func(caps.PowerEvent)
	theme    func(caps.ThemeInfo)
	crash    func(caps.ViewCrash)

	suspended bool
	locked    bool
	wtsDone   bool

	badge        string
	prog         caps.Progress
	overlay      uintptr // HICON of the current badge
	overlayText  string
	taskbar      unsafe.Pointer // ITaskbarList3, main thread
	taskbarMsg   uint32
	winTheme     string
	sysBlockers  int
	dispBlockers int
}

// ---- taskbar badge and progress ----

// taskbarList returns the ITaskbarList3 object (main thread), or nil.
func taskbarList() unsafe.Pointer {
	if sysSt.taskbar != nil {
		return sysSt.taskbar
	}
	clsid, iid := mustGUID(clsidTaskbarList), mustGUID(iidTaskbarList3)
	var obj unsafe.Pointer
	hr, _, _ := pCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsid)), 0, 1 /* CLSCTX_INPROC_SERVER */, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&obj)))
	if failed(hr) || obj == nil {
		return nil
	}
	if failed(vcall(obj, tbHrInit)) {
		comRelease(obj)
		return nil
	}
	sysSt.taskbar = obj
	return obj
}

// iconFromBadge draws the badge for text into an HICON the caller owns.
func iconFromBadge(text string) (uintptr, error) {
	const size = 32
	pix := caps.BGRAPremultiplied(caps.BadgeImage(text, size))
	stride := ((size + 15) / 16) * 2
	maskBits := make([]byte, stride*size)
	color, _, _ := pCreateBitmap.Call(size, size, 1, 32, uintptr(unsafe.Pointer(&pix[0])))
	mask, _, _ := pCreateBitmap.Call(size, size, 1, 1, uintptr(unsafe.Pointer(&maskBits[0])))
	defer func() {
		for _, o := range []uintptr{color, mask} {
			if o != 0 {
				_, _, _ = pDeleteObject.Call(o)
			}
		}
	}()
	if color == 0 || mask == 0 {
		return 0, errors.New("windows: creating the badge bitmap failed")
	}
	ii := iconInfo{icon: 1, mask: mask, color: color}
	h, _, _ := pCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	if h == 0 {
		return 0, errors.New("windows: CreateIconIndirect failed")
	}
	return h, nil
}

// applyTaskbar sends the stored badge and progress to one window's button.
// Main thread.
func (a *App) applyTaskbar(hwnd uintptr) {
	tb := taskbarList()
	if tb == nil {
		return
	}
	sysSt.mu.Lock()
	prog, badge, icon := sysSt.prog, sysSt.badge, sysSt.overlay
	sysSt.mu.Unlock()
	vcall(tb, tbSetProgressState, hwnd, uintptr(taskbarFlags(prog.State)))
	switch prog.State {
	case caps.ProgressNormal, caps.ProgressError, caps.ProgressPaused:
		vcall(tb, tbSetProgressValue, hwnd, uintptr(prog.Value*1000+0.5), 1000)
	}
	if badge == "" || icon == 0 {
		vcall(tb, tbSetOverlayIcon, hwnd, 0, 0)
	} else {
		vcall(tb, tbSetOverlayIcon, hwnd, icon, uintptr(unsafe.Pointer(wstr(badge))))
	}
}

func (a *App) applyTaskbarAll() {
	for hwnd := range a.windows {
		a.applyTaskbar(hwnd)
	}
}

// SetBadge shows text as an overlay icon on the taskbar button; "" removes it.
func (a *App) SetBadge(text string) (err error) {
	var icon uintptr
	if text != "" {
		if icon, err = iconFromBadge(text); err != nil {
			return err
		}
	}
	a.callMain(func() {
		sysSt.mu.Lock()
		old := sysSt.overlay
		sysSt.badge, sysSt.overlay = text, icon
		sysSt.mu.Unlock()
		a.applyTaskbarAll()
		if old != 0 {
			_, _, _ = pDestroyIcon.Call(old)
		}
	})
	return nil
}

// SetProgress shows progress on the taskbar button.
func (a *App) SetProgress(p caps.Progress) error {
	a.callMain(func() {
		sysSt.mu.Lock()
		sysSt.prog = p
		sysSt.mu.Unlock()
		a.applyTaskbarAll()
	})
	return nil
}

type flashInfo struct {
	size    uint32
	hwnd    uintptr
	flags   uint32
	count   uint32
	timeout uint32
}

// RequestAttention flashes the taskbar buttons until the window is focused.
func (a *App) RequestAttention(on bool) error {
	a.callMain(func() {
		for hwnd := range a.windows {
			fi := flashInfo{size: uint32(unsafe.Sizeof(flashInfo{})), hwnd: hwnd}
			if on {
				fi.flags = flashAllUntilFocus
			}
			_, _, _ = pFlashWindowEx.Call(uintptr(unsafe.Pointer(&fi)))
		}
	})
	return nil
}

// ---- displays ----

type monitorInfoEx struct {
	size    uint32
	monitor rect
	work    rect
	flags   uint32
	device  [32]uint16
}

var (
	monMu   sync.Mutex
	monList []uintptr
	monCB   = syscall.NewCallback(func(hMon, hdc, rc, lparam uintptr) uintptr {
		monList = append(monList, hMon)
		return 1
	})
)

// Displays lists the monitors, primary first.
func (a *App) Displays() ([]caps.Display, error) {
	monMu.Lock()
	defer monMu.Unlock()
	monList = nil
	if r, _, e := pEnumDisplayMonitors.Call(0, 0, monCB, 0); r == 0 {
		return nil, fmt.Errorf("windows: EnumDisplayMonitors: %v", e)
	}
	var out []caps.Display
	for _, h := range monList {
		mi := monitorInfoEx{size: uint32(unsafe.Sizeof(monitorInfoEx{}))}
		if r, _, _ := pGetMonitorInfoW.Call(h, uintptr(unsafe.Pointer(&mi))); r == 0 {
			continue
		}
		dev := xw.UTF16ToString(mi.device[:])
		d := caps.Display{
			ID: displayID(dev), Name: dev,
			X: int(mi.monitor.left), Y: int(mi.monitor.top),
			Width: int(mi.monitor.right - mi.monitor.left), Height: int(mi.monitor.bottom - mi.monitor.top),
			WorkX: int(mi.work.left), WorkY: int(mi.work.top),
			WorkWidth: int(mi.work.right - mi.work.left), WorkHeight: int(mi.work.bottom - mi.work.top),
			Scale: 1, Primary: mi.flags&1 != 0,
		}
		var dpiX, dpiY uint32
		if pGetDpiForMonitor.Find() == nil {
			if hr, _, _ := pGetDpiForMonitor.Call(h, 0 /* MDT_EFFECTIVE_DPI */, uintptr(unsafe.Pointer(&dpiX)), uintptr(unsafe.Pointer(&dpiY))); !failed(hr) {
				d.Scale = dpiScale(dpiX)
			}
		}
		// DEVMODEW: dmSize at 68, dmDisplayFrequency at 184, 220 bytes long.
		var dm [220]byte
		binary.LittleEndian.PutUint16(dm[68:], uint16(len(dm)))
		if r, _, _ := pEnumDisplaySettingsW.Call(uintptr(unsafe.Pointer(&mi.device[0])), ^uintptr(0) /* ENUM_CURRENT_SETTINGS */, uintptr(unsafe.Pointer(&dm[0]))); r != 0 {
			d.RefreshHz = float64(binary.LittleEndian.Uint32(dm[184:]))
		}
		out = append(out, d)
	}
	if len(out) == 0 {
		return nil, errors.New("windows: no monitors")
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Primary && !out[j].Primary })
	return out, nil
}

// OnDisplays sets the handler for WM_DISPLAYCHANGE.
func (a *App) OnDisplays(fn func()) {
	sysSt.mu.Lock()
	sysSt.displays = fn
	sysSt.mu.Unlock()
}

// ---- power ----

type systemPowerStatus struct {
	acLine, batteryFlag, batteryPercent, systemFlag byte
	lifeTime, fullLifeTime                          uint32
}

type lastInputInfo struct {
	size uint32
	time uint32
}

// PowerSample reads the AC line and the idle time.
func (a *App) PowerSample() (caps.PowerSample, error) {
	s := caps.PowerSample{IdleSeconds: -1}
	var ps systemPowerStatus
	if r, _, _ := pGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&ps))); r != 0 {
		s.OnBattery = ps.acLine == 0
	}
	li := lastInputInfo{size: uint32(unsafe.Sizeof(lastInputInfo{}))}
	if r, _, _ := pGetLastInputInfo.Call(uintptr(unsafe.Pointer(&li))); r != 0 {
		tick, _, _ := pGetTickCount.Call()
		s.IdleSeconds = float64(idleMillis(uint32(tick), li.time)) / 1000
	}
	sysSt.mu.Lock()
	s.Locked = sysSt.locked
	sysSt.mu.Unlock()
	return s, nil
}

// OnPower sets the handler for suspend, resume, lock and unlock, and asks for
// session notifications on first use.
func (a *App) OnPower(fn func(caps.PowerEvent)) {
	sysSt.mu.Lock()
	sysSt.power = fn
	reg := !sysSt.wtsDone
	sysSt.wtsDone = true
	sysSt.mu.Unlock()
	if reg {
		a.callMain(func() {
			_, _, _ = pWTSRegisterSessionNotification.Call(a.utilWnd, 0 /* NOTIFY_FOR_THIS_SESSION */)
		})
	}
}

var execHolder struct {
	once sync.Once
	ch   chan uint32
}

// setExecutionState passes flags to SetThreadExecutionState on one thread
// kept for the purpose: the state is released when its thread ends.
func setExecutionState(flags uint32) {
	execHolder.once.Do(func() {
		execHolder.ch = make(chan uint32, 8)
		go func() {
			runtime.LockOSThread()
			for f := range execHolder.ch {
				_, _, _ = pSetThreadExecutionState.Call(uintptr(f))
			}
		}()
	})
	execHolder.ch <- flags
}

// PreventSleep keeps the system (and with display, the screen) awake.
func (a *App) PreventSleep(reason string, display bool) (func(), error) {
	change := func(sys, disp int) {
		sysSt.mu.Lock()
		sysSt.sysBlockers += sys
		sysSt.dispBlockers += disp
		f := executionFlags(sysSt.sysBlockers, sysSt.dispBlockers)
		sysSt.mu.Unlock()
		setExecutionState(f)
	}
	if display {
		change(0, 1)
	} else {
		change(1, 0)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			if display {
				change(0, -1)
			} else {
				change(-1, 0)
			}
		})
	}, nil
}

// ---- theme ----

// Theme reads the apps colour mode and the high contrast flag.
func (a *App) Theme() (caps.ThemeInfo, error) {
	return systemTheme(), nil
}

func systemTheme() (info caps.ThemeInfo) {
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE); err == nil {
		if v, _, err := k.GetIntegerValue("AppsUseLightTheme"); err == nil {
			info.Dark = v == 0
		}
		k.Close()
	}
	hc := struct {
		size   uint32
		flags  uint32
		scheme *uint16
	}{size: 16}
	if r, _, _ := pSystemParametersInfoW.Call(spiGetHighContrast, uintptr(hc.size), uintptr(unsafe.Pointer(&hc)), 0); r != 0 {
		info.HighContrast = hc.flags&hcfHighContrastOn != 0
	}
	return info
}

// OnTheme sets the handler for theme changes (WM_SETTINGCHANGE).
func (a *App) OnTheme(fn func(caps.ThemeInfo)) {
	sysSt.mu.Lock()
	sysSt.theme = fn
	sysSt.mu.Unlock()
}

// SetWindowTheme makes the title bars light, dark or follow the system.
func (a *App) SetWindowTheme(mode string) error {
	a.callMain(func() {
		sysSt.mu.Lock()
		sysSt.winTheme = mode
		sysSt.mu.Unlock()
		for hwnd := range a.windows {
			applyTitleBar(hwnd)
		}
	})
	return nil
}

// applyTitleBar sets the immersive dark mode attribute of one window.
func applyTitleBar(hwnd uintptr) {
	sysSt.mu.Lock()
	mode := sysSt.winTheme
	sysSt.mu.Unlock()
	dark := mode == "dark" || (mode != "light" && systemTheme().Dark)
	v := int32(0)
	if dark {
		v = 1
	}
	if pDwmSetWindowAttribute.Find() != nil {
		return
	}
	hr, _, _ := pDwmSetWindowAttribute.Call(hwnd, dwmUseImmersiveDarkMode, uintptr(unsafe.Pointer(&v)), 4)
	if failed(hr) {
		// Windows 10 before 20H1 knew the attribute as 19.
		_, _, _ = pDwmSetWindowAttribute.Call(hwnd, dwmUseImmersiveDarkModeOld, uintptr(unsafe.Pointer(&v)), 4)
	}
	_, _, _ = pSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged)
}

// ---- window hooks and the utility window's messages ----

// windowCreated is called when a window exists, before it is shown.
func (a *App) windowCreated(hwnd uintptr) {
	if sysSt.taskbarMsg == 0 {
		m, _, _ := pRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(wstr("TaskbarButtonCreated"))))
		sysSt.taskbarMsg = uint32(m)
	}
	applyTitleBar(hwnd)
}

// windowMessage handles messages of an app window; it reports whether it did.
func (a *App) windowMessage(hwnd, message uintptr) bool {
	if sysSt.taskbarMsg != 0 && message == uintptr(sysSt.taskbarMsg) {
		// The taskbar button exists (again): send the badge and progress.
		a.applyTaskbar(hwnd)
		return true
	}
	return false
}

// systemMessage handles the broadcasts the utility window receives.
func (a *App) systemMessage(message, wParam, lParam uintptr) (handled bool, ret uintptr) {
	switch message {
	case wmDisplayChange:
		sysSt.mu.Lock()
		f := sysSt.displays
		sysSt.mu.Unlock()
		if f != nil {
			f()
		}
		return true, 0
	case wmPowerBroadcast:
		sysSt.mu.Lock()
		kind, ok := powerEvent(wParam, sysSt.suspended)
		if ok {
			sysSt.suspended = kind == caps.PowerSuspend
		}
		f := sysSt.power
		sysSt.mu.Unlock()
		if ok && f != nil {
			f(caps.PowerEvent{Kind: kind})
		}
		return true, 1 // TRUE: the request is granted
	case wmWTSSessionChange:
		kind, ok := sessionEvent(wParam)
		if !ok {
			return true, 0
		}
		sysSt.mu.Lock()
		sysSt.locked = kind == caps.PowerLock
		f := sysSt.power
		sysSt.mu.Unlock()
		if f != nil {
			f(caps.PowerEvent{Kind: kind})
		}
		return true, 0
	case wmSettingChange:
		area := ""
		if lParam != 0 {
			area = xw.UTF16PtrToString((*uint16)(ptr(lParam)))
		}
		if !themeSettingChange(wParam, area) {
			return false, 0
		}
		for hwnd := range a.windows {
			applyTitleBar(hwnd)
		}
		sysSt.mu.Lock()
		f := sysSt.theme
		sysSt.mu.Unlock()
		if f != nil {
			f(systemTheme())
		}
		return false, 0 // let DefWindowProc see it too
	}
	return false, 0
}

// ---- WebView2 process failures ----

// OnViewCrash sets the handler for render process failures.
func (a *App) OnViewCrash(fn func(caps.ViewCrash)) {
	sysSt.mu.Lock()
	sysSt.crash = fn
	sysSt.mu.Unlock()
}

// connectProcessFailed hooks add_ProcessFailed for a view (main thread).
func (a *App) connectProcessFailed(v *view) {
	v.hFail = newHandler(iidProcessFailedEvent, false, func(sender, args uintptr) uintptr {
		var kind int32
		if v.w.destroyed || args == 0 || failed(vcall(ptr(args), 3 /* get_ProcessFailedKind */, uintptr(unsafe.Pointer(&kind)))) {
			return sOK
		}
		reason, report := processFailedReason(kind)
		if !report {
			return sOK
		}
		sysSt.mu.Lock()
		f := sysSt.crash
		sysSt.mu.Unlock()
		if f != nil {
			f(caps.ViewCrash{View: v.ref, Window: v.w.Ref(), Reason: reason})
		}
		return sOK
	})
	var token int64
	vcall(v.webview, wvAddProcessFailed, uintptr(v.hFail), uintptr(unsafe.Pointer(&token)))
}

// ReloadView navigates the view to its current route again, which recovers a
// render process that failed.
func (a *App) ReloadView(ref caps.ViewRef) (err error) {
	a.callMain(func() {
		for _, w := range a.windows {
			if v := w.viewByRef(ref); v != nil {
				if v.webview == nil {
					err = errors.New("windows: the view has no webview")
					return
				}
				vcall(v.webview, wvNavigate, uintptr(unsafe.Pointer(wstr(navURL(v.currentRoute())))))
				return
			}
		}
		err = fmt.Errorf("windows: no view %d", ref)
	})
	return
}
