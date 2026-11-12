// Package port connects two ends of a channel across windows, views and
// workers without host code relaying each message.
//
// A channel has two ends. Whoever creates a channel holds both; an end is
// handed to a window, a view (by its SDK id), a worker ("worker:<id>") or back
// to host code with Transfer, and only the current holder can Post to it,
// Listen on it or Close it. A message posted on one end is queued for the
// other and delivered to its holder's Listen stream, in order, with binary
// values (Uint8Array, []byte), Dates, Maps and Sets carried by the wire codec.
// Host code never sees a message unless it holds an end.
//
// The shell is the broker: the engines give two top-level webviews no way to
// share a MessagePort, so a page gets a Port object (@krate/app/port) backed
// by these ids. Inside one page the real MessageChannel is untouched.
//
// Permissions: "port" creates channels and transfers ends; "port:receive" uses
// an end and may be handed one. "port" implies "port:receive". An end can only
// be handed to a window, view or worker whose scope has "port:receive".
package port

import (
	"context"
	"encoding/json"

	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/wire"
)

// Permission names.
const (
	Permission        = "port"
	PermissionReceive = "port:receive"
)

// Host function ids.
const (
	IDChannel  = "krate/port.channel"
	IDTransfer = "krate/port.transfer"
	IDPost     = "krate/port.post"
	IDListen   = "krate/port.listen"
	IDClose    = "krate/port.close"
)

// Ends are the two ends of a new channel.
type Ends struct {
	A string `json:"a"`
	B string `json:"b"`
}

// WorkerOwner is the owner id of a worker, for Transfer.
func WorkerOwner(workerID string) string { return WorkerOwnerPrefix + workerID }

// Channel creates a channel; host code holds both ends.
func Channel() (Ends, error) {
	d, err := bridge.Call(context.Background(), IDChannel, nil)
	if err != nil {
		return Ends{}, err
	}
	raw, err := d.RawValue()
	if err != nil {
		return Ends{}, err
	}
	var e Ends
	return e, json.Unmarshal(raw, &e)
}

// Transfer hands an end to a window or view id, "worker:<id>" or "" (host).
func Transfer(end, to string) error {
	return bridge.CallDo(context.Background(), IDTransfer, func(e *wire.Encoder) { e.String(end); e.String(to) })
}

// Post sends a JSON-encodable value to the other end.
func Post(end string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return bridge.CallDo(context.Background(), IDPost, func(e *wire.Encoder) { e.String(end); e.Raw(b) })
}

// PostBytes sends binary data to the other end.
func PostBytes(end string, data []byte) error {
	return bridge.CallDo(context.Background(), IDPost, func(e *wire.Encoder) { e.String(end); e.Bytes(data) })
}

// Close closes the channel the end belongs to.
func Close(end string) error {
	return bridge.CallDo(context.Background(), IDClose, func(e *wire.Encoder) { e.String(end) })
}

// Message is one received message: Data is its JSON, or Bytes when the sender
// posted binary data.
type Message struct {
	Data  json.RawMessage
	Bytes []byte
}

// Listen calls fn for each message that arrives on an end held by host code,
// and onClose (may be nil) once when the channel closes. Call cancel to stop.
func Listen(end string, fn func(Message), onClose func()) (cancel func(), err error) {
	var stop func()
	stop, err = bridge.SubscribeArgs(context.Background(), IDListen, func(e *wire.Encoder) { e.String(end) }, func(d *wire.Decoder) {
		m, closed := decodeEvent(d)
		if closed {
			if onClose != nil {
				onClose()
			}
			if stop != nil {
				stop()
			}
			return
		}
		fn(m)
	})
	return func() {
		if stop != nil {
			stop()
		}
	}, err
}

// decodeEvent reads {"data": v} or {"closed": true}.
func decodeEvent(d *wire.Decoder) (m Message, closed bool) {
	if d.BeginObject() != nil {
		return m, false
	}
	for d.More() {
		k, err := d.Key()
		if err != nil {
			return m, false
		}
		switch k {
		case "closed":
			_ = d.Skip()
			closed = true
		case "data":
			if d.Kind() == wire.KindBytes {
				m.Bytes, _ = d.Bytes()
				continue
			}
			raw, err := d.RawValue()
			if err != nil {
				return m, false
			}
			m.Data = append(json.RawMessage(nil), raw...)
		default:
			_ = d.Skip()
		}
	}
	return m, closed
}
