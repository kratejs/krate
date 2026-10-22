package build

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// Render-scope values of React source components: derived values, custom
// hook results, context values and module-level state (react_derived.go,
// irtree/derived.go, irtree/moduleconsts_ir.go).

func TestDerivedRenderValuesHydrateAsGetters(t *testing.T) {
	html, js := buildReactPage(t, `
		import { useState, useEffect } from 'react';
		export default function Page() {
			const [items, setItems] = useState(["a", "b"]);
			const [count, setCount] = useState(3);
			const n = items.length;
			const label = n > 1 ? "items" : "item";
			let cls = "base";
			if (count > 2) cls += " on";
			const value = Number(count);
			useEffect(() => { console.log(value); }, [value]);
			return (
				<div className={cls}>
					<p id="out">{n} {label}</p>
					<button onClick={() => setItems([...items, "z"])}>add</button>
					<button onClick={() => setCount(count + 1)}>inc</button>
				</div>
			);
		}
	`)
	for _, want := range []string{
		"var n=krateDerived(()=>items().length);",
		"var label=krateDerived(()=>((n() > 1)?'items':'item'));",
		"var cls=krateDerived(()=>{\n  let cls='base';\n  if ((count() > 2)) {\n    (cls += ' on');\n  }\n  return cls;\n});",
		"var value=krateDerived(()=>Number(count()));",
		"return [value()];",
		`kbindText("3",()=>n());`,
		`kbindText("4",()=>label());`,
		`kbindAttr("1","class",()=>cls());`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("hydration JS missing %q:\n%s", want, js)
		}
	}
	// The SSR output is each getter's build-time value.
	for _, want := range []string{`class="base on"`, "<!--k:3-->2<!--/k:3-->", "<!--k:4-->items<!--/k:4-->"} {
		if !strings.Contains(html, want) {
			t.Errorf("SSR missing %q:\n%s", want, html)
		}
	}
}

func TestDerivedValuesOfStaticComponentStayStatic(t *testing.T) {
	_, js := buildReactPage(t, `
		import { useState } from 'react';
		function Badge(props: { tone: string }) {
			const cls = props.tone === "warn" ? "badge warn" : "badge";
			return <span className={cls}>{props.tone}</span>;
		}
		export default function Page() {
			return <div><Badge tone="warn" /></div>;
		}
	`)
	if strings.Contains(js, "krateDerived") {
		t.Errorf("a component with only static props needs no derived getter at runtime:\n%s", js)
	}
}

func TestContextValueIsPlainThroughIntermediateComponent(t *testing.T) {
	html, js := buildReactPage(t, `
		import { createContext, useContext, useState } from 'react';
		const Theme = createContext({ color: "black", size: 12 });
		function Label() {
			const theme = useContext(Theme);
			return <span id="l" data-color={theme.color} style={{ fontSize: theme.size }}>x</span>;
		}
		function Toolbar() { return <div><Label /></div>; }
		export default function Page() {
			const [theme, setTheme] = useState({ color: "teal", size: 14 });
			return (
				<Theme.Provider value={theme}>
					<Toolbar />
					<button onClick={() => setTheme({ ...theme, size: theme.size + 2 })}>bigger</button>
				</Theme.Provider>
			);
		}
	`)
	for _, want := range []string{
		"__krate_ctx_Theme:()=>props.__krate_ctx_Theme()",
		"var theme=krateDerived(()=>props.__krate_ctx_Theme());",
		`,"data-color",()=>theme().color);`,
		`,"style",()=>({fontSize:theme().size}));`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("hydration JS missing %q:\n%s", want, js)
		}
	}
	if !strings.Contains(html, `data-color="teal"`) || !strings.Contains(html, "font-size:14px") {
		t.Errorf("SSR should render the provided value:\n%s", html)
	}
}

func TestModuleStateIsSharedAndHelpersBundled(t *testing.T) {
	outDir := buildWithFiles(t, map[string]string{
		"src/lib/store.ts": `
			export function createStore(initial: number) {
				let value = initial;
				return { get: () => value, set: (n: number) => { value = n; } };
			}
		`,
		"src/pages/index.tsx": `
			import { useState } from 'react';
			import { createStore } from '../lib/store';
			const store = createStore(0);
			if (typeof window !== "undefined") { (window as any).__store = store; }
			function Badge() {
				const [n, setN] = useState(0);
				return <span onClick={() => setN(store.get())}>{n}</span>;
			}
			export default function Page() {
				return <div><Badge /><button onClick={() => store.set(store.get() + 1)}>add</button></div>;
			}
		`,
	})
	js := readHydrationJS(t, outDir)
	if got := strings.Count(js, `var store=__krateModule("store",function(){return createStore(0);});`); got != 2 {
		t.Errorf("each component should read the one shared store (got %d declarations):\n%s", got, js)
	}
	if !strings.Contains(js, "function createStore(initial)") {
		t.Errorf("imported helper used by a module constant must be bundled:\n%s", js)
	}
	if !strings.Contains(js, `__krateModule("#stmt0",function(){if ((typeof window !== 'undefined')) {`) {
		t.Errorf("module statement must run once with the shared value:\n%s", js)
	}
}

func TestUnbundledImportIsDiagnosed(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/lib/counter.ts", `const pair = [5, 6];
export const [total] = pair;`)
	writeFileRel(t, root, "src/pages/index.tsx", `
		import { useState } from 'react';
		import { total } from '../lib/counter';
		export default function Page() {
			const [n, setN] = useState(0);
			return <button onClick={() => setN(total + n)}>{n}</button>;
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
		t.Errorf("expected a KR8302 warning for the unbundled import:\n%s", out)
	}
}

func readHydrationJS(t *testing.T, outDir string) string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(outDir, "index.*.js"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no hydration bundle in %s", outDir)
	}
	return readOut(t, outDir, filepath.Base(files[0]))
}
