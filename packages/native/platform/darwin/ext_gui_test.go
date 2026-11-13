//go:build darwin

package darwin

import (
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// GUI tests for the tray, global shortcuts and notifications. They run under
// KRATE_NATIVE_GUI=1 like gui_test.go and need a logged-in session (a status
// bar and a window server), which GitHub's macOS runners have. What needs a
// human (clicking the real status item, pressing the hotkey, seeing the
// banner) is listed in MANUAL_TEST.md.

// menuInfo is the readable state of one NSMenuItem.
type menuInfo struct {
	Title     string
	Enabled   bool
	State     int
	Separator bool
	Sub       []menuInfo
}

// readMenu snapshots an NSMenu (main thread).
func readMenu(m objc.ID) []menuInfo {
	var out []menuInfo
	n := objc.Send[int](m, sel("numberOfItems"))
	for i := 0; i < n; i++ {
		it := m.Send(sel("itemAtIndex:"), i)
		mi := menuInfo{
			Title:     goString(it.Send(sel("title"))),
			Enabled:   objc.Send[bool](it, sel("isEnabled")),
			State:     objc.Send[int](it, sel("state")),
			Separator: objc.Send[bool](it, sel("isSeparatorItem")),
		}
		if mi.Separator {
			mi.Enabled = true // AppKit's answer for separators is not specified
		}
		if sub := it.Send(sel("submenu")); sub != 0 {
			mi.Sub = readMenu(sub)
		}
		out = append(out, mi)
	}
	return out
}

type trayView struct {
	Present  bool
	Title    string
	Tooltip  string
	HasImage bool
	Template bool
	ImageW   float64
	Menu     []menuInfo
}

func readTray() (v trayView) {
	runOnMain(func() {
		withPool(func() {
			if trayState.item == 0 {
				return
			}
			v.Present = true
			b := trayState.item.Send(sel("button"))
			v.Title = goString(b.Send(sel("title")))
			v.Tooltip = goString(b.Send(sel("toolTip")))
			if img := b.Send(sel("image")); img != 0 {
				v.HasImage = true
				v.Template = objc.Send[bool](img, sel("isTemplate"))
				v.ImageW = objc.Send[nsSize](img, sel("size")).Width
			}
			if trayState.menu != 0 {
				v.Menu = readMenu(trayState.menu)
			}
		})
	})
	return
}

type trayRecorder struct {
	mu  sync.Mutex
	evs []caps.TrayEvent
}

func (r *trayRecorder) add(e caps.TrayEvent) { r.mu.Lock(); r.evs = append(r.evs, e); r.mu.Unlock() }
func (r *trayRecorder) take() []caps.TrayEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.evs
	r.evs = nil
	return out
}

func blackIcon(t *testing.T, size int) []byte {
	return encodePNG(t, filled(size, size, color.NRGBA{0, 0, 0, 255}))
}

func trayMenu() []caps.MenuItem {
	return []caps.MenuItem{
		{ID: "open", Label: "&Open", Accelerator: "CmdOrCtrl+O"},
		{ID: "auto", Label: "Autosave", Kind: caps.MenuItemCheck, Checked: true},
		{Kind: caps.MenuItemSeparator},
		{ID: "r1", Label: "Mode A", Kind: caps.MenuItemRadio, Checked: true},
		{ID: "r2", Label: "Mode B", Kind: caps.MenuItemRadio},
		{Label: "More", Kind: caps.MenuItemSubmenu, Submenu: []caps.MenuItem{
			{ID: "deep", Label: "Deep item"},
			{ID: "off", Label: "Disabled item", Disabled: true},
		}},
	}
}

func performItem(m objc.ID, i int) {
	runOnMain(func() { withPool(func() { m.Send(sel("performActionForItemAtIndex:"), i) }) })
}

func TestTrayItemAndMenu(t *testing.T) {
	requireGUI(t)
	var rec trayRecorder
	guiApp.OnTray(rec.add)
	t.Cleanup(func() { _ = guiApp.SetTray(nil) })

	if err := guiApp.SetTray(&caps.TrayIcon{Tooltip: "Krate test", Title: "KT", PNG: blackIcon(t, 36), Menu: trayMenu()}); err != nil {
		t.Fatal(err)
	}
	v := readTray()
	if !v.Present || v.Title != "KT" || v.Tooltip != "Krate test" || !v.HasImage {
		t.Fatalf("tray = %+v", v)
	}
	if !v.Template {
		t.Error("a black icon should be installed as a template image")
	}
	if v.ImageW != 18 {
		t.Errorf("36 px icon should be 18 pt wide, got %v", v.ImageW)
	}
	want := []menuInfo{
		{Title: "Open", Enabled: true},
		{Title: "Autosave", Enabled: true, State: nsControlStateOn},
		{Separator: true, Enabled: true},
		{Title: "Mode A", Enabled: true, State: nsControlStateOn},
		{Title: "Mode B", Enabled: true},
		{Title: "More", Enabled: true, Sub: []menuInfo{
			{Title: "Deep item", Enabled: true},
			{Title: "Disabled item", Enabled: false},
		}},
	}
	if fmt.Sprintf("%+v", v.Menu) != fmt.Sprintf("%+v", want) {
		t.Errorf("menu:\n got %+v\nwant %+v", v.Menu, want)
	}

	// Drive the real menu actions: item events, check toggling, radio group.
	menu := trayState.menu
	performItem(menu, 0)
	performItem(menu, 1)
	performItem(menu, 4)
	evs := rec.take()
	wantEvs := []caps.TrayEvent{{Kind: "menu", ID: "open"}, {Kind: "menu", ID: "auto"}, {Kind: "menu", ID: "r2"}}
	if fmt.Sprint(evs) != fmt.Sprint(wantEvs) {
		t.Fatalf("events = %+v, want %+v", evs, wantEvs)
	}
	v = readTray()
	if v.Menu[1].State != nsControlStateOff {
		t.Error("check item should have toggled off")
	}
	if v.Menu[3].State != nsControlStateOff || v.Menu[4].State != nsControlStateOn {
		t.Errorf("radio group: A=%d B=%d", v.Menu[3].State, v.Menu[4].State)
	}

	// An update replaces the menu and keeps one item.
	if err := guiApp.SetTray(&caps.TrayIcon{Tooltip: "second", PNG: encodePNG(t, filled(16, 16, color.NRGBA{200, 30, 30, 255})),
		Menu: []caps.MenuItem{{ID: "only", Label: "Only"}}}); err != nil {
		t.Fatal(err)
	}
	v = readTray()
	if v.Tooltip != "second" || v.Title != "" || v.Template || len(v.Menu) != 1 {
		t.Errorf("after update: %+v", v)
	}
	rec.take()
	performItem(trayState.menu, 0)
	if evs := rec.take(); len(evs) != 1 || evs[0].ID != "only" {
		t.Errorf("events after update = %+v", evs)
	}

	// Without a menu the button action reports a click.
	if err := guiApp.SetTray(&caps.TrayIcon{Title: "no menu"}); err != nil {
		t.Fatal(err)
	}
	rec.take()
	runOnMain(func() { withPool(func() { trayState.item.Send(sel("button")).Send(sel("performClick:"), objc.ID(0)) }) })
	if evs := rec.take(); len(evs) != 1 || evs[0].Kind != "click" {
		t.Errorf("click events = %+v", evs)
	}

	if err := guiApp.SetTray(nil); err != nil {
		t.Fatal(err)
	}
	if readTray().Present {
		t.Error("SetTray(nil) left the item")
	}
	if err := guiApp.SetTray(nil); err != nil {
		t.Errorf("removing twice: %v", err)
	}
}

func TestTrayRejectsBadInput(t *testing.T) {
	requireGUI(t)
	t.Cleanup(func() { _ = guiApp.SetTray(nil) })
	if err := guiApp.SetTray(&caps.TrayIcon{PNG: []byte("nope")}); err == nil || !strings.Contains(err.Error(), "PNG") {
		t.Errorf("bad PNG: %v", err)
	}
	if err := guiApp.SetTray(&caps.TrayIcon{}); err == nil {
		t.Error("an icon with no image and no title must be an error")
	}
	if readTray().Present {
		t.Error("a failed SetTray created an item")
	}
}

func TestHotKeyRegisterUnregister(t *testing.T) {
	requireGUI(t)
	guiApp.OnShortcut(func(string) {})
	// An unlikely combination so it cannot clash with the runner's own keys.
	acc := caps.Accelerator{Ctrl: true, Alt: true, Shift: true, Super: true, Key: "F19"}
	if err := guiApp.RegisterShortcut("hk1", acc); err != nil {
		t.Fatalf("register: %v", err)
	}
	err := guiApp.RegisterShortcut("hk2", acc)
	if !errors.Is(err, errShortcutTaken) || !strings.Contains(err.Error(), "already") {
		t.Errorf("duplicate combination: %v", err)
	}
	if err := guiApp.RegisterShortcut("hk1", caps.Accelerator{Ctrl: true, Alt: true, Shift: true, Super: true, Key: "F18"}); err == nil {
		t.Error("duplicate handle accepted")
	}
	if err := guiApp.RegisterShortcut("hk3", caps.Accelerator{Super: true, Key: "NoSuchKey"}); err == nil {
		t.Error("unknown key accepted")
	}
	if err := guiApp.UnregisterShortcut("hk1"); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	// The combination is free again.
	if err := guiApp.RegisterShortcut("hk2", acc); err != nil {
		t.Fatalf("register after unregister: %v", err)
	}
	if err := guiApp.UnregisterShortcut("hk2"); err != nil {
		t.Fatal(err)
	}
	if err := guiApp.UnregisterShortcut("never-registered"); err != nil {
		t.Errorf("unknown handle: %v", err)
	}
}

func TestNotifyUnbundled(t *testing.T) {
	requireGUI(t)
	if os.Getenv("KRATE_NOTIFY_CHILD") == "1" {
		t.Skip("runs in the unbundled parent only")
	}
	err := guiApp.Notify(caps.Notification{ID: "n1", Title: "hello"})
	if !errors.Is(err, krate.ErrUnsupported) {
		t.Fatalf("Notify error = %v, want ErrUnsupported", err)
	}
	for _, want := range []string{"app bundle", "krate native dev"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if guiApp.NotifyAvailability() == nil {
		t.Error("NotifyAvailability must report the unbundled state")
	}
	if err := guiApp.CloseNotification("n1"); !errors.Is(err, krate.ErrUnsupported) {
		t.Errorf("CloseNotification error = %v", err)
	}
}

// TestBundledNotifyChild runs inside the generated .app (see
// TestNotifyBundled) and prints one RESULT line.
func TestBundledNotifyChild(t *testing.T) {
	requireGUI(t)
	if os.Getenv("KRATE_NOTIFY_CHILD") != "1" {
		t.Skip("only runs inside the generated bundle")
	}
	notifyAuthTimeout = 10 * time.Second
	id, path, err := guiApp.mainBundle()
	if err != nil {
		t.Fatal(err)
	}
	avail := guiApp.NotifyAvailability()
	nerr := guiApp.Notify(caps.Notification{ID: "child", Title: "Krate bundled test", Body: "body",
		Actions: []caps.NotificationAction{{ID: "a", Label: "Do it"}}})
	fmt.Printf("RESULT bundle=%s path=%s available=%v notify=%v\n", id, path, avail == nil, nerr)
	if avail != nil {
		t.Errorf("bundled process reports unavailable: %v", avail)
	}
	if nerr != nil && strings.Contains(nerr.Error(), "app bundle") {
		t.Errorf("Notify still reports an unbundled process: %v", nerr)
	}
}

// TestNotifyBundled copies this test binary into a minimal .app and runs the
// child test there: the process then has a bundle identifier, so the
// bundled path is taken. Whether the notification is allowed depends on the
// runner (the permission prompt cannot be answered); the test asserts that
// the bundle is recognized and Notify gets past the bundle check without
// crashing or hanging, and accepts success or a clear refusal.
func TestNotifyBundled(t *testing.T) {
	requireGUI(t)
	if os.Getenv("KRATE_NOTIFY_CHILD") == "1" {
		t.Skip("runs in the unbundled parent only")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	const bundleID = "dev.krate.guitest.notify"
	app := filepath.Join(t.TempDir(), "KrateNotifyTest.app")
	exe := filepath.Join(app, "Contents", "MacOS", "KrateNotifyTest")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>KrateNotifyTest</string>
<key>CFBundleIdentifier</key><string>` + bundleID + `</string>
<key>CFBundleName</key><string>KrateNotifyTest</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleVersion</key><string>1</string>
<key>CFBundleShortVersionString</key><string>1.0</string>
</dict></plist>
`
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	dst, err := os.OpenFile(exe, os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	src.Close()
	dst.Close()
	// Best effort: a fresh ad hoc signature keeps arm64 happy if the copy
	// lost its own.
	if cs, err := exec.LookPath("codesign"); err == nil {
		_ = exec.Command(cs, "--force", "--sign", "-", app).Run()
	}

	cmd := exec.Command(exe, "-test.run", "^TestBundledNotifyChild$", "-test.v", "-test.timeout", "90s")
	cmd.Env = append(os.Environ(), "KRATE_NOTIFY_CHILD=1", "KRATE_NATIVE_GUI=1")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("child failed: %v\n%s", err, out.String())
		}
	case <-time.After(2 * time.Minute):
		_ = cmd.Process.Kill()
		t.Fatalf("child did not finish\n%s", out.String())
	}
	if !strings.Contains(out.String(), "RESULT bundle="+bundleID) || !strings.Contains(out.String(), "available=true") {
		t.Errorf("child did not see the bundle:\n%s", out.String())
	}
	t.Logf("child: %s", lastLine(out.String(), "RESULT"))
}

func lastLine(s, prefix string) string {
	var found string
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, prefix) {
			found = l
		}
	}
	return found
}
