package hostchild_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/gohost"
	"github.com/kratejs/krate/packages/native/hostchild"
	"github.com/kratejs/krate/packages/native/internal/gohosttest"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/wire"
)

const hostScript = `import { start } from "./hostchild.mjs";
import { readFileSync } from "node:fs";
const h = globalThis.__krateHost;
const first = Number(readFileSync(new URL("./start.txt", import.meta.url), "utf8"));
h.register("t.Echo", (v) => v);
h.register("t.Add", (a, b) => a + b);
h.register("t.Window", function () { return this.window; });
h.register("t.Fail", () => { const e = new Error("no such task"); e.code = "not_found"; e.data = { id: 7 }; throw e; });
h.register("t.Plain", () => { throw new Error("boom"); });
h.register("t.Bad", () => () => 1);
h.register("t.Wait", function () {
  return new Promise((_, rej) => this.signal.addEventListener("abort", () => { console.log("aborted:" + this.window); rej(new Error("x")); }));
});
h.register("t.Fs", () => readFileSync(new URL("./start.txt", import.meta.url), "utf8").trim());
h.register("t.Count", async function* (n) {
  try { for (let i = 0; i < n; i++) { yield i; await new Promise((r) => setTimeout(r, 2)); } }
  finally { console.log("count-return"); }
}, "stream");
h.register("t.Forever", async function* () {
  try { let i = 0; for (;;) { yield i++; await new Promise((r) => setTimeout(r, 5)); } }
  finally { console.log("forever-closed"); }
}, "stream");
h.register("t.Throws", async function* () { yield 1; const e = new Error("mid-stream"); e.code = "oops"; throw e; }, "stream");
const subs = new Set();
const sig = { v: first, get() { return this.v; }, subscribe(f) { subs.add(f); return () => subs.delete(f); } };
h.registerSignal("t.Count", sig);
h.register("t.Bump", () => { sig.v++; for (const f of [...subs]) f(sig.v); });
h.register("t.Subs", () => subs.size);
console.log("version " + first);
start();
`

type rt struct {
	name, bin string
	// transport is the child transport under test: "unix" or "tcp".
	transport string
}

func runtimes(t *testing.T) []rt {
	t.Helper()
	var out []rt
	for _, n := range []string{"node", "bun", "deno"} {
		if p, err := exec.LookPath(n); err == nil {
			out = append(out, rt{name: n, bin: p})
		}
	}
	if len(out) == 0 {
		if os.Getenv("KRATE_REQUIRE_E2E") != "" {
			t.Fatal("no JavaScript runtime on PATH")
		}
		t.Skip("no node, bun or deno on PATH")
	}
	return out
}

func (r rt) args(script string) []string {
	switch r.name {
	case "bun":
		return []string{"run", script}
	case "deno":
		return []string{"run", "-A", "--no-check", script}
	}
	return []string{script}
}

type logSink struct {
	mu    sync.Mutex
	lines []string
}

func (l *logSink) logf(format string, args ...any) {
	l.mu.Lock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
	l.mu.Unlock()
}

type fixture struct {
	dir   string
	child *gohost.Child
	ts    *httptest.Server
	logs  *logSink
}

func setup(t *testing.T, r rt, mutate func(src string) string) *fixture {
	t.Helper()
	return setupEnv(t, r, mutate, nil, nil, nil, nil)
}

// setupEnv is setup with extra child environment and the ids the shell
// registers on top of the standard ones.
func setupEnv(t *testing.T, r rt, mutate func(src string) string, env, funcs, streams, signals []string) *fixture {
	t.Helper()
	dir, cleanup, err := gohosttest.TempDir("hostchild-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	src := hostScript
	if mutate != nil {
		src = mutate(src)
	}
	write := func(name, s string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("hostchild.mjs", hostchild.Source)
	write("host.mjs", src)
	write("start.txt", "10")
	f := &fixture{dir: dir, logs: &logSink{}}
	f.child = gohost.NewChild(gohost.ChildOptions{
		Logf: f.logs.logf, Prefix: "[js] ", Transport: r.transport, Args: r.args(filepath.Join(dir, "host.mjs")), ReadyAnyLine: true, ReadyTimeout: 60 * time.Second,
	})
	if err := f.child.Start(context.Background(), r.bin, env); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.child.Stop(); err != nil {
			t.Error(err)
		}
	})
	reg := scheme.NewRegistry()
	f.child.Remote().RegisterInto(reg,
		append([]string{"t.Echo", "t.Add", "t.Window", "t.Fail", "t.Plain", "t.Bad", "t.Wait", "t.Fs", "t.Bump", "t.Subs", "t.Missing"}, funcs...),
		append([]string{"t.Count", "t.Forever", "t.Throws"}, streams...), append([]string{"t.Count", "t.Nope"}, signals...))
	f.ts = httptest.NewServer(scheme.New(scheme.Options{Registry: reg}))
	t.Cleanup(f.ts.Close)
	return f
}

type result struct {
	ok  *wire.Decoder
	err *krate.Error
	raw []byte
}

func (f *fixture) post(t *testing.T, fn string, window string, args func(*wire.Encoder)) result {
	t.Helper()
	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("calls")
	e.BeginArray()
	e.BeginObject()
	e.Key("id")
	e.Int(1)
	e.Key("fn")
	e.String(fn)
	e.Key("args")
	e.BeginArray()
	if args != nil {
		args(e)
	}
	e.EndArray()
	e.EndObject()
	e.EndArray()
	e.EndObject()
	req, _ := http.NewRequest(http.MethodPost, f.ts.URL+"/__krate/call", bytes.NewReader(e.Frame()))
	if window != "" {
		req.Header.Set("x-krate-window", window)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	d, err := wire.ReadFrame(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var res result
	if err := d.BeginObject(); err != nil {
		t.Fatal(err)
	}
	for d.More() {
		k, _ := d.Key()
		switch k {
		case "ok":
			res.ok, _ = d.SubDecoder()
		case "err":
			raw, _ := d.RawValue()
			res.raw = raw
			sub, _ := wire.NewDecoder(append([]byte{}, mustFrame(raw)...))
			res.err = readErr(t, sub)
		default:
			if err := d.Skip(); err != nil {
				t.Fatal(err)
			}
		}
	}
	return res
}

func mustFrame(json []byte) []byte {
	e := wire.NewEncoder()
	e.Raw(json)
	b, _ := e.Finish()
	return b
}

func readErr(t *testing.T, d *wire.Decoder) *krate.Error {
	t.Helper()
	e := &krate.Error{}
	if err := d.BeginObject(); err != nil {
		t.Fatal(err)
	}
	for d.More() {
		k, _ := d.Key()
		switch k {
		case "code":
			e.Code, _ = d.String()
		case "message":
			e.Message, _ = d.String()
		case "data":
			raw, _ := d.RawValue()
			if string(raw) != "null" {
				e.Data = string(raw)
			}
		default:
			if err := d.Skip(); err != nil {
				t.Fatal(err)
			}
		}
	}
	return e
}

// each runs fn for every runtime on the transport the shell uses on this OS
// and, where that is unix sockets, on loopback TCP as well (the Windows
// transport), so the TCP path is exercised on every CI OS.
func each(t *testing.T, fn func(t *testing.T, r rt)) {
	for _, r := range runtimes(t) {
		native := gohost.TransportFor(runtime.GOOS, true)
		trs := []string{native}
		if native != "tcp" {
			trs = append(trs, "tcp")
		}
		for _, tr := range trs {
			r := r
			r.transport = tr
			name := r.name
			if tr != native {
				name += "-" + tr
			}
			t.Run(name, func(t *testing.T) { fn(t, r) })
		}
	}
}

func TestCalls(t *testing.T) {
	each(t, func(t *testing.T, r rt) {
		f := setup(t, r, nil)

		res := f.post(t, "t.Add", "", func(e *wire.Encoder) { e.Int(20); e.Int(22) })
		if n, err := res.ok.Int(); err != nil || n != 42 {
			t.Fatalf("add = %d, %v", n, err)
		}
		res = f.post(t, "t.Window", "w7", nil)
		if s, _ := res.ok.String(); s != "w7" {
			t.Fatalf("window = %q", s)
		}
		res = f.post(t, "t.Fs", "", nil)
		if s, _ := res.ok.String(); s != "10" {
			t.Fatalf("node:fs call = %q", s)
		}

		// The echo of a value with bytes, a date, a map and a set is the same frame.
		when := time.Date(2026, 11, 11, 7, 23, 15, 123_000_000, time.UTC)
		build := func(e *wire.Encoder) {
			e.BeginObject()
			e.Key("bytes")
			e.Bytes([]byte{0, 1, 2, 255})
			e.Key("when")
			e.Time(when)
			e.Key("map")
			e.BeginMap()
			e.BeginArray()
			e.String("k")
			e.Int(1)
			e.EndArray()
			e.BeginArray()
			e.Int(2)
			e.String("two")
			e.EndArray()
			e.EndMap()
			e.Key("set")
			e.BeginSet()
			e.Int(3)
			e.String("x")
			e.EndSet()
			e.Key("tag")
			e.BeginObject()
			e.Key("$d")
			e.String("not a date")
			e.EndObject()
			e.Key("n")
			e.Float(1.5e-7)
			e.Key("s")
			e.String("h\u00e9llo \u2713")
			e.EndObject()
		}
		res = f.post(t, "t.Echo", "", build)
		if res.ok == nil {
			t.Fatalf("echo failed: %s", res.raw)
		}
		got := wire.NewEncoder()
		if err := wire.Copy(got, res.ok); err != nil {
			t.Fatal(err)
		}
		gotFrame, _ := got.Finish()
		want := wire.NewEncoder()
		build(want)
		wantFrame, _ := want.Finish()
		if string(gotFrame) != string(wantFrame) {
			t.Fatalf("echo changed the value:\n got %q\nwant %q", gotFrame, wantFrame)
		}
	})
}

func TestSharedVectors(t *testing.T) {
	each(t, func(t *testing.T, r rt) {
		dir, err := filepath.Abs(filepath.Join("..", "wire", "testdata"))
		if err != nil {
			t.Fatal(err)
		}
		mod, _ := filepath.Abs("hostchild.mjs")
		script, _ := filepath.Abs(filepath.Join("testdata", "vectors.mjs"))
		args := append(r.args(script), dir, mod)
		out, err := exec.Command(r.bin, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if !strings.HasPrefix(string(out), "ok ") {
			t.Fatalf("unexpected output %q", out)
		}
	})
}
