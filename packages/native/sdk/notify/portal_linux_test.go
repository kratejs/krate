//go:build linux

package notify

import (
	"sync"
	"testing"

	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/dbusiface"
)

type portalBus struct {
	dbusiface.Conn
	mu    sync.Mutex
	calls []string
	args  [][]any
	sub   func(dbusiface.Signal)
}

func (p *portalBus) Call(dest, path, iface, method string, args ...any) ([]any, error) {
	p.mu.Lock()
	p.calls = append(p.calls, dest+" "+iface+"."+method)
	p.args = append(p.args, args)
	p.mu.Unlock()
	return nil, nil
}

func (p *portalBus) Subscribe(match string, fn func(dbusiface.Signal)) (func(), error) {
	p.sub = fn
	return func() {}, nil
}

func TestPortalBackendSendsAndRoutesActions(t *testing.T) {
	bus := &portalBus{}
	var got []caps.NotifyEvent
	b, err := newPortalBackend(bus, func(e caps.NotifyEvent) { got = append(got, e) })
	if err != nil {
		t.Fatal(err)
	}
	err = b.send(caps.Notification{ID: "n1", Title: "Hi", Body: "there", Urgency: 2, Icon: "mail",
		Actions: []caps.NotificationAction{{ID: "reply", Label: "Reply"}}})
	if err != nil {
		t.Fatal(err)
	}
	if bus.calls[0] != "org.freedesktop.portal.Desktop org.freedesktop.portal.Notification.AddNotification" {
		t.Fatalf("call = %q", bus.calls[0])
	}
	if bus.args[0][0] != "n1" {
		t.Errorf("id = %v", bus.args[0][0])
	}
	body := bus.args[0][1].(map[string]dbusiface.Variant)
	if body["title"].Value != "Hi" || body["priority"].Value != "urgent" {
		t.Errorf("body = %#v", body)
	}
	if _, ok := body["buttons"]; !ok || body["default-action"].Value != "default" {
		t.Errorf("actions missing: %#v", body)
	}
	if _, ok := body["icon"].Value.(themedIcon); !ok {
		t.Errorf("icon = %#v", body["icon"])
	}

	bus.sub(dbusiface.Signal{Body: []any{"n1", "reply", []any{}}})
	bus.sub(dbusiface.Signal{Body: []any{"n1", "default", []any{}}})
	bus.sub(dbusiface.Signal{Body: []any{"other", "x", []any{}}})
	if len(got) != 2 || got[0].Action != "reply" || got[1].Action != "" || got[1].ID != "n1" {
		t.Errorf("events = %#v", got)
	}
	if err := b.close("n1"); err != nil || bus.calls[len(bus.calls)-1] != "org.freedesktop.portal.Desktop org.freedesktop.portal.Notification.RemoveNotification" {
		t.Errorf("close: %v %v", err, bus.calls)
	}
	_ = b.send(caps.Notification{Title: "no id"})
	if id := bus.args[len(bus.args)-1][0]; id != "krate-1" {
		t.Errorf("generated id = %v", id)
	}
}

func TestPortalPriority(t *testing.T) {
	for u, want := range map[int]string{-1: "low", 0: "low", 1: "normal", 2: "urgent"} {
		if got := portalPriority(u); got != want {
			t.Errorf("urgency %d = %s want %s", u, got, want)
		}
	}
}
