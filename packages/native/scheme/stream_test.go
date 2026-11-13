package scheme

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/wire"
)

// openStream calls fn (a stream) and returns its id.
func openStream(t testing.TB, ts *httptest.Server, window, fn string, a func(*wire.Encoder)) string {
	t.Helper()
	resp := post(t, ts, "/__krate/call", window, callsFrame(t, callSpec{id: 1, fn: fn, args: a}))
	r := parseResult(t, next(t, resp.Body))
	if r.Stream == "" {
		t.Fatalf("not a stream result: %+v", r)
	}
	return r.Stream
}

func getStream(t testing.TB, ts *httptest.Server, id string) *http.Response {
	t.Helper()
	resp, err := http.Get(ts.URL + "/__krate/stream/" + id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// frameKind reads a stream frame and returns its first key and, for "v", the
// decoder positioned at the value.
func frameKind(t testing.TB, d *wire.Decoder) (string, *wire.Decoder) {
	t.Helper()
	if err := d.BeginObject(); err != nil {
		t.Fatal(err)
	}
	k, err := d.Key()
	if err != nil {
		t.Fatal(err)
	}
	return k, d
}

func TestStreamValuesThenEnd(t *testing.T) {
	reg := NewRegistry()
	reg.Stream("m.Count", func(ctx krate.Ctx, a *wire.Decoder, emit func(func(*wire.Encoder)) error) error {
		if err := a.BeginArray(); err != nil {
			t.Error(err)
		}
		n, _ := a.Int()
		for i := int64(1); i <= n; i++ {
			if err := emit(func(e *wire.Encoder) { e.Int(i) }); err != nil {
				return err
			}
		}
		return nil
	})
	_, ts := newTestServer(t, Options{Registry: reg})
	id := openStream(t, ts, "w", "m.Count", args(func(e *wire.Encoder) { e.Int(3) }))
	if id == "" || id[0] != 's' {
		t.Fatal(id)
	}
	resp := getStream(t, ts, id)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/x-krate-frames" {
		t.Fatalf("%d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	for want := int64(1); want <= 3; want++ {
		k, d := frameKind(t, next(t, resp.Body))
		if k != "v" {
			t.Fatalf("key %q", k)
		}
		if v, _ := d.Int(); v != want {
			t.Fatalf("got %d want %d", v, want)
		}
	}
	k, d := frameKind(t, next(t, resp.Body))
	if v, _ := d.Bool(); k != "end" || !v {
		t.Fatalf("want end, got %q", k)
	}
	if _, err := wire.ReadFrame(resp.Body); err != io.EOF {
		t.Fatalf("want EOF after end, got %v", err)
	}
	if again := getStream(t, ts, id); again.StatusCode != http.StatusNotFound {
		t.Fatalf("stream ids are single use, got %d", again.StatusCode)
	}
}
