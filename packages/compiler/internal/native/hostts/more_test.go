package hostts

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/native/jsruntime"
)

const fakeHost = `globalThis.__krateHost = {
  regs: {},
  register(id, fn, kind) { this.regs[id] = { fn: fn, kind: kind }; },
  registerSignal(id, sig) { this.regs[id] = { sig: sig, kind: "signal" }; },
};
`

// runHost loads the host bundle in the embedded runtime with a fake registry
// and returns the runtime.
func runHost(t *testing.T, res *Result) *jsruntime.Runtime {
	t.Helper()
	rt, err := jsruntime.NewBare()
	if err != nil {
		t.Skipf("embedded runtime unavailable: %v", err)
	}
	t.Cleanup(func() { rt.Close() })
	if _, err := rt.Execute(fakeHost); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Execute(read(t, res.HostBundle)); err != nil {
		t.Fatalf("host.js does not run: %v", err)
	}
	return rt
}

func evalJSON(t *testing.T, rt *jsruntime.Runtime, expr string) string {
	t.Helper()
	if _, err := rt.Execute("globalThis.__out = undefined; Promise.resolve(" + expr + ").then(function (v) { globalThis.__out = JSON.stringify(v); }, function (e) { globalThis.__out = 'ERR ' + e; });"); err != nil {
		t.Fatal(err)
	}
	rt.DrainJobs()
	v, err := rt.GetGlobal("__out")
	if err != nil {
		t.Fatal(err)
	}
	s, _ := v.(string)
	return s
}

func TestRunsInQuickJS(t *testing.T) {
	_, res, ds := buildProject(t, map[string]string{
		"src/lib/tasks.host.ts": `import { hostSignal } from "@krate/app/host";
export const count = hostSignal(3);
export async function add(a: number, b: number) { count.set(count.get() + 1); return a + b; }
export async function* ticks(n: number) { for (let i = 0; i < n; i++) yield i; }
export const reexported = (x: number) => x * 2;
export const notCallable = 42;
`,
		"src/pages/index.tsx": `import { count, add, ticks, reexported } from "../lib/tasks.host";
export default function P() { return <p>{count()}{String(add)}{String(ticks)}{String(reexported)}</p>; }
`,
	}, nil)
	mustNoErrors(t, ds)
	want := "src/lib/tasks.host.add,src/lib/tasks.host.count,src/lib/tasks.host.reexported,src/lib/tasks.host.ticks"
	if got := strings.Join(ids(res), ","); got != want {
		t.Fatalf("registered %s", got)
	}
	kinds := map[string]string{}
	for _, r := range res.Registered {
		kinds[r.ID] = r.Kind
	}
	if kinds["src/lib/tasks.host.count"] != "signal" || kinds["src/lib/tasks.host.ticks"] != "stream" || kinds["src/lib/tasks.host.add"] != "func" {
		t.Fatalf("kinds: %v", kinds)
	}
	js := read(t, res.HostBundle)
	if !strings.Contains(js, `registerSignal("src/lib/tasks.host.count"`) || !strings.Contains(js, `"stream")`) {
		t.Fatalf("static registration calls missing:\n%s", js)
	}

	rt := runHost(t, res)
	if got := evalJSON(t, rt, `__krateHost.regs["src/lib/tasks.host.add"].fn(2, 3)`); got != "5" {
		t.Fatalf("add(2,3) = %s", got)
	}
	if got := evalJSON(t, rt, `__krateHost.regs["src/lib/tasks.host.count"].sig.get()`); got != "4" {
		t.Fatalf("signal after one call = %s", got)
	}
	if got := evalJSON(t, rt, `(async function () { var out = []; for await (var v of __krateHost.regs["src/lib/tasks.host.ticks"].fn(3)) out.push(v); return out; })()`); got != "[0,1,2]" {
		t.Fatalf("stream = %s", got)
	}
	if got := evalJSON(t, rt, `__krateHost.regs["src/lib/tasks.host.reexported"].kind`); got != `"func"` {
		t.Fatalf("kind = %s", got)
	}

	stub := res.Overlays[filepath.Join(filepath.Dir(res.HostBundle), "../../../src/lib/tasks.host.ts")]
	for _, w := range []string{
		`export const count = __krateSig("src/lib/tasks.host.count");`,
		`export const ticks = (...a) => __krateStream("src/lib/tasks.host.ticks", a);`,
		`import { call as __krateCall, signal as __krateSignal, stream as __krateStream } from "@krate/app";`,
	} {
		if !strings.Contains(stub, w) {
			t.Errorf("stub lacks %q:\n%s", w, stub)
		}
	}
	if strings.Contains(stub, "notCallable") {
		t.Errorf("a plain value was stubbed:\n%s", stub)
	}
}

func TestBoundaryChecks(t *testing.T) {
	cases := []struct {
		name, params, extra string
		bad                 bool
	}{
		{"function type", "cb: () => void", "", true},
		{"function type in object", "o: { done: (n: number) => void }", "", true},
		{"dom element", "el: HTMLElement", "", true},
		{"dom event union", "e: MouseEvent | null", "", true},
		{"function array", "fs: Array<() => void>", "", true},
		{"class instance", "w: Widget", "class Widget { x = 1 }\n", true},
		{"plain data", "title: string, n?: number, tags: string[] = []", "", false},
		{"interface", "t: Task", "interface Task { id: number }\n", false},
		{"shadowed dom name", "e: Event", "interface Event { id: number }\n", false},
		{"unknown type", "x: Something", "", false},
		{"generic data", "m: Map<string, number>", "", false},
		{"destructured", "{ a, b }: { a: string; b: number }", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, ds := buildProject(t, map[string]string{
				"src/lib/f.host.ts":   c.extra + "export async function f(" + c.params + ") { return 1; }\n",
				"src/pages/index.tsx": "import { f } from \"../lib/f.host\";\nexport default function P() { return <p onClick={() => f(null as any)}>x</p>; }\n",
			}, nil)
			var got *diag.Diagnostic
			for i := range ds {
				if ds[i].Code == diag.CodeHostBoundaryType {
					got = &ds[i]
				}
			}
			if c.bad != (got != nil) {
				t.Fatalf("bad=%v diagnostics=%+v", c.bad, ds)
			}
			if got != nil && (got.Line != 1+strings.Count(c.extra, "\n") || got.File != "src/lib/f.host.ts") {
				t.Errorf("location %s:%d", got.File, got.Line)
			}
		})
	}
}
