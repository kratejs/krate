package hostts

import (
	"math"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/native/bake"
	"github.com/kratejs/krate/packages/native/jsruntime"
)

func codesOf(ds []diag.Diagnostic) string {
	var out []string
	for _, d := range ds {
		if !d.IsWarning() {
			out = append(out, string(d.Code))
		}
	}
	return strings.Join(out, ",")
}

const buildHost = `
export function version(): string { return "1.2.3"; }
export async function greet(name: string, opts: { loud: boolean }) {
  return { text: "hi " + name + (opts.loud ? "!" : ""), n: [1, 2, 3].length };
}
export function ctime() { return new Date(0).toISOString(); }
export function nan() { return NaN; }
export function cyc() { const o: any = {}; o.self = o; return o; }
export function fn() { return () => 1; }
export function date() { return new Date(0); }
export function boom() { throw new Error("nope"); }
export async function never() { return new Promise(() => {}); }
export function big() { return "x".repeat(9 * 1024 * 1024); }
export function deep() { let o: any = []; for (let i = 0; i < 600; i++) o = [o]; return o; }
export function loc() { return (1).toLocaleString(); }
export function undef() { return undefined; }
export function runtimeOnly() { return 1; }
`

func TestBuildFunctionsBakeLiteralCalls(t *testing.T) {
	_, res, ds := buildProject(t, map[string]string{
		"src/lib/b.host.ts": strings.Replace(strings.ReplaceAll(buildHost, "export function version", "@build\nexport function version"), "export async function greet", "@build\nexport async function greet", 1) + "\n" +
			"@build\nexport function ctime2() { return new Date(0).toISOString(); }\n",
		"src/pages/index.tsx": `import { version, greet } from "../lib/b.host";
export default function P() {
  const a = version();
  const b = greet("ann", { loud: true });
  const c = greet("bob", { loud: false });
  const d = greet("ann", { loud: true });
  return <p>{a}{b}{c}{d}</p>;
}
`,
	}, nil)
	mustNoErrors(t, ds)
	if len(res.Registered) != 0 {
		t.Fatalf("build functions must not be registered: %v", ids(res))
	}
	got := res.Baked["src/lib/b.host.version"]
	if string(got["[]"]) != `"1.2.3"` {
		t.Fatalf("version: %v", res.Baked)
	}
	g := res.Baked["src/lib/b.host.greet"]
	if len(g) != 2 {
		t.Fatalf("greet baked once per distinct literal call: %v", g)
	}
	k := bake.Key([]any{"ann", map[string]any{"loud": true}})
	if string(g[k]) != `{"n":3,"text":"hi ann!"}` && string(g[k]) != `{"text":"hi ann!","n":3}` {
		t.Fatalf("greet(ann): %s (keys %v)", g[k], g)
	}
	stub := res.Overlays[mustPath(t, res.Overlays, "b.host.ts")]
	if !strings.Contains(stub, `__krateBaked("src/lib/b.host.version", a)`) || strings.Contains(stub, `__krateCall("src/lib/b.host.version"`) {
		t.Fatalf("stub:\n%s", stub)
	}
}

func mustPath(t *testing.T, ov map[string]string, suffix string) string {
	t.Helper()
	for p := range ov {
		if strings.HasSuffix(p, suffix) {
			return p
		}
	}
	t.Fatalf("no overlay for %s in %v", suffix, ov)
	return ""
}

func TestBuildFunctionErrors(t *testing.T) {
	marked := func(names ...string) string {
		s := buildHost
		for _, n := range names {
			s = strings.Replace(s, "export function "+n+"(", "@build\nexport function "+n+"(", 1)
			s = strings.Replace(s, "export async function "+n+"(", "@build\nexport async function "+n+"(", 1)
		}
		return s
	}
	cases := []struct {
		name, ui, want string
		fns            []string
	}{
		{"non-literal arg", `import { greet } from "../lib/b.host"; const x = "a"; export default function P() { greet(x, {loud:true}); return null; }`, "KR7240", []string{"greet"}},
		{"reference", `import { version } from "../lib/b.host"; export default function P() { const f = version; return null; }`, "KR7241", []string{"version"}},
		{"namespace", `import * as b from "../lib/b.host"; export default function P() { b.version(); return null; }`, "KR7241", []string{"version"}},
		{"throws", `import { boom } from "../lib/b.host"; export default function P() { boom(); return null; }`, "KR7242", []string{"boom"}},
		{"never settles", `import { never } from "../lib/b.host"; export default function P() { never(); return null; }`, "KR7242", []string{"never"}},
		{"locale", `import { loc } from "../lib/b.host"; export default function P() { loc(); return null; }`, "KR7242", []string{"loc"}},
		{"NaN", `import { nan } from "../lib/b.host"; export default function P() { nan(); return null; }`, "KR7243", []string{"nan"}},
		{"cycle", `import { cyc } from "../lib/b.host"; export default function P() { cyc(); return null; }`, "KR7243", []string{"cyc"}},
		{"function", `import { fn } from "../lib/b.host"; export default function P() { fn(); return null; }`, "KR7243", []string{"fn"}},
		{"date", `import { date } from "../lib/b.host"; export default function P() { date(); return null; }`, "KR7243", []string{"date"}},
		{"too big", `import { big } from "../lib/b.host"; export default function P() { big(); return null; }`, "KR7243", []string{"big"}},
		{"too deep", `import { deep } from "../lib/b.host"; export default function P() { deep(); return null; }`, "KR7243", []string{"deep"}},
		{"undefined", `import { undef } from "../lib/b.host"; export default function P() { undef(); return null; }`, "KR7243", []string{"undef"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, ds := buildProject(t, map[string]string{
				"src/lib/b.host.ts":   marked(c.fns...),
				"src/pages/index.tsx": c.ui,
			}, nil)
			if got := codesOf(ds); got != c.want {
				t.Fatalf("codes = %q, want %q\n%+v", got, c.want, ds)
			}
		})
	}
}

func TestBuildFunctionInlineAndBodyString(t *testing.T) {
	_, res, ds := buildProject(t, map[string]string{
		"src/pages/index.tsx": `
export function stamp(n: number) {
  "@build";
  return "v" + (n + 1);
}
@build
function label() { return { a: 1 }; }
export default function P() { return <p>{stamp(41)}{label().a}</p>; }
`,
	}, nil)
	mustNoErrors(t, ds)
	if len(res.Registered) != 0 {
		t.Fatalf("registered %v", ids(res))
	}
	if string(res.Baked["src/pages/index#stamp"][bake.Key([]any{41.0})]) != `"v42"` || string(res.Baked["src/pages/index#label"]["[]"]) != `{"a":1}` {
		t.Fatalf("%v", res.Baked)
	}
}

func TestBuildFunctionPlacement(t *testing.T) {
	_, _, ds := buildProject(t, map[string]string{
		"src/lib/b.host.ts":   "@build\nexport async function* gen() { yield 1; }\n",
		"src/pages/index.tsx": `import { gen } from "../lib/b.host"; export default function P() { gen(); return null; }`,
	}, nil)
	if got := codesOf(ds); got != "KR7244" {
		t.Fatalf("codes = %q: %+v", got, ds)
	}
}

func TestBuildFunctionKeyMatchesStub(t *testing.T) {
	// The stub computes the key in JavaScript; it must equal bake.Key.
	args := []any{"a" + string(rune(0x2028)) + "<b>", 1.0, math.Copysign(0, -1), 1e21, 0.1, map[string]any{"z": nil, "a": []any{true, "é"}, "10": 1.0, "2": 2.0}}
	rtKey := evalStubKey(t, bake.ArgsJSON(args))
	if rtKey != bake.Key(args) {
		t.Fatalf("js key %q\ngo key %q", rtKey, bake.Key(args))
	}
}

func evalStubKey(t *testing.T, argsJSON string) string {
	t.Helper()
	rt, err := jsruntime.NewBare()
	if err != nil {
		t.Skipf("embedded runtime unavailable: %v", err)
	}
	defer rt.Close()
	v, err := rt.Execute(bake.KeyJS + "\n__krateKey(" + argsJSON + ")")
	if err != nil {
		t.Fatal(err)
	}
	s, _ := v.(string)
	return s
}
