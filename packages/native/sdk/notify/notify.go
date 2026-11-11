// Package notify posts desktop notifications.
//
// Linux: org.freedesktop.Notifications over the session bus (pure Go, no
// libnotify). Actions come back through the ActionInvoked signal. Servers
// that do not advertise "actions" show a plain notification.
//
// macOS: UNUserNotificationCenter needs an app bundle with a bundle
// identifier; unbundled processes (go run, krate-shell in dev) get
// krate.ErrUnsupported.
//
// Windows: toast notifications through WinRT. An unpackaged app registers its
// AppUserModelID (derived from the app id) under HKCU\Software\Classes\
// AppUserModelId with the product name as display name; a packaged (MSIX)
// app uses the identity of its package. Actions and clicks come back while
// the app runs; clicking a toast after the app exited does nothing (COM
// activation of a closed app is not implemented). Windows has no per-toast
// timeout (short or long display only) and no urgency levels. macOS and
// Windows are compile-only in this release.
package notify

import (
	"context"
	"time"

	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// Permission is the app permission the UI needs to send notifications.
const Permission = "notify"

// Host function ids.
const (
	IDSend   = "krate/notify.send"
	IDClose  = "krate/notify.close"
	IDEvents = "krate/notify.events"
)

// Urgency of a notification.
type Urgency int

// Urgency levels.
const (
	Low Urgency = iota
	Normal
	Critical
)

// Action is a button on a notification.
type Action struct {
	ID    string
	Label string
}

// Notification is a desktop notification.
type Notification struct {
	// ID names the notification; sending the same ID again replaces it, and
	// events carry it back. Optional.
	ID    string
	Title string
	Body  string
	// Icon is an icon name from the theme or an absolute file path.
	Icon    string
	Actions []Action
	Urgency Urgency
	// Timeout is how long to show it; 0 means the server default, negative
	// means until dismissed.
	Timeout time.Duration
	// Silent suppresses the sound.
	Silent bool
}

// Event reports an interaction with a notification.
type Event struct {
	ID string
	// Action is the action id, "" for a click on the notification itself.
	Action string
	// Closed is true when the notification went away without an action.
	Closed bool
}

// Send posts n.
func Send(n Notification) error {
	return bridge.CallDo(context.Background(), IDSend, func(e *wire.Encoder) { encode(e, n) })
}

// Close withdraws the notification with this ID.
func Close(id string) error {
	return bridge.CallDo(context.Background(), IDClose, func(e *wire.Encoder) { e.String(id) })
}

// OnEvent calls fn for notification actions, clicks and dismissals until
// cancel is called.
func OnEvent(fn func(Event)) (cancel func(), err error) {
	return bridge.Subscribe(context.Background(), IDEvents, func(d *wire.Decoder) {
		if ev, err := decodeEvent(d); err == nil {
			fn(ev)
		}
	})
}

func encode(e *wire.Encoder, n Notification) {
	e.BeginObject()
	e.Key("id")
	e.String(n.ID)
	e.Key("title")
	e.String(n.Title)
	e.Key("body")
	e.String(n.Body)
	e.Key("icon")
	e.String(n.Icon)
	e.Key("actions")
	e.BeginArray()
	for _, a := range n.Actions {
		e.BeginObject()
		e.Key("id")
		e.String(a.ID)
		e.Key("label")
		e.String(a.Label)
		e.EndObject()
	}
	e.EndArray()
	e.Key("urgency")
	e.Int(int64(n.Urgency))
	e.Key("timeoutMs")
	e.Int(n.Timeout.Milliseconds())
	e.Key("silent")
	e.Bool(n.Silent)
	e.EndObject()
}

func decode(r *codec.R) (n Notification) {
	r.Object(func(k string) bool {
		switch k {
		case "id":
			n.ID = r.String()
		case "title":
			n.Title = r.String()
		case "body":
			n.Body = r.String()
		case "icon":
			n.Icon = r.String()
		case "actions":
			r.Array(func() {
				var a Action
				r.Object(func(k string) bool {
					switch k {
					case "id":
						a.ID = r.String()
					case "label":
						a.Label = r.String()
					default:
						return false
					}
					return true
				})
				n.Actions = append(n.Actions, a)
			})
		case "urgency":
			n.Urgency = Urgency(r.Int())
		case "timeoutMs":
			n.Timeout = time.Duration(r.Int()) * time.Millisecond
		case "silent":
			n.Silent = r.Bool()
		default:
			return false
		}
		return true
	})
	if n.Title == "" && r.Err() == nil {
		r.Failf("title is required")
	}
	return
}

func encodeEvent(e *wire.Encoder, ev Event) {
	e.BeginObject()
	e.Key("id")
	e.String(ev.ID)
	e.Key("action")
	e.String(ev.Action)
	e.Key("closed")
	e.Bool(ev.Closed)
	e.EndObject()
}

func decodeEvent(d *wire.Decoder) (ev Event, err error) {
	r := &codec.R{D: d, Name: "notification event"}
	r.Object(func(k string) bool {
		switch k {
		case "id":
			ev.ID = r.String()
		case "action":
			ev.Action = r.String()
		case "closed":
			ev.Closed = r.Bool()
		default:
			return false
		}
		return true
	})
	return ev, r.Err()
}
