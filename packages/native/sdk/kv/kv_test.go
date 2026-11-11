package kv

import (
	"bytes"
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/db"
	"github.com/kratejs/krate/packages/native/sdk/internal/sdktest"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/wire"
)

var bg = context.Background()

func store(t *testing.T) *Store {
	t.Helper()
	d, err := db.OpenPath(filepath.Join(t.TempDir(), "kv.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return New(d)
}

func TestGoAPI(t *testing.T) {
	s := store(t)
	if _, ok, err := s.Get(bg, "missing"); ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
	if err := s.SetString(bg, "a", "one"); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := s.GetString(bg, "a"); v != "one" || !ok || err != nil {
		t.Fatalf("%q %v %v", v, ok, err)
	}
	s.SetString(bg, "a", "two") // overwrite
	if v, _, _ := s.GetString(bg, "a"); v != "two" {
		t.Fatalf("%q", v)
	}
	blob := []byte{0, 255, 1}
	s.SetBytes(bg, "b", blob)
	if got, ok, err := s.GetBytes(bg, "b"); !ok || err != nil || !bytes.Equal(got, blob) {
		t.Fatalf("%v %v %v", got, ok, err)
	}
	s.SetWith(bg, "c/1", func(e *wire.Encoder) { e.Int(1) })
	s.SetWith(bg, "c/2", func(e *wire.Encoder) { e.Int(2) })
	s.SetString(bg, "cc", "x")
	s.SetString(bg, "c%", "x") // % must not act as a wildcard
	s.SetString(bg, "éa", "x")
	for prefix, want := range map[string][]string{
		"":          {"a", "b", "c%", "c/1", "c/2", "cc", "éa"},
		"c/":        {"c/1", "c/2"},
		"c":         {"c%", "c/1", "c/2", "cc"},
		"c%":        {"c%"},
		"é":         {"éa"},
		"nope":      {},
		"c/1/extra": {},
	} {
		got, err := s.Keys(bg, prefix)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("Keys(%q) = %v, %v; want %v", prefix, got, err, want)
		}
	}
	s.Delete(bg, "a")
	if _, ok, _ := s.Get(bg, "a"); ok {
		t.Fatal("deleted key still there")
	}
	if err := s.Delete(bg, "a"); err != nil {
		t.Fatalf("deleting twice: %v", err)
	}
}

func TestSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kv.db")
	d, _ := db.OpenPath(path)
	New(d).SetString(bg, "k", "v")
	d.Close()
	d, _ = db.OpenPath(path)
	defer d.Close()
	if v, ok, _ := New(d).GetString(bg, "k"); !ok || v != "v" {
		t.Fatal("value lost across reopen")
	}
}

func caller(t *testing.T, s *Store, perms ...string) *sdktest.Caller {
	t.Helper()
	set, err := permission.Parse(perms)
	if err != nil {
		t.Fatal(err)
	}
	reg := scheme.NewRegistry()
	RegisterWith(reg, set, func() (*Store, error) { return s, nil })
	return sdktest.New(t, reg)
}

func TestUIRoundTripOfWireValues(t *testing.T) {
	s := store(t)
	c := caller(t, s)
	when := time.Date(2026, 11, 11, 7, 23, 15, 123e6, time.UTC)

	set := func(key string, write func(*wire.Encoder)) {
		t.Helper()
		r := c.Call("krate/kv.set", func(e *wire.Encoder) { e.String(key); write(e) })
		if r.Failed || r.OK != "null" {
			t.Fatalf("set %s: %+v", key, r)
		}
	}
	set("str", func(e *wire.Encoder) { e.String("héllo") })
	set("num", func(e *wire.Encoder) { e.Float(1.5) })
	set("null", func(e *wire.Encoder) { e.Null() })
	set("bytes", func(e *wire.Encoder) { e.Bytes([]byte{1, 2, 3}) })
	set("date", func(e *wire.Encoder) { e.Time(when) })
	set("nested", func(e *wire.Encoder) {
		e.BeginObject()
		e.Key("when")
		e.Time(when)
		e.Key("blobs")
		e.BeginArray()
		e.Bytes([]byte{7})
		e.Bytes([]byte{8, 9})
		e.EndArray()
		e.Key("map")
		e.BeginMap()
		e.BeginArray()
		e.String("k")
		e.Int(1)
		e.EndArray()
		e.EndMap()
		e.Key("set")
		e.BeginSet()
		e.Int(1)
		e.Int(2)
		e.EndSet()
		e.Key("$d")
		e.String("escaped")
		e.EndObject()
	})

	if r := c.Call("krate/kv.get", str("str")); r.OK != `"héllo"` && r.OK != "\"héllo\"" {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/kv.get", str("num")); r.OK != "1.5" {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/kv.get", str("null")); r.OK != "null" {
		t.Fatalf("%+v", r)
	}
	r := c.Call("krate/kv.get", str("bytes"))
	if got, err := r.OKDec.Bytes(); err != nil || !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("%v %v", got, err)
	}
	if r := c.Call("krate/kv.get", str("date")); r.OK != `{"$d":"2026-11-11T07:23:15.123Z"}` {
		t.Fatalf("%+v", r)
	}
	r = c.Call("krate/kv.get", str("nested"))
	want := `{"when":{"$d":"2026-11-11T07:23:15.123Z"},"blobs":[{"$b":0},{"$b":1}],"map":{"$m":[["k",1]]},"set":{"$s":[1,2]},"$d":"escaped"}`
	// A lone "$d" key among others is a plain key; the object has several keys.
	if r.OK != want {
		t.Fatalf("got  %s\nwant %s", r.OK, want)
	}
	// The blobs come back intact.
	d := r.OKDec
	d.BeginObject()
	d.Key()
	d.Skip()
	d.Key()
	d.BeginArray()
	b1, _ := d.Bytes()
	b2, _ := d.Bytes()
	if !bytes.Equal(b1, []byte{7}) || !bytes.Equal(b2, []byte{8, 9}) {
		t.Fatalf("%v %v", b1, b2)
	}

	if r := c.Call("krate/kv.get", str("missing")); r.OK != "null" {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/kv.keys", nil); r.OK != `["bytes","date","nested","null","num","str"]` {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/kv.keys", str("n")); r.OK != `["nested","null","num"]` {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/kv.delete", str("str")); r.Failed {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/kv.get", str("str")); r.OK != "null" {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/kv.set", str("only-key")); r.ErrCode != "bad_args" || !strings.Contains(r.ErrMessage, "value") {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/kv.set", func(e *wire.Encoder) { e.String("k"); e.Int(1); e.Int(2) }); r.ErrCode != "bad_args" {
		t.Fatalf("extra argument: %+v", r)
	}
}

func str(s string) func(*wire.Encoder) { return func(e *wire.Encoder) { e.String(s) } }

func TestUIDenied(t *testing.T) {
	c := caller(t, store(t), "db")
	r := c.Call("krate/kv.get", str("a"))
	if r.ErrCode != "permission_denied" || r.ErrData != `{"permission":"kv"}` {
		t.Fatalf("%+v", r)
	}
}
