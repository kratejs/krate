//go:build windows

package windows

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	xw "golang.org/x/sys/windows"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// A small UI Automation client (IUIAutomation over raw vtable calls) used by
// the accessibility GUI tests. UI Automation is the tree Narrator, NVDA and
// JAWS read, so these tests see our windows, menus, message boxes and the
// WebView2 host the way a screen reader does. They run in the CI native job
// on windows-latest with the other GUI tests (KRATE_NATIVE_GUI=1).

var (
	oleaut32       = xw.NewLazySystemDLL("oleaut32.dll")
	pSysFreeString = oleaut32.NewProc("SysFreeString")
)

const (
	clsidUIAutomation = "ff48dba4-60ef-4201-aa87-54103eef594e"
	iidUIAutomation   = "30cbe57d-d9d0-452a-ab13-7ac5ac4825ee"

	uiaTreeChildren    = 2
	uiaTreeDescendants = 4

	uiaButton   = 50000
	uiaDocument = 50030
	uiaMenuBar  = 50010
	uiaMenuItem = 50011
	uiaMenu     = 50009
	uiaWindow   = 50032
	uiaTitleBar = 50037
)

// IUIAutomation vtable slots.
const (
	uiaGetRootElement   = 5
	uiaElementFromHndl  = 6
	uiaCreateTrueCond   = 21
	elFindAll           = 6
	elControlType       = 21
	elName              = 23
	elHasFocus          = 26
	elFocusable         = 27
	elEnabled           = 28
	elClassName         = 30
	elNativeWindow      = 36
	elIsOffscreen       = 38
	arrLength           = 3
	arrGetElement       = 4
	comMultithreaded    = 0x0
	rpcEChangedModeTest = 0x80010106
)

type uiaClient struct {
	p    unsafe.Pointer
	cond unsafe.Pointer
}

// withUIA runs fn on a locked OS thread that is a COM MTA thread, with a
// client, then releases everything. UI Automation must not be called from the
// thread that runs the window procedures (the main thread).
func withUIA(t *testing.T, fn func(u *uiaClient)) {
	t.Helper()
	needGUI(t)
	done := make(chan struct{})
	var setupErr error
	go func() {
		// A failed poll ends this goroutine with runtime.Goexit, so the
		// channel is closed by a defer.
		defer close(done)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		hr, _, _ := pCoInitializeEx.Call(0, comMultithreaded)
		if failed(hr) && uint32(hr) != rpcEChangedModeTest {
			setupErr = fmt.Errorf("CoInitializeEx: 0x%08x", uint32(hr))
			return
		}
		p, err := coCreate(clsidUIAutomation, iidUIAutomation)
		if err != nil {
			setupErr = err
			return
		}
		u := &uiaClient{p: p}
		var cond uintptr
		if failed(vcall(p, uiaCreateTrueCond, uintptr(unsafe.Pointer(&cond)))) || cond == 0 {
			setupErr = fmt.Errorf("CreateTrueCondition failed")
			return
		}
		u.cond = ptr(cond)
		defer comRelease(u.cond)
		defer comRelease(p)
		fn(u)
	}()
	<-done
	if setupErr != nil {
		t.Skipf("UI Automation is not available: %v", setupErr)
	}
}

type uiaEl struct {
	u *uiaClient
	p unsafe.Pointer
}

func (u *uiaClient) fromHandle(hwnd uintptr) (uiaEl, bool) {
	var out uintptr
	if failed(vcall(u.p, uiaElementFromHndl, hwnd, uintptr(unsafe.Pointer(&out)))) || out == 0 {
		return uiaEl{}, false
	}
	return uiaEl{u, ptr(out)}, true
}

func (e uiaEl) release() { comRelease(e.p) }

func (e uiaEl) name() string {
	var b uintptr
	if failed(vcall(e.p, elName, uintptr(unsafe.Pointer(&b)))) || b == 0 {
		return ""
	}
	defer func() { _, _, _ = pSysFreeString.Call(b) }()
	return xw.UTF16PtrToString((*uint16)(ptr(b)))
}

func (e uiaEl) className() string {
	var b uintptr
	if failed(vcall(e.p, elClassName, uintptr(unsafe.Pointer(&b)))) || b == 0 {
		return ""
	}
	defer func() { _, _, _ = pSysFreeString.Call(b) }()
	return xw.UTF16PtrToString((*uint16)(ptr(b)))
}

func (e uiaEl) intProp(slot int) int32 {
	var v int32
	vcall(e.p, slot, uintptr(unsafe.Pointer(&v)))
	return v
}

func (e uiaEl) controlType() int { return int(e.intProp(elControlType)) }
func (e uiaEl) offscreen() bool  { return e.intProp(elIsOffscreen) != 0 }

// all returns the elements under e in the scope; the caller releases them.
func (e uiaEl) all(scope int) []uiaEl {
	var arr uintptr
	if failed(vcall(e.p, elFindAll, uintptr(scope), uintptr(e.u.cond), uintptr(unsafe.Pointer(&arr)))) || arr == 0 {
		return nil
	}
	a := ptr(arr)
	defer comRelease(a)
	var n int32
	vcall(a, arrLength, uintptr(unsafe.Pointer(&n)))
	out := make([]uiaEl, 0, n)
	for i := int32(0); i < n; i++ {
		var el uintptr
		if !failed(vcall(a, arrGetElement, uintptr(i), uintptr(unsafe.Pointer(&el)))) && el != 0 {
			out = append(out, uiaEl{e.u, ptr(el)})
		}
	}
	return out
}

func releaseAll(es []uiaEl) {
	for _, e := range es {
		e.release()
	}
}

// dump renders names and control types under e for failure messages.
func (e uiaEl) dump() string {
	var b strings.Builder
	all := e.all(uiaTreeDescendants)
	defer releaseAll(all)
	for _, c := range all {
		fmt.Fprintf(&b, "  type=%d class=%q name=%q offscreen=%v\n", c.controlType(), c.className(), c.name(), c.offscreen())
	}
	return b.String()
}

func uiaWin(t *testing.T, o caps.ChromeOptions, title string) *Window {
	t.Helper()
	needGUI(t)
	w, err := guiApp.NewWindow(WindowConfig{Title: title, Width: 480, Height: 320, Resizable: true, Path: "/", Chrome: o})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	return w
}

func pollUIA(t *testing.T, what string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	// Called from the UI Automation goroutine, which FailNow cannot end.
	t.Errorf("timed out waiting for %s", what)
	runtime.Goexit()
}

func TestGUIUIAWindowAndWebviewInEveryChrome(t *testing.T) {
	for _, c := range []struct {
		name   string
		chrome caps.ChromeOptions
		// caption says whether the caption buttons must be found (native
		// only; for the other styles they are reported, not required).
		caption bool
	}{
		{"native", caps.ChromeOptions{}, true},
		{"hidden", caps.ChromeOptions{TitleBar: caps.TitleBarHidden, Overlay: &caps.TitleBarOverlay{Height: 40}}, false},
		{"none", caps.ChromeOptions{TitleBar: caps.TitleBarNone}, false},
		{"transparent", caps.ChromeOptions{TitleBar: caps.TitleBarNone, Transparent: true}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			title := "UIA " + c.name
			w := uiaWin(t, c.chrome, title)
			withUIA(t, func(u *uiaClient) {
				el, ok := u.fromHandle(w.hwnd)
				if !ok {
					t.Error("UI Automation has no element for the window")
					return
				}
				defer el.release()
				if got := el.name(); got != title {
					t.Errorf("window name %q, want the title %q", got, title)
				}
				if el.controlType() != uiaWindow {
					t.Errorf("window control type %d, want Window %d", el.controlType(), uiaWindow)
				}
				if el.offscreen() {
					t.Error("a normal window is reported offscreen")
				}
				// The WebView2 host: a Document, or the Chromium window classes.
				var host bool
				pollUIA(t, "the WebView2 content in the UI Automation tree", func() bool {
					all := el.all(uiaTreeDescendants)
					defer releaseAll(all)
					for _, d := range all {
						if d.controlType() == uiaDocument || strings.HasPrefix(d.className(), "Chrome_") {
							host = true
						}
					}
					return host
				})
				// Caption buttons: Windows draws them, so they are named
				// (Minimize, Maximize, Close) and focusable by the system.
				var names []string
				for _, b := range el.all(uiaTreeDescendants) {
					if b.controlType() == uiaButton {
						names = append(names, b.name())
					}
					b.release()
				}
				has := func(want string) bool {
					for _, n := range names {
						if strings.EqualFold(n, want) {
							return true
						}
					}
					return false
				}
				if c.caption && !(has("Close") && has("Minimize")) {
					t.Errorf("native caption buttons missing, buttons: %v\n%s", names, el.dump())
				}
				if !c.caption {
					t.Logf("%s: buttons found in the UIA tree: %v", c.name, names)
				}
			})
		})
	}
}
