//go:build windows

package windows

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows/registry"

	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/toastscheme"
)

// ErrActivationForwarded is returned by New when this process was started by
// a toast click and handed the click to the instance that is already running.
// The caller should exit without an error.
var ErrActivationForwarded = errors.New("windows: the notification click was handed to the running instance")

// copyDataMagic marks a WM_COPYDATA message as a toast activation ("KTOA").
const copyDataMagic = 0x4B544F41

const (
	wmCopyData      = 0x004A
	smtoAbortIfHung = 0x0002
	asfrwAny        = ^uintptr(0) // ASFRW_ANY: any process may take the foreground
	mbTopmost       = 0x00040000
	mbSetForeground = 0x00010000
)

var (
	pFindWindow               = user32.NewProc("FindWindowW")
	pSendMessageTimeout       = user32.NewProc("SendMessageTimeoutW")
	pGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	pAllowSetForegroundWindow = user32.NewProc("AllowSetForegroundWindow")
	pShellExecuteW            = shell32.NewProc("ShellExecuteW")
	dialogsSuppressed         = os.Getenv("KRATE_NATIVE_NO_DIALOG") != ""
)

// copyDataStruct is COPYDATASTRUCT.
type copyDataStruct struct {
	data   uintptr
	size   uint32
	buffer uintptr
}

// forwardActivation hands a toast click to the running instance of the app.
// It reports whether an instance took it.
func forwardActivation(scheme, id, action string) bool {
	hwnd, _, _ := pFindWindow.Call(uintptr(unsafe.Pointer(wstr(msgClassName))), uintptr(unsafe.Pointer(wstr(sinkWindowName(scheme)))))
	if hwnd == 0 {
		return false
	}
	// The click gave this process the right to take the foreground; pass it on
	// so the running app can raise its window.
	var pid uint32
	_, _, _ = pGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid != 0 {
		_, _, _ = pAllowSetForegroundWindow.Call(uintptr(pid))
	}
	units := encodeActivation(toastscheme.URI(scheme, id, action))
	cds := copyDataStruct{data: copyDataMagic, size: uint32(len(units) * 2), buffer: uintptr(unsafe.Pointer(&units[0]))}
	var result uintptr
	r, _, _ := pSendMessageTimeout.Call(hwnd, wmCopyData, 0, uintptr(unsafe.Pointer(&cds)), smtoAbortIfHung, 5000, uintptr(unsafe.Pointer(&result)))
	return r != 0 && result == 1
}

// activationMessage handles WM_COPYDATA on the utility window: a later
// launch of the app (a toast click) handing its click over.
func (a *App) activationMessage(lParam uintptr) uintptr {
	cds := (*copyDataStruct)(ptr(lParam))
	if cds == nil || cds.data != copyDataMagic || cds.buffer == 0 || cds.size == 0 || cds.size%2 != 0 || cds.size > 16384 {
		return 0
	}
	units := make([]uint16, cds.size/2)
	copy(units, unsafe.Slice((*uint16)(ptr(cds.buffer)), len(units)))
	id, action, ok := decodeActivation(a.toastScheme, units)
	if !ok {
		return 0
	}
	a.raiseWindow()
	emitNotify(caps.NotifyEvent{ID: id, Action: action})
	return 1
}

// raiseWindow brings a window of the app to the foreground, restoring it when
// minimized.
func (a *App) raiseWindow() {
	for h, w := range a.windows {
		if r, _, _ := pIsIconic.Call(h); r != 0 {
			_, _, _ = pShowWindow.Call(h, swRestore)
		}
		w.Focus()
		return
	}
}

// registerToastProtocol writes the HKCU registration that makes the toast
// scheme launch this executable. A packaged app gets it from its manifest.
func registerToastProtocol(scheme, productName string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("windows: locating the executable for toast activation: %w", err)
	}
	if exe, err = filepath.Abs(exe); err != nil {
		return err
	}
	for _, v := range toastscheme.RegistryValues(scheme, productName, exe) {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, v.Key, registry.SET_VALUE)
		if err != nil {
			return fmt.Errorf("windows: registering the notification activation scheme: %w", err)
		}
		err = k.SetStringValue(v.Name, v.Value)
		k.Close()
		if err != nil {
			return fmt.Errorf("windows: registering the notification activation scheme: %w", err)
		}
	}
	return nil
}

// reportRuntimeFailure tells the user, in a message box, that the WebView2
// Runtime is missing or broken, and offers the download page. A program built
// for the GUI subsystem has no console to print to, so without this the app
// would just not appear.
func (a *App) reportRuntimeFailure(err error) {
	var re *runtimeError
	if !errors.As(err, &re) || dialogsSuppressed {
		return
	}
	text, caption := runtimeDialogText(a.displayName(), re.missing, re.detail)
	showRuntimeDialog(text, caption)
}

func showRuntimeDialog(text, caption string) {
	r, _, _ := pMessageBoxW.Call(0, uintptr(unsafe.Pointer(wstr(text))), uintptr(unsafe.Pointer(wstr(caption))),
		mbYesNo|mbIconError|mbTopmost|mbSetForeground)
	if r == idYes {
		_, _, _ = pShellExecuteW.Call(0, uintptr(unsafe.Pointer(wstr("open"))), uintptr(unsafe.Pointer(wstr(webview2DownloadPage))), 0, 0, 1)
	}
}
