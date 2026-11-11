//go:build windows

package windows

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
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
	pFindWindowW = user32.NewProc("FindWindowW")
	pKeybdEvent  = user32.NewProc("keybd_event")
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

func taskbarRunning() bool {
	h, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(wstr("Shell_TrayWnd"))), 0)
	return h != 0
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
	if err := guiApp.SetTray(&caps.TrayIcon{Tooltip: "Krate test", PNG: testPNG(t, color.NRGBA{R: 200, A: 255}), Menu: menu}); err != nil {
		t.Fatalf("add: %v", err)
	}
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

type hotEvents struct {
	mu sync.Mutex
	hs []string
}

func (e *hotEvents) add(h string) { e.mu.Lock(); e.hs = append(e.hs, h); e.mu.Unlock() }
func (e *hotEvents) n() int       { e.mu.Lock(); defer e.mu.Unlock(); return len(e.hs) }

func sendKeys(vks ...byte) {
	for _, vk := range vks {
		_, _, _ = pKeybdEvent.Call(uintptr(vk), 0, 0, 0)
	}
	for i := len(vks) - 1; i >= 0; i-- {
		_, _, _ = pKeybdEvent.Call(uintptr(vks[i]), 0, 2, 0) // KEYEVENTF_KEYUP
	}
}

func TestGUIHotkeys(t *testing.T) {
	needGUI(t)
	var evs hotEvents
	guiApp.OnShortcut(evs.add)
	acc := caps.Accelerator{Ctrl: true, Alt: true, Shift: true, Key: "F9"}

	if err := guiApp.RegisterShortcut("hk1", acc); err != nil {
		t.Fatalf("register: %v", err)
	}
	defer func() { _ = guiApp.UnregisterShortcut("hk1") }()

	// The same combination again is refused and the error says why.
	err := guiApp.RegisterShortcut("hk2", acc)
	if err == nil || !strings.Contains(err.Error(), "already registered by another app") {
		t.Errorf("duplicate combination: %v", err)
	}
	// The same handle again is refused.
	if err := guiApp.RegisterShortcut("hk1", caps.Accelerator{Ctrl: true, Alt: true, Shift: true, Key: "F10"}); err == nil {
		t.Error("duplicate handle accepted")
	}
	// An unknown key never reaches Windows.
	if err := guiApp.RegisterShortcut("hk3", caps.Accelerator{Ctrl: true, Key: "NoSuchKey"}); err == nil {
		t.Error("unknown key accepted")
	}

	// A WM_HOTKEY for the registered id reaches the handler by handle.
	var id int32
	guiApp.callMain(func() { id = guiApp.hot.byName["hk1"] })
	guiApp.Dispatch(func() { _, _, _ = pPostMessageW.Call(guiApp.utilWnd, wmHotkey, uintptr(id), 0) })
	waitFor(t, "WM_HOTKEY", func() bool { return evs.n() >= 1 })
	if evs.hs[0] != "hk1" {
		t.Errorf("handle = %q", evs.hs[0])
	}

	// Best effort: press the real keys. A locked or non-interactive desktop
	// drops injected input, so this only logs.
	before := evs.n()
	sendKeys(0x11, 0x12, 0x10, 0x78)
	for i := 0; i < 100 && evs.n() == before; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if evs.n() == before {
		t.Log("injected key presses did not fire the hotkey (desktop not interactive?)")
	} else {
		t.Log("injected key presses fired the hotkey")
	}

	// Release, then the combination is free again.
	if err := guiApp.UnregisterShortcut("hk1"); err != nil {
		t.Fatal(err)
	}
	if err := guiApp.UnregisterShortcut("hk1"); err != nil {
		t.Errorf("second unregister: %v", err)
	}
	if err := guiApp.RegisterShortcut("hk4", acc); err != nil {
		t.Fatalf("register after release: %v", err)
	}
	if err := guiApp.UnregisterShortcut("hk4"); err != nil {
		t.Fatal(err)
	}
	// A press for a released id is ignored.
	n := evs.n()
	guiApp.Dispatch(func() { _, _, _ = pPostMessageW.Call(guiApp.utilWnd, wmHotkey, uintptr(id), 0) })
	time.Sleep(100 * time.Millisecond)
	if evs.n() != n {
		t.Error("a released hotkey still fired")
	}
}

type notifyEvents struct {
	mu  sync.Mutex
	evs []caps.NotifyEvent
}

func (e *notifyEvents) add(ev caps.NotifyEvent) {
	e.mu.Lock()
	e.evs = append(e.evs, ev)
	e.mu.Unlock()
}
func (e *notifyEvents) snapshot() []caps.NotifyEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]caps.NotifyEvent(nil), e.evs...)
}

func TestGUIToast(t *testing.T) {
	needGUI(t)
	var evs notifyEvents
	guiApp.OnNotify(evs.add)
	aumid := aumidFor(guiApp.cfg.AppID)
	if !isPackaged() {
		defer deleteAUMID(aumid)
	}

	n := caps.Notification{
		ID: "t1", Title: "Krate toast test", Body: "Shown by the GUI test <&>",
		Actions: []caps.NotificationAction{{ID: "ok", Label: "OK"}, {ID: "later", Label: "Later"}},
	}
	err := guiApp.Notify(n)
	if err != nil && strings.Contains(err.Error(), "turned off") {
		t.Skipf("notifications are disabled on this machine: %v", err)
	}
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}

	// The checkable signals: Show succeeded, the app is registered under its
	// AppUserModelID with its name, the toast is tracked and no Failed event
	// came back. (Reading ToastNotificationHistory would add little over
	// Show's HRESULT.)
	if !isPackaged() {
		if got := readAUMIDName(aumid); got != "Krate GUI Test" {
			t.Errorf("registered display name = %q", got)
		}
	}
	var tracked bool
	guiApp.callMain(func() { tracked = guiApp.toast.live["t1"] != nil })
	if !tracked {
		t.Error("the toast is not tracked after Show")
	}
	time.Sleep(1500 * time.Millisecond)
	if n := toastFailures.Load(); n != 0 {
		t.Errorf("%d toast(s) reported a Failed event", n)
	}
	for _, ev := range evs.snapshot() {
		if ev.ID == "t1" && ev.Closed {
			// A runner without a foreground session or with Focus Assist on
			// has Windows dismiss the toast at once; that is not a failure.
			t.Logf("the toast was dismissed right away (reason %d): %+v", lastDismissReason.Load(), ev)
		}
	}

	// Same id replaces; a second toast works; close removes.
	n.Body = "replaced"
	if err := guiApp.Notify(n); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if err := guiApp.Notify(caps.Notification{ID: "t2", Title: "second", Silent: true, TimeoutMS: 20000}); err != nil {
		t.Fatalf("second: %v", err)
	}
	if err := guiApp.CloseNotification("t1"); err != nil {
		t.Errorf("close: %v", err)
	}
	if err := guiApp.CloseNotification("t1"); err != nil {
		t.Errorf("closing twice: %v", err)
	}
	if err := guiApp.CloseNotification("nope"); err != nil {
		t.Errorf("closing an unknown id: %v", err)
	}
	_ = guiApp.CloseNotification("t2")
	guiApp.callMain(func() {
		if len(guiApp.toast.live) != 0 {
			// Dismissed handlers remove entries asynchronously.
			t.Logf("tracked toasts left: %d", len(guiApp.toast.live))
		}
	})
}
