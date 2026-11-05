package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func modPage(top, body string) map[string]string {
	return map[string]string{"src/pages/index.tsx": top + "\nexport default function P() { return " + body + "; }\n"}
}

// Top-level statements that change a module value run at build time, in order,
// so the static page shows the value they leave.
func TestModuleStatementsChangeFoldedValues(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		want  string
	}{
		"loop and push": {modPage("const squares = [];\nfor (let i = 0; i < 3; i++) squares.push(i * i);", "<ul>{squares.map((s) => <li>{s}</li>)}</ul>"),
			"<ul><li>0</li><li>1</li><li>4</li></ul>"},
		"loop over a list built by a loop": {modPage("const squares = [];\nfor (let i = 0; i < 3; i++) squares.push(i * i);\nlet total = 0;\nfor (const s of squares) total += s;", "<p>{total}</p>"),
			"<p>5</p>"},
		"assignment":                   {modPage("let total = 1;\ntotal = total + 4;\ntotal += 10;", "<p>{total}</p>"), "<p>15</p>"},
		"increment":                    {modPage("let n = 0;\nn++;\nn++;", "<p>{n}</p>"), "<p>2</p>"},
		"property write":               {modPage("const cfg = { a: 1 };\ncfg.a = 2;\ncfg.b = 3;", "<p>{cfg.a}{cfg.b}</p>"), "<p>23</p>"},
		"Object.assign":                {modPage("const o = {};\nObject.assign(o, { x: 1 }, { y: 2 });", "<p>{o.x}{o.y}</p>"), "<p>12</p>"},
		"function with arguments":      {modPage("const list = [];\nfunction add(x) { list.push(x); }\nadd(1);\nadd(2);", "<p>{list.join(',')}</p>"), "<p>1,2</p>"},
		"function without arguments":   {modPage("const list = [];\nfunction init() { list.push('a'); list.push('b'); }\ninit();", "<p>{list.join(',')}</p>"), "<p>a,b</p>"},
		"branch":                       {modPage("const v = [];\nif (v.length === 0) { v.push('x'); } else { v.push('y'); }", "<p>{v.join(',')}</p>"), "<p>x</p>"},
		"values read before and after": {modPage("const a = [1];\nconst before = a.length;\na.push(2);\nconst after = a.length;", "<p>{before}-{after}</p>"), "<p>1-2</p>"},
		"Set and Map":                  {modPage("const s = new Set();\ns.add(1);\ns.add(2);\nconst m = new Map();\nm.set('k', 'v');", "<p>{s.size}{m.get('k')}</p>"), "<p>2v</p>"},
		"alias of the changed value":   {modPage("const a = [];\nconst b = a;\nb.push(7);", "<p>{a.join(',')}</p>"), "<p>7</p>"},
		"changed in an imported module": {map[string]string{
			"src/lib/data.ts":     "export const squares = [];\nfor (let i = 0; i < 3; i++) squares.push(i * i);\nexport let count = 0;\ncount += squares.length;\n",
			"src/pages/index.tsx": "import { squares, count } from '../lib/data';\nexport default function P() { return <ul>{squares.map((s) => <li>{s}</li>)}<li>n={count}</li></ul>; }\n",
		}, "<ul><li>0</li><li>1</li><li>4</li><li>n=3</li></ul>"},
		"read by a component rendered at build time": {map[string]string{
			"src/pages/index.tsx": "const squares = [];\nfor (let i = 0; i < 3; i++) squares.push(i * i);\nfunction List() { return <ul>{squares.map((s) => <li>{s}</li>)}</ul>; }\nexport default function P() { return <main><List /></main>; }\n",
		}, "<main><ul><li>0</li><li>1</li><li>4</li></ul></main>"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			html, _ := buildPlain(t, c.files)
			if !strings.Contains(html, c.want) {
				t.Errorf("want %s in:\n%s", c.want, rootOf(t, html))
			}
		})
	}
}

// Top-level code that cannot change a module value leaves it folded, with no
// script added to the page.
func TestModuleStatementsThatChangeNothingStillFold(t *testing.T) {
	cases := map[string]map[string]string{
		"logging":           modPage("const cfg = { a: 1 };\nconsole.log(cfg);", "<p>{cfg.a}</p>"),
		"write to window":   modPage("const cfg = { a: 1 };\nwindow.__cfg = cfg;", "<p>{cfg.a}</p>"),
		"listener callback": modPage("let hits = 0;\nconst cfg = { a: 1 };\ndocument.addEventListener('click', () => { hits++; cfg.a = 2; });", "<p>{hits}{cfg.a}</p>"),
	}
	want := map[string]string{"logging": "<p>1</p>", "write to window": "<p>1</p>", "listener callback": "<p>01</p>"}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			html, _ := buildPlain(t, files)
			if !strings.Contains(html, want[name]) {
				t.Errorf("want %s in:\n%s", want[name], rootOf(t, html))
			}
			if strings.Contains(html, "<script src=") {
				t.Errorf("a script was added to a static page:\n%s", rootOf(t, html))
			}
		})
	}
}

// A statement the build cannot run leaves what it may have changed unknown:
// the page defers the value to the runtime instead of baking in the initial
// one, and the hydration code replays the statement.
func TestModuleStatementThatCannotRunDefersToRuntime(t *testing.T) {
	cases := map[string]map[string]string{
		"reads the browser": modPage("const items = [];\nitems.push(window.location.href);", "<p>{items.length}</p>"),
		"calls an imported function": {
			"src/lib/reg.ts":      "export function register(list) { list.push(globalThis.x); }\n",
			"src/pages/index.tsx": "import { register } from '../lib/reg';\nconst items = [1];\nregister(items);\nexport default function P() { return <p>{items.length}</p>; }\n",
		},
		"sorts in place": modPage("const a = [3, 1, 2];\na.sort();", "<p>{a.join(',')}</p>"),
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			_, out, _ := buildFiles(t, files, nil)
			htmlBytes, err := os.ReadFile(filepath.Join(out, "index.html"))
			if err != nil {
				t.Fatal(err)
			}
			html := string(htmlBytes)
			if !strings.Contains(html, "<p><!--k:") {
				t.Errorf("the value should be a runtime slot, not baked in:\n%s", rootOf(t, html))
			}
			js, _ := filepath.Glob(filepath.Join(out, "index.*.js"))
			if len(js) != 1 {
				t.Fatalf("want one hydration bundle, got %v", js)
			}
			code, _ := os.ReadFile(js[0])
			if !strings.Contains(string(code), `__krateModule("#stmt`) {
				t.Errorf("hydration code does not replay the statement:\n%s", code)
			}
		})
	}
}

// A page rendered at build time has no run time to fall back on: a value that
// top-level code leaves unknown fails the build with KR8322.
func TestBlockedModuleValueInBuildTimePageIsAnError(t *testing.T) {
	top := "const items = [];\nitems.push(window.location.href);\n"
	for name, src := range map[string]string{
		"static page": "// @static\n" + top + "export default function P() { return <p>{items.length}</p>; }\n",
		"server page": "// @server\n" + top + "export default function P() { return <p>{items.length}</p>; }\n",
	} {
		t.Run(name, func(t *testing.T) {
			got := buildErrText(t, map[string]string{"src/pages/index.tsx": src})
			if !strings.Contains(got, "KR8322") || !strings.Contains(got, "items") {
				t.Errorf("want a KR8322 error naming items, got %q", got)
			}
		})
	}
}

// Static and hydrated output agree for a list built by top-level code, and a
// value deferred to the runtime shows after hydration.
func TestModuleStatementsBrowser(t *testing.T) {
	page := dynTagSig + `const squares = [];
for (let i = 0; i < 3; i++) squares.push(i * i);
const order = [3, 1, 2];
order.sort();
export default function Page() {
	const [n, setN] = createSignal(0);
	return (
		<main>
			<ul class="sq" onClick={() => setN(n() + 1)}>{squares.map((s) => <li>{s}</li>)}<li>n={n()}</li></ul>
			<p class="ord">{order.join(",")}</p>
		</main>
	);
}
`
	evalFixBrowser(t, map[string]string{"src/pages/index.tsx": page}, []evalFixCheck{
		{Path: "/", Selector: "ul.sq li", Hydrated: "0|1|4|n=0", Click: "ul.sq", AfterClick: strp("0|1|4|n=1")},
		// sort() runs in place at run time; the build cannot, so it defers.
		{Path: "/", Selector: "p.ord", Static: strp(""), Hydrated: "1,2,3"},
	})
}
