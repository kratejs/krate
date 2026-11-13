package scheme

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/wire"
)

// remoteChild is a child handler with a real Registry.
type remoteChild struct {
	reg      *Registry
	sig      *krate.Signal[int64]
	slowCtx  chan krate.Ctx
	slowDone chan error
	window   atomic.Value
	streamUp atomic.Int32
	streamCx chan struct{}
}

func newRemoteChild(initial int64) *remoteChild {
	c := &remoteChild{
		reg:      NewRegistry(),
		sig:      krate.NewSignal(initial),
		slowCtx:  make(chan krate.Ctx, 4),
		slowDone: make(chan error, 4),
		streamCx: make(chan struct{}, 4),
	}
	c.reg.Func("c.add", func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
		c.window.Store(ctx.Window())
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
	c.reg.Func("c.rev", func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
		if err := a.BeginArray(); err != nil {
			return err
		}
		b, err := a.Bytes()
		if err != nil {
			return err
		}
		if err := a.EndArray(); err != nil {
			return err
		}
		r := make([]byte, len(b))
		for i := range b {
			r[len(b)-1-i] = b[i]
		}
		out.BeginObject()
		out.Key("rev")
		out.Bytes(r)
		out.Key("at")
		out.Time(time.Date(2026, 11, 11, 7, 23, 15, 123e6, time.UTC))
		out.EndObject()
		return nil
	})
	c.reg.Func("c.fail", func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
		e := krate.Errorf("not_found", "no task %d", 7)
		e.(*krate.Error).Data = map[string]any{"id": 7}
		return e
	})
	c.reg.Func("c.slow", func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
		c.slowCtx <- ctx
		<-ctx.Done()
		c.slowDone <- ctx.Err()
		return ctx.Err()
	})
	c.reg.Stream("c.count", func(ctx krate.Ctx, a *wire.Decoder, emit func(func(*wire.Encoder)) error) error {
		if err := a.BeginArray(); err != nil {
			return err
		}
		n, _ := a.Int()
		_ = a.EndArray()
		c.streamUp.Add(1)
		defer func() { c.streamCx <- struct{}{} }()
		for i := int64(0); i < n; i++ {
			if err := emit(func(e *wire.Encoder) { e.Int(i) }); err != nil {
				return err
			}
		}
		if n < 0 {
			<-ctx.Done()
			return ctx.Err()
		}
		if n == 99 {
			return krate.Errorf("boom", "stream broke")
		}
		return nil
	})
	c.reg.Signal("c.count", SignalOf(c.sig, func(e *wire.Encoder, v int64) { e.Int(v) }))
	return c
}

func (c *remoteChild) serve(t *testing.T) *httptest.Server {
	ts := httptest.NewServer(New(Options{Registry: c.reg, Dev: true}))
	t.Cleanup(ts.Close)
	return ts
}

// parent returns a handler whose Registry forwards to the child at base.
func newParent(t *testing.T, base string) (*Remote, *httptest.Server) {
	t.Helper()
	rem := NewRemote(http.DefaultClient, base)
	reg := NewRegistry()
	rem.RegisterInto(reg, []string{"c.add", "c.rev", "c.fail", "c.slow", "c.missing"}, []string{"c.count"}, []string{"c.count"})
	rem.RegisterInto(reg, []string{"c.add"}, nil, nil) // second call must not panic
	ts := httptest.NewServer(New(Options{Registry: reg, Dev: true}))
	t.Cleanup(ts.Close)
	return rem, ts
}

func callOne(t *testing.T, ts *httptest.Server, window string, spec callSpec) (*wire.Decoder, result) {
	t.Helper()
	resp := post(t, ts, "/__krate/call", window, callsFrame(t, spec))
	d := next(t, resp.Body)
	return d, parseResult(t, d)
}

func TestRemoteFunc(t *testing.T) {
	child := newRemoteChild(1)
	cts := child.serve(t)
	_, pts := newParent(t, cts.URL+"/")

	_, r := callOne(t, pts, "w9", callSpec{id: 5, fn: "c.add", args: args(func(e *wire.Encoder) { e.Int(2); e.Int(40) })})
	if r.ID != 5 || r.Err != nil {
		t.Fatalf("result %+v", r)
	}
	if v, err := r.OK.Int(); err != nil || v != 42 {
		t.Fatalf("add = %d, %v", v, err)
	}
	if w := child.window.Load(); w != "w9" {
		t.Errorf("child saw window %v, want w9", w)
	}
}

func TestRemoteBytesAndTime(t *testing.T) {
	child := newRemoteChild(1)
	cts := child.serve(t)
	_, pts := newParent(t, cts.URL)

	_, r := callOne(t, pts, "", callSpec{id: 1, fn: "c.rev", args: args(func(e *wire.Encoder) { e.Bytes([]byte{1, 2, 3, 4}) })})
	if r.Err != nil {
		t.Fatalf("err %+v", r.Err)
	}
	if err := r.OK.BeginObject(); err != nil {
		t.Fatal(err)
	}
	for r.OK.More() {
		k, _ := r.OK.Key()
		switch k {
		case "rev":
			b, err := r.OK.Bytes()
			if err != nil || string(b) != "\x04\x03\x02\x01" {
				t.Errorf("rev = %v, %v", b, err)
			}
		case "at":
			tm, err := r.OK.Time()
			if err != nil || tm.UnixMilli() != time.Date(2026, 11, 11, 7, 23, 15, 123e6, time.UTC).UnixMilli() {
				t.Errorf("at = %v, %v", tm, err)
			}
		}
	}
}

func TestRemoteErrors(t *testing.T) {
	child := newRemoteChild(1)
	cts := child.serve(t)
	_, pts := newParent(t, cts.URL)

	_, r := callOne(t, pts, "", callSpec{id: 1, fn: "c.fail"})
	if r.Err == nil || r.Err.Code != "not_found" || r.Err.Message != "no task 7" || r.Err.Data != `{"id":7}` {
		t.Errorf("fail: %+v", r.Err)
	}
	// The child does not know c.missing: its not_found passes through.
	_, r = callOne(t, pts, "", callSpec{id: 2, fn: "c.missing"})
	if r.Err == nil || r.Err.Code != "not_found" {
		t.Errorf("missing: %+v", r.Err)
	}
	// Bad arguments are rejected by the child's wrapper, not swallowed.
	_, r = callOne(t, pts, "", callSpec{id: 3, fn: "c.add", args: func(e *wire.Encoder) { e.String("nope") }})
	if r.Err == nil {
		t.Errorf("bad args: want an error")
	}
}

func TestRemoteUnavailable(t *testing.T) {
	rem := NewRemote(nil, "")
	reg := NewRegistry()
	rem.RegisterInto(reg, []string{"c.add"}, nil, nil)
	pts := httptest.NewServer(New(Options{Registry: reg}))
	defer pts.Close()
	_, r := callOne(t, pts, "", callSpec{id: 1, fn: "c.add"})
	if r.Err == nil || r.Err.Code != "unavailable" {
		t.Errorf("no target: %+v", r.Err)
	}

	child := newRemoteChild(1)
	cts := child.serve(t)
	dead := cts.URL
	cts.Close()
	rem.SetTarget(http.DefaultClient, dead)
	_, r = callOne(t, pts, "", callSpec{id: 2, fn: "c.add"})
	if r.Err == nil || r.Err.Code != "unavailable" {
		t.Errorf("dead child: %+v", r.Err)
	}
}

func TestRemoteCancelPropagates(t *testing.T) {
	child := newRemoteChild(1)
	cts := child.serve(t)
	_, pts := newParent(t, cts.URL)

	// Cancel through the parent's own cancel endpoint, the way the UI does.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, pts.URL+"/__krate/call",
		bytesReader(callsFrame(t, callSpec{id: 1, fn: "c.slow"})))
	req.Header.Set("x-krate-window", "w1")
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body) // the test cancels the request
			resp.Body.Close()
		}
	}()
	cctx := recv(t, child.slowCtx, "child call start")
	if cctx.Window() != "w1" {
		t.Errorf("window = %q", cctx.Window())
	}
	post(t, pts, "/__krate/cancel", "w1", jsonFrame(t, `{"ids":[1]}`))
	if err := recv(t, child.slowDone, "child cancel"); !errors.Is(err, context.Canceled) {
		t.Errorf("child ctx err = %v", err)
	}

	// Dropping the parent request cancels the child as well.
	req2, _ := http.NewRequestWithContext(ctx, http.MethodPost, pts.URL+"/__krate/call",
		bytesReader(callsFrame(t, callSpec{id: 2, fn: "c.slow"})))
	ctx2, cancel2 := context.WithCancel(ctx)
	req2 = req2.WithContext(ctx2)
	go func() {
		resp, err := http.DefaultClient.Do(req2)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body) // the test cancels the request
			resp.Body.Close()
		}
	}()
	recv(t, child.slowCtx, "second child call start")
	cancel2()
	if err := recv(t, child.slowDone, "child cancel after disconnect"); !errors.Is(err, context.Canceled) {
		t.Errorf("child ctx err = %v", err)
	}
}

func readStream(t *testing.T, ts *httptest.Server, window string, spec callSpec) (vals []int64, final string, code string) {
	t.Helper()
	_, r := callOne(t, ts, window, spec)
	if r.Err != nil {
		t.Fatalf("stream start: %+v", r.Err)
	}
	if r.Stream == "" {
		t.Fatal("no stream id")
	}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/__krate/stream/"+r.Stream, nil)
	req.Header.Set("x-krate-window", window)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	for {
		d := next(t, resp.Body)
		if err := d.BeginObject(); err != nil {
			t.Fatal(err)
		}
		k, _ := d.Key()
		switch k {
		case "v":
			v, err := d.Int()
			if err != nil {
				t.Fatal(err)
			}
			vals = append(vals, v)
		case "end":
			return vals, "end", ""
		case "err":
			info := parseErr(t, d)
			return vals, "err", info.Code
		}
	}
}

func TestRemoteStream(t *testing.T) {
	child := newRemoteChild(1)
	cts := child.serve(t)
	_, pts := newParent(t, cts.URL)

	vals, final, _ := readStream(t, pts, "w1", callSpec{id: 1, fn: "c.count", args: args(func(e *wire.Encoder) { e.Int(4) })})
	if final != "end" || len(vals) != 4 || vals[3] != 3 {
		t.Errorf("stream: %v %s", vals, final)
	}
	vals, final, code := readStream(t, pts, "w1", callSpec{id: 2, fn: "c.count", args: args(func(e *wire.Encoder) { e.Int(99) })})
	if final != "err" || code != "boom" || len(vals) != 99 {
		t.Errorf("stream error: %d values, %s, %q", len(vals), final, code)
	}
}

func TestRemoteStreamCancel(t *testing.T) {
	child := newRemoteChild(1)
	cts := child.serve(t)
	_, pts := newParent(t, cts.URL)

	_, r := callOne(t, pts, "w1", callSpec{id: 1, fn: "c.count", args: args(func(e *wire.Encoder) { e.Int(-1) })})
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, pts.URL+"/__krate/stream/"+r.Stream, nil)
	req.Header.Set("x-krate-window", "w1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	eventually(t, "child stream start", func() bool { return child.streamUp.Load() == 1 })
	cancel()
	recv(t, child.streamCx, "child stream producer to stop")
}

// readSignals collects "v" values of a parent's signal connection.
func readSignals(t *testing.T, ts *httptest.Server) (<-chan int64, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan int64, 64)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/__krate/signals?names=c.count", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		defer resp.Body.Close()
		for {
			d, err := wire.ReadFrame(resp.Body)
			if err != nil {
				return
			}
			if d.BeginObject() != nil {
				return
			}
			for d.More() {
				k, _ := d.Key()
				if k == "v" {
					v, _ := d.Int()
					out <- v
				} else {
					if d.Skip() != nil {
						return
					}
				}
			}
		}
	}()
	return out, cancel
}

func TestRemoteSignalAndSwap(t *testing.T) {
	child1 := newRemoteChild(10)
	c1 := child1.serve(t)
	rem, pts := newParent(t, c1.URL)

	vals, stop := readSignals(t, pts)
	defer stop()
	if v := recv(t, vals, "initial"); v != 10 {
		t.Fatalf("initial = %d", v)
	}
	child1.sig.Set(11)
	if v := recv(t, vals, "update"); v != 11 {
		t.Fatalf("update = %d", v)
	}

	// Replace the child by a different server: the reader keeps its own
	// connection to the parent and sees the new child's current value.
	child2 := newRemoteChild(500)
	c2 := child2.serve(t)
	rem.SetBase(c2.URL)
	c1.Close()
	if v := recv(t, vals, "value from replacement child"); v != 500 {
		t.Fatalf("after swap = %d", v)
	}
	child2.sig.Set(501)
	if v := recv(t, vals, "update from replacement child"); v != 501 {
		t.Fatalf("after swap update = %d", v)
	}
}

func TestRemoteSignalReconnectSameAddress(t *testing.T) {
	child1 := newRemoteChild(1)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s1 := httptest.NewUnstartedServer(New(Options{Registry: child1.reg}))
	s1.Listener = ln
	s1.Start()
	addr := ln.Addr().String()

	_, pts := newParent(t, "http://"+addr)
	vals, stop := readSignals(t, pts)
	defer stop()
	if v := recv(t, vals, "initial"); v != 1 {
		t.Fatalf("initial = %d", v)
	}

	// Kill the child and bring a new one up later on the same address, with
	// no call to SetBase: the backoff loop must find it.
	s1.CloseClientConnections()
	s1.Close()
	time.Sleep(250 * time.Millisecond)
	child2 := newRemoteChild(77)
	var ln2 net.Listener
	for i := 0; i < 50; i++ {
		if ln2, err = net.Listen("tcp", addr); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if ln2 == nil {
		t.Fatalf("cannot rebind %s: %v", addr, err)
	}
	s2 := httptest.NewUnstartedServer(New(Options{Registry: child2.reg}))
	s2.Listener = ln2
	s2.Start()
	defer func() { stop(); s2.Close() }()
	if v := recv(t, vals, "value after reconnect"); v != 77 {
		t.Fatalf("after reconnect = %d", v)
	}
}

func TestRemoteSignalCancelStopsUpstream(t *testing.T) {
	child := newRemoteChild(1)
	var conns atomic.Int32
	var mu sync.Mutex
	open := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__krate/signals" {
			conns.Add(1)
			mu.Lock()
			open++
			mu.Unlock()
			defer func() { mu.Lock(); open--; mu.Unlock() }()
		}
		New(Options{Registry: child.reg}).ServeHTTP(w, r)
	}))
	defer ts.Close()
	_, pts := newParent(t, ts.URL)
	vals, stop := readSignals(t, pts)
	recv(t, vals, "initial")
	stop()
	eventually(t, "upstream signal connection to close", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return open == 0
	})
	time.Sleep(400 * time.Millisecond)
	if n := conns.Load(); n != 1 {
		t.Errorf("upstream connections = %d, want 1 (no reconnect after cancel)", n)
	}
}
