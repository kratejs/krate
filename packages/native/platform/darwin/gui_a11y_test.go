//go:build darwin

package darwin

import (
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Accessibility GUI tests. The first group asks our own AppKit objects through
// the NSAccessibility protocol (accessibilityRole, accessibilityLabel, ...),
// which needs no permission and so runs on a CI runner. The second group walks
// the tree the way VoiceOver does, through the AX API, and needs the test
// process to be trusted for accessibility; without that it skips with the
// reason. Both run under KRATE_NATIVE_GUI=1 in the CI native job.

func axRole(obj objc.ID) string  { return goString(obj.Send(sel("accessibilityRole"))) }
func axLabel(obj objc.ID) string { return goString(obj.Send(sel("accessibilityLabel"))) }
func axHidden(obj objc.ID) bool  { return objc.Send[bool](obj, sel("isAccessibilityHidden")) }

func TestGUIAccessibilityWindowIsNamedInEveryChrome(t *testing.T) {
	for _, c := range []struct {
		name   string
		chrome caps.ChromeOptions
		// lights says whether the traffic lights must be there.
		lights bool
	}{
		{"native", caps.ChromeOptions{}, true},
		{"hidden", caps.ChromeOptions{TitleBar: caps.TitleBarHidden}, true},
		{"none", caps.ChromeOptions{TitleBar: caps.TitleBarNone}, false},
		{"transparent", caps.ChromeOptions{TitleBar: caps.TitleBarNone, Transparent: true}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := chromeWin(t, c.chrome)
			var title, role string
			var hidden, webHidden bool
			var closeRole, closeLabel string
			var closeShown bool
			var webLabelOrRole string
			onMain(func() {
				title = goString(w.win.Send(sel("title")))
				role = axRole(w.win)
				hidden = axHidden(w.win)
				webHidden = axHidden(w.pv.web)
				webLabelOrRole = axRole(w.pv.web)
				b := w.win.Send(sel("standardWindowButton:"), nsWindowCloseButton)
				if b != 0 {
					closeShown = !objc.Send[bool](b, sel("isHidden"))
					closeRole = axRole(b)
					closeLabel = axLabel(b)
				}
			})
			if title != t.Name() {
				t.Errorf("window title %q, want %q: the title names the window for VoiceOver", title, t.Name())
			}
			if role != "AXWindow" {
				t.Errorf("window role %q, want AXWindow", role)
			}
			if hidden || webHidden {
				t.Errorf("window hidden=%v webview hidden=%v: the webview must be reachable", hidden, webHidden)
			}
			if webLabelOrRole == "" {
				t.Error("the webview has no accessibility role")
			}
			if c.lights {
				if !closeShown || closeRole != "AXButton" {
					t.Errorf("close traffic light shown=%v role=%q, want a visible AXButton", closeShown, closeRole)
				}
				if closeLabel == "" {
					t.Error("the close traffic light has no accessibility label")
				}
			} else if closeShown {
				t.Error("a window with no title bar shows a traffic light")
			}
		})
	}
}

func TestGUIAccessibilityOffscreenWindowIsHidden(t *testing.T) {
	requireGUI(t)
	w, err := guiApp.NewWindow(WindowOptions{Title: "a11y offscreen", Width: 320, Height: 200, Path: "/blank", Offscreen: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	var winHidden, contentHidden bool
	onMain(func() {
		winHidden = axHidden(w.win)
		contentHidden = axHidden(w.win.Send(sel("contentView")))
	})
	if !winHidden || !contentHidden {
		t.Errorf("offscreen window hidden=%v content hidden=%v, want both hidden from assistive technology", winHidden, contentHidden)
	}
}

func TestGUIAccessibilityMenuBar(t *testing.T) {
	requireGUI(t)
	err := guiApp.SetAppMenu([]caps.MenuItem{{Label: "&Tools", Kind: caps.MenuItemSubmenu, Submenu: []caps.MenuItem{
		{ID: "run", Label: "&Run", Accelerator: "CmdOrCtrl+R"},
		{ID: "off", Label: "Off", Disabled: true},
		{Kind: caps.MenuItemSeparator},
		{ID: "wrap", Label: "Wrap", Kind: caps.MenuItemCheck, Checked: true},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guiApp.SetAppMenu(nil) })
	var top string
	var runRole, runTitle, runKey string
	var offEnabled bool
	var wrapState int
	var seps int
	onMain(func() {
		bar := guiApp.nsApp.Send(sel("mainMenu"))
		n := objc.Send[int](bar, sel("numberOfItems"))
		for i := 0; i < n; i++ {
			it := bar.Send(sel("itemAtIndex:"), i)
			if sub := it.Send(sel("submenu")); sub != 0 && goString(sub.Send(sel("title"))) == "Tools" {
				top = goString(it.Send(sel("title")))
				m := n2menu(sub)
				for j := 0; j < m; j++ {
					mi := sub.Send(sel("itemAtIndex:"), j)
					switch goString(mi.Send(sel("title"))) {
					case "Run":
						runRole = axRole(mi)
						runTitle = goString(mi.Send(sel("title")))
						runKey = goString(mi.Send(sel("keyEquivalent")))
					case "Off":
						offEnabled = objc.Send[bool](mi, sel("isEnabled"))
					case "Wrap":
						wrapState = objc.Send[int](mi, sel("state"))
					case "":
						if objc.Send[bool](mi, sel("isSeparatorItem")) {
							seps++
						}
					}
				}
			}
		}
	})
	if top != "Tools" {
		t.Errorf("the menu bar item is titled %q, want Tools (an empty title leaves the menu unnamed)", top)
	}
	if runTitle != "Run" || runRole != "AXMenuItem" {
		t.Errorf("Run item title=%q role=%q", runTitle, runRole)
	}
	if runKey != "r" {
		t.Errorf("Run key equivalent %q, want r", runKey)
	}
	if offEnabled {
		t.Error("the disabled item is enabled")
	}
	if wrapState != nsControlStateOn {
		t.Errorf("checked item state %d", wrapState)
	}
	if seps != 1 {
		t.Errorf("%d separators, want 1", seps)
	}
}

func n2menu(menu objc.ID) int { return objc.Send[int](menu, sel("numberOfItems")) }

func TestGUIAccessibilityTrayButtonIsNamed(t *testing.T) {
	requireGUI(t)
	for _, c := range []struct {
		name string
		icon caps.TrayIcon
		want string
	}{
		{"tooltip", caps.TrayIcon{Tooltip: "3 unread"}, "3 unread"},
		{"title", caps.TrayIcon{Title: "Mail", Tooltip: "3 unread"}, "Mail"},
		{"neither", caps.TrayIcon{}, caps.AppDisplayName()},
	} {
		ic := c.icon
		ic.Menu = []caps.MenuItem{{ID: "quit", Label: "Quit"}}
		if err := guiApp.SetTray(&ic); err != nil {
			t.Skipf("no status bar here: %v", err)
		}
		var label string
		onMain(func() {
			label = axLabel(trayState.item.Send(sel("button")))
		})
		if label != c.want {
			t.Errorf("%s: status button label %q, want %q", c.name, label, c.want)
		}
	}
	_ = guiApp.SetTray(nil)
}

func TestGUIAccessibilitySplitViewRole(t *testing.T) {
	w := chromeWin(t, caps.ChromeOptions{})
	if _, err := w.AddView(caps.ViewOptions{Path: "/blank", Beside: w.PrimaryView(), Axis: caps.Horizontal}); err != nil {
		t.Fatal(err)
	}
	var role string
	var hidden bool
	onMain(func() {
		cv := w.win.Send(sel("contentView"))
		// The split view is the content view or its only subview.
		sv := cv
		if !objc.Send[bool](sv, sel("isKindOfClass:"), cls("NSSplitView")) {
			subs := cv.Send(sel("subviews"))
			if objc.Send[int](subs, sel("count")) > 0 {
				sv = subs.Send(sel("objectAtIndex:"), 0)
			}
		}
		role = axRole(sv)
		hidden = axHidden(sv)
	})
	if role != "AXSplitGroup" {
		t.Errorf("split view role %q, want AXSplitGroup", role)
	}
	if hidden {
		t.Error("the split view is hidden from assistive technology")
	}
}

// --- AX API (VoiceOver's view of the process) ---

var (
	axOnce sync.Once
	axErr  error

	axIsProcessTrusted   func() uint8
	axCreateApp          func(pid int32) uintptr
	axCopyAttribute      func(el, attr uintptr, out *uintptr) int32
	cfStringCreate       func(alloc uintptr, s string, enc uint32) uintptr
	cfStringGetCString   func(s uintptr, buf *byte, size int, enc uint32) uint8
	cfArrayGetCount      func(a uintptr) int
	cfArrayGetValueAtIdx func(a uintptr, i int) uintptr
	cfRelease            func(o uintptr)
	cfGetTypeID          func(o uintptr) uint
	cfStringGetTypeID    func() uint
	cfArrayGetTypeID     func() uint
)

const cfUTF8 = 0x08000100

func loadAX() error {
	axOnce.Do(func() {
		as, err := purego.Dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			axErr = err
			return
		}
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			axErr = err
			return
		}
		purego.RegisterLibFunc(&axIsProcessTrusted, as, "AXIsProcessTrusted")
		purego.RegisterLibFunc(&axCreateApp, as, "AXUIElementCreateApplication")
		purego.RegisterLibFunc(&axCopyAttribute, as, "AXUIElementCopyAttributeValue")
		purego.RegisterLibFunc(&cfStringCreate, cf, "CFStringCreateWithCString")
		purego.RegisterLibFunc(&cfStringGetCString, cf, "CFStringGetCString")
		purego.RegisterLibFunc(&cfArrayGetCount, cf, "CFArrayGetCount")
		purego.RegisterLibFunc(&cfArrayGetValueAtIdx, cf, "CFArrayGetValueAtIndex")
		purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
		purego.RegisterLibFunc(&cfGetTypeID, cf, "CFGetTypeID")
		purego.RegisterLibFunc(&cfStringGetTypeID, cf, "CFStringGetTypeID")
		purego.RegisterLibFunc(&cfArrayGetTypeID, cf, "CFArrayGetTypeID")
	})
	return axErr
}

// axValue copies an attribute of an AX element; the caller releases it.
func axValue(el uintptr, attr string) uintptr {
	key := cfStringCreate(0, attr, cfUTF8)
	defer cfRelease(key)
	var out uintptr
	if axCopyAttribute(el, key, &out) != 0 {
		return 0
	}
	return out
}

func axText(el uintptr, attr string) string {
	v := axValue(el, attr)
	if v == 0 {
		return ""
	}
	defer cfRelease(v)
	if cfGetTypeID(v) != cfStringGetTypeID() {
		return ""
	}
	buf := make([]byte, 1024)
	if cfStringGetCString(v, &buf[0], len(buf), cfUTF8) == 0 {
		return ""
	}
	return strings.TrimRight(string(buf), "\x00")
}

// axKids lists the elements an array-valued attribute holds. They stay valid
// while the array is alive, so the array is leaked on purpose (a test).
func axKids(el uintptr, attr string) []uintptr {
	v := axValue(el, attr)
	if v == 0 || cfGetTypeID(v) != cfArrayGetTypeID() {
		return nil
	}
	n := cfArrayGetCount(v)
	out := make([]uintptr, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, cfArrayGetValueAtIdx(v, i))
	}
	return out
}

func axApp(t *testing.T) uintptr {
	t.Helper()
	requireGUI(t)
	if err := loadAX(); err != nil {
		t.Skipf("the AX API is not available: %v", err)
	}
	if axIsProcessTrusted() == 0 {
		t.Skip("this process is not trusted for accessibility (System Settings > Privacy & Security > Accessibility); the in-process NSAccessibility tests above still ran")
	}
	return axCreateApp(int32(os.Getpid()))
}

func TestGUIAccessibilityAXWindows(t *testing.T) {
	app := axApp(t)
	w := chromeWin(t, caps.ChromeOptions{TitleBar: caps.TitleBarHidden})
	off, err := guiApp.NewWindow(WindowOptions{Title: "ax offscreen secret", Width: 320, Height: 200, Path: "/blank", Offscreen: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(off.Close)
	var titles []string
	waitCond(t, "the window in the AX tree", func() bool {
		titles = nil
		for _, win := range axKids(app, "AXWindows") {
			titles = append(titles, axText(win, "AXTitle"))
		}
		for _, s := range titles {
			if s == t.Name() {
				return true
			}
		}
		return false
	})
	_ = w
	for _, s := range titles {
		if s == "ax offscreen secret" {
			t.Errorf("the offscreen window is in AXWindows: %v", titles)
		}
	}
}

func TestGUIAccessibilityAXMenuBar(t *testing.T) {
	app := axApp(t)
	if err := guiApp.SetAppMenu([]caps.MenuItem{{Label: "&Reports", Kind: caps.MenuItemSubmenu, Submenu: []caps.MenuItem{{ID: "r", Label: "Weekly"}}}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guiApp.SetAppMenu(nil) })
	var found bool
	waitCond(t, "the menu in the AX menu bar", func() bool {
		bar := axValue(app, "AXMenuBar")
		if bar == 0 {
			return false
		}
		for _, it := range axKids(bar, "AXChildren") {
			if axText(it, "AXTitle") == "Reports" {
				found = true
			}
		}
		return found
	})
}
