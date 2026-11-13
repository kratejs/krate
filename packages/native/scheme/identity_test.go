package scheme

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/wire"
)

// nativeIdentity stands in for the platform: a request with x-test-view comes
// from that native view, whatever x-krate-window says.
func nativeIdentity(r *http.Request) (string, bool) {
	if v := r.Header.Get("x-test-view"); v != "" {
		return v, true
	}
	return "", false
}

func send(t *testing.T, method, url, view, header string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if view != "" {
		req.Header.Set("x-test-view", view)
	}
	if header != "" {
		req.Header.Set("x-krate-window", header)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// A stream belongs to the view that opened it. Stream ids are a counter, so
// another view (a narrower scope) could otherwise open the stream first and
// read what the producer, running with the opener's permissions, emits.
func TestStreamCannotBeOpenedByAnotherView(t *testing.T) {
	reg := NewRegistry()
	reg.Stream("m.Secret", func(ctx krate.Ctx, _ *wire.Decoder, emit func(func(*wire.Encoder)) error) error {
		return emit(func(e *wire.Encoder) { e.String("for " + ctx.Window()) })
	})
	_, ts := newTestServer(t, Options{Registry: reg, Identify: nativeIdentity})
	call := send(t, http.MethodPost, ts.URL+"/__krate/call", "wide", "", callsFrame(t, callSpec{id: 1, fn: "m.Secret"}))
	id := parseResult(t, next(t, call.Body)).Stream
	if id == "" {
		t.Fatal("no stream id")
	}
	for _, forged := range []string{"", "wide"} {
		resp := send(t, http.MethodGet, ts.URL+"/__krate/stream/"+id, "narrow", forged, nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("the narrow view (header %q) opened the wide view's stream: status %d", forged, resp.StatusCode)
		}
	}
	resp := send(t, http.MethodGet, ts.URL+"/__krate/stream/"+id, "wide", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the opener could not read its stream after the refused attempts: %d", resp.StatusCode)
	}
	k, d := frameKind(t, next(t, resp.Body))
	if s, _ := d.String(); k != "v" || s != "for wide" {
		t.Fatalf("frame %q %q", k, s)
	}
}

// A signal gate gets the window the platform reported, not the header the
// page set: a scoped view cannot claim another window's scope, or none.
func TestSignalGateSeesTheNativeIdentity(t *testing.T) {
	reg := NewRegistry()
	intSignal(reg, "p.Level", krate.NewSignal(int64(7)))
	var seen []string
	reg.SignalGate("p.Level", func(window string) error {
		seen = append(seen, window)
		if window != "wide" {
			return krate.Errorf("permission_denied", "not for %q", window)
		}
		return nil
	})
	_, ts := newTestServer(t, Options{Registry: reg, Identify: nativeIdentity})
	for _, forged := range []string{"", "wide"} {
		resp := send(t, http.MethodGet, ts.URL+"/__krate/signals?names=p.Level", "narrow", forged, nil)
		f := readSig(t, resp.Body)
		if f.Err == nil || f.Err.Code != "permission_denied" {
			t.Fatalf("narrow view with header %q read the gated signal: %+v (gate saw %q)", forged, f, seen)
		}
	}
	resp := send(t, http.MethodGet, ts.URL+"/__krate/signals?names=p.Level", "wide", "", nil)
	if f := readSig(t, resp.Body); f.Err != nil || f.Int != 7 {
		t.Fatalf("wide view: %+v", f)
	}
}
