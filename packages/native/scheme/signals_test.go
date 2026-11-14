package scheme

import (
	"io"
	"net/http"
	"net/http/httptest"
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
	if err := d.EndObject(); err != nil {
		t.Fatal(err)
	}
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
