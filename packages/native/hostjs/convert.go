package hostjs

import (
	"encoding/binary"
	"fmt"

	"github.com/kratejs/krate/packages/native/wire"
)

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
	if err := wire.Copy(e, args); err != nil {
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
