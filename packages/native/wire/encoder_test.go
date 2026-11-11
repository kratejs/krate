package wire

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestAppendStringMatchesJSON(t *testing.T) {
	cases := []string{
		"", "plain", `q"uote`, `back\slash`, "a/b", "<>&", "\x00\x01\x1f\x7f", "\b\f\n\r\t",
		"héllo", "日本", "\U0001F600", "\xe2\x80\xa8\xe2\x80\xa9", "bad\xffutf8", "\xed\xa0\x80",
	}
	for _, s := range cases {
		var wb bytes.Buffer
		enc := json.NewEncoder(&wb)
		enc.SetEscapeHTML(false)
		enc.Encode(s)
		want := bytes.TrimSuffix(wb.Bytes(), []byte("\n"))
		if got := appendString(nil, s); !bytes.Equal(got, want) {
			t.Errorf("%q: got %s want %s", s, got, want)
		}
	}
}

func TestFloatMatchesJSON(t *testing.T) {
	for _, f := range []float64{0, 1, -1, 1.5, 0.1, 1e-7, 1e-6, 123456789.125, 1e20, 1e21, 1e300, -2.5e-10, math.MaxFloat64, math.SmallestNonzeroFloat64, math.Copysign(0, -1)} {
		want, _ := json.Marshal(f)
		e := NewEncoder()
		e.Float(f)
		fr, err := e.Finish()
		if err != nil {
			t.Fatal(err)
		}
		if got := string(fr[4 : len(fr)-2]); got != string(want) {
			t.Errorf("%v: got %s want %s", f, got, want)
		}
	}
}

func TestEncoderErrors(t *testing.T) {
	cases := map[string]func(e *Encoder){
		"nan":           func(e *Encoder) { e.Float(math.NaN()) },
		"inf":           func(e *Encoder) { e.Float(math.Inf(-1)) },
		"no key":        func(e *Encoder) { e.BeginObject(); e.Int(1) },
		"key no value":  func(e *Encoder) { e.BeginObject(); e.Key("a"); e.EndObject() },
		"two keys":      func(e *Encoder) { e.BeginObject(); e.Key("a"); e.Key("b") },
		"key in array":  func(e *Encoder) { e.BeginArray(); e.Key("a") },
		"two roots":     func(e *Encoder) { e.Int(1); e.Int(2) },
		"unbalanced":    func(e *Encoder) { e.BeginArray() },
		"empty":         func(e *Encoder) {},
		"end mismatch":  func(e *Encoder) { e.BeginArray(); e.EndObject() },
		"year":          func(e *Encoder) { e.Time(time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)) },
		"empty raw":     func(e *Encoder) { e.Raw(nil) },
		"end no begin":  func(e *Encoder) { e.EndArray() },
		"value no root": func(e *Encoder) { e.BeginObject(); e.Key("a"); e.Int(1); e.Int(2) },
	}
	for name, f := range cases {
		e := NewEncoder()
		f(e)
		if _, err := e.Finish(); err == nil {
			t.Errorf("%s: want error", name)
		}
		if e.Frame() != nil {
			t.Errorf("%s: Frame must be nil on error", name)
		}
	}
}

func TestEncoderShapes(t *testing.T) {
	e := NewEncoder()
	e.BeginObject()
	e.Key("a")
	e.BeginArray()
	e.Int(-3)
	e.Uint(7)
	e.Bool(true)
	e.Null()
	e.Bytes([]byte{9})
	e.Time(time.Date(2026, 11, 11, 7, 23, 15, 123999999, time.FixedZone("x", 3600)))
	e.EndArray()
	e.Key("r")
	e.Raw([]byte(`{"k":[1]}`))
	e.EndObject()
	fr, err := e.Finish()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":[-3,7,true,null,{"$b":0},{"$d":"2026-11-11T06:23:15.123Z"}],"r":{"k":[1]}}`
	if got := string(fr[4 : 4+len(want)]); got != want {
		t.Errorf("got %s want %s", got, want)
	}
	var buf bytes.Buffer
	if n, err := e.WriteTo(&buf); err != nil || int(n) != len(fr) || !bytes.Equal(buf.Bytes(), fr) {
		t.Errorf("WriteTo n=%d err=%v", n, err)
	}
	e.Reset()
	e.Null()
	if string(e.Frame()) != "\x00\x00\x00\x04null\x00\x00" {
		t.Errorf("after Reset: %q", e.Frame())
	}
}

func TestSoleKeyEscape(t *testing.T) {
	for _, key := range []string{"$b", "$b$"} {
		e := NewEncoder()
		e.BeginArray()
		e.BeginObject()
		e.Key(key)
		e.Int(1)
		e.EndObject()
		e.BeginObject()
		e.Key(key)
		e.Int(1)
		e.Key("other")
		e.Int(2)
		e.EndObject()
		e.EndArray()
		fr := string(e.Frame())
		want := `[{"$b$":{"` + key + `":1}},{"` + key + `":1,"other":2}]`
		if !strings.Contains(fr, want) {
			t.Fatalf("got %q want %s", fr, want)
		}
		d, err := NewDecoder([]byte(fr))
		if err != nil {
			t.Fatal(err)
		}
		d.BeginArray()
		d.BeginObject()
		k, _ := d.Key()
		v, _ := d.Int()
		if k != key || v != 1 || d.More() {
			t.Fatalf("unwrap: %q %d", k, v)
		}
		if err := d.EndObject(); err != nil {
			t.Fatal(err)
		}
	}
}
