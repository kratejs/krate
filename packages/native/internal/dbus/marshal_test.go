package dbus

import (
	"reflect"
	"testing"
)

func enc(t *testing.T, sig string, vals ...any) []byte {
	t.Helper()
	e := &encoder{}
	if err := e.encodeAll(sig, vals); err != nil {
		t.Fatalf("encode %q: %v", sig, err)
	}
	return e.buf
}

func TestRoundTrip(t *testing.T) {
	tests := []struct {
		sig  string
		vals []any
	}{
		{"ybnqiuxtd", []any{byte(1), true, int16(-2), uint16(3), int32(-4), uint32(5), int64(-6), uint64(7), 8.25}},
		{"sog", []any{"héllo", ObjectPath("/a/b"), Signature("a{sv}")}},
		{"ay", []any{[]byte{1, 2, 3}}},
		{"as", []any{[]string{"a", "bb"}}},
		{"ao", []any{[]ObjectPath{"/x", "/y"}}},
		{"as", []any{[]string{}}},
		{"a{sv}", []any{map[string]Variant{"a": {"s", "x"}, "b": {"u", uint32(9)}, "c": {"ay", []byte{1}}}}},
		{"(oayays)", []any{[]any{ObjectPath("/s"), []byte{1}, []byte("val"), "text/plain"}}},
		{"a(is)", []any{[]any{[]any{int32(1), "a"}, []any{int32(2), "b"}}}},
		{"v", []any{Variant{"a{ss}", map[string]string{"x": "y"}}}},
	}
	for _, tc := range tests {
		b := enc(t, tc.sig, tc.vals...)
		d := &decoder{buf: b, ord: leOrder}
		got, err := d.decodeAll(tc.sig)
		if err != nil {
			t.Fatalf("%s: decode: %v", tc.sig, err)
		}
		if d.pos != len(b) {
			t.Errorf("%s: consumed %d of %d bytes", tc.sig, d.pos, len(b))
		}
		if !looselyEqual(got, tc.vals) {
			t.Errorf("%s: got %#v, want %#v", tc.sig, got, tc.vals)
		}
	}
}

// looselyEqual compares decoded values with the inputs, treating typed maps
// as the generic maps the decoder returns.
func looselyEqual(got, want []any) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if !valEqual(got[i], want[i]) {
			return false
		}
	}
	return true
}

func valEqual(g, w any) bool {
	if gs, ok := g.(Struct); ok {
		ws, ok := w.([]any)
		return ok && looselyEqual(gs, ws)
	}
	if gv, ok := g.(Variant); ok {
		wv, ok := w.(Variant)
		return ok && gv.Sig == wv.Sig && valEqual(gv.Value, wv.Value)
	}
	if reflect.DeepEqual(g, w) {
		return true
	}
	rw := reflect.ValueOf(w)
	if rw.Kind() == reflect.Map {
		gm, ok := g.(map[string]any)
		if !ok || len(gm) != rw.Len() {
			return false
		}
		for _, k := range rw.MapKeys() {
			gv, ok := gm[k.String()]
			if !ok || !valEqual(gv, rw.MapIndex(k).Interface()) {
				return false
			}
		}
		return true
	}
	if rw.Kind() == reflect.Slice {
		gs, ok := g.([]any)
		if !ok || len(gs) != rw.Len() {
			return false
		}
		for i := range gs {
			if !valEqual(gs[i], rw.Index(i).Interface()) {
				return false
			}
		}
		return true
	}
	return false
}
