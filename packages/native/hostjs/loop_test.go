package hostjs

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/wire"
)

const loopSrc = `
declare const __krateHost: any;
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));
const log: string[] = [];

function timerOrder() {
  const out: string[] = [];
  return new Promise<string[]>((resolve) => {
    setTimeout(() => out.push("t20"), 20);
    setTimeout(() => out.push("t5"), 5);
    setTimeout(() => out.push("t5b"), 5);
    setTimeout(() => out.push("t0"), 0);
    setImmediate(() => out.push("imm"));
    setTimeout(() => resolve(out), 40);
  });
}

// Node prints: sync, nextTick-free microtasks in order, then the macrotasks.
function microtaskOrder() {
  const out: string[] = [];
  return new Promise<string[]>((resolve) => {
    setTimeout(() => { out.push("timeout"); }, 0);
    Promise.resolve().then(() => out.push("p1")).then(() => out.push("p3"));
    queueMicrotask(() => out.push("q"));
    (async () => { out.push("async-start"); await null; out.push("async-after"); })();
    out.push("sync");
    setTimeout(() => resolve(out), 10);
  });
}

function intervalTicks(n: number) {
  return new Promise<number>((resolve) => {
    let ticks = 0;
    const h = setInterval(() => {
      ticks++;
      if (ticks === n) { clearInterval(h); setTimeout(() => resolve(ticks), 30); }
    }, 3);
  });
}

function clearBeforeFire() {
  return new Promise<string>((resolve) => {
    const h = setTimeout(() => resolve("fired"), 5);
    clearTimeout(h);
    setTimeout(() => resolve("cleared"), 20);
  });
}

// Persistence: a timer started by one call keeps running with no request in
// flight and updates a signal.
const ticks = (globalThis as any).__hostSignal("ticks", 0);
function startTicker() {
  let n = 0;
  setInterval(() => ticks.set(++n), 5);
  return true;
}

__krateHost.register("loop.timerOrder", timerOrder, "func");
__krateHost.register("loop.microtaskOrder", microtaskOrder, "func");
__krateHost.register("loop.intervalTicks", intervalTicks, "func");
__krateHost.register("loop.clearBeforeFire", clearBeforeFire, "func");
__krateHost.register("loop.startTicker", startTicker, "func");

// fetch
const base = (u: string) => u;
async function slowFetch(url: string) {
  const res = await fetch(url + "/slow");
  return "slow:" + (await res.text());
}
async function fastFetch(url: string) {
  const res = await fetch(url + "/echo", { method: "POST", body: "hello", headers: { "x-test": "1" } });
  return { status: res.status, body: await res.text(), header: res.headers.get("x-reply") };
}
async function abortedFetch(url: string) {
  const ac = new AbortController();
  const p = fetch(url + "/hang", { signal: ac.signal });
  setTimeout(() => ac.abort(), 10);
  try { await p; return "completed"; } catch (e: any) { return e.name; }
}
async function binaryFetch(url: string) {
  const res = await fetch(url + "/bytes");
  return new Uint8Array(await res.arrayBuffer());
}
function quick() { return "quick"; }
__krateHost.register("net.slowFetch", slowFetch, "func");
__krateHost.register("net.fastFetch", fastFetch, "func");
__krateHost.register("net.abortedFetch", abortedFetch, "func");
__krateHost.register("net.binaryFetch", binaryFetch, "func");
__krateHost.register("net.quick", quick, "func");

// streams
let closed = 0;
let started = 0;
async function* countTo(n: number) {
  started++;
  try {
    for (let i = 1; i <= n; i++) { await sleep(1); yield { i, data: new Uint8Array([i]) }; }
  } finally { closed++; }
}
async function* forever() {
  try { for (let i = 0; ; i++) { await sleep(2); yield i; } } finally { closed++; }
}
function* syncGen() { yield "a"; yield "b"; }
async function* failing() { yield 1; throw Object.assign(new Error("stream broke"), { code: "broken" }); }
function streamStats() { return { closed, started }; }
__krateHost.register("st.countTo", countTo, "stream");
__krateHost.register("st.forever", forever, "stream");
__krateHost.register("st.sync", syncGen, "stream");
__krateHost.register("st.failing", failing, "stream");
__krateHost.register("st.stats", streamStats, "func");

// cancellation of a call
let sawAbort = false;
let waitStarted = false;
async function waitForAbort(this: any) {
  waitStarted = true;
  // A cancel that arrives before this body runs has already aborted the
  // signal, and an abort listener added after that never fires.
  if (this.signal.aborted) { sawAbort = true; return "aborted"; }
  await new Promise<void>((resolve) => this.signal.addEventListener("abort", () => { sawAbort = true; resolve(); }));
  return "aborted";
}
function abortSeen() { return sawAbort; }
__krateHost.register("c.waitForAbort", waitForAbort, "func");
__krateHost.register("c.abortSeen", abortSeen, "func");
__krateHost.register("c.waitStarted", () => waitStarted, "func");
`

// signalPrelude defines the hostSignal shape of packages/app/src/host.ts that
// test bundles use.
const signalPrelude = `
(globalThis).__hostSignal = function (id, initial) {
  var value = initial, subs = new Set();
  var sig = {
    get: function () { return value; },
    set: function (v) { value = v; Array.from(subs).forEach(function (f) { f(v); }); },
    update: function (f) { sig.set(f(value)); },
    subscribe: function (fn) { subs.add(fn); return function () { subs.delete(fn); }; }
  };
  globalThis.__krateHost.registerSignal(id, sig);
  return sig;
};
`

func newLoopRig(t testing.TB, src string) *rig {
	t.Helper()
	js, sm := bundleTS(t, "src/lib/host.ts", src)
	logs := &syncLog{mu: make(chan struct{}, 1)}
	h, err := New(Options{Log: logs.add})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	if err := h.Load([]byte(signalPrelude+"\n"), nil, "prelude.js"); err != nil {
		t.Fatal(err)
	}
	// The prelude registers nothing; the bundle needs __hostSignal first.
	if err := h.Load(js, sm, "host.js"); err != nil {
		t.Fatal(err)
	}
	return finishRig(t, h, logs)
}

func strs(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestTimers(t *testing.T) {
	r := newLoopRig(t, loopSrc)

	t.Run("setTimeout order", func(t *testing.T) {
		got := strings.Join(strs(r.call("loop.timerOrder", nil).OK), ",")
		// Node: t0 and imm are due first (t0 clamps to 1ms), then t5, t5b, t20.
		if got != "t0,imm,t5,t5b,t20" && got != "imm,t0,t5,t5b,t20" {
			t.Fatalf("got %s", got)
		}
	})
	t.Run("microtask order matches node", func(t *testing.T) {
		got := strings.Join(strs(r.call("loop.microtaskOrder", nil).OK), ",")
		// node: sync code, then microtasks in queue order, then timers.
		if want := "async-start,sync,p1,q,async-after,p3,timeout"; got != want {
			t.Fatalf("got  %s\nwant %s", got, want)
		}
	})
	t.Run("setInterval and clear", func(t *testing.T) {
		if got := r.call("loop.intervalTicks", func(e *wire.Encoder) { e.Int(4) }).OK; got != float64(4) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("clearTimeout", func(t *testing.T) {
		if got := r.call("loop.clearBeforeFire", nil).OK; got != "cleared" {
			t.Fatalf("got %v", got)
		}
	})
}

func TestTimerWithoutRequestUpdatesSignal(t *testing.T) {
	r := newLoopRig(t, loopSrc)
	// Connect a reader first, then start the ticker. After the call returns
	// nothing is in flight, yet the interval keeps updating the signal.
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, r.ts.URL+"/__krate/signals?names=ticks", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	first := toAny(t, readFrame(t, resp.Body)).(map[string]any)
	if first["sig"] != "ticks" || first["v"] != float64(0) {
		t.Fatalf("initial frame %v", first)
	}
	if r.call("loop.startTicker", nil).OK != true {
		t.Fatal("startTicker")
	}
	seen := 0.0
	deadline := time.Now().Add(5 * time.Second)
	for seen < 3 && time.Now().Before(deadline) {
		f := toAny(t, readFrame(t, resp.Body)).(map[string]any)
		if v := f["v"].(float64); v > seen {
			seen = v
		}
	}
	if seen < 3 {
		t.Fatalf("the signal only reached %v", seen)
	}
}

func TestFetchDoesNotBlockTheLoop(t *testing.T) {
	release := make(chan struct{})
	var inFlight, hung, hungDone atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/slow":
			inFlight.Add(1)
			select {
			case <-release:
			case <-req.Context().Done():
			}
			if _, err := io.WriteString(w, "done"); err != nil {
				t.Error(err)
			}
		case "/hang":
			hung.Add(1)
			<-req.Context().Done()
			hungDone.Add(1)
		case "/echo":
			b, _ := io.ReadAll(req.Body)
			w.Header().Set("x-reply", req.Header.Get("x-test"))
			if _, err := io.WriteString(w, "got:"+string(b)); err != nil {
				t.Error(err)
			}
		case "/bytes":
			if _, err := w.Write([]byte{0, 1, 2, 255}); err != nil {
				t.Error(err)
			}
		}
	}))
	defer srv.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()

	r := newLoopRig(t, loopSrc)
	slowCh := make(chan callResult, 1)
	go func() {
		resp := r.post("/__krate/call", r.callFrame(1, "net.slowFetch", func(e *wire.Encoder) { e.String(srv.URL) }))
		slowCh <- readResult(t, resp.Body)
	}()
	for inFlight.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	// The slow fetch is pending. Other calls, including another fetch, finish.
	if got := r.call("net.quick", nil).OK; got != "quick" {
		t.Fatalf("got %v", got)
	}
	fast := r.call("net.fastFetch", func(e *wire.Encoder) { e.String(srv.URL) })
	m, _ := fast.OK.(map[string]any)
	if m["status"] != float64(200) || m["body"] != "got:hello" || m["header"] != "1" {
		t.Fatalf("%+v", fast)
	}
	select {
	case res := <-slowCh:
		t.Fatalf("the slow call finished early: %+v", res)
	default:
	}
	close(release)
	select {
	case res := <-slowCh:
		if res.OK != "slow:done" {
			t.Fatalf("%+v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the slow call never finished")
	}

	t.Run("binary body", func(t *testing.T) {
		res := r.call("net.binaryFetch", func(e *wire.Encoder) { e.String(srv.URL) })
		if b, _ := res.OK.([]byte); string(b) != "\x00\x01\x02\xff" {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("abort", func(t *testing.T) {
		res := r.call("net.abortedFetch", func(e *wire.Encoder) { e.String(srv.URL) })
		if res.OK != "AbortError" {
			t.Fatalf("%+v", res)
		}
		// The abort also cancels the Go request, which frees the server side.
		deadline := time.Now().Add(5 * time.Second)
		for hungDone.Load() == 0 {
			if time.Now().After(deadline) {
				t.Fatal("the request was not cancelled")
			}
			time.Sleep(time.Millisecond)
		}
	})
	t.Run("connection error", func(t *testing.T) {
		res := r.call("net.fastFetch", func(e *wire.Encoder) { e.String("http://127.0.0.1:1") })
		if res.Err == nil || !strings.Contains(str(res.Err["message"]), "fetch failed") {
			t.Fatalf("%+v", res)
		}
	})
}

func (r *rig) streamID(fn string, args argsFn) string {
	r.t.Helper()
	res := r.call(fn, args)
	if res.Strm == "" {
		r.t.Fatalf("%s did not answer with a stream: %+v", fn, res)
	}
	return res.Strm
}

func (r *rig) openStream(ctx context.Context, id string) *http.Response {
	r.t.Helper()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, r.ts.URL+"/__krate/stream/"+id, nil)
	req.Header.Set("x-krate-window", "w1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	return resp
}

func (r *rig) stats() (closed, started float64) {
	m := r.call("st.stats", nil).OK.(map[string]any)
	return m["closed"].(float64), m["started"].(float64)
}

func TestStreams(t *testing.T) {
	r := newLoopRig(t, loopSrc)

	t.Run("async generator", func(t *testing.T) {
		id := r.streamID("st.countTo", func(e *wire.Encoder) { e.Int(3) })
		resp := r.openStream(t.Context(), id)
		defer resp.Body.Close()
		for i := 1; i <= 3; i++ {
			f := toAny(t, readFrame(t, resp.Body)).(map[string]any)
			v := f["v"].(map[string]any)
			if v["i"] != float64(i) || len(v["data"].([]byte)) != 1 || v["data"].([]byte)[0] != byte(i) {
				t.Fatalf("frame %d: %v", i, f)
			}
		}
		if f := toAny(t, readFrame(t, resp.Body)).(map[string]any); f["end"] != true {
			t.Fatalf("want end, got %v", f)
		}
	})
	t.Run("sync iterable", func(t *testing.T) {
		resp := r.openStream(t.Context(), r.streamID("st.sync", nil))
		defer resp.Body.Close()
		var got []string
		for {
			f := toAny(t, readFrame(t, resp.Body)).(map[string]any)
			if f["end"] == true {
				break
			}
			got = append(got, f["v"].(string))
		}
		if strings.Join(got, "") != "ab" {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("error", func(t *testing.T) {
		resp := r.openStream(t.Context(), r.streamID("st.failing", nil))
		defer resp.Body.Close()
		if f := toAny(t, readFrame(t, resp.Body)).(map[string]any); f["v"] != float64(1) {
			t.Fatalf("%v", f)
		}
		f := toAny(t, readFrame(t, resp.Body)).(map[string]any)
		e, _ := f["err"].(map[string]any)
		if e["code"] != "broken" || e["message"] != "stream broke" {
			t.Fatalf("%v", f)
		}
	})
	t.Run("cancel on close", func(t *testing.T) {
		before, _ := r.stats()
		ctx, cancel := context.WithCancel(t.Context())
		resp := r.openStream(ctx, r.streamID("st.forever", nil))
		for range 3 {
			readFrame(t, resp.Body)
		}
		cancel()
		resp.Body.Close()
		deadline := time.Now().Add(5 * time.Second)
		for {
			if closed, _ := r.stats(); closed > before {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("the generator's finally never ran after the reader closed")
			}
			time.Sleep(5 * time.Millisecond)
		}
	})
}

func TestCallCancellation(t *testing.T) {
	r := newLoopRig(t, loopSrc)
	ctx, cancel := context.WithCancel(t.Context())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, r.ts.URL+"/__krate/call",
		strings.NewReader(string(r.callFrame(5, "c.waitForAbort", nil))))
	req.Header.Set("x-krate-window", "w1")
	done := make(chan struct{})
	go func() {
		defer close(done)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
		}
	}()
	cancelCall := func() {
		r.post("/__krate/cancel", func() []byte {
			e := wire.NewEncoder()
			e.Raw([]byte(`{"ids":[5]}`))
			return e.Frame()
		}())
	}
	// Cancel a call whose function is running: a cancel that arrives before
	// the call is registered is ignored, and one that arrives before the
	// function starts stops it from starting at all.
	for deadline := time.Now().Add(10 * time.Second); r.call("c.waitStarted", nil).OK != true; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the JS function never started")
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for r.call("c.abortSeen", nil).OK != true {
		if time.Now().After(deadline) {
			t.Fatal("the JS function never saw its AbortSignal fire")
		}
		cancelCall()
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
}

func TestReload(t *testing.T) {
	v1 := `declare const __krateHost: any;
__krateHost.register("r.version", () => 1, "func");
__krateHost.register("r.gone", () => "here", "func");
(globalThis as any).__hostSignal("r.count", 10);
const sig = (globalThis as any).__hostSignal("r.value", 1);
__krateHost.register("r.bump", () => { sig.set(sig.get() + 1); return sig.get(); }, "func");
setInterval(() => (globalThis as any).oldTimerRan = true, 2);
`
	v2 := `declare const __krateHost: any;
__krateHost.register("r.version", () => 2, "func");
__krateHost.register("r.added", () => "new", "func");
const sig = (globalThis as any).__hostSignal("r.value", 1);
__krateHost.register("r.bump", () => { sig.set(sig.get() + 100); return sig.get(); }, "func");
__krateHost.register("r.oldTimer", () => (globalThis as any).oldTimerRan === true, "func");
__krateHost.register("r.resetTimerFlag", () => { (globalThis as any).oldTimerRan = false; return true; }, "func");
`
	js1, sm1 := bundleTS(t, "src/lib/host.ts", v1)
	js2, sm2 := bundleTS(t, "src/lib/host.ts", v2)
	logs := &syncLog{mu: make(chan struct{}, 1)}
	h, err := New(Options{Log: logs.add})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := h.Load([]byte(signalPrelude), nil, "prelude.js"); err != nil {
		t.Fatal(err)
	}
	if err := h.Load(js1, sm1, "host.js"); err != nil {
		t.Fatal(err)
	}
	r := finishRig(t, h, logs)

	if got := r.call("r.version", nil).OK; got != float64(1) {
		t.Fatalf("v1: %v", got)
	}
	r.call("r.bump", nil) // r.value = 2

	// A reader connected across the reload.
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, r.ts.URL+"/__krate/signals?names=r.value", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if f := toAny(t, readFrame(t, resp.Body)).(map[string]any); f["v"] != float64(2) {
		t.Fatalf("before reload: %v", f)
	}

	if err := h.Reload(js2, sm2, "host.js"); err != nil {
		t.Fatal(err)
	}
	h.Register(r.reg) // adds r.added, skips what is registered

	if got := r.call("r.version", nil).OK; got != float64(2) {
		t.Fatalf("after reload: %v", got)
	}
	if got := r.call("r.added", nil).OK; got != "new" {
		t.Fatalf("new function: %v", got)
	}
	if res := r.call("r.gone", nil); res.Err == nil || res.Err["code"] != "not_found" {
		t.Fatalf("removed function: %+v", res)
	}
	// The signal kept its value (2) across the reload, then bump adds 100.
	if got := r.call("r.bump", nil).OK; got != float64(102) {
		t.Fatalf("signal value was not kept: %v", got)
	}
	// The connected reader moved to the new signal: it sees the carried value
	// and then the bump.
	var last float64
	for last != 102 {
		f := toAny(t, readFrame(t, resp.Body)).(map[string]any)
		last = f["v"].(float64)
	}
	// The old bundle's interval was cleared.
	r.call("r.resetTimerFlag", nil)
	time.Sleep(30 * time.Millisecond)
	if r.call("r.oldTimer", nil).OK == true {
		t.Fatal("the old bundle's interval kept running after the reload")
	}

	t.Run("a failing reload keeps the old code", func(t *testing.T) {
		if err := h.Reload([]byte("throw new Error('nope')"), nil, "host.js"); err == nil {
			t.Fatal("want an error")
		}
		if got := r.call("r.version", nil).OK; got != float64(2) {
			t.Fatalf("got %v", got)
		}
	})
}
