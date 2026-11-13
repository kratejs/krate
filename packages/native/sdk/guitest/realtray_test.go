//go:build linux

package guitest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/platform/linux"
	"github.com/kratejs/krate/packages/native/sdk/internal/dbusiface"
	"github.com/kratejs/krate/packages/native/sdk/internal/sessionbus"
	"github.com/kratejs/krate/packages/native/sdk/menu"
	"github.com/kratejs/krate/packages/native/sdk/tray"
)

// startStalonetray runs a real XEmbed system tray manager at a fixed spot and
// returns the window id of the tray.
func startStalonetray(t *testing.T) string {
	t.Helper()
	dir := scratchDir(t)
	writeFile(t, filepath.Join(dir, "stalonetrayrc"), "geometry 1x1+300+300\nicon_size 32\nicon_gravity NW\ngrow_gravity SE\nskip_taskbar true\nsticky true\nwindow_type dock\nbackground grey\n", 0o644)
	d := startDaemon(t, "stalonetray", "-c", filepath.Join(dir, "stalonetrayrc"))
	var win string
	waitFor(t, "stalonetray's window", 8*time.Second, func() bool {
		if d.exited() {
			t.Fatalf("stalonetray exited: %s", d.out.String())
		}
		out, err := exec.Command("xdotool", "search", "--onlyvisible", "--class", "stalonetray").Output()
		if err != nil {
			return false
		}
		if f := strings.Fields(string(out)); len(f) > 0 {
			win = f[0]
			return true
		}
		return false
	})
	return win
}

// trayCenter returns the screen position of the middle of the tray window.
func trayCenter(t *testing.T, win string) (int, int) {
	t.Helper()
	out, err := exec.Command("xdotool", "getwindowgeometry", "--shell", win).Output()
	if err != nil {
		t.Fatal(err)
	}
	vals := map[string]int{}
	for _, l := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			n, _ := strconv.Atoi(v)
			vals[k] = n
		}
	}
	return vals["X"] + vals["WIDTH"]/2, vals["Y"] + vals["HEIGHT"]/2
}

func xdo(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("xdotool", args...).CombinedOutput(); err != nil {
		t.Fatalf("xdotool %v: %v\n%s", args, err, out)
	}
}

func TestRealXEmbedTray(t *testing.T) {
	needX11(t)
	needServices(t, "stalonetray", "xdotool")
	xe, ok := plat.(caps.XEmbedTray)
	if !ok {
		t.Skip("the platform has no XEmbed tray support")
	}
	privateBus(t) // a bus with no StatusNotifierWatcher
	win := startStalonetray(t)
	waitFor(t, "the tray manager to be seen", 5*time.Second, xe.XEmbedTrayAvailable)
	if !host.SupportsForTest("tray") {
		t.Fatal("tray is not reported as supported with an XEmbed tray running")
	}

	events := make(chan tray.Event, 16)
	cancel, err := tray.OnEvent(func(e tray.Event) { events <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	menuClicks := make(chan menu.Event, 4)
	cancel2, _ := menu.OnItem("quit", func(e menu.Event) { menuClicks <- e })
	defer cancel2()
	eventually(t, "tray subscriber", func() bool { return host.Subscribers("tray") > 0 })

	opts := tray.Options{
		Tooltip: "XEmbed", Title: "App", Icon: mustPNG(),
		Menu: menu.New(menu.Action("open", "&Open", ""), menu.Separator(), menu.Action("quit", "Quit", "")),
	}
	tr, err := tray.New(opts)
	if err != nil {
		t.Fatalf("tray.New with only an XEmbed tray: %v", err)
	}
	defer func() {
		if err := tr.Remove(); err != nil {
			t.Error(err)
		}
	}()
	docked := func() bool { return linux.XEmbedTrayEmbedded(plat) }
	// A tray manager that has only just taken the selection can miss the
	// first dock request; a real tray has been up for a while.
	if !waitUntil(3*time.Second, docked) {
		if err := tr.Remove(); err != nil {
			t.Fatal(err)
		}
		if tr, err = tray.New(opts); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, "the icon to be docked in the tray", docked)
	time.Sleep(400 * time.Millisecond) // let the tray lay the icon out

	x, y := trayCenter(t, win)
	xdo(t, "mousemove", strconv.Itoa(x), strconv.Itoa(y))
	xdo(t, "click", "1")
	if e := recvTray(t, events); e.Kind != "click" {
		t.Fatalf("left click gave %+v", e)
	}
	xdo(t, "click", "2")
	if e := recvTray(t, events); e.Kind != "middle-click" {
		t.Fatalf("middle click gave %+v", e)
	}
	xdo(t, "click", "3")
	if e := recvTray(t, events); e.Kind != "right-click" {
		t.Fatalf("right click gave %+v", e)
	}
	// The context menu is up; walk to the last item and activate it.
	time.Sleep(300 * time.Millisecond)
	xdo(t, "key", "Down", "Down", "Return")
	if e := recvTray(t, events); e.Kind != "menu" || e.ID != "quit" {
		t.Fatalf("menu selection gave %+v", e)
	}
	select {
	case e := <-menuClicks:
		if e.ID != "quit" {
			t.Fatalf("menu event %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no menu.OnItem event for the tray item")
	}

	// Updates keep one icon; removing takes it out of the tray.
	if err := tr.SetTooltip("changed"); err != nil {
		t.Fatal(err)
	}
	if err := tr.SetMenu(menu.New(menu.Action("other", "Other", ""))); err != nil {
		t.Fatal(err)
	}
	if err := tr.Remove(); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the icon to leave the tray", func() bool { return !linux.XEmbedTrayEmbedded(plat) })
}

func recvTray(t *testing.T, ch chan tray.Event) tray.Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(4 * time.Second):
		t.Fatal("no tray event")
		return tray.Event{}
	}
}

func TestRealNoTrayAtAll(t *testing.T) {
	need(t)
	privateBus(t)
	if host.SupportsForTest("tray") {
		t.Skip("an XEmbed tray manager is already running on this display")
	}
	_, err := tray.New(tray.Options{Tooltip: "x"})
	if !krate.IsUnsupported(err) || !strings.Contains(err.Error(), "AppIndicator") {
		t.Fatalf("err = %v, want ErrUnsupported naming the AppIndicator extension", err)
	}
}

// indicatorService finds the real StatusNotifier watcher and host used by the
// SNI test: the Ayatana application indicator service, which is the host side
// of the StatusNotifierItem protocol in Ubuntu's panels.
func indicatorService() string {
	const name = "ayatana-indicator-application-service"
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	cands, _ := filepath.Glob("/usr/lib*/ayatana-indicator-application/" + name)
	more, _ := filepath.Glob("/usr/lib/*/ayatana-indicator-application/" + name)
	cands = append(cands, more...)
	if len(cands) > 0 {
		return cands[0]
	}
	return ""
}

func TestRealStatusNotifierHost(t *testing.T) {
	need(t)
	bin := indicatorService()
	if bin == "" {
		if os.Getenv("KRATE_REQUIRE_E2E") != "" {
			t.Fatal("ayatana-indicator-application-service is required (KRATE_REQUIRE_E2E) but is not installed")
		}
		t.Skip("ayatana-indicator-application-service is not installed")
	}
	addr := privateBus(t)
	d := startDaemon(t, bin)
	waitFor(t, "the StatusNotifierWatcher", 8*time.Second, func() bool {
		if d.exited() {
			t.Fatalf("host exited: %s", d.out.String())
		}
		return sessionbus.NameHasOwner("org.kde.StatusNotifierWatcher")
	})
	if !host.SupportsForTest("tray") {
		t.Fatal("tray is not reported as supported with a StatusNotifier host running")
	}
	hostConn := busClient(t, addr)
	const svcName, svcPath, svcIface = "org.ayatana.indicator.application", "/org/ayatana/indicator/application/service", "org.ayatana.indicator.application.service"
	apps := func() []dbusiface.Struct {
		out, err := hostConn.Call(svcName, svcPath, svcIface, "GetApplications")
		if err != nil {
			t.Fatal(err)
		}
		var res []dbusiface.Struct
		for _, a := range out[0].([]any) {
			res = append(res, a.(dbusiface.Struct))
		}
		return res
	}

	events := make(chan tray.Event, 8)
	cancel, err := tray.OnEvent(func(e tray.Event) { events <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	menuClicks := make(chan menu.Event, 4)
	cancel2, _ := menu.OnItem("quit", func(e menu.Event) { menuClicks <- e })
	defer cancel2()
	eventually(t, "tray subscriber", func() bool { return host.Subscribers("tray") > 0 })

	tr, err := tray.New(tray.Options{
		Tooltip: "Real host", Title: "Krate test", Icon: mustPNG(),
		Menu: menu.New(menu.Action("open", "&Open", "Ctrl+O"), menu.Separator(), menu.Check("pause", "Pause", true), menu.Sub("More", menu.Action("quit", "Quit", ""))),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tr.Remove(); err != nil {
			t.Error(err)
		}
	}()

	var entry dbusiface.Struct
	eventually(t, "the host to list our item", func() bool {
		a := apps()
		if len(a) == 1 {
			entry = a[0]
			return true
		}
		return false
	})
	// (iconname, position, address, menu object, iconpath, label, labelguide, desc, hint, title)
	address, _ := entry[2].(string)
	menuObj := string(entry[3].(dbusiface.ObjectPath))
	if !strings.HasPrefix(address, "org.kde.StatusNotifierItem-") || menuObj != "/MenuBar" {
		t.Fatalf("the host registered %q %q, want our item and its /MenuBar", address, menuObj)
	}
	if title, _ := entry[9].(string); title != "Krate test" {
		t.Fatalf("title %q, entry %v", title, entry)
	}

	// The watcher reports the item and a host.
	items, err := hostConn.Call("org.kde.StatusNotifierWatcher", "/StatusNotifierWatcher", "org.freedesktop.DBus.Properties", "Get", "org.kde.StatusNotifierWatcher", "RegisteredStatusNotifierItems")
	if err != nil {
		t.Fatal(err)
	}
	list, _ := items[0].(dbusiface.Variant).Value.([]any)
	if len(list) != 1 || !strings.Contains(list[0].(string), "org.kde.StatusNotifierItem-") {
		t.Fatalf("RegisteredStatusNotifierItems = %v", list)
	}

	// The menu the host would draw: read it the way a dbusmenu client does and
	// activate an entry.
	lay, err := hostConn.Call(address, menuObj, "com.canonical.dbusmenu", "GetLayout", int32(0), int32(-1), []string{})
	if err != nil {
		t.Fatal(err)
	}
	root := lay[1].(dbusiface.Struct)
	kids := root[2].([]any)
	if len(kids) != 4 {
		t.Fatalf("menu has %d top-level entries, want 4", len(kids))
	}
	more := kids[3].(dbusiface.Variant).Value.(dbusiface.Struct)
	quit := more[2].([]any)[0].(dbusiface.Variant).Value.(dbusiface.Struct)
	if _, err := hostConn.Call(address, menuObj, "com.canonical.dbusmenu", "Event", quit[0].(int32), "clicked", dbusiface.Variant{Value: ""}, uint32(0)); err != nil {
		t.Fatal(err)
	}
	if e := recvTray(t, events); e.Kind != "menu" || e.ID != "quit" {
		t.Fatalf("tray event %+v", e)
	}
	select {
	case e := <-menuClicks:
		if e.ID != "quit" {
			t.Fatalf("menu event %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no menu.OnItem event")
	}

	// A middle click goes through the host's own API to the item. Ayatana
	// hosts use their XAyatanaSecondaryActivate method for it.
	if _, err := hostConn.Call(svcName, svcPath, svcIface, "ApplicationSecondaryActivateEvent", address, menuObj, uint32(0)); err != nil {
		t.Fatal(err)
	}
	if e := recvTray(t, events); e.Kind != "middle-click" {
		t.Fatalf("host secondary activate gave %+v", e)
	}

	// Removing the icon takes it off the host.
	if err := tr.Remove(); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the host to drop our item", func() bool { return len(apps()) == 0 })
}

func waitUntil(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return false
}
