package build

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

const moduleValuesLib = `
export const shout = (s: string) => s.toUpperCase() + "!";
export const cfg = { name: "Krate", n: 3 };
export const nf = new Intl.NumberFormat("en-US");
export class Store { v = 1; get() { return this.v; } }
export enum Color { Red, Green }
export namespace Util { export function twice(n: number) { return n * 2; } }
export function add(a: number, b: number) { return a + b; }
`

func TestImportedModuleValuesUsedInTextReachHydration(t *testing.T) {
	files := map[string]string{
		"src/lib/helpers.ts": moduleValuesLib,
		"src/pages/index.tsx": `
import { shout, cfg, nf, Store, Color, Util, add } from "../lib/helpers";
import * as ns from "../lib/helpers";
import { createSignal } from "@krate/runtime";
export default function Home() {
  const [n, setN] = createSignal(1);
  return (
    <div>
      <p id="a">{shout("hi")}</p>
      <p id="b">{cfg.name}</p>
      <p id="c">{nf.format(1234567)}</p>
      <p id="d">{new Store().get()}</p>
      <p id="e">{Color[1]}</p>
      <p id="f">{Util.twice(4)}</p>
      <p id="g">{ns.add(1, 2)}</p>
      <button onClick={() => setN(n() + add(1, 1) + Util.twice(1))}>{n()}</button>
    </div>
  );
}`,
	}
	html, js, err := buildKrateProject(t, files)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// Pure helper folds into the server HTML.
	if !strings.Contains(html, `<p id="a">HI!</p>`) {
		t.Errorf("shout(\"hi\") was not folded for SSR:\n%s", html)
	}
	if !strings.Contains(html, `<p id="b">Krate</p>`) {
		t.Errorf("object member did not fold:\n%s", html)
	}
	// The namespace's initializer statement runs at build time, so the call
	// folds too; the handler still needs the statement at run time.
	if !strings.Contains(html, `<p id="f">8</p>`) {
		t.Errorf("namespace member call did not fold:\n%s", html)
	}
	// Everything else is computed on the client, so each binding must be
	// declared in the hydration scope.
	for _, want := range []string{"var Store=(class Store", "Intl.NumberFormat", "(Util2.twice = twice)", `var ns={"add":add}`} {
		if !strings.Contains(js, want) {
			t.Errorf("hydration JS misses %q:\n%s", want, js)
		}
	}
	if !strings.Contains(js, `"#stmt`) {
		t.Errorf("namespace initializer statement not replayed:\n%s", js)
	}
}

func TestSameModuleClassAndNamespaceReachHandlers(t *testing.T) {
	files := map[string]string{
		"src/pages/index.tsx": `
import { createSignal } from "@krate/runtime";
class Store { v = 2; get() { return this.v; } }
namespace Util { export function twice(n: number) { return n * 2; } }
export default function Home() {
  const [n, setN] = createSignal(0);
  return <button onClick={() => setN(new Store().get() + Util.twice(2))}>{n()}</button>;
}`,
	}
	_, js, err := buildKrateProject(t, files)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(js, "var Store=(class Store") || !strings.Contains(js, ".twice = twice") {
		t.Errorf("class and namespace not emitted for handler use:\n%s", js)
	}
}

func TestJSONImportsRenderInHTML(t *testing.T) {
	files := map[string]string{
		"src/data/site.json": `{"title":"Site T","z":1,"a":2,"posts":[{"t":"One"},{"t":"Two"}]}`,
		"src/pages/index.tsx": `
import site from "../data/site.json";
import { title } from "../data/site.json";
import * as all from "../data/site.json";
export default function Home() {
  return (
    <div>
      <h1>{site.title}</h1>
      <h2>{title}</h2>
      <h3>{all.default.title}</h3>
      <ul>{site.posts.map((p) => <li>{p.t}</li>)}</ul>
    </div>
  );
}`,
	}
	html, _, err := buildKrateProject(t, files)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, want := range []string{"<h1>Site T</h1>", "<h2>Site T</h2>", "<li>One</li>", "<li>Two</li>"} {
		if !strings.Contains(html, want) {
			t.Errorf("html misses %q:\n%s", want, html)
		}
	}
}

func TestInvalidJSONImportIsLocatedError(t *testing.T) {
	files := map[string]string{
		"src/data/bad.json": "{\n  \"a\": 1,\n  \"b\": oops\n}\n",
		"src/pages/index.tsx": `
import bad from "../data/bad.json";
export default function Home() { return <p>{bad.a}</p>; }`,
	}
	_, _, err := buildKrateProject(t, files)
	if err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
	msg := err.Error()
	if !strings.Contains(msg, "bad.json:3:") || !strings.Contains(msg, "invalid JSON") {
		t.Errorf("error is not located at the JSON syntax error: %s", msg)
	}
}

func TestRawAndURLImports(t *testing.T) {
	files := map[string]string{
		"src/data/note.txt": "hello raw",
		"src/data/logo.svg": "<svg xmlns='http://www.w3.org/2000/svg'/>",
		"src/pages/index.tsx": `
import note from "../data/note.txt?raw";
import logo from "../data/logo.svg?url";
export default function Home() { return <div><p>{note}</p><img src={logo} alt="" /></div>; }`,
	}
	html, _, err := buildKrateProject(t, files)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(html, "<p>hello raw</p>") {
		t.Errorf("?raw text missing:\n%s", html)
	}
	if !strings.Contains(html, `src=/assets/logo-`) && !strings.Contains(html, `src="/assets/logo-`) {
		t.Errorf("?url asset URL missing:\n%s", html)
	}
}

func tinylibFiles() map[string]string {
	return map[string]string{
		"node_modules/tinylib/package.json": `{"name":"tinylib","version":"1.0.0","exports":{".":{"node":"./n.js","browser":"./b.js","default":"./d.js"}}}`,
		"node_modules/tinylib/b.js": `export function greet(n) { return "browser:" + n; }
export function unusedExport() { return "tree-shaken-away"; }
export default function def() { return "def-browser"; }`,
		"node_modules/tinylib/n.js": `export function greet(n) { return "node:" + n; }`,
		"node_modules/tinylib/d.js": `export function greet(n) { return "default:" + n; }`,
	}
}

func TestNpmFunctionsUsedByClientCodeAreBundled(t *testing.T) {
	files := tinylibFiles()
	files["src/lib/ids.ts"] = `import { greet } from "tinylib";
export function hello(n: string) { return greet(n); }`
	files["src/pages/index.tsx"] = `
import { createSignal } from "@krate/runtime";
import def, { greet } from "tinylib";
import { hello } from "../lib/ids";
export default function Home() {
  const [who, setWho] = createSignal("a");
  return (
    <div>
      <p id="t">{greet(who())}</p>
      <p id="d">{def()}</p>
      <button onClick={() => setWho(hello("b"))}>go</button>
    </div>
  );
}`
	_, js, err := buildKrateProject(t, files)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(js, "__krateVendor") || !strings.Contains(js, "browser:") || !strings.Contains(js, "def-browser") {
		t.Errorf("tinylib was not bundled for the browser:\n%s", js)
	}
	if strings.Contains(js, "node:") && !strings.Contains(js, "browser:") {
		t.Errorf("server condition leaked into the client bundle")
	}
	if strings.Contains(js, "tree-shaken-away") {
		t.Errorf("unused export was not tree-shaken:\n%s", js)
	}
}

func TestNodeBuiltinUsedByClientCodeIsDiagnosed(t *testing.T) {
	files := map[string]string{
		"src/pages/index.tsx": `
import { createSignal } from "@krate/runtime";
import { join } from "node:path";
export default function Home() {
  const [p, setP] = createSignal("");
  return <button onClick={() => setP(join("a", "b"))}>{p()}</button>;
}`,
	}
	_, _, err := buildKrateProject(t, files)
	if err == nil || !strings.Contains(err.Error(), "node:path") || !strings.Contains(err.Error(), "browser") {
		t.Fatalf("expected a node: built-in diagnostic, got %v", err)
	}
}

// A re-export from another module is not copied into the page script; a
// text-only use of it is still reported by KR8302. (Destructured module
// consts are bundled now and no longer reach this path.)
func TestUnbundledImportInTextIsDiagnosed(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/lib/other.ts", "export const value = 5;\n")
	writeFileRel(t, root, "src/lib/counter.ts", "export { value as total } from \"./other\";\n")
	writeFileRel(t, root, "src/pages/index.tsx", `
import { createSignal } from "@krate/runtime";
import { total } from "../lib/counter";
export default function Page() {
  const [n, setN] = createSignal(0);
  return <div><p>{total + n()}</p><button onClick={() => setN(n() + 1)}>go</button></div>;
}
`)
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if !strings.Contains(out, "KR8302") || !strings.Contains(out, "`total` imported from") {
		t.Errorf("a text-only use of an unbundleable import must be diagnosed:\n%s", out)
	}
}

func TestHelperCallWithSignalArgIsNotDropped(t *testing.T) {
	files := map[string]string{
		"src/pages/index.tsx": `
import { createSignal } from "@krate/runtime";
const fmt = (n: number) => "v" + n;
export default function Page() {
  const [n, setN] = createSignal(1);
  return <div><p>{fmt(n())}</p><button onClick={() => setN(n() + 1)}>go</button></div>;
}`,
	}
	html, js, err := buildKrateProject(t, files)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(html, "v1") || !strings.Contains(js, "fmt(n())") {
		t.Errorf("fmt(n()) must render and stay reactive:\nhtml=%s\njs=%s", html, js)
	}
}

func envProject(t *testing.T, env map[string]string, page string) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", page)
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	b := New(root, cfg)
	b.Env = env
	out, err := captureOutput(func() error { return b.BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	html := readOut(t, cfg.OutDir, "index.html")
	js := ""
	if files, _ := filepath.Glob(filepath.Join(cfg.OutDir, "index.*.js")); len(files) > 0 {
		js = readOut(t, cfg.OutDir, filepath.Base(files[0]))
	}
	return html, js, out
}

func TestPublicEnvInlinedInSSRAndClient(t *testing.T) {
	t.Setenv("KRATE_PUBLIC_FROM_SHELL", "shell-value")
	html, js, out := envProject(t, map[string]string{
		"KRATE_PUBLIC_API":  "https://api.example.test",
		"SECRET_TOKEN":      "do-not-leak-1234",
		"KRATE_PUBLIC_MODE": "x",
	}, `
import { createSignal } from "@krate/runtime";
export default function Page() {
  const [n, setN] = createSignal(0);
  return (
    <div>
      <p id="a">{import.meta.env.KRATE_PUBLIC_API}</p>
      <p id="b">{process.env.KRATE_PUBLIC_FROM_SHELL}</p>
      <p id="c">{import.meta.env.MODE}</p>
      <p id="d">{String(import.meta.env.PROD)}</p>
      <p id="e">{import.meta.env.BASE_URL}</p>
      <button onClick={() => setN(n() + 1)} data-api={import.meta.env.KRATE_PUBLIC_API}>{n()}</button>
    </div>
  );
}`)
	for _, want := range []string{`<p id="a">https://api.example.test</p>`, `<p id="b">shell-value</p>`, `<p id="c">production</p>`, `<p id="d">true</p>`, `<p id="e">/</p>`} {
		if !strings.Contains(html, want) {
			t.Errorf("html misses %s:\n%s\n%s", want, html, out)
		}
	}
	if strings.Contains(html+js, "do-not-leak-1234") || strings.Contains(html+js, "SECRET_TOKEN") {
		t.Errorf("a non-public variable reached client output")
	}
	if strings.Contains(js, "import.meta") {
		t.Errorf("import.meta survived into the classic hydration script:\n%s", js)
	}
}

func TestNonPublicEnvReadIsDiagnosedAndSafe(t *testing.T) {
	html, js, out := envProject(t, map[string]string{"SECRET_TOKEN": "do-not-leak-1234"}, `
import { createSignal } from "@krate/runtime";
export default function Page() {
  const [n, setN] = createSignal(0);
  return (
    <div>
      <p id="a">{process.env.SECRET_TOKEN}</p>
      <button onClick={() => setN(n() + (import.meta.env.OTHER ? 1 : 2))}>{n()}</button>
    </div>
  );
}`)
	if !strings.Contains(out, "KR8310") || !strings.Contains(out, "process.env.SECRET_TOKEN") || !strings.Contains(out, "import.meta.env.OTHER") || !strings.Contains(out, "KRATE_PUBLIC_") {
		t.Errorf("expected KR8310 warnings with the prefix hint:\n%s", out)
	}
	if strings.Contains(html+js, "do-not-leak-1234") {
		t.Errorf("secret reached output")
	}
	if strings.Contains(js, "import.meta") || strings.Contains(js, "process.env") {
		t.Errorf("unexposed environment read left in the hydration script:\n%s", js)
	}
}

// A static page reading a module-level factory value built with `new Set`
// must not render an empty slot with no code and no diagnostic.
func TestStaticReadOfFactoryModuleValueIsNotSilentlyEmpty(t *testing.T) {
	outDir := buildWithFiles(t, map[string]string{
		"src/lib/store.ts": `
			export function create() {
				const items = new Set<string>(["a", "b"]);
				return { get: () => ({ count: items.size }) };
			}
		`,
		"src/pages/index.tsx": `
			import { create } from '../lib/store';
			const store = create();
			export default function Page() {
				return <main><p id="c">count:{store.get().count}</p></main>;
			}
		`,
	})
	html := readOut(t, outDir, "index.html")
	js := ""
	if files, _ := filepath.Glob(filepath.Join(outDir, "index.*.js")); len(files) > 0 {
		js = readOut(t, outDir, filepath.Base(files[0]))
	}
	if strings.Contains(html, "count:2") {
		return
	}
	// Not foldable at build time: it must hydrate rather than stay empty.
	if !strings.Contains(js, "kbindContent(") || !strings.Contains(js, "store.get().count") || !strings.Contains(js, "new Set(") {
		t.Errorf("slot rendered empty with no hydration code:\nhtml=%s\njs=%s", html, js)
	}
	if !strings.Contains(html, `<script src="/index.`) {
		t.Errorf("page must load its hydration script:\n%s", html)
	}
}

// The same read from a child component (which would otherwise be rendered
// once at build time and fail on `new Set`) hydrates too.
func TestChildReadOfFactoryModuleValueHydrates(t *testing.T) {
	outDir := buildWithFiles(t, map[string]string{
		"src/lib/store.ts": `
			export function create() {
				const items = new Set<string>(["a", "b"]);
				return { get: () => ({ count: items.size }) };
			}
		`,
		"src/pages/index.tsx": `
			import { create } from '../lib/store';
			const store = create();
			const LABEL = "items";
			function Count() { return <p id="c">{LABEL}:{store.get().count}</p>; }
			export default function Page() { return <main><Count /></main>; }
		`,
	})
	html := readOut(t, outDir, "index.html")
	js := readHydrationJS(t, outDir)
	if !strings.Contains(js, "store.get().count") || !strings.Contains(js, "new Set(") {
		t.Errorf("child must hydrate the module value:\n%s", js)
	}
	if !strings.Contains(html, "items:") {
		t.Errorf("foldable module const should still render at build time:\n%s", html)
	}
}
