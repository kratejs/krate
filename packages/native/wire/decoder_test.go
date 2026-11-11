package wire

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func frameOf(js string, blobs ...[]byte) []byte {
	var b []byte
	b = binary.BigEndian.AppendUint32(b, uint32(len(js)))
	b = append(b, js...)
	b = binary.BigEndian.AppendUint16(b, uint16(len(blobs)))
	for _, x := range blobs {
		b = binary.BigEndian.AppendUint32(b, uint32(len(x)))
		b = append(b, x...)
	}
	return b
}

func dec(t *testing.T, js string, blobs ...[]byte) *Decoder {
	t.Helper()
	d, err := NewDecoder(frameOf(js, blobs...))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDecodeStruct(t *testing.T) {
	d := dec(t, ` { "id" : 7 , "title":"a\u00e9\ud83d\ude00\n", "done":false, "n":null, "skip":{"x":[1,{"y":2}]},
	  "tags":["a","b"], "raw":{"$b":1}, "at":{"$d":"2026-11-11T07:23:15.123Z"}, "f":-1.5e2, "u":18446744073709551615 } `,
		[]byte("zero"), []byte("one"))
	if err := d.BeginObject(); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for d.More() {
		k, err := d.Key()
		if err != nil {
			t.Fatal(err)
		}
		seen++
		switch k {
		case "id":
			if v, err := d.Int(); err != nil || v != 7 {
				t.Fatal(v, err)
			}
		case "title":
			if v, err := d.String(); err != nil || v != "aé\U0001F600\n" {
				t.Fatalf("%q %v", v, err)
			}
		case "done":
			if v, err := d.Bool(); err != nil || v {
				t.Fatal(v, err)
			}
		case "n":
			if !d.IsNull() {
				t.Fatal("IsNull")
			}
		case "skip":
			if err := d.Skip(); err != nil {
				t.Fatal(err)
			}
		case "tags":
			d.BeginArray()
			var got []string
			for d.More() {
				s, err := d.String()
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, s)
			}
			if err := d.EndArray(); err != nil || strings.Join(got, ",") != "a,b" {
				t.Fatal(got, err)
			}
		case "raw":
			if b, err := d.Bytes(); err != nil || string(b) != "one" {
				t.Fatalf("%q %v", b, err)
			}
		case "at":
			want := time.Date(2026, 11, 11, 7, 23, 15, 123e6, time.UTC)
			if v, err := d.Time(); err != nil || !v.Equal(want) {
				t.Fatal(v, err)
			}
		case "f":
			if v, err := d.Float(); err != nil || v != -150 {
				t.Fatal(v, err)
			}
		case "u":
			if v, err := d.Uint(); err != nil || v != 18446744073709551615 {
				t.Fatal(v, err)
			}
		}
	}
	if seen != 10 {
		t.Fatalf("saw %d keys", seen)
	}
	if err := d.EndObject(); err != nil {
		t.Fatal(err)
	}
	if err := d.Finish(); err != nil {
		t.Fatal(err)
	}
}

func TestIsNullDoesNotConsumeOther(t *testing.T) {
	d := dec(t, `[1,null,"x"]`)
	d.BeginArray()
	d.More()
	if d.IsNull() {
		t.Fatal("1 is not null")
	}
	if v, _ := d.Int(); v != 1 {
		t.Fatal(v)
	}
	d.More()
	if !d.IsNull() {
		t.Fatal("want null")
	}
	d.More()
	if s, _ := d.String(); s != "x" {
		t.Fatal(s)
	}
	if d.More() {
		t.Fatal("More at end")
	}
	if err := d.EndArray(); err != nil {
		t.Fatal(err)
	}
}

func TestBytesBase64String(t *testing.T) {
	for _, js := range []string{`"AQID"`, `"AQID"`} {
		d := dec(t, js)
		if b, err := d.Bytes(); err != nil || !bytes.Equal(b, []byte{1, 2, 3}) {
			t.Fatal(b, err)
		}
	}
	d := dec(t, `"AQ"`)
	if b, err := d.Bytes(); err != nil || !bytes.Equal(b, []byte{1}) {
		t.Fatal("raw base64", b, err)
	}
}

func TestDecodeErrors(t *testing.T) {
	cases := []struct {
		name string
		js   string
		f    func(d *Decoder) error
	}{
		{"not object", `[1]`, func(d *Decoder) error { return d.BeginObject() }},
		{"string for int", `"a"`, func(d *Decoder) error { _, err := d.Int(); return err }},
		{"float for int", `1.5`, func(d *Decoder) error { _, err := d.Int(); return err }},
		{"int overflow", `9223372036854775808`, func(d *Decoder) error { _, err := d.Int(); return err }},
		{"negative uint", `-1`, func(d *Decoder) error { _, err := d.Uint(); return err }},
		{"leading zero", `01`, func(d *Decoder) error { d.Int(); return d.Finish() }},
		{"bad escape", `"\q"`, func(d *Decoder) error { _, err := d.String(); return err }},
		{"unterminated", `"abc`, func(d *Decoder) error { _, err := d.String(); return err }},
		{"ctl in string", "\"a\nb\"", func(d *Decoder) error { _, err := d.String(); return err }},
		{"blob range", `{"$b":3}`, func(d *Decoder) error { _, err := d.Bytes(); return err }},
		{"blob extra", `{"$b":0,"x":1}`, func(d *Decoder) error { _, err := d.Bytes(); return err }},
		{"bad time", `{"$d":"yesterday"}`, func(d *Decoder) error { _, err := d.Time(); return err }},
		{"bad base64", `"!!"`, func(d *Decoder) error { _, err := d.Bytes(); return err }},
		{"value before key", `{"a":1}`, func(d *Decoder) error { d.BeginObject(); _, err := d.Int(); return err }},
		{"unread member", `{"a":1}`, func(d *Decoder) error { d.BeginObject(); return d.EndObject() }},
		{"unread element", `[1]`, func(d *Decoder) error { d.BeginArray(); return d.EndArray() }},
		{"trailing comma", `[1,]`, func(d *Decoder) error {
			d.BeginArray()
			d.More()
			d.Int()
			d.More()
			return d.Skip()
		}},
		{"missing comma", `[1 2]`, func(d *Decoder) error {
			d.BeginArray()
			d.More()
			d.Int()
			_, err := d.Int()
			return err
		}},
		{"skip garbage", `{"a":tru}`, func(d *Decoder) error { return d.Skip() }},
		{"trailing data", `1 2`, func(d *Decoder) error { d.Int(); return d.Finish() }},
		{"two roots", `1 2`, func(d *Decoder) error { d.Int(); _, err := d.Int(); return err }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := dec(t, c.js, []byte("b"))
			err := c.f(d)
			var we *Error
			if !errors.As(err, &we) {
				t.Fatalf("want *wire.Error, got %v", err)
			}
			if we.Offset < 4 {
				t.Errorf("offset %d should point into the JSON section", we.Offset)
			}
		})
	}
}

func TestStickyError(t *testing.T) {
	d := dec(t, `[1]`)
	if err := d.BeginObject(); err == nil {
		t.Fatal("want error")
	}
	if err := d.BeginArray(); err == nil {
		t.Fatal("error must stick")
	}
	if d.More() {
		t.Fatal("More after error")
	}
}

func TestDepthLimit(t *testing.T) {
	js := strings.Repeat("[", 600) + strings.Repeat("]", 600)
	d := dec(t, js)
	if err := d.Skip(); err == nil {
		t.Fatal("Skip must hit depth limit")
	}
	d = dec(t, js)
	var err error
	for i := 0; i < 600 && err == nil; i++ {
		err = d.BeginArray()
	}
	if err == nil {
		t.Fatal("BeginArray must hit depth limit")
	}
	d, _ = NewDecoder(frameOf(strings.Repeat("[", 10)+strings.Repeat("]", 10)), WithMaxDepth(5))
	if err := d.Skip(); err == nil {
		t.Fatal("custom depth limit")
	}
}

func TestEnvelopeErrors(t *testing.T) {
	good := frameOf(`{}`, []byte("x"))
	bad := map[string][]byte{
		"short":       {0, 0},
		"json len":    {0, 0, 0, 9, '{', '}'},
		"no count":    {0, 0, 0, 2, '{', '}'},
		"blob header": append(frameOf(`{}`)[:len(frameOf(`{}`))-2:len(frameOf(`{}`))-2], 0, 1, 0, 0),
		"blob len":    append(append([]byte{}, good[:len(good)-5]...), 0, 0, 0, 99, 'x'),
		"trailing":    append(append([]byte{}, good...), 0),
	}
	for name, f := range bad {
		if _, err := NewDecoder(f); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if _, err := NewDecoder(good, WithMaxFrame(len(good)-1)); err == nil {
		t.Error("max frame")
	}
	if _, err := NewDecoder(good); err != nil {
		t.Error(err)
	}
}

func TestReadFrameLimits(t *testing.T) {
	// A huge claimed length must be rejected before anything is allocated.
	hdr := []byte{0xff, 0xff, 0xff, 0xff}
	if _, err := ReadFrame(bytes.NewReader(hdr)); err == nil {
		t.Fatal("want limit error")
	}
	if _, err := ReadFrame(bytes.NewReader(hdr[:2])); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("short header: %v", err)
	}
	if _, err := ReadFrame(bytes.NewReader(nil)); err != io.EOF {
		t.Fatalf("empty: %v", err)
	}
	if _, err := ReadFrame(bytes.NewReader(frameOf(`{}`)[:5])); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated: %v", err)
	}
	f := frameOf(`{"a":1}`, make([]byte, 100))
	if _, err := ReadFrame(bytes.NewReader(f), WithMaxFrame(50)); err == nil {
		t.Fatal("blob over limit")
	}
	d, err := ReadFrame(bytes.NewReader(f))
	if err != nil || d.Blobs() != 1 {
		t.Fatal(err)
	}
}

func TestReadFramesStopsOnError(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(frameOf(`1`))
	buf.Write(frameOf(`2`))
	stop := errors.New("stop")
	n := 0
	err := ReadFrames(&buf, func(d *Decoder) error { n++; return stop })
	if !errors.Is(err, stop) || n != 1 {
		t.Fatal(n, err)
	}
}

func TestSubDecoder(t *testing.T) {
	d := dec(t, `{"args":[{"$b":0},"x"],"z":1}`, []byte("blob"))
	d.BeginObject()
	d.More()
	d.Key()
	sub, err := d.SubDecoder()
	if err != nil {
		t.Fatal(err)
	}
	d.More()
	d.Key()
	d.Skip()
	if err := d.EndObject(); err != nil {
		t.Fatal(err)
	}
	if err := sub.BeginArray(); err != nil {
		t.Fatal(err)
	}
	sub.More()
	b, err := sub.Bytes()
	if err != nil || string(b) != "blob" {
		t.Fatal(b, err)
	}
	sub.More()
	if s, _ := sub.String(); s != "x" {
		t.Fatal(s)
	}
	if err := sub.EndArray(); err != nil {
		t.Fatal(err)
	}
	if err := sub.Finish(); err != nil {
		t.Fatal(err)
	}
}

func TestRoundTripRandomish(t *testing.T) {
	e := NewEncoder()
	e.BeginArray()
	for i := 0; i < 1000; i++ {
		e.BeginObject()
		e.Key("i")
		e.Int(int64(i) - 500)
		e.Key("s")
		e.String(strings.Repeat("é\"", i%5))
		e.Key("b")
		e.Bytes(bytes.Repeat([]byte{byte(i)}, i%7))
		e.EndObject()
	}
	e.EndArray()
	d, err := NewDecoder(e.Frame())
	if err != nil {
		t.Fatal(err)
	}
	d.BeginArray()
	for i := 0; d.More(); i++ {
		d.BeginObject()
		for d.More() {
			k, _ := d.Key()
			switch k {
			case "i":
				if v, _ := d.Int(); v != int64(i)-500 {
					t.Fatal(i, v)
				}
			case "s":
				if v, _ := d.String(); v != strings.Repeat("é\"", i%5) {
					t.Fatal(i, v)
				}
			case "b":
				if v, _ := d.Bytes(); !bytes.Equal(v, bytes.Repeat([]byte{byte(i)}, i%7)) {
					t.Fatal(i, v)
				}
			}
		}
		d.EndObject()
	}
	d.EndArray()
	if err := d.Finish(); err != nil {
		t.Fatal(err)
	}
}
