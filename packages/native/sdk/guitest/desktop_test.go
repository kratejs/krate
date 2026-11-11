//go:build linux

package guitest

import (
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/internal/dbus/dbustest"
	"github.com/kratejs/krate/packages/native/sdk/internal/dbusiface"
	"github.com/kratejs/krate/packages/native/sdk/internal/sessionbus"
	"github.com/kratejs/krate/packages/native/sdk/notify"
	"github.com/kratejs/krate/packages/native/sdk/tray"
)

// With the real platform present, notifications and the tray still go over
// D-Bus; a fake Notifications service and StatusNotifierWatcher stand in for
// the desktop.
func TestNotificationAndTrayWithFakeDesktopServices(t *testing.T) {
	need(t)
	addr := dbustest.Start(t)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", addr)
	sessionbus.Reset()
	defer sessionbus.Reset()
	svc, err := dbusiface.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	if err := svc.RequestName("org.freedesktop.Notifications"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RequestName("org.kde.StatusNotifierWatcher"); err != nil {
		t.Fatal(err)
	}
	summaries := make(chan string, 2)
	svc.Export("/org/freedesktop/Notifications", "org.freedesktop.Notifications", map[string]dbusiface.Method{
		"Notify": func(a []any) ([]any, error) { summaries <- a[3].(string); return []any{uint32(3)}, nil },
	})
	registered := make(chan string, 1)
	svc.Export("/StatusNotifierWatcher", "org.kde.StatusNotifierWatcher", map[string]dbusiface.Method{
		"RegisterStatusNotifierItem": func(a []any) ([]any, error) { registered <- a[0].(string); return nil, nil },
	})

	got := make(chan notify.Event, 1)
	cancel, err := notify.OnEvent(func(e notify.Event) { got <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	eventually(t, "notify subscriber", func() bool { return host.Subscribers("notify") > 0 })
	if err := notify.Send(notify.Notification{ID: "x", Title: "Hello", Actions: []notify.Action{{ID: "go", Label: "Go"}}}); err != nil {
		t.Fatal(err)
	}
	if s := <-summaries; s != "Hello" {
		t.Fatalf("summary %q", s)
	}
	if err := svc.Emit("/org/freedesktop/Notifications", "org.freedesktop.Notifications", "ActionInvoked", uint32(3), "go"); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-got:
		if e.ID != "x" || e.Action != "go" {
			t.Fatalf("event %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no action event")
	}

	tr, err := tray.New(tray.Options{Tooltip: "tip"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-registered:
	case <-time.After(3 * time.Second):
		t.Fatal("tray item never registered")
	}
	if err := tr.Remove(); err != nil {
		t.Fatal(err)
	}
}
