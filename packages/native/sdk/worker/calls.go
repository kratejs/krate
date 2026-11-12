package worker

import (
	"context"
	"encoding/json"

	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/wire"
)

func writeJSONArgs(e *wire.Encoder, args []any) {
	for _, a := range args {
		if a == nil {
			continue
		}
		if o, ok := a.(*StartOptions); ok && o == nil {
			continue
		}
		b, err := json.Marshal(a)
		if err != nil {
			e.String("")
			continue
		}
		e.Raw(b)
	}
}

func doJSON(id string, args ...any) error {
	return bridge.CallDo(context.Background(), id, func(e *wire.Encoder) { writeJSONArgs(e, args) })
}

func queryJSON[T any](id string, args ...any) (v T, err error) {
	d, err := bridge.Call(context.Background(), id, func(e *wire.Encoder) { writeJSONArgs(e, args) })
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
