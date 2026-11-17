package jsworker

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/jsruntime"
)

// runtimes returns the worker runtimes installed here. Node is required when
// KRATE_REQUIRE_E2E is set.
func runtimes(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, n := range []string{"node", "bun"} {
		if _, err := exec.LookPath(n); err == nil {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		if os.Getenv("KRATE_REQUIRE_E2E") != "" {
			t.Fatal("node is not installed")
		}
		t.Skip("neither node nor bun is installed")
	}
	return out
}

func eachRuntime(t *testing.T, fn func(t *testing.T, p *Pool)) {
	for _, n := range runtimes(t) {
		t.Run(n, func(t *testing.T) {
			path, _ := exec.LookPath(n)
			p := New(n, path, 1)
			p.Stdout, p.Stderr = &bytes.Buffer{}, &bytes.Buffer{}
			t.Cleanup(p.Close)
			fn(t, p)
		})
	}
}

func mustRun(t *testing.T, p *Pool, req Request) Result {
	t.Helper()
	res, err := p.Run(req)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return res
}

func TestSelect(t *testing.T) {
	has := map[string]bool{"node": true, "bun": true}
	look := func(n string) (string, error) {
		if has[n] {
			return "/bin/" + n, nil
		}
		return "", errors.New("missing")
	}
	cases := []struct {
		env   string
		pref  []string
		want  string
		nodes bool
	}{
		{"", nil, "node", true},
		{"", []string{"bun"}, "bun", true},
		{"", []string{"quickjs", "bun"}, "bun", true},
		{"", []string{"deno"}, "node", true},
		{"quickjs", []string{"bun"}, "", true},
		{"bun", nil, "bun", true},
		{"node", []string{"bun"}, "node", true},
		{"", nil, "bun", false},
		{"node", nil, "", false},
	}
	for _, c := range cases {
		has["node"] = c.nodes
		got, _ := selectWith(look, c.env, c.pref...)
		if got != c.want {
			t.Errorf("env %q pref %v node %v: got %q, want %q", c.env, c.pref, c.nodes, got, c.want)
		}
	}
}

func TestScriptReadsAndErrors(t *testing.T) {
	eachRuntime(t, func(t *testing.T, p *Pool) {
		res := mustRun(t, p, Request{Code: `var a = 1; globalThis.s = "x" + a; globalThis.n = 5;`, Reads: []string{"s", "n", "undefinedName", "missing.prop"}})
		if res.Threw || len(res.Reads) != 4 {
			t.Fatalf("result: %+v", res)
		}
		if r := res.Reads[0]; !r.IsString || r.S != "x1" {
			t.Errorf("s = %+v", r)
		}
		if r := res.Reads[1]; !r.Set || r.IsString || r.S != "5" {
			t.Errorf("n = %+v", r)
		}
		if r := res.Reads[2]; r.Err == "" || !strings.Contains(r.Err, "ReferenceError") {
			t.Errorf("undefined name = %+v", r)
		}

		res = mustRun(t, p, Request{Code: "function foo() { throw new TypeError('bad thing'); }\nfoo();"})
		if !res.Threw || res.Err != "TypeError: bad thing" {
			t.Errorf("script throw: %+v", res)
		}
		res = mustRun(t, p, Request{Code: "throw 'str'"})
		if !res.Threw || res.Err != "str" {
			t.Errorf("string throw: %+v", res)
		}
		// A module's throw is not reported, as QuickJS's ExecuteModule does
		// not; what ran before it is kept.
		res = mustRun(t, p, Request{Code: "globalThis.before = 1; throw new Error('late');", Module: true, Reads: []string{"String(before)"}})
		if res.Threw || res.Reads[0].S != "1" {
			t.Errorf("module throw: %+v", res)
		}
	})
}

func TestModuleScopeAndUnsupported(t *testing.T) {
	eachRuntime(t, func(t *testing.T, p *Pool) {
		res := mustRun(t, p, Request{Code: "var local = 1; function f() { return this; }\nglobalThis.r = typeof globalThis.local + ',' + typeof f();", Module: true, Reads: []string{"r"}})
		if got := res.Reads[0].S; got != "undefined,undefined" {
			t.Errorf("module scope: %q", got)
		}
		res = mustRun(t, p, Request{Code: "globalThis.u = import.meta.url;", Module: true})
		if !res.Unsupported {
			t.Errorf("import.meta should be unsupported: %+v", res)
		}
		res = mustRun(t, p, Request{Code: "await Promise.resolve(1);", Module: true})
		if !res.Unsupported {
			t.Errorf("top-level await should be unsupported: %+v", res)
		}
	})
}

func TestIsolation(t *testing.T) {
	eachRuntime(t, func(t *testing.T, p *Pool) {
		mustRun(t, p, Request{Code: `globalThis.leak = 1; Array.prototype.leaked = 1; process.env.LEAK = "1"; setTimeout(() => { globalThis.late = 1; }, 1);`})
		res := mustRun(t, p, Request{Code: "", Reads: []string{
			"typeof leak", "typeof [].leaked", "String(process.env.LEAK)",
		}})
		for i, want := range []string{"undefined", "undefined", "undefined"} {
			if res.Reads[i].S != want {
				t.Errorf("read %d = %+v, want %q", i, res.Reads[i], want)
			}
		}
		if p.Running() != 1 {
			t.Errorf("pool runs %d workers, want 1", p.Running())
		}
	})
}

func TestAwaitAndEnv(t *testing.T) {
	eachRuntime(t, func(t *testing.T, p *Pool) {
		res := mustRun(t, p, Request{
			Code: `Promise.resolve().then(() => new Promise((r) => setTimeout(r, 20))).then(() => { globalThis.done = process.env.K; });`,
			Env:  map[string]string{"K": "v\"1"}, Await: true, Until: "globalThis.done !== undefined", Reads: []string{"done"},
		})
		if got := res.Reads[0]; got.S != `v"1` {
			t.Errorf("await: %+v", got)
		}
		// Microtasks drain even with nothing pending.
		res = mustRun(t, p, Request{Code: `Promise.resolve().then(() => { globalThis.m = "ok"; });`, Await: true, Reads: []string{"m"}})
		if got := res.Reads[0]; got.S != "ok" {
			t.Errorf("microtasks: %+v", got)
		}
		// A promise that never settles ends the wait once nothing is pending.
		start := time.Now()
		res = mustRun(t, p, Request{Code: `new Promise(() => {});`, Await: true, Until: "globalThis.x", Reads: []string{"globalThis.x"}})
		if res.Reads[0].Set || time.Since(start) > 5*time.Second {
			t.Errorf("idle wait: %+v after %s", res, time.Since(start))
		}
		// An unref'd interval, as libraries start at load, does not hold it.
		start = time.Now()
		mustRun(t, p, Request{Code: `setInterval(() => {}, 10).unref(); new Promise(() => {});`, Await: true, Until: "globalThis.x"})
		if d := time.Since(start); d > 5*time.Second {
			t.Errorf("an unref'd interval held the wait for %s", d)
		}
		res = mustRun(t, p, Request{Code: `globalThis.e = JSON.stringify(process.env);`, Reads: []string{"e"}})
		if res.Reads[0].S != "{}" {
			t.Errorf("default env: %+v", res.Reads[0])
		}
	})
}

func TestTimeout(t *testing.T) {
	eachRuntime(t, func(t *testing.T, p *Pool) {
		res := mustRun(t, p, Request{Code: "for (;;) {}", Timeout: 100 * time.Millisecond})
		if !res.Threw {
			t.Errorf("loop: %+v", res)
		}
		res = mustRun(t, p, Request{Code: "globalThis.k = 1", Reads: []string{"String(k)"}})
		if res.Reads[0].S != "1" {
			t.Errorf("after timeout: %+v", res)
		}
	})
}

func TestConsole(t *testing.T) {
	eachRuntime(t, func(t *testing.T, p *Pool) {
		mustRun(t, p, Request{Code: `console.log("a", 1, {b: 2}); console.warn("w"); console.error(new Error("e").message);`})
		out := p.Stdout.(*bytes.Buffer).String()
		errOut := p.Stderr.(*bytes.Buffer).String()
		if out != "a 1 {\"b\":2}\n" {
			t.Errorf("stdout %q", out)
		}
		if errOut != "WARN: w\nERROR: e\n" {
			t.Errorf("stderr %q", errOut)
		}
	})
}

// TestGlobalsMatchQuickJS checks that a worker context has the globals of
// the embedded runtime.
func TestGlobalsMatchQuickJS(t *testing.T) {
	rt, err := jsruntime.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	names, err := rt.Execute(`Object.getOwnPropertyNames(globalThis).filter((n) => !/^__krate|^_go_|^__require/.test(n)).join(",")`)
	if err != nil {
		t.Fatal(err)
	}
	// Globals QuickJS has and Node or Bun do not: an engine error class, and
	// EventSource, which Node keeps behind a flag.
	engineOnly := map[string]bool{"InternalError": true, "EventSource": true}
	probe := `[` + quoteList(names.(string), engineOnly) + `].map((n) => n + ":" + typeof globalThis[n]).join(",")`
	want, err := rt.Execute(probe)
	if err != nil {
		t.Fatal(err)
	}
	probeModule := `globalThis.__r = typeof require + typeof process + typeof process.env + typeof require("path").join;`
	wantMod, err := rt.Execute(probeModule + "__r")
	if err != nil {
		t.Fatal(err)
	}
	eachRuntime(t, func(t *testing.T, p *Pool) {
		res := mustRun(t, p, Request{Code: "", Reads: []string{probe}})
		// A global this runtime version lacks (Node 22 has no Float16Array)
		// is allowed when code reading it runs in the embedded engine.
		got, wantList := strings.Split(res.Reads[0].S, ","), strings.Split(want.(string), ",")
		if len(got) != len(wantList) {
			t.Fatalf("globals differ:\n got %s\nwant %s", res.Reads[0].S, want)
		}
		for i := range got {
			if got[i] == wantList[i] {
				continue
			}
			name, _, _ := strings.Cut(wantList[i], ":")
			if !strings.HasSuffix(got[i], ":undefined") {
				t.Errorf("global %s: got %s, want %s", name, got[i], wantList[i])
				continue
			}
			r, err := p.Run(Request{Code: "globalThis.__r = typeof " + name + ";", Reads: []string{"__r"}})
			if err != nil || !r.Unsupported {
				t.Errorf("code reading %s, which this runtime lacks, must run in the embedded engine: %+v %v", name, r, err)
			}
		}
		res = mustRun(t, p, Request{Code: probeModule, Reads: []string{"__r"}})
		if got := res.Reads[0].S; got != wantMod.(string) {
			t.Errorf("node globals: got %s want %s", got, wantMod)
		}
	})
}

// TestLocaleMatchesQuickJS checks the methods whose output depends on Intl
// against the embedded runtime, which has none.
func TestLocaleMatchesQuickJS(t *testing.T) {
	exprs := []string{
		`typeof Intl + typeof WebAssembly`,
		`[0, 1e12, -1e12, 951782400000, 1700000000000, 1700040000000].map((t) => { const d = new Date(t); return [d.toString(), d.toTimeString(), d.toLocaleString(), d.toLocaleDateString(), d.toLocaleTimeString()].join("|"); }).join("\n")`,
		`new Date(NaN).toLocaleString() + new Date(NaN).toString() + new Date(NaN).toLocaleTimeString()`,
		`new Date(-62198755200000).toLocaleDateString()`,
		`[(1234567.891).toLocaleString(), (0).toLocaleString(), (-1.5e21).toLocaleString(), (12n).toLocaleString(), [1000, 2.5].toLocaleString()].join(" ")`,
		`["b", "a", "C", "é", "é", "\u{1F600}", "￿", "", "ab", "a"].sort((x, y) => x.localeCompare(y)).join(",")`,
		`["é".localeCompare("é"), "a".localeCompare("b"), "b".localeCompare("a"), "a".localeCompare("a")].join()`,
	}
	rt, err := jsruntime.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	eachRuntime(t, func(t *testing.T, p *Pool) {
		for _, e := range exprs {
			want, err := rt.Execute(e)
			if err != nil {
				t.Fatalf("%s: %v", e, err)
			}
			res := mustRun(t, p, Request{Code: "", Reads: []string{e}})
			if got := res.Reads[0].S; got != want.(string) {
				t.Errorf("%s\n got %q\nwant %q", e, got, want)
			}
		}
	})
}

func quoteList(csv string, skip map[string]bool) string {
	var parts []string
	for _, n := range strings.Split(csv, ",") {
		if n != "" && !skip[n] {
			parts = append(parts, `"`+n+`"`)
		}
	}
	return strings.Join(parts, ",")
}

func TestCrashFallsBack(t *testing.T) {
	eachRuntime(t, func(t *testing.T, p *Pool) {
		mustRun(t, p, Request{Code: "1"})
		p.mu.Lock()
		w := p.idle[0]
		p.mu.Unlock()
		_ = w.cmd.Process.Kill()
		<-w.exited
		if _, err := p.Run(Request{Code: "1"}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("run on a dead worker: %v", err)
		}
		if _, err := p.Run(Request{Code: "1"}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("pool should stay unavailable: %v", err)
		}
		if p.Running() != 0 {
			t.Errorf("%d workers left", p.Running())
		}
		p.Close()
		if _, err := p.Run(Request{Code: "1"}); err != nil {
			t.Fatalf("closed pool should start again: %v", err)
		}
	})
}

func TestStartFailure(t *testing.T) {
	p := New("node", "/nonexistent/node", 1)
	if _, err := p.Run(Request{Code: "1"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
	if p.Broken() == nil {
		t.Fatal("pool should be broken")
	}
	var nilPool *Pool
	if _, err := nilPool.Run(Request{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil pool: %v", err)
	}
}

func TestCloseStopsWorkers(t *testing.T) {
	eachRuntime(t, func(t *testing.T, p *Pool) {
		p.Size = 3
		done := make(chan struct{})
		for range 3 {
			go func() {
				defer func() { done <- struct{}{} }()
				mustRun(t, p, Request{Code: "var t = Date.now(); while (Date.now() - t < 100) {}"})
			}()
		}
		for range 3 {
			<-done
		}
		p.mu.Lock()
		ws := append([]*worker(nil), p.idle...)
		p.mu.Unlock()
		if len(ws) == 0 || len(ws) > 3 {
			t.Fatalf("%d idle workers", len(ws))
		}
		p.Close()
		for _, w := range ws {
			select {
			case <-w.exited:
			case <-time.After(5 * time.Second):
				t.Fatal("worker did not exit")
			}
		}
		if p.Running() != 0 {
			t.Errorf("%d workers left", p.Running())
		}
	})
}

func TestIdleTimeout(t *testing.T) {
	eachRuntime(t, func(t *testing.T, p *Pool) {
		p.IdleTimeout = 50 * time.Millisecond
		mustRun(t, p, Request{Code: "1"})
		deadline := time.Now().Add(5 * time.Second)
		for p.Running() != 0 && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if p.Running() != 0 {
			t.Fatal("idle worker was not stopped")
		}
		mustRun(t, p, Request{Code: "1"})
	})
}

func TestNamedGlobals(t *testing.T) {
	for code, want := range map[string]bool{
		"new Float16Array(2)":        true,
		"x=Float16Array":             true,
		"myFloat16Array()":           false,
		"Float16Arrays":              false,
		"obj.Float16Array_x":         false,
		"const a = 1; Float16Array;": true,
	} {
		if got := len(namedGlobals(code, []string{"Float16Array"})) > 0; got != want {
			t.Errorf("namedGlobals(%q) = %v, want %v", code, got, want)
		}
	}
}

// Code that only names a global the worker lacks runs on the worker; code
// that reads it reports Unsupported without printing its console lines, so
// the embedded engine's rerun prints them once.
func TestMissingGlobalTrappedOnRead(t *testing.T) {
	eachRuntime(t, func(t *testing.T, p *Pool) {
		probe := mustRun(t, p, Request{Code: "", Reads: []string{"typeof globalThis['InternalError']"}})
		if probe.Reads[0].S != "undefined" {
			t.Skip("this runtime has InternalError")
		}
		res := mustRun(t, p, Request{Code: `console.log("named"); globalThis.__r = "InternalError".length + (1 ? 0 : InternalError);`, Reads: []string{"__r"}})
		if res.Unsupported || res.Reads[0].S != "13" {
			t.Fatalf("code naming InternalError without reading it must run on the worker: %+v", res)
		}
		for _, code := range []string{
			`console.log("trapped"); globalThis.__r = typeof InternalError;`,
			`console.log("trapped"); setTimeout(() => { globalThis.__r = e instanceof InternalError; }, 1); var e = 1;`,
			`console.log("trapped"); try { new InternalError("x") } catch (e) { globalThis.__r = 1 }`,
		} {
			res := mustRun(t, p, Request{Code: code, Await: true, Reads: []string{"__r"}})
			if !res.Unsupported {
				t.Errorf("code reading InternalError must report Unsupported: %q: %+v", code, res)
			}
		}
		if out := p.Stdout.(*bytes.Buffer).String(); strings.Contains(out, "trapped") || !strings.Contains(out, "named") {
			t.Errorf("console lines: trapped runs must print nothing, the other must print: %q", out)
		}
		res = mustRun(t, p, Request{Code: `InternalError = function () {}; globalThis.__r = typeof InternalError;`, Reads: []string{"__r"}})
		if res.Unsupported || res.Reads[0].S != "function" {
			t.Errorf("code defining InternalError before reading it runs on the worker: %+v", res)
		}
	})
}

func TestSizeForBudget(t *testing.T) {
	const gib = int64(1) << 30
	cases := []struct {
		cpus   int
		budget int64
		want   int
	}{
		{12, 18 * gib, 4},
		{12, 0, 4},
		{2, 18 * gib, 1},
		{12, 5 * gib, 2},
		{12, gib, 1},
	}
	for _, c := range cases {
		if got := sizeFor(c.cpus, c.budget); got != c.want {
			t.Errorf("sizeFor(%d, %d) = %d; want %d", c.cpus, c.budget, got, c.want)
		}
	}
}

func TestHeapLimitMB(t *testing.T) {
	const gib = int64(1) << 30
	for _, c := range []struct {
		budget int64
		want   int
	}{{18 * gib, 4096}, {8 * gib, 2048}, {2 * gib, 1024}, {256 << 20, 1024}} {
		if got := heapLimitMB(c.budget); got != c.want {
			t.Errorf("heapLimitMB(%d) = %d; want %d", c.budget, got, c.want)
		}
	}
}
