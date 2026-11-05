package build

import (
	"strings"
	"testing"
)

// TestAliasedHelperImportFoldsStatically covers `import { plain as pp }`: the
// helper folds into the server HTML under its local name.
func TestAliasedHelperImportFoldsStatically(t *testing.T) {
	html, _, err := buildKrateProject(t, map[string]string{
		"src/lib/x.ts": `export function plain() { return "plain-value"; }
export default function fallback() { return "default-value"; }`,
		"src/pages/index.tsx": `
			import dflt, { plain as pp } from "../lib/x";
			export default function Page() {
				return <div><span id="a">{pp()}</span><span id="b">{dflt()}</span></div>;
			}
		`,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, want := range []string{">plain-value</span>", ">default-value</span>"} {
		if !strings.Contains(html, want) {
			t.Errorf("aliased helper did not render %q:\n%s", want, html)
		}
	}
}

// TestAliasedHelperImportHydrates covers an aliased helper that depends on a
// signal: the hydration script must declare the helper under its local name,
// or the browser throws `pp is not defined`.
func TestAliasedHelperImportHydrates(t *testing.T) {
	_, js, err := buildKrateProject(t, map[string]string{
		"src/lib/x.ts": `export function plain(n: number) { return "n=" + n; }`,
		"src/pages/index.tsx": `
			import { createSignal } from '@krate/runtime';
			import { plain as pp } from "../lib/x";
			export default function Page() {
				const [n, setN] = createSignal(1);
				return <button onClick={() => setN(n() + 1)}>{pp(n())}</button>;
			}
		`,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(js, "function pp(") {
		t.Errorf("hydration script does not declare the aliased helper pp:\n%s", js)
	}
}

// A compiled dependency may declare a class inside a function (immer's
// enableMapSet does); the page does not emit that code, so the build must not
// reject it.
func TestCompiledDependencyClassInFunctionDoesNotFailBuild(t *testing.T) {
	html, _, err := buildKrateProject(t, map[string]string{
		"node_modules/fakelib/package.json": `{"name":"fakelib","version":"1.0.0","main":"index.js","module":"index.js"}`,
		"node_modules/fakelib/index.js": `export function enableMapSet() {
  class DraftMap extends Map { get(k) { return super.get(k); } }
  return DraftMap;
}
export const registry = enableMapSet();
export function configure() { return 1; }`,
		"src/pages/index.tsx": `
			import { registry } from "fakelib";
			export default function Page() {
				return <h1>{typeof registry}</h1>;
			}
		`,
	})
	if err != nil {
		t.Fatalf("build must not fail on library code: %v", err)
	}
	if !strings.Contains(html, "<h1>") {
		t.Errorf("page did not render:\n%s", html)
	}
}

// `export default () => ...` and `export default memo(...)` are components:
// they used to build to an empty page without a word.
func TestInlineDefaultExportComponentsRender(t *testing.T) {
	for name, page := range map[string]string{
		"arrow":   `export default () => <main>inline arrow page</main>;`,
		"memo":    `export default memo(function V() { return <main>inline arrow page</main>; });`,
		"props":   `export default (props: { n?: number }) => { const n = props.n ?? 1; return <main>inline arrow page {n}</main>; };`,
		"default": `import Card from "../card"; export default () => <Card />;`,
	} {
		t.Run(name, func(t *testing.T) {
			files := map[string]string{"src/pages/index.tsx": page}
			if name == "default" {
				files["src/card.tsx"] = `export default () => <main>inline arrow page</main>;`
			}
			html, _, err := buildKrateProject(t, files)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if !strings.Contains(html, "inline arrow page") {
				t.Errorf("page rendered empty:\n%s", html)
			}
		})
	}
}

// A tier decorator on an inline default export classifies the component.
func TestTierDecoratorOnInlineDefaultExportBuilds(t *testing.T) {
	html, _, err := buildKrateProject(t, map[string]string{
		"src/pages/index.tsx": "@static\nexport default () => <main>static arrow</main>;\n",
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(html, "static arrow") {
		t.Errorf("page rendered empty:\n%s", html)
	}
}
