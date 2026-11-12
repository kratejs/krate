//go:build linux

package notify

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/native/distinfo"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/dbusiface"
	"github.com/kratejs/krate/packages/native/sdk/internal/sessionbus"
)

const (
	portalDest  = "org.freedesktop.portal.Desktop"
	portalPath  = "/org/freedesktop/portal/desktop"
	portalIface = "org.freedesktop.portal.Notification"
)

// portalBackend sends notifications through org.freedesktop.portal.Notification,
// the only route a Flatpak sandbox is guaranteed to have. The portal has no
// "closed" signal, so dismissals are not reported.
type portalBackend struct {
	conn    dbusiface.Conn
	publish func(caps.NotifyEvent)
	cancel  func()

	mu  sync.Mutex
	seq int
	ids map[string]bool
}

// themedIcon is the portal's (sv) icon: ("themed", ["name"]).
type themedIcon struct {
	Kind  string
	Value dbusiface.Variant
}

func newPortalBackend(conn dbusiface.Conn, publish func(caps.NotifyEvent)) (*portalBackend, error) {
	b := &portalBackend{conn: conn, publish: publish, ids: map[string]bool{}}
	c, err := conn.Subscribe("type='signal',interface='"+portalIface+"',member='ActionInvoked'", b.onAction)
	if err != nil {
		return nil, err
	}
	b.cancel = c
	return b, nil
}

// usePortal reports whether notifications should go through the portal.
func usePortal() bool {
	return distinfo.Current().Sandboxed() && sessionbus.NameHasOwner(portalDest)
}

func (b *portalBackend) onAction(s dbusiface.Signal) {
	if len(s.Body) < 2 {
		return
	}
	id, _ := s.Body[0].(string)
	action, _ := s.Body[1].(string)
	b.mu.Lock()
	ok := b.ids[id]
	b.mu.Unlock()
	if !ok {
		return
	}
	if action == "default" {
		action = ""
	}
	b.publish(caps.NotifyEvent{ID: id, Action: action})
}

func (b *portalBackend) current() bool {
	c, err := sessionbus.Get()
	return err == nil && c == b.conn
}

func (b *portalBackend) release() {
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
}

// portalPriority maps the notification urgency (0 low, 1 normal, 2 critical).
func portalPriority(urgency int) string {
	switch {
	case urgency <= 0:
		return "low"
	case urgency >= 2:
		return "urgent"
	}
	return "normal"
}

func (b *portalBackend) send(n caps.Notification) error {
	id := n.ID
	b.mu.Lock()
	if id == "" {
		b.seq++
		id = fmt.Sprintf("krate-%d", b.seq)
	}
	b.ids[id] = true
	b.mu.Unlock()
	body := map[string]dbusiface.Variant{
		"title":    {Value: n.Title},
		"body":     {Value: n.Body},
		"priority": {Value: portalPriority(n.Urgency)},
	}
	if len(n.Actions) > 0 {
		body["default-action"] = dbusiface.Variant{Value: "default"}
		buttons := make([]map[string]dbusiface.Variant, 0, len(n.Actions))
		for _, a := range n.Actions {
			buttons = append(buttons, map[string]dbusiface.Variant{
				"label": {Value: a.Label}, "action": {Value: a.ID},
			})
		}
		body["buttons"] = dbusiface.Variant{Value: buttons}
	}
	if n.Icon != "" && !filepath.IsAbs(n.Icon) && !strings.Contains(n.Icon, "/") {
		body["icon"] = dbusiface.Variant{Value: themedIcon{"themed", dbusiface.Variant{Value: []string{n.Icon}}}}
	}
	_, err := b.conn.Call(portalDest, portalPath, portalIface, "AddNotification", id, body)
	if err != nil {
		var de *dbusiface.Error
		if errors.As(err, &de) && (strings.HasSuffix(de.Name, "ServiceUnknown") || strings.HasSuffix(de.Name, "NameHasNoOwner")) {
			return fmt.Errorf("%w: no notification portal is running", krate.ErrUnsupported)
		}
		return err
	}
	return nil
}

func (b *portalBackend) close(id string) error {
	b.mu.Lock()
	delete(b.ids, id)
	b.mu.Unlock()
	_, err := b.conn.Call(portalDest, portalPath, portalIface, "RemoveNotification", id)
	return err
}
