//go:build windows

package windows

import (
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	xw "golang.org/x/sys/windows"
)

const ptrSize = unsafe.Sizeof(uintptr(0))

// HRESULT values.
const (
	sOK          = 0
	sFalse       = 1
	eNoInterface = 0x80004002
	eNotImpl     = 0x80004001
	eFail        = 0x80004005
	eOutOfMemory = 0x8007000E
	eInvalidArg  = 0x80070057
	// STG_E_INVALIDFUNCTION, returned by Seek on a non-seekable stream.
	stgEInvalidFunction = 0x80030001
	stgEAccessDenied    = 0x80030005
)

func failed(hr uintptr) bool { return int32(hr) < 0 }

// ptr converts an address received from foreign code to a pointer without
// tripping the unsafeptr vet check.
func ptr(u uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&u)) }

// vcall calls a COM method by vtable slot. The explicit uintptr conversions
// at call sites stay alive for the duration of the call.
//
//go:uintptrescapes
func vcall(obj unsafe.Pointer, slot int, args ...uintptr) uintptr {
	vt := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vt, uintptr(slot)*ptrSize))
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, uintptr(obj))
	all = append(all, args...)
	r, _, _ := syscall.SyscallN(fn, all...)
	return r
}

func comAddRef(obj unsafe.Pointer) {
	if obj != nil {
		vcall(obj, 1)
	}
}

func comRelease(obj unsafe.Pointer) {
	if obj != nil {
		vcall(obj, 2)
	}
}

// wstr returns a NUL-terminated UTF-16 copy of s (NUL bytes are dropped).
func wstr(s string) *uint16 {
	if strings.IndexByte(s, 0) >= 0 {
		s = strings.ReplaceAll(s, "\x00", "")
	}
	p, _ := xw.UTF16PtrFromString(s)
	return p
}

// comString calls a "get string" method (LPWSTR out) and frees the result.
func comString(obj unsafe.Pointer, slot int) string {
	var p uintptr
	if failed(vcall(obj, slot, uintptr(unsafe.Pointer(&p)))) || p == 0 {
		return ""
	}
	s := xw.UTF16PtrToString((*uint16)(ptr(p)))
	xw.CoTaskMemFree(ptr(p))
	return s
}

type point struct{ x, y int32 }

type rect struct{ left, top, right, bottom int32 }

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
	private uint32
}

type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   uintptr
	icon       uintptr
	cursor     uintptr
	background uintptr
	menuName   *uint16
	className  *uint16
	iconSm     uintptr
}

// Win32 constants.
const (
	wsOverlappedWindow = 0x00CF0000
	wsThickFrame       = 0x00040000
	wsMaximizeBox      = 0x00010000
	wsClipChildren     = 0x02000000
	wsClipSiblings     = 0x04000000
	wsPopup            = 0x80000000

	cwUseDefault = 0x80000000

	wmDestroy    = 0x0002
	wmSize       = 0x0005
	wmSetFocus   = 0x0007
	wmClose      = 0x0010
	wmQuit       = 0x0012
	wmDpiChanged = 0x02E0
	wmApp        = 0x8000
	wmDispatch   = wmApp + 0x4B

	sizeMinimized = 1

	swShow = 5

	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010

	pmRemove = 0x0001

	qsAllInput          = 0x04FF
	mwmoInputAvailable  = 0x0004
	idcArrow            = 32512
	colorWindow         = 5
	csHRedraw           = 0x0002
	csVRedraw           = 0x0001
	coinitApartment     = 0x2
	rpcEChangedMode     = 0x80010106
	webviewMoveProgram  = 0           // COREWEBVIEW2_MOVE_FOCUS_REASON_PROGRAMMATIC
	resourceContextAll  = 0           // COREWEBVIEW2_WEB_RESOURCE_CONTEXT_ALL
	dpiAwarenessPMv2    = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
	defaultClientWidth  = 1024
	defaultClientHeight = 768
)

// hwndMessage is HWND_MESSAGE (-3).
const hwndMessage = ^uintptr(2)

var (
	user32   = xw.NewLazySystemDLL("user32.dll")
	kernel32 = xw.NewLazySystemDLL("kernel32.dll")
	ole32    = xw.NewLazySystemDLL("ole32.dll")
	shlwapi  = xw.NewLazySystemDLL("shlwapi.dll")
	shcore   = xw.NewLazySystemDLL("shcore.dll")

	pRegisterClassExW     = user32.NewProc("RegisterClassExW")
	pCreateWindowExW      = user32.NewProc("CreateWindowExW")
	pDefWindowProcW       = user32.NewProc("DefWindowProcW")
	pDestroyWindow        = user32.NewProc("DestroyWindow")
	pGetMessageW          = user32.NewProc("GetMessageW")
	pPeekMessageW         = user32.NewProc("PeekMessageW")
	pTranslateMessage     = user32.NewProc("TranslateMessage")
	pDispatchMessageW     = user32.NewProc("DispatchMessageW")
	pPostMessageW         = user32.NewProc("PostMessageW")
	pPostQuitMessage      = user32.NewProc("PostQuitMessage")
	pSetWindowTextW       = user32.NewProc("SetWindowTextW")
	pSetWindowPos         = user32.NewProc("SetWindowPos")
	pGetClientRect        = user32.NewProc("GetClientRect")
	pShowWindow           = user32.NewProc("ShowWindow")
	pUpdateWindow         = user32.NewProc("UpdateWindow")
	pLoadCursorW          = user32.NewProc("LoadCursorW")
	pAdjustWindowRectEx   = user32.NewProc("AdjustWindowRectEx")
	pAdjustWindowRectExDP = user32.NewProc("AdjustWindowRectExForDpi")
	pGetDpiForWindow      = user32.NewProc("GetDpiForWindow")
	pGetDpiForSystem      = user32.NewProc("GetDpiForSystem")
	pSetDpiAwarenessCtx   = user32.NewProc("SetProcessDpiAwarenessContext")
	pSetProcessDPIAware   = user32.NewProc("SetProcessDPIAware")
	pMsgWaitForMultiple   = user32.NewProc("MsgWaitForMultipleObjectsEx")

	pGetModuleHandleW   = kernel32.NewProc("GetModuleHandleW")
	pGetCurrentThreadID = kernel32.NewProc("GetCurrentThreadId")

	pCoInitializeEx = ole32.NewProc("CoInitializeEx")
	pCoTaskMemAlloc = ole32.NewProc("CoTaskMemAlloc")

	pSHCreateMemStream = shlwapi.NewProc("SHCreateMemStream")

	pSetProcessDpiAwareness = shcore.NewProc("SetProcessDpiAwareness")
)

func currentThreadID() uint32 {
	r, _, _ := pGetCurrentThreadID.Call()
	return uint32(r)
}

// setDPIAware opts the process into per-monitor v2 scaling, falling back to
// older APIs. Failure is harmless (the OS then bitmap-scales the window).
func setDPIAware() {
	if pSetDpiAwarenessCtx.Find() == nil {
		if r, _, _ := pSetDpiAwarenessCtx.Call(dpiAwarenessPMv2); r != 0 {
			return
		}
	}
	if pSetProcessDpiAwareness.Find() == nil {
		if r, _, _ := pSetProcessDpiAwareness.Call(2); r == 0 {
			return
		}
	}
	if pSetProcessDPIAware.Find() == nil {
		pSetProcessDPIAware.Call()
	}
}

func systemDPI() int {
	if pGetDpiForSystem.Find() == nil {
		if r, _, _ := pGetDpiForSystem.Call(); r != 0 {
			return int(r)
		}
	}
	return 96
}

func windowDPI(hwnd uintptr) int {
	if pGetDpiForWindow.Find() == nil {
		if r, _, _ := pGetDpiForWindow.Call(hwnd); r != 0 {
			return int(r)
		}
	}
	return systemDPI()
}

// frameSize converts a client size in device pixels to the outer window size.
func frameSize(style uint32, cw, ch, dpi int) (int, int) {
	r := rect{0, 0, int32(cw), int32(ch)}
	if pAdjustWindowRectExDP.Find() == nil {
		pAdjustWindowRectExDP.Call(uintptr(unsafe.Pointer(&r)), uintptr(style), 0, 0, uintptr(dpi))
	} else {
		pAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&r)), uintptr(style), 0, 0)
	}
	return int(r.right - r.left), int(r.bottom - r.top)
}

func scaleDPI(v, dpi int) int { return (v*dpi + 48) / 96 }

// shCreateMemStream wraps a copy of b in a system IStream (refcount 1). It
// returns nil for empty data.
func shCreateMemStream(b []byte) unsafe.Pointer {
	if len(b) == 0 || pSHCreateMemStream.Find() != nil {
		return nil
	}
	r, _, _ := pSHCreateMemStream.Call(uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	runtime.KeepAlive(b)
	return ptr(r)
}
