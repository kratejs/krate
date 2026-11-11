//go:build windows

package windows

// Capability implementations for the SDK (compile-only in this release: they
// are written against the Win32 API but not exercised by tests here).
//
//   - Window controls, clipboard (text, PNG, files) and menus use plain Win32.
//   - File dialogs use GetOpenFileNameW / GetSaveFileNameW and
//     SHBrowseForFolderW, not the newer IFileDialog COM interfaces: the
//     classic dialogs need no COM vtable code. Folder choice is single
//     selection.
//   - MessageBoxW has fixed button sets, so custom button labels are mapped
//     to OK / OK+Cancel / Yes+No / Yes+No+Cancel by count and the labels are
//     ignored.
//   - Menu accelerators are shown as text only; there is no accelerator table.
//   - Tray, global shortcuts and toast notifications are in tray_windows.go,
//     hotkey_windows.go and toast_windows.go; they share the hidden utility
//     window created in New.

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf16"
	"unsafe"

	xw "golang.org/x/sys/windows"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var (
	comdlg32 = xw.NewLazySystemDLL("comdlg32.dll")
	shell32  = xw.NewLazySystemDLL("shell32.dll")

	pGetOpenFileNameW     = comdlg32.NewProc("GetOpenFileNameW")
	pGetSaveFileNameW     = comdlg32.NewProc("GetSaveFileNameW")
	pSHBrowseForFolderW   = shell32.NewProc("SHBrowseForFolderW")
	pSHGetPathFromIDListW = shell32.NewProc("SHGetPathFromIDListW")
	pDragQueryFileW       = shell32.NewProc("DragQueryFileW")
	pMessageBoxW          = user32.NewProc("MessageBoxW")
	pGetWindowRect        = user32.NewProc("GetWindowRect")
	pIsZoomed             = user32.NewProc("IsZoomed")
	pIsIconic             = user32.NewProc("IsIconic")
	pIsWindowVisible      = user32.NewProc("IsWindowVisible")
	pGetForegroundWindow  = user32.NewProc("GetForegroundWindow")
	pSetForegroundWindow  = user32.NewProc("SetForegroundWindow")
	pGetWindowLongPtrW    = user32.NewProc("GetWindowLongPtrW")
	pSetWindowLongPtrW    = user32.NewProc("SetWindowLongPtrW")
	pGetWindowTextW       = user32.NewProc("GetWindowTextW")
	pMonitorFromWindow    = user32.NewProc("MonitorFromWindow")
	pGetMonitorInfoW      = user32.NewProc("GetMonitorInfoW")
	pOpenClipboard        = user32.NewProc("OpenClipboard")
	pCloseClipboard       = user32.NewProc("CloseClipboard")
	pEmptyClipboard       = user32.NewProc("EmptyClipboard")
	pGetClipboardData     = user32.NewProc("GetClipboardData")
	pSetClipboardData     = user32.NewProc("SetClipboardData")
	pRegisterClipboardFmt = user32.NewProc("RegisterClipboardFormatW")
	pCreateMenu           = user32.NewProc("CreateMenu")
	pCreatePopupMenu      = user32.NewProc("CreatePopupMenu")
	pAppendMenuW          = user32.NewProc("AppendMenuW")
	pSetMenu              = user32.NewProc("SetMenu")
	pDrawMenuBar          = user32.NewProc("DrawMenuBar")
	pDestroyMenu          = user32.NewProc("DestroyMenu")
	pTrackPopupMenuEx     = user32.NewProc("TrackPopupMenuEx")
	pGetCursorPos         = user32.NewProc("GetCursorPos")
	pGlobalAlloc          = kernel32.NewProc("GlobalAlloc")
	pGlobalLock           = kernel32.NewProc("GlobalLock")
	pGlobalUnlock         = kernel32.NewProc("GlobalUnlock")
	pGlobalSize           = kernel32.NewProc("GlobalSize")
)

const (
	wmCommand   = 0x0111
	wmKillFocus = 0x0008

	gwlStyle = ^uintptr(15) // -16

	wsCaption     = 0x00C00000
	wsOverlapped  = wsOverlappedWindow
	swHide        = 0
	swMinimize    = 6
	swMaximize    = 3
	swRestore     = 9
	hwndTopmost   = ^uintptr(0) // -1
	hwndNoTopmost = ^uintptr(1) // -2
	swpFrameChg   = 0x0020
	swpNoOwnerZ   = 0x0200

	cfUnicodeText = 13
	cfHDrop       = 15
	gmemMoveable  = 0x0002

	mfString    = 0x0000
	mfGrayed    = 0x0001
	mfChecked   = 0x0008
	mfPopup     = 0x0010
	mfSeparator = 0x0800

	tpmReturnCmd = 0x0100

	ofnAllowMultiselect = 0x00000200
	ofnExplorer         = 0x00080000
	ofnOverwritePrompt  = 0x00000002
	ofnFileMustExist    = 0x00001000
	ofnPathMustExist    = 0x00000800
	ofnNoChangeDir      = 0x00000008

	bifReturnOnlyFSDirs = 0x0001
	bifNewDialogStyle   = 0x0040

	mbOK          = 0x0
	mbOKCancel    = 0x1
	mbYesNoCancel = 0x3
	mbYesNo       = 0x4
	mbIconError   = 0x10
	mbIconQuest   = 0x20
	mbIconWarn    = 0x30
	mbIconInfo    = 0x40
	idOK          = 1
	idCancel      = 2
	idYes         = 6
	idNo          = 7

	monitorDefaultToNearest = 2
)

var (
	_ caps.WindowControls = (*Window)(nil)
	_ caps.WindowEvents   = (*App)(nil)
	_ caps.Dialogs        = (*App)(nil)
	_ caps.Clipboard      = (*App)(nil)
	_ caps.Menus          = (*App)(nil)
)

type winExtState struct {
	ref          caps.WindowRef
	preventClose bool
	wantTop      bool
	fullscreen   bool
	prevStyle    uintptr
	prevRect     rect
	menu         uintptr
	menuCmds     map[uint16]menuCmd
}

type menuCmd struct {
	id      string
	isCheck bool
	checked bool
}

var (
	extMu      sync.Mutex
	extStates  = map[uintptr]*winExtState{}
	nextRef    atomic.Uint64
	winHandler func(caps.WindowEvent)
	menuFn     func(caps.MenuEvent)
	appMenu    []caps.MenuItem
)

func stateOf(w *Window) *winExtState {
	extMu.Lock()
	defer extMu.Unlock()
	s := extStates[w.hwnd]
	if s == nil {
		s = &winExtState{ref: caps.WindowRef(nextRef.Add(1))}
		extStates[w.hwnd] = s
	}
	return s
}

// callMain runs fn on the main thread and waits.
func (a *App) callMain(fn func()) {
	if a.onMain() {
		fn()
		return
	}
	done := make(chan struct{})
	a.Dispatch(func() { fn(); close(done) })
	<-done
}

// OnWindowEvent sets the handler for window events.
func (a *App) OnWindowEvent(fn func(caps.WindowEvent)) {
	extMu.Lock()
	winHandler = fn
	extMu.Unlock()
}

func emit(w *Window, kind caps.WindowEventKind) {
	extMu.Lock()
	fn := winHandler
	extMu.Unlock()
	if fn != nil {
		fn(caps.WindowEvent{Window: stateOf(w).ref, Kind: kind})
	}
}

// winExtMessage is called from the window procedure before default handling.
func winExtMessage(a *App, w *Window, message, wParam, lParam uintptr) (handled bool, ret uintptr) {
	switch message {
	case wmClose:
		if stateOf(w).preventClose {
			emit(w, caps.WindowCloseRequested)
			return true, 0
		}
		if a.parkable(w) {
			w.release()
			return true, 0
		}
	case wmDestroy:
		emit(w, caps.WindowClosed)
		extMu.Lock()
		delete(extStates, w.hwnd)
		extMu.Unlock()
	case wmSetFocus:
		emit(w, caps.WindowFocused)
	case wmKillFocus:
		emit(w, caps.WindowBlurred)
	case wmCommand:
		if lParam == 0 { // from a menu
			cmd := uint16(wParam & 0xFFFF)
			s := stateOf(w)
			if mc, ok := s.menuCmds[cmd]; ok {
				extMu.Lock()
				fn := menuFn
				extMu.Unlock()
				if mc.isCheck {
					mc.checked = !mc.checked
					s.menuCmds[cmd] = mc
				}
				if fn != nil {
					fn(caps.MenuEvent{ID: mc.id, Window: s.ref, Checked: mc.checked})
				}
				return true, 0
			}
		}
	}
	return false, 0
}

// Ref identifies the window to the capability interfaces.
func (w *Window) Ref() caps.WindowRef { return stateOf(w).ref }

func (w *Window) onMain(fn func()) {
	w.app.Dispatch(func() {
		if !w.destroyed {
			fn()
		}
	})
}

// SetAlwaysOnTop keeps the window above others.
func (w *Window) SetAlwaysOnTop(on bool) {
	w.onMain(func() {
		stateOf(w).wantTop = on
		after := hwndNoTopmost
		if on {
			after = hwndTopmost
		}
		pSetWindowPos.Call(w.hwnd, after, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
	})
}

// SetFullscreen covers the whole monitor without borders.
func (w *Window) SetFullscreen(on bool) {
	w.onMain(func() {
		s := stateOf(w)
		if on == s.fullscreen {
			return
		}
		if on {
			style, _, _ := pGetWindowLongPtrW.Call(w.hwnd, gwlStyle)
			s.prevStyle = style
			pGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&s.prevRect)))
			mon, _, _ := pMonitorFromWindow.Call(w.hwnd, monitorDefaultToNearest)
			var mi struct {
				size    uint32
				monitor rect
				work    rect
				flags   uint32
			}
			mi.size = uint32(unsafe.Sizeof(mi))
			pGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
			pSetWindowLongPtrW.Call(w.hwnd, gwlStyle, style&^wsOverlapped)
			pSetWindowPos.Call(w.hwnd, 0, uintptr(mi.monitor.left), uintptr(mi.monitor.top),
				uintptr(mi.monitor.right-mi.monitor.left), uintptr(mi.monitor.bottom-mi.monitor.top), swpNoZOrder|swpFrameChg)
			s.fullscreen = true
			return
		}
		pSetWindowLongPtrW.Call(w.hwnd, gwlStyle, s.prevStyle)
		r := s.prevRect
		pSetWindowPos.Call(w.hwnd, 0, uintptr(r.left), uintptr(r.top), uintptr(r.right-r.left), uintptr(r.bottom-r.top), swpNoZOrder|swpFrameChg)
		s.fullscreen = false
	})
}

// SetDecorated(false) removes the caption and the sizing frame.
func (w *Window) SetDecorated(on bool) {
	w.onMain(func() {
		style, _, _ := pGetWindowLongPtrW.Call(w.hwnd, gwlStyle)
		if on {
			style |= wsCaption
		} else {
			style &^= wsCaption
		}
		pSetWindowLongPtrW.Call(w.hwnd, gwlStyle, style)
		pSetWindowPos.Call(w.hwnd, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChg)
	})
}

// SetPosition moves the window (screen pixels).
func (w *Window) SetPosition(x, y int) {
	w.onMain(func() {
		pSetWindowPos.Call(w.hwnd, 0, uintptr(int32(x)), uintptr(int32(y)), 0, 0, swpNoSize|swpNoZOrder|swpNoActivate)
	})
}

// Minimize iconifies the window.
func (w *Window) Minimize() { w.onMain(func() { pShowWindow.Call(w.hwnd, swMinimize) }) }

// Maximize maximizes the window.
func (w *Window) Maximize() { w.onMain(func() { pShowWindow.Call(w.hwnd, swMaximize) }) }

// Restore undoes minimize, maximize and fullscreen.
func (w *Window) Restore() {
	w.SetFullscreen(false)
	w.onMain(func() { pShowWindow.Call(w.hwnd, swRestore) })
}

// Focus brings the window to the foreground.
func (w *Window) Focus() {
	w.onMain(func() {
		w.revive()
		pSetForegroundWindow.Call(w.hwnd)
	})
}

// Show shows the window. A released window gets its webview back on the route
// and scroll position it had.
func (w *Window) Show() {
	w.onMain(func() {
		w.revive()
		pShowWindow.Call(w.hwnd, swShow)
		w.setViewHidden(false)
	})
}

// Hide hides the window. The page gets a krate:memory-pressure event, the
// controller becomes invisible and the browser is asked to use less memory
// for it. Release also closes the controller.
func (w *Window) Hide() {
	w.onMain(func() {
		pShowWindow.Call(w.hwnd, swHide)
		w.setViewHidden(true)
	})
}

// SetPreventClose turns WM_CLOSE into a close-requested event.
func (w *Window) SetPreventClose(on bool) { w.onMain(func() { stateOf(w).preventClose = on }) }

// State reads the window state (positions in screen pixels).
func (w *Window) State() (st caps.WindowState) {
	w.app.callMain(func() {
		if w.destroyed {
			return
		}
		var r rect
		pGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
		st.X, st.Y = int(r.left), int(r.top)
		st.Width, st.Height = int(r.right-r.left), int(r.bottom-r.top)
		z, _, _ := pIsZoomed.Call(w.hwnd)
		ic, _, _ := pIsIconic.Call(w.hwnd)
		vis, _, _ := pIsWindowVisible.Call(w.hwnd)
		fg, _, _ := pGetForegroundWindow.Call()
		style, _, _ := pGetWindowLongPtrW.Call(w.hwnd, gwlStyle)
		s := stateOf(w)
		st.Maximized, st.Minimized, st.Visible = z != 0, ic != 0, vis != 0
		st.Focused = fg == w.hwnd
		st.Decorated = style&wsCaption == wsCaption
		st.Fullscreen = s.fullscreen
		st.AlwaysOnTop = s.wantTop
		buf := make([]uint16, 512)
		n, _, _ := pGetWindowTextW.Call(w.hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		st.Title = string(utf16.Decode(buf[:n]))
	})
	return
}

func (a *App) parentHwnd(ref caps.WindowRef) uintptr {
	extMu.Lock()
	defer extMu.Unlock()
	for h, s := range extStates {
		if s.ref == ref {
			return h
		}
	}
	return 0
}

// FileDialog shows a classic common dialog on the main thread.
func (a *App) FileDialog(parent caps.WindowRef, o caps.FileDialogOptions) (paths []string, err error) {
	owner := a.parentHwnd(parent)
	a.callMain(func() {
		if o.Mode == caps.OpenFolder {
			paths, err = browseFolder(owner, o)
			return
		}
		paths, err = fileName(owner, o)
	})
	return
}

type openFileName struct {
	lStructSize       uint32
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        uintptr
	dwReserved        uint32
	flagsEx           uint32
}

// filterString builds "Name\0*.a;*.b\0...\0\0".
func filterString(fs []caps.FileFilter) *uint16 {
	if len(fs) == 0 {
		fs = []caps.FileFilter{{Name: "All files", Patterns: []string{"*.*"}}}
	}
	var u []uint16
	for _, f := range fs {
		u = append(u, utf16.Encode([]rune(f.Name))...)
		u = append(u, 0)
		u = append(u, utf16.Encode([]rune(strings.Join(f.Patterns, ";")))...)
		u = append(u, 0)
	}
	u = append(u, 0)
	return &u[0]
}

func fileName(owner uintptr, o caps.FileDialogOptions) ([]string, error) {
	buf := make([]uint16, 64*1024)
	copy(buf, utf16.Encode([]rune(o.Name)))
	ofn := openFileName{
		hwndOwner:   owner,
		lpstrFilter: filterString(o.Filters),
		lpstrFile:   &buf[0],
		nMaxFile:    uint32(len(buf)),
		flags:       ofnExplorer | ofnNoChangeDir | ofnPathMustExist,
	}
	ofn.lStructSize = uint32(unsafe.Sizeof(ofn))
	if o.Title != "" {
		ofn.lpstrTitle = wstr(o.Title)
	}
	if o.Dir != "" {
		ofn.lpstrInitialDir = wstr(o.Dir)
	}
	proc := pGetOpenFileNameW
	if o.Mode == caps.SaveFile {
		proc = pGetSaveFileNameW
		if o.ConfirmWrite {
			ofn.flags |= ofnOverwritePrompt
		}
	} else {
		ofn.flags |= ofnFileMustExist
		if o.Multiple {
			ofn.flags |= ofnAllowMultiselect
		}
	}
	ok, _, _ := proc.Call(uintptr(unsafe.Pointer(&ofn)))
	if ok == 0 {
		return nil, nil // cancelled (or an extended error we cannot distinguish cheaply)
	}
	// Result: "path" or "dir\0name1\0name2\0\0".
	var parts []string
	start := 0
	for i := 0; i < len(buf); i++ {
		if buf[i] == 0 {
			if i == start {
				break
			}
			parts = append(parts, string(utf16.Decode(buf[start:i])))
			start = i + 1
		}
	}
	if len(parts) > 1 {
		dir := parts[0]
		out := make([]string, 0, len(parts)-1)
		for _, n := range parts[1:] {
			out = append(out, strings.TrimRight(dir, `\`)+`\`+n)
		}
		return out, nil
	}
	return parts, nil
}

type browseInfo struct {
	hwndOwner      uintptr
	pidlRoot       uintptr
	pszDisplayName *uint16
	lpszTitle      *uint16
	ulFlags        uint32
	lpfn           uintptr
	lParam         uintptr
	iImage         int32
}

func browseFolder(owner uintptr, o caps.FileDialogOptions) ([]string, error) {
	name := make([]uint16, 260)
	bi := browseInfo{hwndOwner: owner, pszDisplayName: &name[0], ulFlags: bifReturnOnlyFSDirs | bifNewDialogStyle}
	if o.Title != "" {
		bi.lpszTitle = wstr(o.Title)
	}
	pidl, _, _ := pSHBrowseForFolderW.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return nil, nil
	}
	defer xw.CoTaskMemFree(ptr(pidl))
	path := make([]uint16, 32768)
	if ok, _, _ := pSHGetPathFromIDListW.Call(pidl, uintptr(unsafe.Pointer(&path[0]))); ok == 0 {
		return nil, errors.New("windows: the chosen folder has no file system path")
	}
	return []string{xw.UTF16ToString(path)}, nil
}

// MessageDialog shows MessageBoxW. See the file comment for the button
// mapping.
func (a *App) MessageDialog(parent caps.WindowRef, o caps.MessageDialogOptions) (n int, err error) {
	owner := a.parentHwnd(parent)
	flags := uintptr(mbOK)
	switch len(o.Buttons) {
	case 2:
		flags = mbYesNo
	case 3:
		flags = mbYesNoCancel
	}
	switch o.Kind {
	case caps.MessageWarning:
		flags |= mbIconWarn
	case caps.MessageError:
		flags |= mbIconError
	case caps.MessageQuestion:
		flags |= mbIconQuest
	default:
		flags |= mbIconInfo
	}
	text := o.Message
	if o.Detail != "" {
		text += "\n\n" + o.Detail
	}
	n = -1
	a.callMain(func() {
		r, _, _ := pMessageBoxW.Call(owner, uintptr(unsafe.Pointer(wstr(text))), uintptr(unsafe.Pointer(wstr(o.Title))), flags)
		switch r {
		case idOK, idYes:
			n = 0
		case idNo:
			n = 1
		case idCancel:
			n = len(o.Buttons) - 1
			if len(o.Buttons) < 2 {
				n = 0
			}
		}
	})
	return n, nil
}

func (a *App) withClipboard(fn func() error) (err error) {
	a.callMain(func() {
		var ok uintptr
		for i := 0; i < 10 && ok == 0; i++ {
			ok, _, _ = pOpenClipboard.Call(a.msgWnd)
		}
		if ok == 0 {
			err = errors.New("windows: the clipboard is busy")
			return
		}
		defer pCloseClipboard.Call()
		err = fn()
	})
	return
}

func setClip(format uintptr, data []byte) error {
	h, _, _ := pGlobalAlloc.Call(gmemMoveable, uintptr(len(data)))
	if h == 0 {
		return errors.New("windows: out of memory")
	}
	p, _, _ := pGlobalLock.Call(h)
	copy(unsafe.Slice((*byte)(ptr(p)), len(data)), data)
	pGlobalUnlock.Call(h)
	if r, _, _ := pSetClipboardData.Call(format, h); r == 0 {
		return errors.New("windows: SetClipboardData failed")
	}
	return nil
}

func getClip(format uintptr) []byte {
	h, _, _ := pGetClipboardData.Call(format)
	if h == 0 {
		return nil
	}
	size, _, _ := pGlobalSize.Call(h)
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		return nil
	}
	defer pGlobalUnlock.Call(h)
	return append([]byte(nil), unsafe.Slice((*byte)(ptr(p)), size)...)
}

func pngFormat() uintptr {
	f, _, _ := pRegisterClipboardFmt.Call(uintptr(unsafe.Pointer(wstr("PNG"))))
	return f
}

// ReadText returns the clipboard text.
func (a *App) ReadText() (s string, err error) {
	err = a.withClipboard(func() error {
		b := getClip(cfUnicodeText)
		if len(b) >= 2 {
			u := unsafe.Slice((*uint16)(unsafe.Pointer(&b[0])), len(b)/2)
			s = xw.UTF16ToString(u)
		}
		return nil
	})
	return
}

// WriteText replaces the clipboard with text.
func (a *App) WriteText(s string) error {
	u := utf16.Encode([]rune(s + "\x00"))
	b := unsafe.Slice((*byte)(unsafe.Pointer(&u[0])), len(u)*2)
	return a.withClipboard(func() error {
		pEmptyClipboard.Call()
		return setClip(cfUnicodeText, b)
	})
}

// ReadImage returns PNG bytes when the clipboard carries the registered "PNG"
// format (what browsers and most editors put there).
func (a *App) ReadImage() (b []byte, err error) {
	err = a.withClipboard(func() error { b = getClip(pngFormat()); return nil })
	return
}

// WriteImage puts PNG bytes on the clipboard under the "PNG" format.
func (a *App) WriteImage(png []byte) error {
	return a.withClipboard(func() error {
		pEmptyClipboard.Call()
		return setClip(pngFormat(), png)
	})
}

// ReadFiles returns the file paths of a CF_HDROP clipboard entry.
func (a *App) ReadFiles() (paths []string, err error) {
	err = a.withClipboard(func() error {
		h, _, _ := pGetClipboardData.Call(cfHDrop)
		if h == 0 {
			return nil
		}
		n, _, _ := pDragQueryFileW.Call(h, ^uintptr(0), 0, 0)
		for i := uintptr(0); i < n; i++ {
			buf := make([]uint16, 32768)
			l, _, _ := pDragQueryFileW.Call(h, i, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
			paths = append(paths, string(utf16.Decode(buf[:l])))
		}
		return nil
	})
	return
}

// WriteFiles puts a CF_HDROP file list on the clipboard.
func (a *App) WriteFiles(paths []string) error {
	// DROPFILES { DWORD pFiles; POINT pt; BOOL fNC; BOOL fWide; } is 20 bytes.
	hdr := make([]byte, 20)
	hdr[0] = 20
	hdr[16] = 1
	var body []uint16
	for _, p := range paths {
		body = append(body, utf16.Encode([]rune(p))...)
		body = append(body, 0)
	}
	body = append(body, 0)
	data := append(hdr, unsafe.Slice((*byte)(unsafe.Pointer(&body[0])), len(body)*2)...)
	return a.withClipboard(func() error {
		pEmptyClipboard.Call()
		return setClip(cfHDrop, data)
	})
}

// Clear empties the clipboard.
func (a *App) Clear() error {
	return a.withClipboard(func() error { pEmptyClipboard.Call(); return nil })
}

// OnMenu sets the handler for menu clicks.
func (a *App) OnMenu(fn func(caps.MenuEvent)) {
	extMu.Lock()
	menuFn = fn
	extMu.Unlock()
}

func menuText(it caps.MenuItem) string {
	label := strings.ReplaceAll(it.Label, "&&", "\x00")
	label = strings.ReplaceAll(label, "&", "&")
	label = strings.ReplaceAll(label, "\x00", "&&")
	if it.Accelerator != "" {
		label += "\t" + strings.ReplaceAll(it.Accelerator, "CmdOrCtrl", "Ctrl")
	}
	return label
}

func buildHMenu(popup bool, items []caps.MenuItem, cmds map[uint16]menuCmd, next *uint16) uintptr {
	var m uintptr
	if popup {
		m, _, _ = pCreatePopupMenu.Call()
	} else {
		m, _, _ = pCreateMenu.Call()
	}
	for _, it := range items {
		switch {
		case it.Kind == caps.MenuItemSeparator:
			pAppendMenuW.Call(m, mfSeparator, 0, 0)
		case len(it.Submenu) > 0 || it.Kind == caps.MenuItemSubmenu:
			sub := buildHMenu(true, it.Submenu, cmds, next)
			pAppendMenuW.Call(m, mfPopup, sub, uintptr(unsafe.Pointer(wstr(menuText(it)))))
		default:
			*next++
			flags := uintptr(mfString)
			if it.Disabled {
				flags |= mfGrayed
			}
			if it.Checked {
				flags |= mfChecked
			}
			cmds[*next] = menuCmd{id: it.ID, isCheck: it.Kind == caps.MenuItemCheck || it.Kind == caps.MenuItemRadio, checked: it.Checked}
			pAppendMenuW.Call(m, flags, uintptr(*next), uintptr(unsafe.Pointer(wstr(menuText(it)))))
		}
	}
	return m
}

func (a *App) applyBar(w *Window, items []caps.MenuItem) {
	s := stateOf(w)
	s.menuCmds = map[uint16]menuCmd{}
	var next uint16 = 100
	var hm uintptr
	if len(items) > 0 {
		hm = buildHMenu(false, items, s.menuCmds, &next)
	}
	pSetMenu.Call(w.hwnd, hm)
	pDrawMenuBar.Call(w.hwnd)
	if s.menu != 0 {
		pDestroyMenu.Call(s.menu)
	}
	s.menu = hm
}

// SetAppMenu installs the menu bar on every window.
func (a *App) SetAppMenu(items []caps.MenuItem) error {
	a.callMain(func() {
		appMenu = append([]caps.MenuItem(nil), items...)
		for _, w := range a.windows {
			a.applyBar(w, items)
		}
	})
	return nil
}

// PopupMenu shows a context menu and reports the choice through OnMenu.
func (a *App) PopupMenu(parent caps.WindowRef, items []caps.MenuItem, x, y int) error {
	hwnd := a.parentHwnd(parent)
	if hwnd == 0 {
		return krate.ErrUnsupported
	}
	if len(items) == 0 {
		return fmt.Errorf("windows: empty menu")
	}
	a.Dispatch(func() {
		cmds := map[uint16]menuCmd{}
		var next uint16 = 100
		hm := buildHMenu(true, items, cmds, &next)
		defer pDestroyMenu.Call(hm)
		var pt struct{ x, y int32 }
		pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
		cmd, _, _ := pTrackPopupMenuEx.Call(hm, tpmReturnCmd, uintptr(pt.x), uintptr(pt.y), hwnd, 0)
		if mc, ok := cmds[uint16(cmd)]; ok {
			extMu.Lock()
			fn := menuFn
			extMu.Unlock()
			if fn != nil {
				fn(caps.MenuEvent{ID: mc.id, Window: stateOfHwnd(hwnd).ref})
			}
		}
	})
	return nil
}

func stateOfHwnd(h uintptr) *winExtState {
	extMu.Lock()
	defer extMu.Unlock()
	if s := extStates[h]; s != nil {
		return s
	}
	return &winExtState{}
}
