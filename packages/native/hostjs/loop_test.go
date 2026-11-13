package hostjs

import (
	"context"
	"net/http"
	"strings"
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
