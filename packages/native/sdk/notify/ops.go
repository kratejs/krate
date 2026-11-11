package notify

import (
	"fmt"
	"sync"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// backend sends notifications.
type backend interface {
	send(n caps.Notification) error
	close(id string) error
}

// Register adds the platform-side operations to t.
func Register(t *bridge.Table, env *bridge.Env) {
	topic := env.Topic("notify")
	t.Stream(IDEvents, Permission, true, topic.Stream())

	publish := func(ev caps.NotifyEvent) {
		topic.Publish(func(e *wire.Encoder) { encodeEvent(e, Event{ID: ev.ID, Action: ev.Action, Closed: ev.Closed}) })
	}
	var once sync.Once
	var be backend
	var beErr error
	get := func() (backend, error) {
		once.Do(func() {
			if n, ok := env.App.(caps.Notifier); ok {
				n.OnNotify(publish)
				be = platformBackend{n}
				return
			}
			be, beErr = newDefaultBackend(publish)
		})
		return be, beErr
	}

	t.Func(IDSend, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSend)
		n := decode(r)
		if err := r.Done(); err != nil {
			return err
		}
		b, err := get()
		if err != nil {
			return err
		}
		cn := caps.Notification{ID: n.ID, Title: n.Title, Body: n.Body, Icon: n.Icon, Urgency: int(n.Urgency), TimeoutMS: int(n.Timeout.Milliseconds()), Silent: n.Silent}
		if n.Timeout < 0 {
			cn.TimeoutMS = -1
		}
		for _, a := range n.Actions {
			if a.ID == "" {
				return krate.Errorf("bad_args", "%s: notification action needs an id", IDSend)
			}
			cn.Actions = append(cn.Actions, caps.NotificationAction{ID: a.ID, Label: a.Label})
		}
		out.Null()
		return b.send(cn)
	})
	t.Func(IDClose, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDClose)
		id := r.String()
		if err := r.Done(); err != nil {
			return err
		}
		b, err := get()
		if err != nil {
			return err
		}
		out.Null()
		return b.close(id)
	})
}

type platformBackend struct{ n caps.Notifier }

func (p platformBackend) send(n caps.Notification) error { return p.n.Notify(n) }
func (p platformBackend) close(id string) error {
	if c, ok := p.n.(caps.NotificationCloser); ok {
		return c.CloseNotification(id)
	}
	return fmt.Errorf("%w: closing notifications is not supported on this platform", krate.ErrUnsupported)
}
