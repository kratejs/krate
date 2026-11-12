//go:build windows

package windows

import (
	"os"
	"testing"
	"time"
	"unsafe"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// GUI tests of the OS integration capabilities; they run with the other GUI
// tests under KRATE_NATIVE_GUI=1 on windows-latest. System broadcasts are
// delivered the real way, with SendMessage to the utility window, so the
// window procedure wiring is covered.

var pSendMessageW = user32.NewProc("SendMessageW")

func send(hwnd uintptr, msg, wParam, lParam uintptr) uintptr {
	r, _, _ := pSendMessageW.Call(hwnd, msg, wParam, lParam)
	return r
}

func TestGUIBadgeAndProgress(t *testing.T) {
	needGUI(t)
	w, err := guiApp.NewWindow(WindowConfig{Title: "badge", Width: 300, Height: 200, Resizable: true, Path: "/viewping?tag=badge"})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	waitViewPing(t, "badge")

	var haveTaskbar bool
	guiApp.callMain(func() { haveTaskbar = taskbarList() != nil })
	if !haveTaskbar {
		t.Skip("this desktop has no taskbar (ITaskbarList3 is unavailable)")
	}
	if err := guiApp.SetBadge("7"); err != nil {
		t.Fatal(err)
	}
	sysSt.mu.Lock()
	icon := sysSt.overlay
	sysSt.mu.Unlock()
	if icon == 0 {
		t.Fatal("no overlay icon was created for the badge")
	}
	if err := guiApp.SetBadge("120"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []caps.Progress{
		{State: caps.ProgressNormal, Value: 0.5}, {State: caps.ProgressPaused, Value: 0.5},
		{State: caps.ProgressError, Value: 1}, {State: caps.ProgressIndeterminate}, {State: caps.ProgressNone},
	} {
		if err := guiApp.SetProgress(p); err != nil {
			t.Fatalf("%+v: %v", p, err)
		}
	}
	if err := guiApp.SetBadge(""); err != nil {
		t.Fatal(err)
	}
	sysSt.mu.Lock()
	icon = sysSt.overlay
	sysSt.mu.Unlock()
	if icon != 0 {
		t.Fatal("the overlay icon is still held after clearing")
	}
	if err := guiApp.RequestAttention(true); err != nil {
		t.Fatal(err)
	}
	if err := guiApp.RequestAttention(false); err != nil {
		t.Fatal(err)
	}
}

func TestGUIDisplays(t *testing.T) {
	needGUI(t)
	ds, err := guiApp.Displays()
	if err != nil {
		t.Fatal(err)
	}
	d := ds[0]
	if !d.Primary || d.ID == "" || d.Width <= 0 || d.Height <= 0 || d.Scale < 1 || d.WorkWidth <= 0 {
		t.Fatalf("primary display %+v", d)
	}
	if d.X != 0 || d.Y != 0 {
		t.Errorf("the primary display sits at the origin, got %d,%d", d.X, d.Y)
	}
	if d.WorkWidth > d.Width || d.WorkHeight > d.Height {
		t.Errorf("work area %+v is larger than the display", d)
	}
	// WM_DISPLAYCHANGE reaches the handler through the window procedure.
	got := make(chan struct{}, 1)
	guiApp.OnDisplays(func() { got <- struct{}{} })
	t.Cleanup(func() { guiApp.OnDisplays(nil) })
	send(guiApp.utilWnd, wmDisplayChange, 32, 0)
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("WM_DISPLAYCHANGE did not reach the handler")
	}
}

func TestGUIPowerMessages(t *testing.T) {
	needGUI(t)
	s, err := guiApp.PowerSample()
	if err != nil {
		t.Fatal(err)
	}
	if s.IdleSeconds < 0 {
		t.Errorf("idle seconds = %v", s.IdleSeconds)
	}
	events := make(chan caps.PowerEventKind, 8)
	guiApp.OnPower(func(e caps.PowerEvent) { events <- e.Kind })
	t.Cleanup(func() { guiApp.OnPower(nil) })
	want := []struct {
		msg, wParam uintptr
		kind        caps.PowerEventKind
	}{
		{wmPowerBroadcast, pbtAPMSuspend, caps.PowerSuspend},
		{wmPowerBroadcast, pbtAPMResumeAuto, caps.PowerResume},
		{wmWTSSessionChange, wtsSessionLock, caps.PowerLock},
		{wmWTSSessionChange, wtsSessionUnlock, caps.PowerUnlock},
	}
	for _, w := range want {
		send(guiApp.utilWnd, w.msg, w.wParam, 0)
		select {
		case k := <-events:
			if k != w.kind {
				t.Errorf("got %s, want %s", k, w.kind)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no %s event", w.kind)
		}
	}
	for _, display := range []bool{false, true} {
		release, err := guiApp.PreventSleep("krate test", display)
		if err != nil {
			t.Fatal(err)
		}
		release()
		release()
	}
	sysSt.mu.Lock()
	n := sysSt.sysBlockers + sysSt.dispBlockers
	sysSt.mu.Unlock()
	if n != 0 {
		t.Errorf("%d blockers left after release", n)
	}
}

func TestGUIThemeFollowsSettings(t *testing.T) {
	needGUI(t)
	info, err := guiApp.Theme()
	if err != nil {
		t.Fatal(err)
	}
	if info != systemTheme() {
		t.Errorf("Theme = %+v, registry says %+v", info, systemTheme())
	}
	changed := make(chan caps.ThemeInfo, 2)
	guiApp.OnTheme(func(i caps.ThemeInfo) { changed <- i })
	t.Cleanup(func() { guiApp.OnTheme(nil) })
	area := wstr("ImmersiveColorSet")
	send(guiApp.utilWnd, wmSettingChange, 0, uintptr(unsafe.Pointer(area)))
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("WM_SETTINGCHANGE did not reach the handler")
	}
	// Another setting is not a theme change.
	other := wstr("intl")
	send(guiApp.utilWnd, wmSettingChange, 0, uintptr(unsafe.Pointer(other)))
	select {
	case <-changed:
		t.Error("an unrelated setting was reported as a theme change")
	case <-time.After(300 * time.Millisecond):
	}

	w, err := guiApp.NewWindow(WindowConfig{Title: "theme", Width: 300, Height: 200, Resizable: true, Path: "/viewping?tag=theme"})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	waitViewPing(t, "theme")
	for _, mode := range []string{"dark", "light", "system"} {
		if err := guiApp.SetWindowTheme(mode); err != nil {
			t.Fatal(err)
		}
		sysSt.mu.Lock()
		got := sysSt.winTheme
		sysSt.mu.Unlock()
		if got != mode {
			t.Errorf("window theme = %q, want %q", got, mode)
		}
	}
}

// A killed WebView2 render process raises ProcessFailed; ReloadView recovers.
func TestGUIWebviewCrash(t *testing.T) {
	needGUI(t)
	w, err := guiApp.NewWindow(WindowConfig{Title: "crash", Width: 400, Height: 300, Resizable: true, Path: "/viewping?tag=crash"})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	waitViewPing(t, "crash")
	crashes := make(chan caps.ViewCrash, 4)
	guiApp.OnViewCrash(func(c caps.ViewCrash) { crashes <- c })
	t.Cleanup(func() { guiApp.OnViewCrash(nil) })

	procs, notes := guiApp.WebviewProcesses()
	killed := 0
	for _, p := range procs {
		if p.Role == "webview:web" {
			if proc, err := os.FindProcess(p.PID); err == nil && proc.Kill() == nil {
				killed++
			}
		}
	}
	if killed == 0 {
		t.Skipf("cannot find the render process to kill (%v)", notes)
	}
	select {
	case c := <-crashes:
		if c.Reason == "" || c.View != w.PrimaryView() {
			t.Errorf("crash %+v (primary view %d)", c, w.PrimaryView())
		}
	case <-time.After(20 * time.Second):
		t.Fatal("no ProcessFailed event")
	}
	if err := guiApp.ReloadView(w.PrimaryView()); err != nil {
		t.Fatal(err)
	}
	waitViewPing(t, "crash")
}
