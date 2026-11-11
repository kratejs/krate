//go:build linux

package sdk_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/internal/dbus/dbustest"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/sdk"
	"github.com/kratejs/krate/packages/native/sdk/internal/dbusiface"
	"github.com/kratejs/krate/packages/native/sdk/internal/fakeplat"
	"github.com/kratejs/krate/packages/native/sdk/internal/sessionbus"
	"github.com/kratejs/krate/packages/native/sdk/menu"
	"github.com/kratejs/krate/packages/native/sdk/notify"
	"github.com/kratejs/krate/packages/native/sdk/shortcut"
	"github.com/kratejs/krate/packages/native/sdk/tray"
)

// session starts a private bus and points the SDK at it.
func session(t *testing.T) string {
	t.Helper()
	addr := dbustest.Start(t)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", addr)
	sessionbus.Reset()
	t.Cleanup(sessionbus.Reset)
	return addr
}

func dial(t *testing.T, addr string) *dbusiface.Client {
	t.Helper()
	c, err := dbusiface.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

type sentNotification struct {
	App, Icon, Summary, Body string
	Replaces                 uint32
	Actions                  []string
	Hints                    map[string]any
	Timeout                  int32
}

func TestNotifyOverDBus(t *testing.T) {
	addr := session(t)
	svc := dial(t, addr)
	if err := svc.RequestName("org.freedesktop.Notifications"); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var sent []sentNotification
	var closed []uint32
	svc.Export("/org/freedesktop/Notifications", "org.freedesktop.Notifications", map[string]dbusiface.Method{
		"Notify": func(a []any) ([]any, error) {
			n := sentNotification{App: a[0].(string), Replaces: a[1].(uint32), Icon: a[2].(string), Summary: a[3].(string), Body: a[4].(string), Timeout: a[7].(int32)}
			for _, x := range a[5].([]any) {
				n.Actions = append(n.Actions, x.(string))
			}
			n.Hints = map[string]any{}
			for k, v := range a[6].(map[string]any) {
				n.Hints[k] = v.(dbusiface.Variant).Value
			}
			mu.Lock()
			sent = append(sent, n)
			id := uint32(7)
			mu.Unlock()
			return []any{id}, nil
		},
		"CloseNotification": func(a []any) ([]any, error) {
			mu.Lock()
			closed = append(closed, a[0].(uint32))
			mu.Unlock()
			return nil, nil
		},
	})
	r := newRig(t)
	events := make(chan notify.Event, 8)
	cancel, err := notify.OnEvent(func(e notify.Event) { events <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	waitSubs(t, r, "notify")

	if !r.host.SupportsForTest("notify") {
		t.Fatal("notify is not reported as supported with a service running")
	}
	err = notify.Send(notify.Notification{
		ID: "n1", Title: "Build done", Body: "3 warnings", Icon: "dialog-information",
		Actions: []notify.Action{{ID: "open", Label: "Open"}, {ID: "dismiss", Label: "Dismiss"}},
		Urgency: notify.Critical, Timeout: 5 * time.Second, Silent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	n := sent[0]
	mu.Unlock()
	if n.Summary != "Build done" || n.Body != "3 warnings" || n.Icon != "dialog-information" || n.Timeout != 5000 ||
		strings.Join(n.Actions, ",") != "default,,open,Open,dismiss,Dismiss" || n.Hints["urgency"] != byte(2) || n.Hints["suppress-sound"] != true {
		t.Fatalf("server saw %+v", n)
	}
	// Sending the same ID again replaces the server-side notification.
	if err := notify.Send(notify.Notification{ID: "n1", Title: "Again"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if sent[1].Replaces != 7 {
		t.Fatalf("replaces_id = %d", sent[1].Replaces)
	}
	mu.Unlock()

	// ActionInvoked comes back as an event with our ID.
	svc.Emit("/org/freedesktop/Notifications", "org.freedesktop.Notifications", "ActionInvoked", uint32(7), "open")
	select {
	case e := <-events:
		if e.ID != "n1" || e.Action != "open" {
			t.Fatalf("event %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ActionInvoked was not delivered")
	}
	// A dismissal after an action is not reported twice; a plain one is.
	svc.Emit("/org/freedesktop/Notifications", "org.freedesktop.Notifications", "NotificationClosed", uint32(7), uint32(2))
	select {
	case e := <-events:
		t.Fatalf("closed after an action reported: %+v", e)
	case <-time.After(300 * time.Millisecond):
	}
	notify.Send(notify.Notification{ID: "n2", Title: "Two"})
	svc.Emit("/org/freedesktop/Notifications", "org.freedesktop.Notifications", "NotificationClosed", uint32(7), uint32(2))
	select {
	case e := <-events:
		if e.ID != "n2" || !e.Closed {
			t.Fatalf("event %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("NotificationClosed was not delivered")
	}
	if err := notify.Close("n2"); err != nil {
		t.Fatal(err)
	}
	if err := notify.Send(notify.Notification{}); err == nil || !strings.Contains(err.Error(), "title") {
		t.Fatalf("empty title accepted: %v", err)
	}
}

func TestNotifyWithoutServiceIsUnsupported(t *testing.T) {
	session(t)
	newRig(t)
	err := notify.Send(notify.Notification{Title: "x"})
	if !krate.IsUnsupported(err) {
		t.Fatalf("err = %v", err)
	}
}

func testPNG(t *testing.T) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.NRGBA{255, 0, 0, 255})
	img.Set(1, 0, color.NRGBA{0, 255, 0, 128})
	img.Set(0, 1, color.NRGBA{0, 0, 255, 255})
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func TestTrayOverDBus(t *testing.T) {
	addr := session(t)
	watcher := dial(t, addr)
	if err := watcher.RequestName("org.kde.StatusNotifierWatcher"); err != nil {
		t.Fatal(err)
	}
	registered := make(chan string, 2)
	watcher.Export("/StatusNotifierWatcher", "org.kde.StatusNotifierWatcher", map[string]dbusiface.Method{
		"RegisterStatusNotifierItem": func(a []any) ([]any, error) { registered <- a[0].(string); return nil, nil },
	})
	r := newRig(t)
	if !r.host.SupportsForTest("tray") {
		t.Fatal("tray not supported with a watcher running")
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
	waitSubs(t, r, "tray")

	tr, err := tray.New(tray.Options{
		Tooltip: "My App", Title: "App", Icon: testPNG(t),
		Menu: menu.New(menu.Action("open", "&Open", "Ctrl+O"), menu.Separator(), menu.Check("pause", "Pause", true), menu.Sub("More", menu.Action("quit", "Quit", ""))),
	})
	if err != nil {
		t.Fatal(err)
	}
	var service string
	select {
	case service = <-registered:
	case <-time.After(3 * time.Second):
		t.Fatal("the item never registered with the watcher")
	}
	if !strings.HasPrefix(service, "org.kde.StatusNotifierItem-") {
		t.Fatalf("service %q", service)
	}

	host := dial(t, addr)
	all, err := host.Call(service, "/StatusNotifierItem", "org.freedesktop.DBus.Properties", "GetAll", "org.kde.StatusNotifierItem")
	if err != nil {
		t.Fatal(err)
	}
	props := all[0].(map[string]any)
	pm := props["IconPixmap"].(dbusiface.Variant).Value.([]any)[0].(dbusiface.Struct)
	data := pm[2].([]byte)
	if pm[0] != int32(2) || pm[1] != int32(2) || len(data) != 16 || data[0] != 255 || data[1] != 255 || data[2] != 0 {
		t.Fatalf("pixmap %v data %v (want ARGB, red first)", pm[:2], data)
	}
	tt := props["ToolTip"].(dbusiface.Variant).Value.(dbusiface.Struct)
	if tt[2] != "My App" || props["Title"].(dbusiface.Variant).Value != "App" || props["Menu"].(dbusiface.Variant).Value != dbusiface.ObjectPath("/MenuBar") {
		t.Fatalf("props %v %v", tt, props["Title"])
	}

	lay, err := host.Call(service, "/MenuBar", "com.canonical.dbusmenu", "GetLayout", int32(0), int32(-1), []string{})
	if err != nil {
		t.Fatal(err)
	}
	root := lay[1].(dbusiface.Struct)
	kids := root[2].([]any)
	if len(kids) != 4 {
		t.Fatalf("layout children %d", len(kids))
	}
	first := kids[0].(dbusiface.Variant).Value.(dbusiface.Struct)
	if first[1].(map[string]any)["label"].(dbusiface.Variant).Value != "_Open" {
		t.Fatalf("first item %v", first)
	}
	sep := kids[1].(dbusiface.Variant).Value.(dbusiface.Struct)
	if sep[1].(map[string]any)["type"].(dbusiface.Variant).Value != "separator" {
		t.Fatalf("separator %v", sep)
	}
	more := kids[3].(dbusiface.Variant).Value.(dbusiface.Struct)
	quit := more[2].([]any)[0].(dbusiface.Variant).Value.(dbusiface.Struct)
	quitID := quit[0].(int32)

	if _, err := host.Call(service, "/MenuBar", "com.canonical.dbusmenu", "Event", quitID, "clicked", dbusiface.Variant{Value: ""}, uint32(0)); err != nil {
		t.Fatal(err)
	}
	if e := recvTray(t, events); e.Kind != "menu" || e.ID != "quit" {
		t.Fatalf("tray event %+v", e)
	}
	if e := <-menuClicks; e.ID != "quit" {
		t.Fatalf("menu event %+v", e)
	}
	if _, err := host.Call(service, "/StatusNotifierItem", "org.kde.StatusNotifierItem", "Activate", int32(1), int32(2)); err != nil {
		t.Fatal(err)
	}
	if e := recvTray(t, events); e.Kind != "click" {
		t.Fatalf("tray event %+v", e)
	}
	host.Call(service, "/StatusNotifierItem", "org.kde.StatusNotifierItem", "ContextMenu", int32(1), int32(2))
	if e := recvTray(t, events); e.Kind != "right-click" {
		t.Fatalf("tray event %+v", e)
	}

	// Updates re-read from the same item.
	if err := tr.SetTooltip("Changed"); err != nil {
		t.Fatal(err)
	}
	one, _ := host.Call(service, "/StatusNotifierItem", "org.freedesktop.DBus.Properties", "Get", "org.kde.StatusNotifierItem", "ToolTip")
	if one[0].(dbusiface.Variant).Value.(dbusiface.Struct)[2] != "Changed" {
		t.Fatalf("tooltip %v", one)
	}
	if err := tr.Remove(); err != nil {
		t.Fatal(err)
	}
}

func recvTray(t *testing.T, ch chan tray.Event) tray.Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(3 * time.Second):
		t.Fatal("no tray event")
		return tray.Event{}
	}
}

func TestTrayWithoutHostIsUnsupported(t *testing.T) {
	session(t)
	r := newRig(t)
	if r.host.SupportsForTest("tray") {
		t.Fatal("tray supported without a watcher")
	}
	if _, err := tray.New(tray.Options{Tooltip: "x"}); !krate.IsUnsupported(err) {
		t.Fatalf("err = %v", err)
	}
}

// portalRig is a host whose platform has no key grabbing, so the
// GlobalShortcuts portal is the only way.
func portalRig(t *testing.T) *rig {
	fa := fakeplat.New()
	h := sdk.NewHost()
	h.Attach(struct{ platform.App }{fa})
	t.Cleanup(h.Close)
	w, _ := fa.NewWindow(platform.WindowOptions{})
	h.AddWindow("main", w)
	return &rig{app: fa, host: h}
}

func TestShortcutPortal(t *testing.T) {
	addr := session(t)
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	p := dial(t, addr)
	if err := p.RequestName("org.freedesktop.portal.Desktop"); err != nil {
		t.Fatal(err)
	}
	const iface = "org.freedesktop.portal.GlobalShortcuts"
	const path = "/org/freedesktop/portal/desktop"
	p.ExportProperties(path, iface, func() map[string]dbusiface.Variant {
		return map[string]dbusiface.Variant{"version": {Value: uint32(1)}}
	})
	var mu sync.Mutex
	var bound [][]string
	respond := func(token string, results map[string]dbusiface.Variant) dbusiface.ObjectPath {
		c, err := sessionbus.Get()
		if err != nil {
			t.Error(err)
			return ""
		}
		un := c.(dbusiface.UniqueNamer).UniqueName()
		req := dbusiface.ObjectPath(path + "/request/" + strings.ReplaceAll(strings.TrimPrefix(un, ":"), ".", "_") + "/" + token)
		go func() {
			time.Sleep(20 * time.Millisecond)
			p.Emit(string(req), "org.freedesktop.portal.Request", "Response", uint32(0), results)
		}()
		return req
	}
	const session = "/org/freedesktop/portal/desktop/session/x/krate"
	p.Export(path, iface, map[string]dbusiface.Method{
		"CreateSession": func(a []any) ([]any, error) {
			tok := a[0].(map[string]any)["handle_token"].(dbusiface.Variant).Value.(string)
			return []any{respond(tok, map[string]dbusiface.Variant{"session_handle": {Value: session}})}, nil
		},
		"BindShortcuts": func(a []any) ([]any, error) {
			opts := a[3].(map[string]any)
			tok := opts["handle_token"].(dbusiface.Variant).Value.(string)
			var ids []string
			for _, s := range a[1].([]any) {
				st := s.(dbusiface.Struct)
				trig := st[1].(map[string]any)["preferred_trigger"].(dbusiface.Variant).Value.(string)
				ids = append(ids, st[0].(string)+"="+trig)
			}
			mu.Lock()
			bound = append(bound, ids)
			mu.Unlock()
			return []any{respond(tok, map[string]dbusiface.Variant{})}, nil
		},
	})

	r := portalRig(t)
	if !r.host.SupportsForTest("shortcut") {
		t.Fatal("shortcut not supported with the portal present")
	}
	fired := make(chan struct{}, 2)
	unreg, err := shortcut.Register("Ctrl+Shift+K", func() { fired <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(bound) != 1 || len(bound[0]) != 1 || !strings.HasSuffix(bound[0][0], "=CTRL+SHIFT+k") {
		t.Fatalf("bound %v", bound)
	}
	id := strings.Split(bound[0][0], "=")[0]
	mu.Unlock()
	waitSubs(t, r, "shortcut")
	p.Emit(path, iface, "Activated", dbusiface.ObjectPath(session), id, uint64(1), map[string]dbusiface.Variant{})
	select {
	case <-fired:
	case <-time.After(3 * time.Second):
		t.Fatal("Activated was not delivered")
	}
	if err := unreg(); err != nil {
		t.Fatal(err)
	}
}

func TestShortcutWaylandWithoutPortalIsUnsupported(t *testing.T) {
	session(t)
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	portalRig(t)
	_, err := shortcut.Register("Ctrl+Shift+K", func() {})
	if !krate.IsUnsupported(err) {
		t.Fatalf("err = %v", err)
	}
}
