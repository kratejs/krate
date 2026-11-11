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
		d.EndObject()
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
		d.EndArray()
	case KindString:
		d.String()
	case KindNumber:
		d.RawValue()
		d.Int()
	case KindBool:
		d.Bool()
	case KindNull:
		d.IsNull()
	case KindBytes:
		d.Bytes()
	case KindTime:
		d.Time()
	default:
		d.Skip()
	}
}

func FuzzDecoder(f *testing.F) {
	seeds, _ := filepath.Glob("testdata/*.bin")
	for _, s := range seeds {
		b, _ := os.ReadFile(s)
		f.Add(b)
	}
	f.Add([]byte{})
	f.Add(frameOf(`[1,2,{"a":"\ud800"}]`))
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := NewDecoder(data, WithMaxFrame(1<<20))
		if err != nil {
			return
		}
		consume(d, 0)
		d.Finish()
		for _, fn := range []func(*Decoder){
			func(d *Decoder) { d.Skip() },
			func(d *Decoder) { d.RawValue() },
			func(d *Decoder) { d.SubDecoder() },
			func(d *Decoder) { d.Time() },
			func(d *Decoder) { d.Bytes() },
			func(d *Decoder) { d.Float() },
			func(d *Decoder) { d.Uint() },
			func(d *Decoder) { d.BeginObject(); d.Key(); d.EndObject() },
		} {
			d2, _ := NewDecoder(data)
			fn(d2)
		}
	})
}
