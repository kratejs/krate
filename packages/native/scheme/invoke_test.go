package scheme

import (
	"errors"
	"testing"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/wire"
)

func resultJSON(t *testing.T, fr []byte) string {
	t.Helper()
	d, err := wire.NewDecoder(fr)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := d.RawValue()
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestInvoke(t *testing.T) {
	r := NewRegistry()
	r.Func("m.add", func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
		if err := a.BeginArray(); err != nil {
			return err
		}
		x, _ := a.Int()
		y, _ := a.Int()
		if err := a.EndArray(); err != nil {
			return err
		}
		out.Int(x + y)
		return nil
	})
	r.Func("m.panics", func(krate.Ctx, *wire.Decoder, *wire.Encoder) error { panic("boom") })
	r.Func("m.coded", func(krate.Ctx, *wire.Decoder, *wire.Encoder) error {
		return krate.Errorf("nope", "no")
	})
	fr, err := r.Invoke(krate.Background(), "m.add", jsonFrame(t, `[2,40]`))
	if err != nil || resultJSON(t, fr) != "42" {
		t.Fatalf("%v %s", err, fr)
	}
	var ke *krate.Error
	if _, err := r.Invoke(krate.Background(), "m.nope", jsonFrame(t, `[]`)); !errors.As(err, &ke) || ke.Code != "not_found" {
		t.Fatalf("%v", err)
	}
	if _, err := r.Invoke(krate.Background(), "m.panics", jsonFrame(t, `[]`)); !errors.As(err, &ke) || ke.Code != "panic" {
		t.Fatalf("%v", err)
	}
	if _, err := r.Invoke(krate.Background(), "m.coded", jsonFrame(t, `[]`)); !errors.As(err, &ke) || ke.Code != "nope" {
		t.Fatalf("%v", err)
	}
}
