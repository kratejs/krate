package hostts

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
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

func TestInlineRunsInQuickJS(t *testing.T) {
	_, res, ds := buildProject(t, map[string]string{
		"src/pages/index.tsx": inlinePage,
		"src/lib/ui.ts":       "export const useThing = () => 'x';\n",
	}, nil)
	mustNoErrors(t, ds)
	rt := runHost(t, res)
	got := evalJSON(t, rt, `__krateHost.regs["src/pages/index#addTask"].fn("Buy milk")`)
	if got != `{"ok":true,"row":{"title":"Buy milk"}}` {
		t.Fatalf("addTask = %s", got)
	}
}

func TestAutoKindResolvedAtRuntime(t *testing.T) {
	_, res, ds := buildProject(t, map[string]string{
		"src/lib/impl.ts": `import { hostSignal } from "@krate/app/host";
export const total = hostSignal(1);
export async function* feed() { yield 1; }
export function plain() { return 1; }
`,
		"src/lib/api.host.ts": `export { total, feed, plain } from "./impl";`,
		"src/pages/index.tsx": `import { total, feed, plain } from "../lib/api.host";
export default function P() { return <p>{String(total)}{String(feed)}{String(plain)}</p>; }
`,
	}, nil)
	mustNoErrors(t, ds)
	for _, r := range res.Registered {
		if r.Kind != "auto" {
			t.Errorf("%s kind %s, want auto", r.ID, r.Kind)
		}
	}
	rt := runHost(t, res)
	got := evalJSON(t, rt, `Object.keys(__krateHost.regs).sort().map(function (k) { return k + ":" + __krateHost.regs[k].kind; })`)
	want := `["src/lib/api.host.feed:stream","src/lib/api.host.plain:func","src/lib/api.host.total:signal"]`
	if got != want {
		t.Fatalf("runtime kinds %s", got)
	}
}

func TestHostOnlyImportedByHostIsNotRegistered(t *testing.T) {
	_, res, ds := buildProject(t, map[string]string{
		"src/lib/inner.host.ts": "export function secret() { return 'inner'; }\n",
		"src/lib/outer.host.ts": `import { secret } from "./inner.host";
export function open() { return secret() + '!'; }
`,
		"src/lib/unused.host.ts": "export function nobody() { return 1; }\n",
		"src/pages/index.tsx": `import { open } from "../lib/outer.host";
export default function P() { return <p onClick={open}>x</p>; }
`,
	}, nil)
	mustNoErrors(t, ds)
	if got := strings.Join(ids(res), ","); got != "src/lib/outer.host.open" {
		t.Fatalf("registered %s", got)
	}
	js := read(t, res.HostBundle)
	if !strings.Contains(js, "inner") || strings.Contains(js, "nobody") {
		t.Fatalf("inner must be bundled (called by outer) and unused must not:\n%s", js)
	}
	rt := runHost(t, res)
	if got := evalJSON(t, rt, `__krateHost.regs["src/lib/outer.host.open"].fn()`); got != `"inner!"` {
		t.Fatalf("open() = %s", got)
	}
	if got := evalJSON(t, rt, `Object.keys(__krateHost.regs).length`); got != "1" {
		t.Fatalf("registry size %s", got)
	}
}

func TestNodeBuiltinsStayExternalOnNode(t *testing.T) {
	_, res, ds := buildProject(t, map[string]string{
		"src/lib/files.host.ts": `import { readFileSync } from "node:fs";
import path from "path";
export function read(p: string) { return readFileSync(path.join("/", p), "utf8"); }
`,
		"src/pages/index.tsx": `import { read } from "../lib/files.host";
export default function P() { return <p onClick={() => read("a")}>x</p>; }
`,
	}, func(c *config.Config) { c.Native = &config.NativeConfig{Host: config.NativeHost{Runtime: "node"}} })
	mustNoErrors(t, ds)
	// A bare built-in is rewritten to its node: form, which every runtime accepts.
	if got := strings.Join(res.Externals, ","); got != "" {
		t.Fatalf("quickjs externals %q on a node project", got)
	}
	if len(res.Groups) != 1 || res.Groups[0].Runtime != "node" {
		t.Fatalf("groups %+v", res.Groups)
	}
	ext := strings.Join(res.Groups[0].Externals, ",")
	if !strings.Contains(ext, "node:fs") || !strings.Contains(ext, "node:path") || strings.Contains(ext, ",path") {
		t.Fatalf("node externals %q", ext)
	}
	if res.HostBundle != "" {
		t.Fatalf("a node project wrote a QuickJS bundle: %s", res.HostBundle)
	}
	code := read(t, res.Groups[0].Bundle)
	for _, want := range []string{`from "node:fs"`, `from "node:path"`, "start();", "createServer", `register("src/lib/files.host.read"`} {
		if !strings.Contains(code, want) {
			t.Errorf("node bundle lacks %q", want)
		}
	}
	if !strings.HasSuffix(filepath.ToSlash(res.Groups[0].Bundle), "host/node/host.mjs") {
		t.Errorf("bundle path %s", res.Groups[0].Bundle)
	}
	if res.Registered[0].Runtime != "node" {
		t.Errorf("registration runtime %q", res.Registered[0].Runtime)
	}
	var m manifest
	if err := json.Unmarshal([]byte(read(t, res.Manifest)), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Groups) != 1 || m.Groups[0].Runtime != "node" || m.Groups[0].Ids[0] != "src/lib/files.host.read" {
		t.Fatalf("manifest groups %+v", m.Groups)
	}
}

func TestUnsupportedAPIWarning(t *testing.T) {
	_, res, ds := buildProject(t, map[string]string{
		"src/lib/x.host.ts": `export function boom() { reportError(new Error("x")); return 1; }`,
		"src/pages/index.tsx": `import { boom } from "../lib/x.host";
export default function P() { return <p onClick={boom}>x</p>; }
`,
	}, nil)
	mustNoErrors(t, ds)
	if res.HostBundle == "" {
		t.Fatal("a warning must not stop the bundle")
	}
	var found *diag.Diagnostic
	for i := range ds {
		if ds[i].Code == diag.CodeHostUnsupportedAPI {
			found = &ds[i]
		}
	}
	if found == nil || !found.IsWarning() || found.File != "src/lib/x.host.ts" || !strings.Contains(found.Hint, "native.host.islands") ||
		!strings.Contains(found.Message, "reportError") {
		t.Fatalf("diagnostics: %+v", ds)
	}
}

func TestUnsupportedAPIInInlineMapsToUIModule(t *testing.T) {
	page := "import { x } from \"../lib/ui\";\n\n@host\nexport async function go() {\n  reportError(1);\n}\n\nexport default function P() { return <p onClick={go}>{x}</p>; }\n"
	_, _, ds := buildProject(t, map[string]string{
		"src/pages/index.tsx": page,
		"src/lib/ui.ts":       "export const x = 1;\n",
	}, nil)
	for _, d := range ds {
		if d.Code == diag.CodeHostUnsupportedAPI {
			if d.File != "src/pages/index.tsx" || d.Line != 5 || strings.TrimSpace(d.Source) != "reportError(1);" {
				t.Fatalf("location %s:%d %q", d.File, d.Line, d.Source)
			}
			return
		}
	}
	t.Fatalf("no unsupported API warning in %+v", ds)
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

func TestBoundaryCheckOnInlineAndNamesParameter(t *testing.T) {
	_, res, ds := buildProject(t, map[string]string{
		"src/pages/index.tsx": "@host\nexport async function save(title: string, onDone: () => void) { return title; }\nexport default function P() { return <p onClick={() => save('a', () => {})}>x</p>; }\n",
	}, nil)
	if res.HostBundle != "" {
		t.Error("an error must not produce a bundle")
	}
	es := errorsOf(ds)
	if len(es) != 1 || es[0].Code != diag.CodeHostBoundaryType || !strings.Contains(es[0].Message, "onDone") || es[0].Line != 2 {
		t.Fatalf("diagnostics: %+v", ds)
	}
}

func TestImportOfMissingAndValueExports(t *testing.T) {
	_, _, ds := buildProject(t, map[string]string{
		"src/lib/f.host.ts":   "export async function f() { return 1; }\nexport const LIMIT = 5;\n",
		"src/pages/index.tsx": "import { f, nope, LIMIT } from \"../lib/f.host\";\nexport default function P() { return <p onClick={() => f()}>{String(nope)}{LIMIT}</p>; }\n",
	}, nil)
	codes := map[diag.Code]bool{}
	for _, d := range errorsOf(ds) {
		codes[d.Code] = true
	}
	if !codes[diag.CodeHostImportMissing] || !codes[diag.CodeHostValueExport] || len(codes) != 2 {
		t.Fatalf("diagnostics: %+v", ds)
	}
}

func TestSplitErrors(t *testing.T) {
	_, _, ds := buildProject(t, map[string]string{
		"src/pages/index.tsx": "export default function P() {\n  async function a() { \"@host\"; return 1; }\n  return <p>{String(a)}</p>;\n}\nexport function Q() {\n  async function a() { \"@host\"; return 2; }\n  return <p>{String(a)}</p>;\n}\n",
	}, nil)
	es := errorsOf(ds)
	if len(es) != 1 || es[0].Code != diag.CodeHostSplit || es[0].Line != 6 {
		t.Fatalf("diagnostics: %+v", ds)
	}
}

func TestBundleErrorIsReported(t *testing.T) {
	_, _, ds := buildProject(t, map[string]string{
		"src/lib/f.host.ts":   "import { gone } from \"./missing\";\nexport async function f() { return gone; }\n",
		"src/pages/index.tsx": "import { f } from \"../lib/f.host\";\nexport default function P() { return <p onClick={() => f()}>x</p>; }\n",
	}, nil)
	es := errorsOf(ds)
	if len(es) != 1 || es[0].Code != diag.CodeHostBundle || es[0].File != "src/lib/f.host.ts" || es[0].Line != 1 {
		t.Fatalf("diagnostics: %+v", ds)
	}
}

func TestTypesAreEmitted(t *testing.T) {
	root, res, ds := buildProject(t, map[string]string{
		"src/lib/tasks.host.ts": `import { hostSignal } from "@krate/app/host";
export const count = hostSignal(0);
export async function add(a: number) { return a; }
export async function* ticks() { yield 1; }
`,
		"src/pages/index.tsx": `import { count, add, ticks } from "../lib/tasks.host";
export default function P() { return <p>{String(count)}{String(add)}{String(ticks)}</p>; }
`,
	}, nil)
	mustNoErrors(t, ds)
	want := filepath.Join(root, ".krate/native/types/src/lib/tasks.host.d.ts")
	if len(res.Types) != 1 || res.Types[0] != want {
		t.Fatalf("types %v", res.Types)
	}
	dts := read(t, want)
	for _, w := range []string{
		`export declare const add: (...a: Parameters<typeof import("../../../../../src/lib/tasks.host").add>) => Promise<Awaited<ReturnType<typeof import("../../../../../src/lib/tasks.host").add>>>;`,
		`AsyncIterable<__Item<ReturnType<typeof import("../../../../../src/lib/tasks.host").ticks>>>`,
		`ReadonlyHostSignal<T, T | undefined>`,
	} {
		if !strings.Contains(dts, w) {
			t.Errorf("d.ts lacks %q:\n%s", w, dts)
		}
	}
}
