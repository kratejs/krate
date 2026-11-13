package scheme

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/wire"
)

func TestBatchFastResultArrivesBeforeSlow(t *testing.T) {
	reg := NewRegistry()
	started := make(chan struct{})
	release := make(chan struct{})
	reg.Func("m.Slow", func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
		close(started)
		<-release
		out.String("slow")
		return nil
	})
	reg.Func("m.Fast", func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
		out.String("fast")
		return nil
	})
	_, ts := newTestServer(t, Options{Registry: reg})
	resp := post(t, ts, "/__krate/call", "w1", callsFrame(t, callSpec{id: 1, fn: "m.Slow"}, callSpec{id: 2, fn: "m.Fast"}))
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/x-krate-frames" {
		t.Fatalf("%d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	first := parseResult(t, next(t, resp.Body))
	if first.ID != 2 {
		t.Fatalf("first frame is call %d, want the fast call 2", first.ID)
	}
	if s, err := first.OK.String(); err != nil || s != "fast" {
		t.Fatal(s, err)
	}
	<-started
	close(release)
	second := parseResult(t, next(t, resp.Body))
	if s, _ := second.OK.String(); second.ID != 1 || s != "slow" {
		t.Fatalf("second frame: %+v", second)
	}
	if _, err := wire.ReadFrame(resp.Body); err != io.EOF {
		t.Fatalf("want clean EOF after the last result, got %v", err)
	}
}

type userErr struct{}

func (userErr) Error() string { return "secret detail" }

func TestErrorMapping(t *testing.T) {
	reg := NewRegistry()
	reg.Func("m.Coded", func(krate.Ctx, *wire.Decoder, *wire.Encoder) error {
		return &krate.Error{Code: "not_found", Message: "no such task", Data: map[string]any{"id": 7}}
	})
	reg.Func("m.Wrapped", func(krate.Ctx, *wire.Decoder, *wire.Encoder) error {
		return krate.Errorf("conflict", "while saving: %w", errors.New("disk"))
	})
	reg.Func("m.Plain", func(krate.Ctx, *wire.Decoder, *wire.Encoder) error { return userErr{} })
	reg.Func("m.Panic", func(krate.Ctx, *wire.Decoder, *wire.Encoder) error { panic("boom") })
	reg.Func("m.NilMap", func(krate.Ctx, *wire.Decoder, *wire.Encoder) error {
		writeNilMap(nil)
		return nil
	})
	reg.Func("m.NoValue", func(krate.Ctx, *wire.Decoder, *wire.Encoder) error { return nil })
	reg.Func("m.TwoValues", func(_ krate.Ctx, _ *wire.Decoder, out *wire.Encoder) error {
		out.Int(1)
		out.Int(2)
		return nil
	})
	reg.Func("m.PartialThenErr", func(_ krate.Ctx, _ *wire.Decoder, out *wire.Encoder) error {
		out.BeginArray()
		out.Int(1)
		return krate.Errorf("late", "failed after writing")
	})

	tests := []struct {
		fn       string
		dev      bool
		code     string
		msgHas   string
		msgNot   string
		dataJSON string
	}{
		{fn: "m.Coded", code: "not_found", msgHas: "no such task", dataJSON: `{"id":7}`},
		{fn: "m.Wrapped", code: "conflict", msgHas: "while saving: disk", dataJSON: "null"},
		{fn: "m.Plain", code: "internal", msgHas: "secret detail", dataJSON: "null"},
		{fn: "m.Panic", code: "panic", msgHas: "panic in host function", msgNot: "boom", dataJSON: "null"},
		{fn: "m.Panic", dev: true, code: "panic", msgHas: "boom", dataJSON: "null"},
		{fn: "m.NilMap", dev: true, code: "panic", msgHas: "assignment to entry in nil map", dataJSON: "null"},
		{fn: "m.NoValue", code: "internal", msgHas: "encoding the result", dataJSON: "null"},
		{fn: "m.TwoValues", code: "internal", msgHas: "encoding the result", dataJSON: "null"},
		{fn: "m.PartialThenErr", code: "late", msgHas: "failed after writing", dataJSON: "null"},
		{fn: "m.Missing", code: "not_found", msgHas: `"m.Missing"`, dataJSON: "null"},
	}
	for _, tc := range tests {
		name := tc.fn
		if tc.dev {
			name += "/dev"
		}
		t.Run(name, func(t *testing.T) {
			_, ts := newTestServer(t, Options{Registry: reg, Dev: tc.dev})
			resp := post(t, ts, "/__krate/call", "", callsFrame(t, callSpec{id: 9, fn: tc.fn}))
			if resp.StatusCode != 200 {
				t.Fatalf("status %d: per-call failures must not be HTTP errors", resp.StatusCode)
			}
			r := parseResult(t, next(t, resp.Body))
			if r.ID != 9 || r.Err == nil {
				t.Fatalf("%+v", r)
			}
			if r.Err.Code != tc.code || !strings.Contains(r.Err.Message, tc.msgHas) {
				t.Errorf("got %+v", r.Err)
			}
			if tc.msgNot != "" && strings.Contains(r.Err.Message, tc.msgNot) {
				t.Errorf("message leaks %q: %s", tc.msgNot, r.Err.Message)
			}
			if tc.dev && !strings.Contains(r.Err.Message, "goroutine") {
				t.Errorf("dev panic message has no stack: %s", r.Err.Message)
			}
			if !tc.dev && tc.code == "panic" && strings.Contains(r.Err.Message, "goroutine") {
				t.Errorf("stack leaked outside dev: %s", r.Err.Message)
			}
			if r.Err.Data != tc.dataJSON {
				t.Errorf("data %s, want %s", r.Err.Data, tc.dataJSON)
			}
		})
	}
}

// writeNilMap panics with "assignment to entry in nil map" when m is nil.
func writeNilMap(m map[string]int) { m["x"] = 1 }
