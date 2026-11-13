//go:build windows

package windows

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Tray, hotkey and toast checks. They run with the other GUI tests under
// KRATE_NATIVE_GUI=1 on a desktop session (GitHub's windows-latest has one).
// Where the desktop cannot show the thing (no taskbar, notifications off) the
// test skips with the reason instead of failing.

var (
	pFindWindowW   = user32.NewProc("FindWindowW")
	pFindWindowExW = user32.NewProc("FindWindowExW")
)

func testPNG(t *testing.T, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// taskbarRunning reports whether the desktop has a visible taskbar with a
// notification area, which Shell_NotifyIcon needs. A bare Shell_TrayWnd is
// not enough: some images have one without a usable tray.
func taskbarRunning() bool {
	h, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(wstr("Shell_TrayWnd"))), 0)
	if h == 0 {
		return false
	}
	if vis, _, _ := pIsWindowVisible.Call(h); vis == 0 {
		return false
	}
	n, _, _ := pFindWindowExW.Call(h, 0, uintptr(unsafe.Pointer(wstr("TrayNotifyWnd"))), 0)
	return n != 0
}

// addTrayOrSkip adds the tray icon. On Windows on ARM runner images the
// shell has a taskbar window yet Shell_NotifyIcon(NIM_ADD) fails; only there
// is that failure a skip, so a runner with a working taskbar still fails.
func addTrayOrSkip(t *testing.T, icon *caps.TrayIcon) {
	t.Helper()
	err := guiApp.SetTray(icon)
	if err == nil {
		return
	}
	if runtime.GOARCH == "arm64" && strings.Contains(err.Error(), "Shell_NotifyIcon failed") {
		t.Skipf("the notification area is not usable on this arm64 runner: %v", err)
	}
	t.Fatalf("add: %v", err)
}

type trayEvents struct {
	mu  sync.Mutex
	evs []caps.TrayEvent
}

func (e *trayEvents) add(ev caps.TrayEvent) { e.mu.Lock(); e.evs = append(e.evs, ev); e.mu.Unlock() }
func (e *trayEvents) kinds() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var k []string
	for _, ev := range e.evs {
		k = append(k, ev.Kind)
	}
	return k
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestGUITrayLifecycle(t *testing.T) {
	needGUI(t)
	if !taskbarRunning() {
		t.Skip("no taskbar (Shell_TrayWnd) on this desktop")
	}
	var evs trayEvents
	guiApp.OnTray(evs.add)
	menu := []caps.MenuItem{{ID: "open", Label: "Open"}, {Kind: caps.MenuItemSeparator}, {ID: "quit", Label: "Quit"}}

	// Add.
	addTrayOrSkip(t, &caps.TrayIcon{Tooltip: "Krate test", PNG: testPNG(t, color.NRGBA{R: 200, A: 255}), Menu: menu})
	// Modify: new tooltip, new image, a tooltip longer than the 127 units
	// the shell holds.
	if err := guiApp.SetTray(&caps.TrayIcon{Tooltip: strings.Repeat("long ", 80), PNG: testPNG(t, color.NRGBA{G: 200, A: 255}), Menu: menu}); err != nil {
		t.Fatalf("modify: %v", err)
	}
	// No image: the stock icon. Title stands in for an empty tooltip.
	if err := guiApp.SetTray(&caps.TrayIcon{Title: "Title only", Menu: menu}); err != nil {
		t.Fatalf("stock icon: %v", err)
	}
	// An invalid image is an error and leaves the icon alone.
	if err := guiApp.SetTray(&caps.TrayIcon{Tooltip: "bad", PNG: []byte("not a png")}); err == nil {
		t.Error("an invalid PNG was accepted")
	}

	// Click events arrive through the window procedure: post the messages
	// the shell would send (version 4 layout).
	post := func(ev uint32) {
		guiApp.Dispatch(func() {
			_, _, _ = pPostMessageW.Call(guiApp.utilWnd, wmTrayCallback, 0, uintptr(ev)|uintptr(trayIconID)<<16)
		})
	}
	post(ninSelect)
	post(wmLButtonDblClk)
	post(0x0202) // WM_LBUTTONUP: ignored, NIN_SELECT already reported the click
	waitFor(t, "click and double-click", func() bool { return len(evs.kinds()) >= 2 })
	if k := evs.kinds(); len(k) != 2 || k[0] != "click" || k[1] != "double-click" {
		t.Errorf("events = %v", k)
	}

	// The taskbar being recreated re-adds the icon.
	guiApp.Dispatch(func() { _, _, _ = pPostMessageW.Call(guiApp.utilWnd, uintptr(guiApp.taskbarMsg), 0, 0) })
	time.Sleep(100 * time.Millisecond)
	if err := guiApp.SetTray(&caps.TrayIcon{Tooltip: "after TaskbarCreated"}); err != nil {
		t.Errorf("modify after re-add: %v", err)
	}

	// Delete, twice.
	if err := guiApp.SetTray(nil); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := guiApp.SetTray(nil); err != nil {
		t.Fatalf("second delete: %v", err)
	}
	// And add again after a delete.
	if err := guiApp.SetTray(&caps.TrayIcon{Tooltip: "again"}); err != nil {
		t.Fatalf("re-add: %v", err)
	}
	_ = guiApp.SetTray(nil)
}
