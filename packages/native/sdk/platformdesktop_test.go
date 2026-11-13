package sdk_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk"
	"github.com/kratejs/krate/packages/native/sdk/app"
	"github.com/kratejs/krate/packages/native/sdk/internal/fakeplat"
	"github.com/kratejs/krate/packages/native/sdk/menu"
	"github.com/kratejs/krate/packages/native/sdk/notify"
	"github.com/kratejs/krate/packages/native/sdk/tray"
)

// These tests cover the SDK on a platform layer that implements the tray and
// notification capabilities itself (macOS). They run on every OS because they
// use the in-memory fake.

func newDesktopRig(t *testing.T) (*fakeplat.Desktop, *sdk.Host) {
	t.Helper()
	d := fakeplat.NewDesktop()
	h := sdk.NewHost()
	h.Attach(d)
	w, err := d.NewWindow(platform.WindowOptions{Title: "main", Width: 800, Height: 600})
	if err != nil {
		t.Fatal(err)
	}
	h.AddWindow("main", w)
	t.Cleanup(h.Close)
	return d, h
}

func waitHost(t *testing.T, h *sdk.Host, topic string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if h.Subscribers(topic) > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("nobody subscribed to %s", topic)
}

func TestPlatformTrayIsUsed(t *testing.T) {
	d, h := newDesktopRig(t)
	if !app.Supports("tray") {
		t.Fatal("tray not reported as supported")
	}
	m := menu.New(
		menu.Action("open", "Open", ""),
		menu.Check("auto", "Autosave", true),
		menu.Separator(),
		menu.Sub("More", menu.Action("deep", "Deep", "")),
	)
	tr, err := tray.New(tray.Options{Tooltip: "tip", Title: "T", Icon: []byte{1, 2, 3}, Menu: m})
	if err != nil {
		t.Fatal(err)
	}
	icon, _ := d.TrayState()
	if icon == nil || icon.Tooltip != "tip" || icon.Title != "T" || len(icon.PNG) != 3 || len(icon.Menu) != 4 {
		t.Fatalf("platform tray = %+v", icon)
	}
	if icon.Menu[1].Kind != caps.MenuItemCheck || !icon.Menu[1].Checked || icon.Menu[2].Kind != caps.MenuItemSeparator ||
		len(icon.Menu[3].Submenu) != 1 || icon.Menu[3].Submenu[0].ID != "deep" {
		t.Fatalf("menu model = %+v", icon.Menu)
	}

	var mu sync.Mutex
	var got []tray.Event
	var clicks []string
	cancel, err := tray.OnEvent(func(e tray.Event) { mu.Lock(); got = append(got, e); mu.Unlock() })
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	cancelMenu, err := menu.OnClick(func(e menu.Event) { mu.Lock(); clicks = append(clicks, e.ID); mu.Unlock() })
	if err != nil {
		t.Fatal(err)
	}
	defer cancelMenu()
	waitHost(t, h, "tray")
	waitHost(t, h, "menu")
	d.EmitTray(caps.TrayEvent{Kind: "click"})
	d.EmitTray(caps.TrayEvent{Kind: "menu", ID: "open"})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n, c := len(got), len(clicks)
		mu.Unlock()
		if n == 2 && c == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	if fmt.Sprint(got) != fmt.Sprint([]tray.Event{{Kind: "click"}, {Kind: "menu", ID: "open"}}) {
		t.Errorf("tray events = %+v", got)
	}
	if len(clicks) != 1 || clicks[0] != "open" {
		t.Errorf("tray menu items must reach menu.OnClick, got %v", clicks)
	}
	mu.Unlock()
	if err := tr.SetTooltip("new tip"); err != nil {
		t.Fatal(err)
	}
	if icon, calls := d.TrayState(); icon == nil || icon.Tooltip != "new tip" || calls != 2 {
		t.Errorf("after SetTooltip: %+v calls %d", icon, calls)
	}
	if err := tr.Remove(); err != nil {
		t.Fatal(err)
	}
	if icon, _ := d.TrayState(); icon != nil {
		t.Error("Remove left the icon")
	}
}

func TestPlatformNotifyAvailability(t *testing.T) {
	d, h := newDesktopRig(t)
	if !app.Supports("notify") {
		t.Fatal("notify should be supported")
	}
	err := notify.Send(notify.Notification{ID: "n1", Title: "hello", Body: "b", Urgency: notify.Critical, Silent: true,
		Actions: []notify.Action{{ID: "a", Label: "Do"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Notified()) != 1 {
		t.Fatalf("notified = %+v", d.Notified())
	}
	n := d.Notified()[0]
	if n.ID != "n1" || n.Title != "hello" || n.Urgency != 2 || !n.Silent || len(n.Actions) != 1 || n.Actions[0].ID != "a" {
		t.Errorf("notification = %+v", n)
	}

	var mu sync.Mutex
	var evs []notify.Event
	cancel, err := notify.OnEvent(func(e notify.Event) { mu.Lock(); evs = append(evs, e); mu.Unlock() })
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	waitHost(t, h, "notify")
	d.EmitNotify(caps.NotifyEvent{ID: "n1", Action: "a"})
	d.EmitNotify(caps.NotifyEvent{ID: "n1", Closed: true})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		c := len(evs)
		mu.Unlock()
		if c == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	if len(evs) != 2 || evs[0].Action != "a" || !evs[1].Closed {
		t.Errorf("events = %+v", evs)
	}
	mu.Unlock()

	if err := notify.Close("n1"); err != nil {
		t.Fatal(err)
	}
	if c := d.Closed(); len(c) != 1 || c[0] != "n1" {
		t.Errorf("closed = %v", c)
	}
	d.SetUnavailable(fmt.Errorf("%w: not in an app bundle", krate.ErrUnsupported))

	if app.Supports("notify") {
		t.Error("notify must be reported unsupported while the platform says it cannot work")
	}
	err = notify.Send(notify.Notification{Title: "x"})
	if err == nil || !krate.IsUnsupported(err) || !errors.Is(err, krate.ErrUnsupported) {
		t.Errorf("Send while unavailable = %v", err)
	}
}

// A click that started the app arrives before the page has subscribed; it
// must be held until the first subscriber (Windows toast activation).
func TestPlatformNotifyLaunchEventWaitsForSubscriber(t *testing.T) {
	d, h := newDesktopRig(t)
	d.EmitNotify(caps.NotifyEvent{ID: "cold", Action: "open", Launch: true})
	d.EmitNotify(caps.NotifyEvent{ID: "plain"}) // not a launch event: dropped without a subscriber

	var mu sync.Mutex
	var evs []notify.Event
	cancel, err := notify.OnEvent(func(e notify.Event) { mu.Lock(); evs = append(evs, e); mu.Unlock() })
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	waitHost(t, h, "notify")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		c := len(evs)
		mu.Unlock()
		if c >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(evs) != 1 || evs[0].ID != "cold" || evs[0].Action != "open" {
		t.Errorf("events = %+v, want only the launch click", evs)
	}
}
