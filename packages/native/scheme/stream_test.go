package scheme

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
		a.BeginArray()
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

func TestStreamError(t *testing.T) {
	reg := NewRegistry()
	reg.Stream("m.Fail", func(ctx krate.Ctx, a *wire.Decoder, emit func(func(*wire.Encoder)) error) error {
		emit(func(e *wire.Encoder) { e.String("first") })
		return krate.Errorf("quota", "out of quota")
	})
	reg.Stream("m.Panic", func(ctx krate.Ctx, a *wire.Decoder, emit func(func(*wire.Encoder)) error) error {
		panic("kaboom")
	})
	reg.Stream("m.BadWrite", func(ctx krate.Ctx, a *wire.Decoder, emit func(func(*wire.Encoder)) error) error {
		return emit(func(e *wire.Encoder) { e.Key("oops") }) // invalid: key outside an object
	})
	_, ts := newTestServer(t, Options{Registry: reg})

	resp := getStream(t, ts, openStream(t, ts, "", "m.Fail", nil))
	if k, _ := frameKind(t, next(t, resp.Body)); k != "v" {
		t.Fatal(k)
	}
	k, d := frameKind(t, next(t, resp.Body))
	if k != "err" {
		t.Fatal(k)
	}
	if e := parseErr(t, d); e.Code != "quota" || e.Message != "out of quota" {
		t.Fatalf("%+v", e)
	}

	for _, fn := range []string{"m.Panic", "m.BadWrite"} {
		resp = getStream(t, ts, openStream(t, ts, "", fn, nil))
		k, d = frameKind(t, next(t, resp.Body))
		if k != "err" {
			t.Fatalf("%s: %q", fn, k)
		}
		want := map[string]string{"m.Panic": "panic", "m.BadWrite": "internal"}[fn]
		if e := parseErr(t, d); e.Code != want {
			t.Fatalf("%s: %+v", fn, e)
		}
	}
}

func TestStreamCancelledWhenReaderCloses(t *testing.T) {
	reg := NewRegistry()
	done := make(chan error, 1)
	reg.Stream("m.Forever", func(ctx krate.Ctx, a *wire.Decoder, emit func(func(*wire.Encoder)) error) error {
		for {
			if err := emit(func(e *wire.Encoder) { e.Int(1) }); err != nil {
				done <- err
				return err
			}
		}
	})
	srv, ts := newTestServer(t, Options{Registry: reg})
	resp := getStream(t, ts, openStream(t, ts, "", "m.Forever", nil))
	next(t, resp.Body)
	resp.Body.Close()
	if err := recv(t, done, "producer to stop"); err == nil {
		t.Fatal("emit must report the cancellation")
	}
	eventually(t, "stream registry to empty", func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return len(srv.streams) == 0
	})
}

func TestStreamCancelByID(t *testing.T) {
	reg := NewRegistry()
	started := make(chan krate.Ctx, 1)
	reg.Stream("m.Block", func(ctx krate.Ctx, a *wire.Decoder, emit func(func(*wire.Encoder)) error) error {
		started <- ctx
		<-ctx.Done()
		return ctx.Err()
	})
	_, ts := newTestServer(t, Options{Registry: reg})
	id := openStream(t, ts, "w1", "m.Block", nil)
	resp := getStream(t, ts, id)
	ctx := recv(t, started, "producer start")
	if ctx.Window() != "w1" {
		t.Fatal(ctx.Window())
	}

	body := jsonFrame(t, `{"streams":["`+id+`"]}`)
	post(t, ts, "/__krate/cancel", "other", body)
	if ctx.Err() != nil {
		t.Fatal("stream cancelled from another window")
	}
	post(t, ts, "/__krate/cancel", "w1", body)
	k, d := frameKind(t, next(t, resp.Body))
	if k != "err" {
		t.Fatal(k)
	}
	if e := parseErr(t, d); e.Code != "canceled" {
		t.Fatalf("%+v", e)
	}
}

func TestStreamExpiresIfNeverOpened(t *testing.T) {
	reg := NewRegistry()
	ran := make(chan struct{}, 1)
	reg.Stream("m.Never", func(ctx krate.Ctx, a *wire.Decoder, emit func(func(*wire.Encoder)) error) error {
		ran <- struct{}{}
		return nil
	})
	srv := newServer(Options{Registry: reg}, 40*time.Millisecond)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	id := openStream(t, ts, "", "m.Never", nil)
	eventually(t, "stream to expire", func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return len(srv.streams) == 0
	})
	resp := getStream(t, ts, id)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expired stream: %d", resp.StatusCode)
	}
	select {
	case <-ran:
		t.Fatal("producer ran although the stream was never opened")
	default:
	}
}

func TestStreamOpenedBeforeExpiryKeepsRunning(t *testing.T) {
	reg := NewRegistry()
	release := make(chan struct{})
	reg.Stream("m.Slow", func(ctx krate.Ctx, a *wire.Decoder, emit func(func(*wire.Encoder)) error) error {
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		return emit(func(e *wire.Encoder) { e.String("late") })
	})
	srv := newServer(Options{Registry: reg}, 30*time.Millisecond)
	ts := httptest.NewServer(srv)
	defer ts.Close()
	resp := getStream(t, ts, openStream(t, ts, "", "m.Slow", nil))
	time.Sleep(100 * time.Millisecond) // well past the expiry; opened streams are exempt
	close(release)
	k, d := frameKind(t, next(t, resp.Body))
	if s, _ := d.String(); k != "v" || s != "late" {
		t.Fatalf("%q %q", k, s)
	}
}

func TestUnknownStream(t *testing.T) {
	_, ts := newTestServer(t, Options{})
	resp := getStream(t, ts, "s999")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatal(resp.StatusCode)
	}
	k, d := frameKind(t, next(t, resp.Body))
	if k != "error" {
		t.Fatal(k)
	}
	if e := parseErr(t, d); e.Code != "not_found" {
		t.Fatalf("%+v", e)
	}
}
