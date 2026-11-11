package scheme

import (
	"context"
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

// A Trusted twin runs for in-process callers while the gated function answers
// the UI.
func TestInvokePrefersTrusted(t *testing.T) {
	r := NewRegistry()
	r.Func("sdk.x", func(krate.Ctx, *wire.Decoder, *wire.Encoder) error {
		return krate.Errorf("permission_denied", "no")
	})
	r.Trusted("sdk.x", func(_ krate.Ctx, _ *wire.Decoder, out *wire.Encoder) error {
		out.String("ok")
		return nil
	})
	fr, err := r.Invoke(krate.Background(), "sdk.x", jsonFrame(t, `[]`))
	if err != nil || resultJSON(t, fr) != `"ok"` {
		t.Fatalf("%v %s", err, fr)
	}
}

func TestInvokeStreamAndSubscribe(t *testing.T) {
	r := NewRegistry()
	r.Stream("m.count", func(ctx krate.Ctx, a *wire.Decoder, emit func(func(*wire.Encoder)) error) error {
		for i := int64(1); i <= 3; i++ {
			if err := emit(func(e *wire.Encoder) { e.Int(i) }); err != nil {
				return err
			}
		}
		return nil
	})
	var got []string
	if err := r.InvokeStream(krate.Background(), "m.count", jsonFrame(t, `[]`), func(fr []byte) error {
		got = append(got, resultJSON(t, fr))
		return nil
	}); err != nil || len(got) != 3 || got[2] != "3" {
		t.Fatalf("%v %v", err, got)
	}
	// Cancelling stops the producer.
	ctx, cancel := context.WithCancel(context.Background())
	n := 0
	err := r.InvokeStream(krate.WithWindow(ctx, ""), "m.count", jsonFrame(t, `[]`), func([]byte) error {
		n++
		cancel()
		return nil
	})
	if n != 1 || !errors.Is(err, context.Canceled) {
		t.Fatalf("n=%d err=%v", n, err)
	}

	sig := krate.NewSignal("a")
	r.Signal("m.sig", SignalOf(sig, func(e *wire.Encoder, s string) { e.String(s) }))
	ch := make(chan string, 4)
	stop, err := r.Subscribe("m.sig", func(fr []byte) { ch <- resultJSON(t, fr) })
	if err != nil {
		t.Fatal(err)
	}
	if first := <-ch; first != `"a"` {
		t.Fatal(first)
	}
	sig.Set("b")
	if second := <-ch; second != `"b"` {
		t.Fatal(second)
	}
	stop()
	if _, err := r.Subscribe("m.none", func([]byte) {}); err == nil {
		t.Fatal("unknown signal")
	}
}
