package worker

import (
	"context"
	"encoding/json"

	"github.com/kratejs/krate/packages/native/gohost"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/wire"
)

// Serve is the whole main of a Go worker:
//
//	func main() { worker.Serve(func(m worker.Message) { ... worker.Emit(...) }) }
//
// handle runs for each message the host posts, one at a time and in order. It
// never returns normally; the process exits when the shell stops it.
func Serve(handle func(Message)) {
	gohost.Serve(func(reg *scheme.Registry) { RegisterHandler(reg, handle) })
}

// RegisterHandler adds the message function to a registry, for a Go worker that
// builds its own gohost main.
func RegisterHandler(reg *scheme.Registry, handle func(Message)) {
	var queue = make(chan Message, 256)
	go func() {
		for m := range queue {
			handle(m)
		}
	}()
	reg.Func(MessageFn, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := args.BeginArray(); err != nil {
			return err
		}
		var m Message
		if args.Kind() == wire.KindBytes {
			m.Bytes, _ = args.Bytes()
		} else {
			raw, err := args.RawValue()
			if err != nil {
				return err
			}
			m.Data = append(json.RawMessage(nil), raw...)
		}
		if err := args.EndArray(); err != nil {
			return err
		}
		select {
		case queue <- m:
		case <-ctx.Done():
			return ctx.Err()
		}
		out.Null()
		return nil
	})
}

// Emit sends a JSON-encodable value from a Go worker to the host.
func Emit(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return bridge.CallDo(context.Background(), IDEmit, func(e *wire.Encoder) { e.Raw(b) })
}

// EmitBytes sends binary data from a Go worker to the host.
func EmitBytes(data []byte) error {
	return bridge.CallDo(context.Background(), IDEmit, func(e *wire.Encoder) { e.Bytes(data) })
}
