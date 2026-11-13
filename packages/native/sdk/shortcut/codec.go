package shortcut

import (
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

func encodeEvent(e *wire.Encoder, handle string) {
	e.BeginObject()
	e.Key("id")
	e.String(handle)
	e.EndObject()
}

func decodeEvent(d *wire.Decoder) (string, error) {
	r := &codec.R{D: d, Name: "shortcut event"}
	var id string
	r.Object(func(k string) bool {
		if k != "id" {
			return false
		}
		id = r.String()
		return true
	})
	return id, r.Err()
}
