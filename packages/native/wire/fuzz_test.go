package wire

import (
	"os"
	"path/filepath"
	"testing"
)

// consume walks whatever value comes next, using every accessor, and must
// never panic or loop forever on arbitrary input.
func consume(d *Decoder, depth int) {
	if depth > 40 || d.Err() != nil {
		return
	}
	before := d.Offset()
	switch d.Kind() {
	case KindObject:
		if d.BeginObject() != nil {
			return
		}
		for d.More() {
			if _, err := d.Key(); err != nil {
				return
			}
			consume(d, depth+1)
			if d.Err() != nil {
				return
			}
		}
		_ = d.EndObject()
	case KindArray:
		if d.BeginArray() != nil {
			return
		}
		for d.More() {
			consume(d, depth+1)
			if d.Err() != nil {
				return
			}
		}
		_ = d.EndArray()
	case KindSet, KindMap:
		var err error
		if d.Kind() == KindSet {
			err = d.BeginSet()
		} else {
			err = d.BeginMap()
		}
		if err != nil {
			return
		}
		for d.More() {
			consume(d, depth+1)
			if d.Err() != nil {
				return
			}
		}
		_ = d.EndArray()
	case KindString:
		_, _ = d.String()
	case KindNumber:
		_, _ = d.RawValue()
		_, _ = d.Int()
	case KindBool:
		_, _ = d.Bool()
	case KindNull:
		if !d.IsNull() {
			_ = d.Skip()
		}
	case KindBytes:
		_, _ = d.Bytes()
	case KindTime:
		_, _ = d.Time()
	default:
		_ = d.Skip()
	}
	if d.Err() == nil && d.Offset() == before {
		// A value that consumed nothing would loop forever in the callers.
		_ = d.Skip()
	}
}

// The fuzz input is arbitrary, so decode errors are expected and ignored here;
// the targets only check for panics and hangs.
func FuzzDecoder(f *testing.F) {
	seeds, _ := filepath.Glob("testdata/*.bin")
	for _, s := range seeds {
		b, _ := os.ReadFile(s)
		f.Add(b)
	}
	f.Add([]byte{})
	f.Add(frameOf(`{"$m":[["a",1],[2,{"$s":[1]}]],"$s":1}`))
	f.Add(frameOf(`[{"$m":[["a",{"$m$":{"$b":1}}]]},{"$s":[]},{"$d$":{"$d":"x"}}]`))
	f.Add(frameOf(`[1,2,{"a":"\ud800"}]`))
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := NewDecoder(data, WithMaxFrame(1<<20))
		if err != nil {
			return
		}
		consume(d, 0)
		_ = d.Finish()
		for _, fn := range []func(*Decoder){
			func(d *Decoder) { _ = d.Skip() },
			func(d *Decoder) { _, _ = d.RawValue() },
			func(d *Decoder) { _, _ = d.SubDecoder() },
			func(d *Decoder) { _, _ = d.Time() },
			func(d *Decoder) { _, _ = d.Bytes() },
			func(d *Decoder) { _, _ = d.Float() },
			func(d *Decoder) { _, _ = d.Uint() },
			func(d *Decoder) { _ = d.BeginObject(); _, _ = d.Key(); _ = d.Skip(); _ = d.EndObject() },
			func(d *Decoder) {
				_ = d.BeginMap()
				_ = d.BeginArray()
				_ = d.Skip()
				_ = d.Skip()
				_ = d.EndArray()
				_ = d.EndArray()
			},
			func(d *Decoder) { _ = d.BeginSet(); _ = d.Skip(); _ = d.EndArray() },
		} {
			d2, _ := NewDecoder(data)
			fn(d2)
		}
	})
}
