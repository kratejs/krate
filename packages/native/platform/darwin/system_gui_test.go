//go:build darwin

package darwin

import (
	"os"
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// GUI tests of the OS integration capabilities; they run with KRATE_NATIVE_GUI=1
// in the macos-latest job. A feature needing a signed bundle (SMAppService)
// is checked for a clean answer, not for success.

func dockTile() objc.ID { return guiApp.nsApp.Send(sel("dockTile")) }

func TestGUIBadgeAndProgress(t *testing.T) {
	requireGUI(t)
	read := func() (label string, hasContent bool) {
		runOnMain(func() {
			withPool(func() {
				tile := dockTile()
				label = goString(tile.Send(sel("badgeLabel")))
				hasContent = tile.Send(sel("contentView")) != 0
			})
		})
		return
	}
	if err := guiApp.SetBadge("12"); err != nil {
		t.Fatal(err)
	}
	if l, _ := read(); l != "12" {
		t.Fatalf("badge label = %q", l)
	}
	if err := guiApp.SetBadge(""); err != nil {
		t.Fatal(err)
	}
	if l, _ := read(); l != "" {
		t.Fatalf("badge label after clearing = %q", l)
	}
	for _, p := range []caps.Progress{{State: caps.ProgressNormal, Value: 0.4}, {State: caps.ProgressIndeterminate}, {State: caps.ProgressError, Value: 1}} {
		if err := guiApp.SetProgress(p); err != nil {
			t.Fatal(err)
		}
		if _, has := read(); !has {
			t.Fatalf("no dock tile content after %+v", p)
		}
	}
	if err := guiApp.SetProgress(caps.Progress{State: caps.ProgressNone}); err != nil {
		t.Fatal(err)
	}
	if _, has := read(); has {
		t.Fatal("progress still shown after none")
	}
	if err := guiApp.RequestAttention(true); err != nil {
		t.Fatal(err)
	}
	if err := guiApp.RequestAttention(false); err != nil {
		t.Fatal(err)
	}
}

func TestGUIDisplays(t *testing.T) {
	requireGUI(t)
	ds, err := guiApp.Displays()
	if err != nil {
		t.Fatal(err)
	}
	if !ds[0].Primary || ds[0].ID == "" || ds[0].Width <= 0 || ds[0].Height <= 0 || ds[0].Scale < 1 || ds[0].Name == "" {
		t.Fatalf("primary display %+v", ds[0])
	}
	if ds[0].X != 0 || ds[0].Y != 0 {
		t.Errorf("the primary display sits at the origin, got %d,%d", ds[0].X, ds[0].Y)
	}
	if ds[0].WorkHeight > ds[0].Height || ds[0].WorkY < ds[0].Y {
		t.Errorf("work area %+v is outside the display", ds[0])
	}
	guiApp.OnDisplays(func() {})
}

func TestGUIThemeFollowsOverride(t *testing.T) {
	requireGUI(t)
	if _, err := guiApp.Theme(); err != nil {
		t.Fatal(err)
	}
	effective := func() string {
		var n string
		runOnMain(func() {
			withPool(func() {
				ap := guiApp.nsApp.Send(sel("effectiveAppearance"))
				arr := cls("NSMutableArray").Send(sel("array"))
				arr.Send(sel("addObject:"), nsString("NSAppearanceNameAqua"))
				arr.Send(sel("addObject:"), nsString("NSAppearanceNameDarkAqua"))
				n = goString(ap.Send(sel("bestMatchFromAppearancesWithNames:"), arr))
			})
		})
		return n
	}
	if err := guiApp.SetWindowTheme("dark"); err != nil {
		t.Fatal(err)
	}
	if got := effective(); got != "NSAppearanceNameDarkAqua" {
		t.Errorf("after dark: %q", got)
	}
	if err := guiApp.SetWindowTheme("light"); err != nil {
		t.Fatal(err)
	}
	if got := effective(); got != "NSAppearanceNameAqua" {
		t.Errorf("after light: %q", got)
	}
	if err := guiApp.SetWindowTheme("system"); err != nil {
		t.Fatal(err)
	}
	guiApp.OnTheme(func(caps.ThemeInfo) {})
}

func TestGUIPower(t *testing.T) {
	requireGUI(t)
	s, err := guiApp.PowerSample()
	if err != nil {
		t.Fatal(err)
	}
	if s.IdleSeconds < 0 {
		t.Errorf("idle seconds = %v", s.IdleSeconds)
	}
	guiApp.OnPower(func(caps.PowerEvent) {})
	for _, display := range []bool{false, true} {
		release, err := guiApp.PreventSleep("krate test", display)
		if err != nil {
			t.Fatalf("PreventSleep(display=%v): %v", display, err)
		}
		release()
	}
}

func TestGUILoginItemAnswersCleanly(t *testing.T) {
	requireGUI(t)
	// The test binary is not a signed app bundle, so registering fails; the
	// state must still read back as off and not crash.
	if err := guiApp.SetLoginItem(true); err == nil {
		t.Cleanup(func() { _ = guiApp.SetLoginItem(false) })
	}
	if _, _, err := guiApp.LoginItem(); err != nil {
		t.Fatal(err)
	}
}

// A killed WebContent process raises webViewWebContentProcessDidTerminate:,
// and ReloadView brings the page back.
func TestGUIWebContentCrash(t *testing.T) {
	requireGUI(t)
	w, err := guiApp.NewWindow(WindowOptions{Title: "crash", Width: 400, Height: 300, Resizable: true, Path: "/viewping?tag=crash"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
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
		t.Skipf("cannot find the WebContent process to kill (%v)", notes)
	}
	select {
	case c := <-crashes:
		if c.Reason == "" {
			t.Errorf("crash %+v has no reason", c)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("no crash event")
	}
	if err := guiApp.ReloadView(w.PrimaryView()); err != nil {
		t.Fatal(err)
	}
	waitViewPing(t, "crash")
}
