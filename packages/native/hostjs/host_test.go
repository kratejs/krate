package hostjs

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/jsruntime"
	"github.com/kratejs/krate/packages/native/wire"
)

const callsSrc = `
declare const __krateHost: any;
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

function add(a: number, b: number) { return a + b; }
async function addLater(a: number, b: number) { await sleep(5); return a + b; }
function echo(v: unknown) { return v; }
function reverse(b: Uint8Array) { return b.slice().reverse(); }
function describe(v: any) {
  return {
    bytes: v.data instanceof Uint8Array, len: v.data.length,
    date: v.when instanceof Date, ms: v.when.getTime(),
    map: v.m instanceof Map, mapEntry: v.m.get("k"),
    set: v.s instanceof Set, setHas: v.s.has(2),
    nested: v.nested.a.b[1],
    escaped: Object.keys(v.esc).join(","),
  };
}
function fail() {
  const e: any = new Error("no such task");
  e.code = "not_found";
  e.data = { id: 7 };
  throw e;
}
function boom() {
  const o: any = null;
  return o.missing.field;
}
class Thing { x = 1 }
function badFn() { return () => 1; }
function badClass() { return new Thing(); }
function badSym() { return { a: Symbol("x") }; }
function binaryFlag() { return (globalThis as any)[Symbol.for("krate.web")].bin; }
function dates() { return [new Date(Date.UTC(2026, 10, 11, 7, 23, 15, 123)), new Map([["a", 1], ["b", new Uint8Array([1, 2])]]), new Set([1, "x"])]; }
function escapes() { return { $b: 1 }; }
function ctxInfo(this: any) { return { window: this.window, aborted: this.signal.aborted }; }

__krateHost.register("host.add", add, "func");
__krateHost.register("host.addLater", addLater, "func");
__krateHost.register("host.echo", echo, "func");
__krateHost.register("host.reverse", reverse, "func");
__krateHost.register("host.describe", describe, "func");
__krateHost.register("host.fail", fail, "func");
__krateHost.register("host.boom", boom, "func");
__krateHost.register("host.badFn", badFn, "func");
__krateHost.register("host.badClass", badClass, "func");
__krateHost.register("host.badSym", badSym, "func");
__krateHost.register("host.binaryFlag", binaryFlag, "func");
__krateHost.register("host.dates", dates, "func");
__krateHost.register("host.escapes", escapes, "func");
__krateHost.register("host.ctxInfo", ctxInfo, "func");
`

func TestCalls(t *testing.T) {
	r := newRig(t, callsSrc, Options{})

	t.Run("sync", func(t *testing.T) {
		res := r.call("host.add", func(e *wire.Encoder) { e.Int(2); e.Int(40) })
		if res.OK != float64(42) {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("async", func(t *testing.T) {
		res := r.call("host.addLater", func(e *wire.Encoder) { e.Int(1); e.Int(2) })
		if res.OK != float64(3) {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("bytes 1MiB", func(t *testing.T) {
		if !jsruntime.BinaryTransferSupported() || !r.h.rt.BinaryTransfer() {
			t.Fatal("raw binary transfer is not available; bytes would cross as base64")
		}
		if res := r.call("host.binaryFlag", nil); res.OK != true {
			t.Fatalf("the VM reports base64 transfer: %+v", res)
		}
		in := make([]byte, 1<<20)
		for i := range in {
			in[i] = byte(i*7 + i>>8)
		}
		start := time.Now()
		res := r.call("host.reverse", func(e *wire.Encoder) { e.Bytes(in) })
		t.Logf("1 MiB round trip: %v", time.Since(start))
		got, ok := res.OK.([]byte)
		if !ok || len(got) != len(in) {
			t.Fatalf("%T %+v", res.OK, res.Err)
		}
		for i := range in {
			if got[i] != in[len(in)-1-i] {
				t.Fatalf("byte %d differs", i)
			}
		}
	})
	t.Run("empty bytes", func(t *testing.T) {
		res := r.call("host.reverse", func(e *wire.Encoder) { e.Bytes(nil) })
		if b, ok := res.OK.([]byte); !ok || len(b) != 0 {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("decoding", func(t *testing.T) {
		res := r.call("host.describe", func(e *wire.Encoder) {
			e.BeginObject()
			e.Key("data")
			e.Bytes([]byte{1, 2, 3})
			e.Key("when")
			e.Time(time.Date(2026, 11, 11, 7, 23, 15, 123e6, time.UTC))
			e.Key("m")
			e.Raw([]byte(`{"$m":[["k",5]]}`))
			e.Key("s")
			e.Raw([]byte(`{"$s":[1,2]}`))
			e.Key("nested")
			e.Raw([]byte(`{"a":{"b":[0,9]}}`))
			e.Key("esc")
			e.Raw([]byte(`{"$m$":{"x":1,"y":2}}`))
			e.EndObject()
		})
		m, ok := res.OK.(map[string]any)
		if !ok {
			t.Fatalf("%+v", res)
		}
		want := map[string]any{
			"bytes": true, "len": float64(3), "date": true,
			"ms":  float64(time.Date(2026, 11, 11, 7, 23, 15, 123e6, time.UTC).UnixMilli()),
			"map": true, "mapEntry": float64(5), "set": true, "setHas": true,
			"nested": float64(9), "escaped": "x,y",
		}
		for k, w := range want {
			if m[k] != w {
				t.Errorf("%s = %v, want %v", k, m[k], w)
			}
		}
	})
	t.Run("encoding", func(t *testing.T) {
		res := r.call("host.dates", nil)
		arr, ok := res.OK.([]any)
		if !ok || len(arr) != 3 {
			t.Fatalf("%+v", res)
		}
		if tm, ok := arr[0].(time.Time); !ok || !tm.Equal(time.Date(2026, 11, 11, 7, 23, 15, 123e6, time.UTC)) {
			t.Errorf("date: %v", arr[0])
		}
		m := arr[1].(map[string]any)["$m"].([]any)
		if len(m) != 2 || m[0].([]any)[0] != "a" {
			t.Errorf("map: %v", arr[1])
		}
		if b, ok := m[1].([]any)[1].([]byte); !ok || !bytes.Equal(b, []byte{1, 2}) {
			t.Errorf("map bytes: %v", m[1])
		}
		if s := arr[2].(map[string]any)["$s"].([]any); len(s) != 2 || s[1] != "x" {
			t.Errorf("set: %v", arr[2])
		}
	})
	t.Run("escaping", func(t *testing.T) {
		res := r.call("host.escapes", nil)
		m, ok := res.OK.(map[string]any)
		// The Go decoder unwraps {"$b$": {...}} to the user object.
		if !ok || m["$b"] != float64(1) {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("echo", func(t *testing.T) {
		res := r.call("host.echo", func(e *wire.Encoder) {
			e.Raw([]byte(`{"a":[1,"two",null,true,{"b":1.5}],"s":"héllo   \"q\""}`))
		})
		m := res.OK.(map[string]any)
		if m["s"] != "héllo   \"q\"" || len(m["a"].([]any)) != 5 {
			t.Fatalf("%+v", m)
		}
	})
	t.Run("error with code and data", func(t *testing.T) {
		res := r.call("host.fail", nil)
		if res.Err == nil || res.Err["code"] != "not_found" || res.Err["message"] != "no such task" {
			t.Fatalf("%+v", res)
		}
		if d, _ := res.Err["data"].(map[string]any); d["id"] != float64(7) {
			t.Fatalf("data: %v", res.Err["data"])
		}
	})
	t.Run("bad results", func(t *testing.T) {
		for _, fn := range []string{"host.badFn", "host.badClass", "host.badSym"} {
			res := r.call(fn, nil)
			if res.Err == nil || res.Err["code"] != "bad_result" {
				t.Errorf("%s: %+v", fn, res)
			}
		}
	})
	t.Run("unknown function", func(t *testing.T) {
		res := r.call("host.nope", nil)
		if res.Err == nil || res.Err["code"] != "not_found" {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("this", func(t *testing.T) {
		res := r.call("host.ctxInfo", nil)
		m, _ := res.OK.(map[string]any)
		if m["window"] != "w1" || m["aborted"] != false {
			t.Fatalf("%+v", res)
		}
	})
}

func TestSourceMappedStack(t *testing.T) {
	src := "declare const __krateHost: any;\n" +
		"\n" +
		"function lookup(id: number) {\n" +
		"  const rows: any = null;\n" +
		"  return rows.items[id];\n" + // line 5
		"}\n" +
		"__krateHost.register(\"tasks.Lookup\", (id: number) => lookup(id), \"func\");\n"
	r := newRig(t, src, Options{})
	res := r.call("tasks.Lookup", func(e *wire.Encoder) { e.Int(1) })
	if res.Err == nil || res.Err["code"] != "internal" {
		t.Fatalf("%+v", res)
	}
	msg, _ := res.Err["message"].(string)
	if !strings.Contains(msg, "(at src/lib/host.ts:5:") {
		t.Errorf("message lacks the original position: %q", msg)
	}
	data, _ := res.Err["data"].(map[string]any)
	stack, _ := data["stack"].(string)
	if !strings.Contains(stack, "lookup (src/lib/host.ts:5:") || !strings.Contains(stack, "src/lib/host.ts:7:") {
		t.Errorf("stack is not mapped:\n%s", stack)
	}
	if strings.Contains(stack, "__kh_") || strings.Contains(stack, "<eval>") {
		t.Errorf("stack leaks host plumbing:\n%s", stack)
	}
	if !strings.Contains(r.logs.String(), "src/lib/host.ts:5:") {
		t.Errorf("unexpected errors should be logged: %q", r.logs.String())
	}
}

func TestLoadErrors(t *testing.T) {
	h, err := New(Options{Log: func(string) {}})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	js, sm := bundleTS(t, "src/lib/host.ts", "declare const __krateHost: any;\n\nthrow new Error('init failed');\n")
	err = h.Load(js, sm, "host.js")
	if err == nil || !strings.Contains(err.Error(), "src/lib/host.ts:3:") {
		t.Fatalf("got %v", err)
	}
	err = h.Load([]byte("function ("), nil, "bad.js")
	if err == nil || !strings.Contains(err.Error(), "bad.js") {
		t.Fatalf("got %v", err)
	}
	if err := h.Load([]byte("__krateHost.register('x', 1)"), nil, "bad.js"); err == nil {
		t.Fatal("register must validate its arguments")
	}
}

func TestClosedHost(t *testing.T) {
	r := newRig(t, callsSrc, Options{})
	if err := r.h.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.h.Close(); err != nil {
		t.Fatal(err)
	}
	res := r.call("host.add", func(e *wire.Encoder) { e.Int(1); e.Int(1) })
	if res.Err == nil {
		t.Fatalf("%+v", res)
	}
	if err := r.h.Load([]byte("1"), nil, "x.js"); err != ErrClosed {
		t.Fatalf("got %v", err)
	}
}

func TestWatchdog(t *testing.T) {
	src := `declare const __krateHost: any;
__krateHost.register("w.spin", () => { for (;;) {} }, "func");
__krateHost.register("w.ok", () => "ok", "func");`
	r := newRig(t, src, Options{Watchdog: 200 * time.Millisecond})
	start := time.Now()
	res := r.call("w.spin", nil)
	if res.Err == nil || res.Err["code"] != "internal" {
		t.Fatalf("%+v", res)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("the watchdog took %v", time.Since(start))
	}
	if res := r.call("w.ok", nil); res.OK != "ok" {
		t.Fatalf("the host did not survive the watchdog: %+v", res)
	}
}

func TestNoWatchdogByDefaultForAwaits(t *testing.T) {
	// Waiting does not count against the watchdog: only synchronous steps do.
	src := `declare const __krateHost: any;
__krateHost.register("w.wait", () => new Promise((r) => setTimeout(() => r("late"), 300)), "func");`
	r := newRig(t, src, Options{Watchdog: 100 * time.Millisecond})
	if res := r.call("w.wait", nil); res.OK != "late" {
		t.Fatalf("%+v", res)
	}
}
