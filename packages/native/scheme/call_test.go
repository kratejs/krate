package scheme

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/wire"
)

func intFunc(v int64) Func {
	return func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
		out.Int(v)
		return nil
	}
}

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
		var m map[string]int
		m["x"] = 1
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

func TestMalformedRequests(t *testing.T) {
	_, ts := newTestServer(t, Options{})
	bad := map[string][]byte{
		"garbage":       []byte("not a frame at all"),
		"empty":         nil,
		"truncated":     callsFrame(t, callSpec{id: 1, fn: "x"})[:10],
		"not object":    jsonFrame(t, `[1]`),
		"missing fn":    jsonFrame(t, `{"calls":[{"id":1,"args":[]}]}`),
		"missing id":    jsonFrame(t, `{"calls":[{"fn":"x"}]}`),
		"duplicate ids": callsFrame(t, callSpec{id: 1, fn: "x"}, callSpec{id: 1, fn: "y"}),
		"bad json":      jsonFrame(t, `{"calls":[{"id":1,"fn":"x","args":[1,]}]}`),
	}
	for name, body := range bad {
		t.Run(name, func(t *testing.T) {
			resp := post(t, ts, "/__krate/call", "", body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status %d", resp.StatusCode)
			}
			d := next(t, resp.Body)
			d.BeginObject()
			d.Key()
			e := parseErr(t, d)
			if e.Code != "bad_request" || e.Message == "" {
				t.Fatalf("%+v", e)
			}
		})
	}
}

func TestMethodAndPathMismatch(t *testing.T) {
	_, ts := newTestServer(t, Options{Dev: false, HMR: func(http.ResponseWriter, *http.Request) {}})
	tests := []struct {
		method, path string
		want         int
	}{
		{"GET", "/__krate/call", 405},
		{"POST", "/__krate/signals?names=a", 405},
		{"POST", "/__krate/stream/s1", 405},
		{"GET", "/__krate/cancel", 405},
		{"GET", "/__krate/unknown", 404},
		{"GET", "/__krate/hmr", 404}, // not Dev
		{"POST", "/", 405},
		{"DELETE", "/index.html", 405},
	}
	for _, tc := range tests {
		req, _ := http.NewRequest(tc.method, ts.URL+tc.path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("%s %s: %d, want %d", tc.method, tc.path, resp.StatusCode, tc.want)
		}
		if resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("Content-Security-Policy") == "" {
			t.Errorf("%s %s: security headers missing", tc.method, tc.path)
		}
	}
}

func TestHMRMountedInDev(t *testing.T) {
	called := make(chan struct{}, 1)
	_, ts := newTestServer(t, Options{Dev: true, HMR: func(w http.ResponseWriter, r *http.Request) {
		called <- struct{}{}
		w.WriteHeader(http.StatusTeapot)
	}})
	resp, err := http.Get(ts.URL + "/__krate/hmr")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	recv(t, called, "HMR handler")
	if resp.StatusCode != http.StatusTeapot {
		t.Fatal(resp.StatusCode)
	}
}

func TestWindowReachesCtx(t *testing.T) {
	reg := NewRegistry()
	reg.Func("m.Window", func(ctx krate.Ctx, _ *wire.Decoder, out *wire.Encoder) error {
		out.String(ctx.Window())
		return nil
	})
	_, ts := newTestServer(t, Options{Registry: reg})
	for _, w := range []string{"main", "", "settings"} {
		resp := post(t, ts, "/__krate/call", w, callsFrame(t, callSpec{id: 1, fn: "m.Window"}))
		r := parseResult(t, next(t, resp.Body))
		if s, _ := r.OK.String(); s != w {
			t.Errorf("window %q, got %q", w, s)
		}
	}
}

// A native identity replaces the header the page sets: a request the platform
// tagged with a view cannot name another window, and one the platform could
// not tag is not trusted either. Without a native identity the header holds.
func TestIdentifyOverridesTheWindowHeader(t *testing.T) {
	reg := NewRegistry()
	reg.Func("m.Window", func(ctx krate.Ctx, _ *wire.Decoder, out *wire.Encoder) error {
		out.String(ctx.Window())
		return nil
	})
	_, ts := newTestServer(t, Options{Registry: reg, Identify: func(r *http.Request) (string, bool) {
		switch r.Header.Get("x-test-native") {
		case "":
			return "", false
		case "unknown":
			return "?unknown", true
		}
		return "scoped", true
	}})
	call := func(native, header string) string {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/__krate/call", bytes.NewReader(callsFrame(t, callSpec{id: 1, fn: "m.Window"})))
		req.Header.Set("x-krate-window", header)
		if native != "" {
			req.Header.Set("x-test-native", native)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		s, _ := parseResult(t, next(t, resp.Body)).OK.String()
		return s
	}
	if got := call("yes", "main"); got != "scoped" {
		t.Errorf("forged header won: %q", got)
	}
	if got := call("unknown", "main"); got != "?unknown" {
		t.Errorf("unknown native view fell back to the header: %q", got)
	}
	if got := call("", "main"); got != "main" {
		t.Errorf("no native identity: want the header, got %q", got)
	}
}

func TestArgsDecoding(t *testing.T) {
	reg := NewRegistry()
	reg.Func("m.Sum", func(_ krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
		if err := a.BeginArray(); err != nil {
			return err
		}
		var sum int64
		for a.More() {
			v, err := a.Int()
			if err != nil {
				return krate.Errorf("bad_args", "%w", err)
			}
			sum += v
		}
		if err := a.EndArray(); err != nil {
			return err
		}
		out.Int(sum)
		return nil
	})
	_, ts := newTestServer(t, Options{Registry: reg})
	resp := post(t, ts, "/__krate/call", "", callsFrame(t,
		callSpec{id: 1, fn: "m.Sum", args: args(func(e *wire.Encoder) { e.Int(1); e.Int(2); e.Int(3) })},
		callSpec{id: 2, fn: "m.Sum", args: args(func(e *wire.Encoder) { e.String("x") })},
	))
	got := map[int64]result{}
	for range 2 {
		r := parseResult(t, next(t, resp.Body))
		got[r.ID] = r
	}
	if v, _ := got[1].OK.Int(); v != 6 {
		t.Errorf("sum %d", v)
	}
	if got[2].Err == nil || got[2].Err.Code != "bad_args" {
		t.Errorf("%+v", got[2])
	}
}

func TestBlobsRoundTrip(t *testing.T) {
	reg := NewRegistry()
	reg.Func("m.Echo", func(_ krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
		a.BeginArray()
		out.BeginArray()
		for a.More() {
			b, err := a.Bytes()
			if err != nil {
				return err
			}
			out.Bytes(b)
		}
		out.EndArray()
		return a.EndArray()
	})
	_, ts := newTestServer(t, Options{Registry: reg})
	big := bytes.Repeat([]byte{0xAB, 0x00, 0xFF}, 40000)
	blobs := [][]byte{{1, 2, 3}, {}, big}
	resp := post(t, ts, "/__krate/call", "", callsFrame(t, callSpec{id: 1, fn: "m.Echo", args: args(func(e *wire.Encoder) {
		for _, b := range blobs {
			e.Bytes(b)
		}
	})}))
	r := parseResult(t, next(t, resp.Body))
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	r.OK.BeginArray()
	for i, want := range blobs {
		r.OK.More()
		got, err := r.OK.Bytes()
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("blob %d: err=%v len=%d want %d", i, err, len(got), len(want))
		}
	}
	if r.OK.More() {
		t.Fatal("extra blob")
	}
}

func TestCancelByID(t *testing.T) {
	reg := NewRegistry()
	started := make(chan krate.Ctx, 4)
	reg.Func("m.Wait", func(ctx krate.Ctx, _ *wire.Decoder, out *wire.Encoder) error {
		started <- ctx
		<-ctx.Done()
		return ctx.Err()
	})
	srv, ts := newTestServer(t, Options{Registry: reg})
	resp := post(t, ts, "/__krate/call", "w1", callsFrame(t, callSpec{id: 5, fn: "m.Wait"}))
	callCtx := recv(t, started, "call start")
	if callCtx.Window() != "w1" {
		t.Fatal(callCtx.Window())
	}

	// A cancel from another window must not touch the call.
	c := post(t, ts, "/__krate/cancel", "other", jsonFrame(t, `{"ids":[5]}`))
	if c.StatusCode != http.StatusNoContent {
		t.Fatalf("cancel status %d", c.StatusCode)
	}
	// The cancel request returns after it has acted, so this check is exact.
	if callCtx.Err() != nil {
		t.Fatal("call cancelled from another window")
	}

	c = post(t, ts, "/__krate/cancel", "w1", jsonFrame(t, `{"ids":[5]}`))
	if c.StatusCode != http.StatusNoContent {
		t.Fatalf("cancel status %d", c.StatusCode)
	}
	r := parseResult(t, next(t, resp.Body))
	if r.ID != 5 || r.Err == nil || r.Err.Code != "canceled" {
		t.Fatalf("%+v", r)
	}
	eventually(t, "call registry to empty", func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return len(srv.calls) == 0
	})

	c = post(t, ts, "/__krate/cancel", "w1", []byte("junk"))
	if c.StatusCode != http.StatusBadRequest {
		t.Fatalf("junk cancel: %d", c.StatusCode)
	}
}

func TestRequestCancelCancelsCtx(t *testing.T) {
	reg := NewRegistry()
	started := make(chan struct{})
	ended := make(chan error, 1)
	reg.Func("m.Wait", func(ctx krate.Ctx, _ *wire.Decoder, out *wire.Encoder) error {
		close(started)
		<-ctx.Done()
		ended <- ctx.Err()
		return ctx.Err()
	})
	_, ts := newTestServer(t, Options{Registry: reg})
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/__krate/call", bytes.NewReader(callsFrame(t, callSpec{id: 1, fn: "m.Wait"})))
	errc := make(chan error, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
		errc <- err
	}()
	recv(t, started, "call start")
	cancel()
	if err := recv(t, ended, "ctx cancellation"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	<-errc
}

func TestConcurrencyIsBounded(t *testing.T) {
	reg := NewRegistry()
	var cur, max atomic.Int32
	gate := make(chan struct{})
	reg.Func("m.Hold", func(_ krate.Ctx, _ *wire.Decoder, out *wire.Encoder) error {
		n := cur.Add(1)
		for {
			m := max.Load()
			if n <= m || max.CompareAndSwap(m, n) {
				break
			}
		}
		<-gate
		cur.Add(-1)
		out.Null()
		return nil
	})
	srv := newServer(Options{Registry: reg}, streamTTL)
	srv.sem = make(chan struct{}, 2)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	var specs []callSpec
	for i := range 8 {
		specs = append(specs, callSpec{id: int64(i), fn: "m.Hold"})
	}
	resp := post(t, ts, "/__krate/call", "", callsFrame(t, specs...))
	eventually(t, "two calls running", func() bool { return cur.Load() == 2 })
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 8 {
			parseResult(t, next(t, resp.Body))
		}
	}()
	close(gate)
	wg.Wait()
	if max.Load() != 2 {
		t.Fatalf("max concurrency %d, want 2", max.Load())
	}
}
