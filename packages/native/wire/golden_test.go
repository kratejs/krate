package wire

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// walk copies the next value from d into e and returns its logical JSON text:
// the wire JSON with each blob inlined as base64.
func walk(d *Decoder, e *Encoder) (string, error) {
	switch d.Kind() {
	case KindObject:
		if err := d.BeginObject(); err != nil {
			return "", err
		}
		e.BeginObject()
		var sb strings.Builder
		sb.WriteByte('{')
		n, firstKey := 0, ""
		for d.More() {
			k, err := d.Key()
			if err != nil {
				return "", err
			}
			if n == 0 {
				firstKey = k
			}
			if n > 0 {
				sb.WriteByte(',')
			}
			n++
			kj, _ := json.Marshal(k)
			sb.Write(kj)
			sb.WriteByte(':')
			e.Key(k)
			v, err := walk(d, e)
			if err != nil {
				return "", err
			}
			sb.WriteString(v)
		}
		if err := d.EndObject(); err != nil {
			return "", err
		}
		e.EndObject()
		sb.WriteByte('}')
		if n == 1 && (firstKey == "$b" || firstKey == "$b$") {
			return `{"$b$":` + sb.String() + `}`, nil
		}
		return sb.String(), nil
	case KindArray:
		if err := d.BeginArray(); err != nil {
			return "", err
		}
		e.BeginArray()
		var sb strings.Builder
		sb.WriteByte('[')
		for i := 0; d.More(); i++ {
			if i > 0 {
				sb.WriteByte(',')
			}
			v, err := walk(d, e)
			if err != nil {
				return "", err
			}
			sb.WriteString(v)
		}
		if err := d.EndArray(); err != nil {
			return "", err
		}
		e.EndArray()
		sb.WriteByte(']')
		return sb.String(), nil
	case KindString:
		s, err := d.String()
		if err != nil {
			return "", err
		}
		e.String(s)
		b, _ := json.Marshal(s)
		return string(b), nil
	case KindNumber:
		raw, err := d.RawValue()
		if err != nil {
			return "", err
		}
		e.Raw(raw)
		return string(raw), nil
	case KindBool:
		v, err := d.Bool()
		if err != nil {
			return "", err
		}
		e.Bool(v)
		return strconv.FormatBool(v), nil
	case KindNull:
		d.IsNull()
		e.Null()
		return "null", nil
	case KindBytes:
		b, err := d.Bytes()
		if err != nil {
			return "", err
		}
		e.Bytes(b)
		return `{"$b":"` + base64.StdEncoding.EncodeToString(b) + `"}`, nil
	case KindTime:
		t, err := d.Time()
		if err != nil {
			return "", err
		}
		e.Time(t)
		return `{"$d":"` + t.Format("2006-01-02T15:04:05.000Z") + `"}`, nil
	}
	return "", d.fail("walk: unexpected input")
}

func vectors(t *testing.T) []string {
	t.Helper()
	m, err := filepath.Glob("testdata/*.bin")
	if err != nil || len(m) < 9 {
		t.Fatalf("vectors: %v %d", err, len(m))
	}
	return m
}

func TestGoldenVectors(t *testing.T) {
	for _, bin := range vectors(t) {
		name := strings.TrimSuffix(filepath.Base(bin), ".bin")
		t.Run(name, func(t *testing.T) {
			frame, err := os.ReadFile(bin)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(strings.TrimSuffix(bin, ".bin") + ".json")
			if err != nil {
				t.Fatal(err)
			}
			d, err := NewDecoder(frame)
			if err != nil {
				t.Fatal(err)
			}
			e := NewEncoder()
			logical, err := walk(d, e)
			if err != nil {
				t.Fatal(err)
			}
			if err := d.Finish(); err != nil {
				t.Fatal(err)
			}
			if !sameJSON(t, logical, string(want)) {
				t.Errorf("logical value\n got %s\nwant %s", logical, want)
			}
			got, err := e.Finish()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, frame) {
				t.Errorf("re-encode differs\n got %x\nwant %x", got, frame)
			}
		})
	}
}

func TestGoldenStream(t *testing.T) {
	var all bytes.Buffer
	files := vectors(t)
	for _, f := range files {
		b, _ := os.ReadFile(f)
		all.Write(b)
	}
	n := 0
	err := ReadFrames(&all, func(d *Decoder) error {
		n++
		return d.Skip()
	})
	if err != nil || n != len(files) {
		t.Fatalf("ReadFrames n=%d err=%v", n, err)
	}
}

// sameJSON compares two JSON texts structurally (key order is checked by the
// byte-for-byte re-encode, string escaping is not significant here).
func sameJSON(t *testing.T, a, b string) bool {
	t.Helper()
	var x, y any
	for i, s := range []string{a, b} {
		dec := json.NewDecoder(strings.NewReader(s))
		dec.UseNumber()
		p := &x
		if i == 1 {
			p = &y
		}
		if err := dec.Decode(p); err != nil {
			t.Fatalf("bad json %q: %v", s, err)
		}
	}
	return reflect.DeepEqual(x, y)
}
