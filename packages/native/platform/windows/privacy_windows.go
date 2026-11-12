//go:build windows

package windows

import (
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	xw "golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/kratejs/krate/packages/native/internal/thumb"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Windows privacy state and capturable screens and windows.
//
// UNTESTED on a real session until the Windows CI job runs the GUI test.
// Windows has no API that makes an unpackaged desktop app show a consent
// prompt, so RequestPrivacy only reports the state; the user changes it in
// Settings, Privacy and security.

var (
	_ caps.Privacy        = (*App)(nil)
	_ caps.CaptureSources = (*App)(nil)
)

const consentRoot = `Software\Microsoft\Windows\CurrentVersion\CapabilityAccessManager\ConsentStore\`

func regValue(root registry.Key, path string) string {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	s, _, err := k.GetStringValue("Value")
	if err != nil {
		return ""
	}
	return s
}

// PrivacyStatus reads the capability access manager: the machine-wide and
// per-user switch of the device, and the one for desktop apps.
func (a *App) PrivacyStatus(kind caps.PermissionKind) (caps.PrivacyStatus, error) {
	key := consentKey(kind)
	if key == "" {
		return caps.PrivacyUnknown, fmt.Errorf("windows: %q has no operating system privacy switch", kind)
	}
	return consentStatus(
		regValue(registry.LOCAL_MACHINE, consentRoot+key),
		regValue(registry.CURRENT_USER, consentRoot+key),
		regValue(registry.CURRENT_USER, consentRoot+key+`\NonPackaged`),
	), nil
}

// RequestPrivacy returns the current state; Windows shows no prompt to an
// unpackaged app.
func (a *App) RequestPrivacy(kind caps.PermissionKind) (caps.PrivacyStatus, error) {
	return a.PrivacyStatus(kind)
}

var (
	pcEnumWindows       = user32.NewProc("EnumWindows")
	pcIsWindowVisible   = user32.NewProc("IsWindowVisible")
	pcGetWindowRect     = user32.NewProc("GetWindowRect")
	pcGetWindowLongW    = user32.NewProc("GetWindowLongW")
	pcGetWindowPID      = user32.NewProc("GetWindowThreadProcessId")
	pcEnumMonitors      = user32.NewProc("EnumDisplayMonitors")
	pcGetMonitorInfoW   = user32.NewProc("GetMonitorInfoW")
	pcGetDC             = user32.NewProc("GetDC")
	pcReleaseDC         = user32.NewProc("ReleaseDC")
	pcPrintWindow       = user32.NewProc("PrintWindow")
	pcCreateCompatDC    = gdi32.NewProc("CreateCompatibleDC")
	pcCreateCompatBmp   = gdi32.NewProc("CreateCompatibleBitmap")
	pcSelectObject      = gdi32.NewProc("SelectObject")
	pcBitBlt            = gdi32.NewProc("BitBlt")
	pcGetDIBits         = gdi32.NewProc("GetDIBits")
	pcDeleteDC          = gdi32.NewProc("DeleteDC")
	dwmapi              = xw.NewLazySystemDLL("dwmapi.dll")
	pcDwmGetWindowAttrs = dwmapi.NewProc("DwmGetWindowAttribute")
)

const (
	gwlExStyle        = -20
	wsExToolWindow    = 0x80
	dwmwaCloaked      = 14
	srcCopy           = 0x00CC0020
	captureBlt        = 0x40000000
	pwRenderFull      = 2
	biRGB             = 0
	dibRGBColors      = 0
	maxCaptureWindows = 200
)

type monitorInfo struct {
	size    uint32
	monitor rect
	work    rect
	flags   uint32
}

type bitmapInfoHeader struct {
	size          uint32
	width, height int32
	planes, bpp   uint16
	compression   uint32
	sizeImage     uint32
	xppm, yppm    int32
	used, imp     uint32
}

// CaptureSources lists the monitors and the top-level windows. Thumbnails are
// taken with GDI (BitBlt of the screen, PrintWindow of a window); a window
// that refuses to paint into a bitmap has none.
func (a *App) CaptureSources(thumbWidth int) ([]caps.CaptureSource, error) {
	var out []caps.CaptureSource

	var mons []rect
	cb := syscall.NewCallback(func(hMon, hdc, rc, data uintptr) uintptr {
		mi := monitorInfo{size: uint32(unsafe.Sizeof(monitorInfo{}))}
		if r, _, _ := pcGetMonitorInfoW.Call(hMon, uintptr(unsafe.Pointer(&mi))); r != 0 {
			mons = append(mons, mi.monitor)
		}
		return 1
	})
	pcEnumMonitors.Call(0, 0, cb, 0)
	for i, m := range mons {
		s := caps.CaptureSource{ID: fmt.Sprintf("screen:%d", i+1), Kind: "screen", Name: monitorName(i + 1),
			Width: int(m.right - m.left), Height: int(m.bottom - m.top)}
		if thumbWidth > 0 {
			s.Thumbnail = grabScreen(m, thumbWidth)
		}
		out = append(out, s)
	}

	var rows []winRow
	wcb := syscall.NewCallback(func(hwnd, data uintptr) uintptr {
		if len(rows) >= maxCaptureWindows {
			return 0
		}
		vis, _, _ := pcIsWindowVisible.Call(hwnd)
		var r rect
		pcGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		var cloaked uint32
		pcDwmGetWindowAttrs.Call(hwnd, dwmwaCloaked, uintptr(unsafe.Pointer(&cloaked)), 4)
		ex, _, _ := pcGetWindowLongW.Call(hwnd, uintptr(^uintptr(0)-19)) // GWL_EXSTYLE (-20)
		var buf [512]uint16
		n, _, _ := pGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		rows = append(rows, winRow{
			hwnd: hwnd, title: syscall.UTF16ToString(buf[:n]), app: processName(hwnd),
			width: int(r.right - r.left), height: int(r.bottom - r.top),
			visible: vis != 0, cloaked: cloaked != 0, toolWindow: uint32(ex)&wsExToolWindow != 0,
		})
		return 1
	})
	pcEnumWindows.Call(wcb, 0)
	for _, s := range windowSources(rows) {
		if thumbWidth > 0 {
			var h uint64
			fmt.Sscanf(s.ID, "window:%d", &h)
			s.Thumbnail = grabWindow(uintptr(h), s.Width, s.Height, thumbWidth)
		}
		out = append(out, s)
	}
	return out, nil
}

// processName is the executable name, without the extension, of the process
// that owns hwnd; "" when it cannot be read.
func processName(hwnd uintptr) string {
	var pid uint32
	pcGetWindowPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return ""
	}
	h, err := xw.OpenProcess(xw.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer xw.CloseHandle(h)
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	if xw.QueryFullProcessImageName(h, 0, &buf[0], &n) != nil {
		return ""
	}
	base := filepath.Base(syscall.UTF16ToString(buf[:n]))
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// withBitmap runs paint on a memory DC holding a w x h bitmap, then reads the
// pixels back as top-down BGRA.
func withBitmap(w, h int, paint func(hdc uintptr) bool) []byte {
	if w <= 0 || h <= 0 || w > 16384 || h > 16384 {
		return nil
	}
	screen, _, _ := pcGetDC.Call(0)
	if screen == 0 {
		return nil
	}
	defer pcReleaseDC.Call(0, screen)
	mem, _, _ := pcCreateCompatDC.Call(screen)
	if mem == 0 {
		return nil
	}
	defer pcDeleteDC.Call(mem)
	bmp, _, _ := pcCreateCompatBmp.Call(screen, uintptr(w), uintptr(h))
	if bmp == 0 {
		return nil
	}
	defer pDeleteObject.Call(bmp)
	old, _, _ := pcSelectObject.Call(mem, bmp)
	defer pcSelectObject.Call(mem, old)
	if !paint(mem) {
		return nil
	}
	bi := bitmapInfoHeader{size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), width: int32(w), height: -int32(h), planes: 1, bpp: 32, compression: biRGB}
	pix := make([]byte, w*h*4)
	if r, _, _ := pcGetDIBits.Call(mem, bmp, 0, uintptr(h), uintptr(unsafe.Pointer(&pix[0])), uintptr(unsafe.Pointer(&bi)), dibRGBColors); r == 0 {
		return nil
	}
	return pix
}

func grabScreen(m rect, width int) []byte {
	w, h := int(m.right-m.left), int(m.bottom-m.top)
	pix := withBitmap(w, h, func(mem uintptr) bool {
		screen, _, _ := pcGetDC.Call(0)
		defer pcReleaseDC.Call(0, screen)
		r, _, _ := pcBitBlt.Call(mem, 0, 0, uintptr(w), uintptr(h), screen, uintptr(uint32(m.left)), uintptr(uint32(m.top)), srcCopy|captureBlt)
		return r != 0
	})
	if pix == nil {
		return nil
	}
	return thumb.PNG(thumb.BGRA(pix, w, h, w*4), width)
}

func grabWindow(hwnd uintptr, w, h, width int) []byte {
	pix := withBitmap(w, h, func(mem uintptr) bool {
		r, _, _ := pcPrintWindow.Call(hwnd, mem, pwRenderFull)
		return r != 0
	})
	if pix == nil {
		return nil
	}
	return thumb.PNG(thumb.BGRA(pix, w, h, w*4), width)
}
