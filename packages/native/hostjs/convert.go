package hostjs

import (
	"encoding/binary"
	"fmt"

	"github.com/kratejs/krate/packages/native/wire"
)

// copyValue reads one value from d and writes it to e, unchanged apart from
// re-indexing blobs. It walks generically: the shape of a JS value is only
// known at run time.
func copyValue(d *wire.Decoder, e *wire.Encoder) error {
	switch d.Kind() {
	case wire.KindObject:
		if err := d.BeginObject(); err != nil {
			return err
		}
		e.BeginObject()
		for d.More() {
			k, err := d.Key()
			if err != nil {
				return err
			}
			e.Key(k)
			if err := copyValue(d, e); err != nil {
				return err
			}
		}
		e.EndObject()
		return d.EndObject()
	case wire.KindArray:
		if err := d.BeginArray(); err != nil {
			return err
		}
		e.BeginArray()
		for d.More() {
			if err := copyValue(d, e); err != nil {
				return err
			}
		}
		e.EndArray()
		return d.EndArray()
	case wire.KindMap:
		if err := d.BeginMap(); err != nil {
			return err
		}
		e.BeginMap()
		for d.More() {
			if err := copyValue(d, e); err != nil {
				return err
			}
		}
		e.EndMap()
		return d.EndArray()
	case wire.KindSet:
		if err := d.BeginSet(); err != nil {
			return err
		}
		e.BeginSet()
		for d.More() {
			if err := copyValue(d, e); err != nil {
				return err
			}
		}
		e.EndSet()
		return d.EndArray()
	case wire.KindBytes:
		b, err := d.Bytes()
		if err != nil {
			return err
		}
		e.Bytes(b)
		return nil
	case wire.KindTime:
		t, err := d.Time()
		if err != nil {
			return err
		}
		e.Time(t)
		return nil
	case wire.KindString, wire.KindNumber, wire.KindBool, wire.KindNull:
		raw, err := d.RawValue()
		if err != nil {
			return err
		}
		e.Raw(raw)
		return nil
	}
	if err := d.Err(); err != nil {
		return err
	}
	return fmt.Errorf("hostjs: unexpected value at offset %d", d.Offset())
}

// splitFrame returns the JSON and blobs of a frame built by an Encoder. The
// results alias frame.
func splitFrame(frame []byte) (json []byte, blobs [][]byte, err error) {
	bad := fmt.Errorf("hostjs: malformed frame")
	if len(frame) < 6 {
		return nil, nil, bad
	}
	n := int(binary.BigEndian.Uint32(frame))
	if len(frame) < 4+n+2 {
		return nil, nil, bad
	}
	json = frame[4 : 4+n]
	p := 4 + n
	k := int(binary.BigEndian.Uint16(frame[p:]))
	p += 2
	for range k {
		if len(frame) < p+4 {
			return nil, nil, bad
		}
		m := int(binary.BigEndian.Uint32(frame[p:]))
		p += 4
		if len(frame) < p+m {
			return nil, nil, bad
		}
		blobs = append(blobs, frame[p:p+m])
		p += m
	}
	return json, blobs, nil
}

// buildFrame assembles a frame from JSON and blobs.
func buildFrame(json []byte, blobs [][]byte) []byte {
	size := 4 + len(json) + 2
	for _, b := range blobs {
		size += 4 + len(b)
	}
	out := make([]byte, 0, size)
	out = binary.BigEndian.AppendUint32(out, uint32(len(json)))
	out = append(out, json...)
	out = binary.BigEndian.AppendUint16(out, uint16(len(blobs)))
	for _, b := range blobs {
		out = binary.BigEndian.AppendUint32(out, uint32(len(b)))
		out = append(out, b...)
	}
	return out
}

// packArgs turns the call arguments (an array, positioned at its start) into
// the JSON the JS side parses and the blobs it fetches by index. Without
// blobs the JSON is the argument text as received.
func packArgs(args *wire.Decoder) (string, [][]byte, error) {
	if args.Blobs() == 0 {
		raw, err := args.RawValue()
		if err != nil {
			return "", nil, err
		}
		return string(raw), nil, args.Finish()
	}
	e := wire.NewEncoder()
	if err := copyValue(args, e); err != nil {
		return "", nil, err
	}
	if err := args.Finish(); err != nil {
		return "", nil, err
	}
	frame, err := e.Finish()
	if err != nil {
		return "", nil, err
	}
	js, blobs, err := splitFrame(frame)
	if err != nil {
		return "", nil, err
	}
	return string(js), blobs, nil
}
