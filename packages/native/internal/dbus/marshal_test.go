package dbus

import (
	"bytes"
	"encoding/hex"
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

func TestMarshalBytes(t *testing.T) {
	tests := []struct {
		name string
		sig  string
		vals []any
		want string
	}{
		{"byte then u32 aligns to 4", "yu", []any{byte(1), uint32(2)}, "01000000" + "02000000"},
		{"string", "s", []any{"hi"}, "02000000" + "686900"},
		{"byte then string", "ys", []any{byte(7), "a"}, "07000000" + "01000000" + "6100"},
		{"signature", "g", []any{Signature("a{sv}")}, "05" + "617b73767d" + "00"},
		{"bool is u32", "b", []any{true}, "01000000"},
		{"int16 then int64", "nx", []any{int16(-1), int64(2)}, "ffff000000000000" + "0200000000000000"},
		{"double", "d", []any{1.5}, "000000000000f83f"},
		{"empty array of u32 keeps length zero", "au", []any{[]uint32{}}, "00000000"},
		{"array of u32", "au", []any{[]uint32{1, 2}}, "08000000" + "01000000" + "02000000"},
		{"array of int64 pads after the length", "ax", []any{[]int64{5}}, "08000000" + "00000000" + "0500000000000000"},
		{"byte array", "ay", []any{[]byte{9, 8}}, "02000000" + "0908"},
		{"struct aligns to 8", "y(yu)", []any{byte(1), []any{byte(2), uint32(3)}}, "01" + "00000000000000" + "02" + "000000" + "03000000"},
		{"variant of string", "v", []any{Variant{"s", "x"}}, "017300" + "00" + "01000000" + "7800"},
		{"dict a{ss}", "a{ss}", []any{map[string]string{"k": "v"}},
			"0e000000" + "00000000" + "01000000" + "6b00" + "0000" + "01000000" + "7600"},
	}
	for _, tc := range tests {
		got := hex.EncodeToString(enc(t, tc.sig, tc.vals...))
		if got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
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

func TestEncodeErrors(t *testing.T) {
	e := &encoder{}
	if err := e.encodeAll("u", []any{"nope"}); err == nil {
		t.Error("string accepted as u")
	}
	if err := e.encodeAll("uu", []any{uint32(1)}); err == nil {
		t.Error("missing value accepted")
	}
	if err := e.encodeAll("s", []any{"a\x00b"}); err == nil {
		t.Error("NUL in string accepted")
	}
	if _, err := splitSig("a{s"); err == nil {
		t.Error("bad signature accepted")
	}
}

func TestDecodeTruncated(t *testing.T) {
	b := enc(t, "s", "hello")
	for n := 0; n < len(b); n++ {
		d := &decoder{buf: b[:n], ord: leOrder}
		if _, err := d.decodeAll("s"); err == nil {
			t.Fatalf("truncated to %d bytes decoded without error", n)
		}
	}
}

func TestMessageRoundTrip(t *testing.T) {
	m := &Message{
		Type: TypeMethodCall, Serial: 7, Destination: "org.example.Svc", Path: "/org/example",
		Interface: "org.example.Iface", Member: "Do", Signature: "sa{sv}",
		Body: []any{"arg", map[string]Variant{"k": {"i", int32(3)}}},
	}
	b, err := m.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadMessage(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != m.Type || got.Serial != 7 || got.Destination != m.Destination || got.Path != m.Path ||
		got.Interface != m.Interface || got.Member != m.Member || got.Signature != m.Signature {
		t.Fatalf("header mismatch: %+v", got)
	}
	if !looselyEqual(got.Body, m.Body) {
		t.Fatalf("body mismatch: %#v", got.Body)
	}

	r := &Message{Type: TypeMethodReturn, ReplySerial: 7, Destination: ":1.5", Signature: "u", Body: []any{uint32(1)}}
	b, _ = r.Marshal()
	got, err = ReadMessage(bytes.NewReader(b))
	if err != nil || got.ReplySerial != 7 || got.Body[0] != uint32(1) {
		t.Fatalf("return: %+v %v", got, err)
	}
}

func TestHelloWireForm(t *testing.T) {
	// The Hello call as every client sends it: header only, 8-aligned.
	m := &Message{Type: TypeMethodCall, Serial: 1, Destination: BusName, Path: BusPath, Interface: BusInterface, Member: "Hello"}
	b, err := m.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if b[0] != 'l' || b[1] != 1 || b[3] != 1 {
		t.Fatalf("fixed header: % x", b[:4])
	}
	if len(b)%8 != 0 {
		t.Fatalf("message length %d is not 8-aligned", len(b))
	}
	// Body length 0, serial 1.
	if got := hex.EncodeToString(b[4:12]); got != "0000000001000000" {
		t.Fatalf("body length and serial: %s", got)
	}
	// First field: PATH (1), signature "o", string of 21 chars.
	if got := hex.EncodeToString(b[16:24]); got != "0101"+"6f00"+"15000000" {
		t.Fatalf("first field: %s", got)
	}
}

func TestParseAddress(t *testing.T) {
	got, err := parseAddress("unix:path=/run/user/1000/bus,guid=abc;unix:abstract=tmp%20x")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/run/user/1000/bus", "@tmp x"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
	if _, err := parseAddress("tcp:host=x,port=1"); err == nil {
		t.Fatal("tcp accepted")
	}
}
