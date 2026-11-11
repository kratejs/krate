package codec

import (
	"testing"

	"github.com/kratejs/krate/packages/native/wire"
)

func args(t *testing.T, fn func(e *wire.Encoder)) *wire.Decoder {
	d, err := EncodeArgs(fn)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestReaderRejectsExtraAndMissingArguments(t *testing.T) {
	d := args(t, func(e *wire.Encoder) { e.String("a"); e.String("extra") })
	r := Args(d, "f")
	if r.String() != "a" {
		t.Fatal("first arg")
	}
	if err := r.Done(); err == nil {
		t.Fatal("extra argument accepted")
	}
	r = Args(args(t, nil), "f")
	_ = r.String()
	if err := r.Done(); err == nil {
		t.Fatal("missing argument accepted")
	}
}

func TestObjectUnknownKeysAreSkipped(t *testing.T) {
	d := args(t, func(e *wire.Encoder) {
		e.BeginObject()
		e.Key("known")
		e.Int(5)
		e.Key("unknown")
		e.BeginArray()
		e.String("x")
		e.EndArray()
		e.EndObject()
	})
	r := Args(d, "f")
	var n int64
	r.Object(func(k string) bool {
		if k == "known" {
			n = r.Int()
			return true
		}
		return false
	})
	if err := r.Done(); err != nil || n != 5 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestWrongTypeIsBadArgs(t *testing.T) {
	r := Args(args(t, func(e *wire.Encoder) { e.Int(3) }), "f")
	_ = r.String()
	err := r.Done()
	if err == nil || err.Error() == "" {
		t.Fatal("type error lost")
	}
}
