package scheme

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/wire"
)

type callSpec struct {
	id   int64
	fn   string
	args func(e *wire.Encoder) // writes the array; nil means []
}

func callsFrame(t testing.TB, specs ...callSpec) []byte {
	t.Helper()
	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("calls")
	e.BeginArray()
	for _, c := range specs {
		e.BeginObject()
		e.Key("id")
		e.Int(c.id)
		e.Key("fn")
		e.String(c.fn)
		e.Key("args")
		if c.args != nil {
			c.args(e)
		} else {
			e.BeginArray()
			e.EndArray()
		}
		e.EndObject()
	}
	e.EndArray()
	e.EndObject()
	fr, err := e.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return fr
}

func jsonFrame(t testing.TB, js string) []byte {
	t.Helper()
	e := wire.NewEncoder()
	e.Raw([]byte(js))
	return e.Frame()
}

func args(f func(e *wire.Encoder)) func(*wire.Encoder) {
	return func(e *wire.Encoder) {
		e.BeginArray()
		f(e)
		e.EndArray()
	}
}

func newTestServer(t testing.TB, o Options) (*server, *httptest.Server) {
	t.Helper()
	srv := newServer(o, streamTTL)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return srv, ts
}

func post(t testing.TB, ts *httptest.Server, path, window string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, ts.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if window != "" {
		req.Header.Set("x-krate-window", window)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// next reads one frame from r or fails after a timeout.
func next(t testing.TB, r io.Reader) *wire.Decoder {
	t.Helper()
	type res struct {
		d   *wire.Decoder
		err error
	}
	ch := make(chan res, 1)
	go func() {
		d, err := wire.ReadFrame(r)
		ch <- res{d, err}
	}()
	select {
	case x := <-ch:
		if x.err != nil {
			t.Fatalf("reading frame: %v", x.err)
		}
		return x.d
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a frame")
		return nil
	}
}

// result is a decoded call result frame.
type result struct {
	ID     int64
	OK     *wire.Decoder // positioned at the ok value
	Err    *errInfo
	Stream string
}

type errInfo struct {
	Code, Message string
	Data          string // raw JSON
}

func parseResult(t testing.TB, d *wire.Decoder) result {
	t.Helper()
	var r result
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(d.BeginObject())
	for d.More() {
		k, err := d.Key()
		must(err)
		switch k {
		case "id":
			r.ID, err = d.Int()
		case "ok":
			r.OK, err = d.SubDecoder()
		case "stream":
			r.Stream, err = d.String()
		case "err":
			r.Err = parseErr(t, d)
		default:
			t.Fatalf("unexpected key %q", k)
		}
		must(err)
	}
	must(d.EndObject())
	return r
}

func parseErr(t testing.TB, d *wire.Decoder) *errInfo {
	t.Helper()
	e := &errInfo{}
	if err := d.BeginObject(); err != nil {
		t.Fatal(err)
	}
	for d.More() {
		k, err := d.Key()
		if err != nil {
			t.Fatal(err)
		}
		switch k {
		case "code":
			e.Code, err = d.String()
		case "message":
			e.Message, err = d.String()
		case "data":
			var raw []byte
			raw, err = d.RawValue()
			e.Data = string(raw)
		default:
			err = d.Skip()
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := d.EndObject(); err != nil {
		t.Fatal(err)
	}
	return e
}

func eventually(t testing.TB, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func recv[T any](t testing.TB, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		panic("unreachable")
	}
}

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }
