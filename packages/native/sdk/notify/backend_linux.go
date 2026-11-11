//go:build linux

package notify

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/dbusiface"
	"github.com/kratejs/krate/packages/native/sdk/internal/sessionbus"
)

const (
	dest  = "org.freedesktop.Notifications"
	path  = "/org/freedesktop/Notifications"
	iface = "org.freedesktop.Notifications"
)

type dbusBackend struct {
	conn    dbusiface.Conn
	publish func(caps.NotifyEvent)

	mu      sync.Mutex
	byID    map[string]uint32 // our id -> server id
	byServ  map[uint32]string // server id -> our id
	acted   map[uint32]bool   // action seen, so the following close is not "dismissed"
	cancels []func()
}

func newDefaultBackend(publish func(caps.NotifyEvent)) (backend, error) {
	conn, err := sessionbus.Get()
	if err != nil {
		return nil, err
	}
	b := &dbusBackend{conn: conn, publish: publish, byID: map[string]uint32{}, byServ: map[uint32]string{}, acted: map[uint32]bool{}}
	c1, err := conn.Subscribe("type='signal',interface='"+iface+"',member='ActionInvoked'", b.onAction)
	if err != nil {
		return nil, err
	}
	c2, err := conn.Subscribe("type='signal',interface='"+iface+"',member='NotificationClosed'", b.onClosed)
	if err != nil {
		c1()
		return nil, err
	}
	b.cancels = []func(){c1, c2}
	return b, nil
}

// current reports whether the backend still uses the live session bus
// connection.
func (b *dbusBackend) current() bool {
	c, err := sessionbus.Get()
	return err == nil && c == b.conn
}

func (b *dbusBackend) release() {
	for _, c := range b.cancels {
		c()
	}
	b.cancels = nil
}

func (b *dbusBackend) onAction(s dbusiface.Signal) {
	if len(s.Body) < 2 {
		return
	}
	sid, _ := s.Body[0].(uint32)
	action, _ := s.Body[1].(string)
	b.mu.Lock()
	id, ok := b.byServ[sid]
	if ok {
		b.acted[sid] = true
	}
	b.mu.Unlock()
	if !ok {
		return
	}
	if action == "default" {
		action = ""
	}
	b.publish(caps.NotifyEvent{ID: id, Action: action})
}

func (b *dbusBackend) onClosed(s dbusiface.Signal) {
	if len(s.Body) < 1 {
		return
	}
	sid, _ := s.Body[0].(uint32)
	b.mu.Lock()
	id, ok := b.byServ[sid]
	acted := b.acted[sid]
	if ok {
		delete(b.byServ, sid)
		delete(b.byID, id)
		delete(b.acted, sid)
	}
	b.mu.Unlock()
	if ok && !acted {
		b.publish(caps.NotifyEvent{ID: id, Closed: true})
	}
}

func (b *dbusBackend) send(n caps.Notification) error {
	var replaces uint32
	if n.ID != "" {
		b.mu.Lock()
		replaces = b.byID[n.ID]
		b.mu.Unlock()
	}
	var actions []string
	if len(n.Actions) > 0 {
		// "default" is the action of a click on the notification body.
		actions = append(actions, "default", "")
		for _, a := range n.Actions {
			actions = append(actions, a.ID, a.Label)
		}
	}
	if actions == nil {
		actions = []string{}
	}
	hints := map[string]dbusiface.Variant{"urgency": {Value: byte(n.Urgency)}}
	if n.Silent {
		hints["suppress-sound"] = dbusiface.Variant{Value: true}
	}
	icon := n.Icon
	if icon != "" && filepath.IsAbs(icon) {
		if _, err := os.Stat(icon); err == nil {
			icon = "file://" + icon
		}
	}
	timeout := int32(n.TimeoutMS)
	if n.TimeoutMS == 0 {
		timeout = -1
	} else if n.TimeoutMS < 0 {
		timeout = 0
	}
	out, err := b.conn.Call(dest, path, iface, "Notify", appName(), replaces, icon, n.Title, n.Body, actions, hints, timeout)
	if err != nil {
		var de *dbusiface.Error
		if errors.As(err, &de) && (strings.HasSuffix(de.Name, "ServiceUnknown") || strings.HasSuffix(de.Name, "NameHasNoOwner")) {
			return fmt.Errorf("%w: no notification service is running", krate.ErrUnsupported)
		}
		return err
	}
	if len(out) != 1 {
		return errors.New("notify: unexpected reply from the notification service")
	}
	sid, _ := out[0].(uint32)
	if n.ID != "" {
		b.mu.Lock()
		b.byID[n.ID] = sid
		b.byServ[sid] = n.ID
		b.mu.Unlock()
	}
	return nil
}

func (b *dbusBackend) close(id string) error {
	b.mu.Lock()
	sid, ok := b.byID[id]
	b.mu.Unlock()
	if !ok {
		return nil
	}
	_, err := b.conn.Call(dest, path, iface, "CloseNotification", sid)
	return err
}

func appName() string {
	if id := os.Getenv("KRATE_APP_ID"); id != "" {
		return id
	}
	return "krate"
}

// Available reports whether a notification service is running.
func Available(env *bridge.Env) bool {
	if _, ok := env.App.(caps.Notifier); ok {
		return true
	}
	return sessionbus.NameHasOwner(dest)
}
