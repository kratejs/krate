// Package jsonwire carries ordinary Go values over the SDK call protocol as
// JSON, for the areas whose payloads are small structs (badge, screen, power,
// login, theme, crash). The hand-written codecs of the older areas avoid
// reflection on hot paths; these calls are rare.
package jsonwire

import (
	"context"
	"encoding/json"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// Result writes v as the result of a call.
func Result(out *wire.Encoder, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return krate.Errorf("internal", "encoding the result: %w", err)
	}
	out.Raw(b)
	return nil
}

// Arg reads the next argument into a value of type T. The first failure
// sticks in r, like the other readers.
func Arg[T any](r *codec.R) (v T) {
	if r.Err() != nil {
		return v
	}
	raw, err := r.D.RawValue()
	if err != nil {
		r.Failf("%v", err)
		return v
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		r.Failf("%v", err)
	}
	return v
}

// Publish sends v to the subscribers of a topic.
func Publish(t *bridge.Topic, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	t.Publish(func(e *wire.Encoder) { e.Raw(b) })
}

func writeArgs(e *wire.Encoder, args []any) {
	for _, a := range args {
		b, err := json.Marshal(a)
		if err != nil {
			e.String("") // the host side rejects it
			continue
		}
		e.Raw(b)
	}
}

// Do calls id with args and ignores the result.
func Do(id string, args ...any) error {
	return bridge.CallDo(context.Background(), id, func(e *wire.Encoder) { writeArgs(e, args) })
}

// Query calls id with args and decodes the result into T.
func Query[T any](id string, args ...any) (v T, err error) {
	d, err := bridge.Call(context.Background(), id, func(e *wire.Encoder) { writeArgs(e, args) })
	if err != nil {
		return v, err
	}
	raw, err := d.RawValue()
	if err != nil {
		return v, err
	}
	err = json.Unmarshal(raw, &v)
	return v, err
}

// Events subscribes to the event stream id and calls fn with each event.
func Events[T any](id string, fn func(T)) (cancel func(), err error) {
	return bridge.Subscribe(context.Background(), id, func(d *wire.Decoder) {
		raw, err := d.RawValue()
		if err != nil {
			return
		}
		var v T
		if json.Unmarshal(raw, &v) == nil {
			fn(v)
		}
	})
}
