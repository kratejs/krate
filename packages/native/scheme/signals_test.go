package scheme

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/wire"
)

func getSignals(t testing.TB, ts *httptest.Server, names string) *http.Response {
	t.Helper()
	resp, err := http.Get(ts.URL + "/__krate/signals?names=" + names)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

type sigFrame struct {
	Name string
	Int  int64
	Str  string
	Err  *errInfo
}

// readSig reads one {"sig":..,"v"|"err":..} frame; values are int or string.
func readSig(t testing.TB, r io.Reader) sigFrame {
	t.Helper()
	d := next(t, r)
	var f sigFrame
	if err := d.BeginObject(); err != nil {
		t.Fatal(err)
	}
	for d.More() {
		k, _ := d.Key()
		var err error
		switch k {
		case "sig":
			f.Name, err = d.String()
		case "v":
			switch d.Kind() {
			case wire.KindString:
				f.Str, err = d.String()
			default:
				f.Int, err = d.Int()
			}
		case "err":
			f.Err = parseErr(t, d)
		default:
			t.Fatalf("key %q", k)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	d.EndObject()
	return f
}

func intSignal(reg *Registry, name string, s *krate.Signal[int64]) {
	reg.Signal(name, SignalOf(s, func(e *wire.Encoder, v int64) { e.Int(v) }))
}

func TestSignalsInitialAndChanges(t *testing.T) {
	reg := NewRegistry()
	count := krate.NewSignal(int64(1))
	title := krate.NewSignal("hello")
	intSignal(reg, "m.Count", count)
	reg.Signal("m.Title", SignalOf(title, func(e *wire.Encoder, v string) { e.String(v) }))
	_, ts := newTestServer(t, Options{Registry: reg})

	resp := getSignals(t, ts, "m.Count,m.Missing,m.Title")
	if resp.Header.Get("Content-Type") != "application/x-krate-frames" {
		t.Fatal(resp.Header.Get("Content-Type"))
	}
	// The unknown name is answered before any values arrive.
	f := readSig(t, resp.Body)
	if f.Name != "m.Missing" || f.Err == nil || f.Err.Code != "not_found" {
		t.Fatalf("%+v", f)
	}
	got := map[string]sigFrame{}
	for range 2 {
		f := readSig(t, resp.Body)
		got[f.Name] = f
	}
	if got["m.Count"].Int != 1 || got["m.Title"].Str != "hello" {
		t.Fatalf("initial values: %+v", got)
	}

	count.Set(2)
	if f := readSig(t, resp.Body); f.Name != "m.Count" || f.Int != 2 {
		t.Fatalf("%+v", f)
	}
	title.Set("bye")
	if f := readSig(t, resp.Body); f.Name != "m.Title" || f.Str != "bye" {
		t.Fatalf("%+v", f)
	}
}

func TestSignalsReconnectGetsCurrentValue(t *testing.T) {
	reg := NewRegistry()
	count := krate.NewSignal(int64(10))
	var subscribed, cancelled atomic.Int32
	inner := SignalOf(count, func(e *wire.Encoder, v int64) { e.Int(v) })
	reg.Signal("m.Count", func(on func(func(*wire.Encoder))) func() {
		subscribed.Add(1)
		c := inner(on)
		return func() { cancelled.Add(1); c() }
	})
	_, ts := newTestServer(t, Options{Registry: reg})

	first := getSignals(t, ts, "m.Count")
	if f := readSig(t, first.Body); f.Int != 10 {
		t.Fatalf("%+v", f)
	}
	first.Body.Close()
	eventually(t, "subscription to be cancelled", func() bool { return cancelled.Load() == 1 })

	count.Set(11) // while disconnected
	second := getSignals(t, ts, "m.Count")
	if f := readSig(t, second.Body); f.Name != "m.Count" || f.Int != 11 {
		t.Fatalf("reconnect must start from the current value: %+v", f)
	}
	if subscribed.Load() != 2 {
		t.Fatalf("subscribed %d times", subscribed.Load())
	}
}

func TestSignalsFastChangesCoalesce(t *testing.T) {
	reg := NewRegistry()
	count := krate.NewSignal(int64(0))
	intSignal(reg, "m.Count", count)
	_, ts := newTestServer(t, Options{Registry: reg})
	resp := getSignals(t, ts, "m.Count")
	readSig(t, resp.Body)
	for i := int64(1); i <= 200; i++ {
		count.Set(i)
	}
	last, n := int64(0), 0
	for last != 200 {
		f := readSig(t, resp.Body)
		if f.Int <= last {
			t.Fatalf("values must increase: %d after %d", f.Int, last)
		}
		last = f.Int
		n++
	}
	if n > 200 {
		t.Fatalf("%d frames", n)
	}
}

func TestSignalsEdgeCases(t *testing.T) {
	reg := NewRegistry()
	reg.Signal("m.Panics", func(func(func(*wire.Encoder))) func() { panic("no signal for you") })
	reg.Signal("m.BadEncoder", func(on func(func(*wire.Encoder))) func() {
		on(func(e *wire.Encoder) { panic("encoder blew up") })
		return func() {}
	})
	_, ts := newTestServer(t, Options{Registry: reg})

	// Only unknown or failing signals: the response ends after the error frames.
	resp := getSignals(t, ts, "m.Nope,m.Panics")
	if f := readSig(t, resp.Body); f.Name != "m.Nope" || f.Err.Code != "not_found" {
		t.Fatalf("%+v", f)
	}
	if f := readSig(t, resp.Body); f.Name != "m.Panics" || f.Err.Code != "panic" {
		t.Fatalf("%+v", f)
	}
	if _, err := wire.ReadFrame(resp.Body); err != io.EOF {
		t.Fatalf("want EOF, got %v", err)
	}

	resp = getSignals(t, ts, "m.BadEncoder")
	if f := readSig(t, resp.Body); f.Name != "m.BadEncoder" || f.Err == nil || f.Err.Code != "panic" {
		t.Fatalf("%+v", f)
	}

	for _, q := range []string{"", "?names=", "?names=,,"} {
		r, err := http.Get(ts.URL + "/__krate/signals" + q)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != http.StatusBadRequest {
			t.Errorf("%q: %d", q, r.StatusCode)
		}
	}
}
