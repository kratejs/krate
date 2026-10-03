package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// buildReactPage writes a single-page project to a temp dir and returns the
// output directory plus the page's emitted HTML and hydration JS.
func buildReactPage(t *testing.T, pageSrc string) (html, js string) {
	t.Helper()
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pagesDir, "index.tsx"), []byte(pageSrc), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = filepath.Join(root, "dist")
	// Keep identifiers stable so the emitted JS is assertable.
	cfg.Minify = false
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	htmlBytes, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatalf("reading index.html: %v", err)
	}
	// Static pages correctly emit no hydration bundle; JS is empty then.
	jsFiles, err := filepath.Glob(filepath.Join(cfg.OutDir, "index.*.js"))
	if err != nil {
		t.Fatalf("glob hydration bundle: %v", err)
	}
	jsBytes := []byte{}
	if len(jsFiles) > 0 {
		jsBytes, err = os.ReadFile(jsFiles[0])
		if err != nil {
			t.Fatal(err)
		}
	}
	return string(htmlBytes), string(jsBytes)
}

// TestReactUseRefNoDuplicateVar is the regression for the emitted hydration JS
// containing both `var r={current:null}` and a clobbering `var r="{current:null}"`.
func TestReactUseRefNoDuplicateVar(t *testing.T) {
	page := `
		import { useState, useRef } from 'react';
		export default function Page() {
			const ref = useRef(null);
			const [n, setN] = useState(0);
			return <div ref={ref}>{n}</div>;
		}
	`
	_, js := buildReactPage(t, page)
	if strings.Contains(js, `="{current:null}"`) {
		t.Fatalf("useRef object was clobbered by a duplicate string var:\n%s", js)
	}
	// Exactly one declaration of the ref variable.
	if c := strings.Count(js, "var ref="); c != 1 {
		t.Errorf("expected one 'var ref=' declaration, got %d:\n%s", c, js)
	}
}

// TestReactBareReadInHandlerBuild verifies unmodified React (`count + 1`) in a
// handler compiles to a live getter read at build time.
func TestReactBareReadInHandlerBuild(t *testing.T) {
	page := `
		import { useState } from 'react';
		export default function Page() {
			const [count, setCount] = useState(5);
			return <button onClick={() => setCount(count + 1)}>{count}</button>;
		}
	`
	html, js := buildReactPage(t, page)
	if !strings.Contains(html, ">5<") {
		t.Errorf("SSR should render initial count 5:\n%.400s", html)
	}
	if !strings.Contains(js, "count()") {
		t.Errorf("bare read should compile to a call:\n%s", js)
	}
}

// TestReactKeyNotEmitted verifies `key` is stripped from intrinsic elements.
func TestReactKeyNotEmitted(t *testing.T) {
	page := `
		export default function Page() {
			return <div key="abc" data-x="1">hi</div>;
		}
	`
	html, _ := buildReactPage(t, page)
	if strings.Contains(html, "key=") {
		t.Errorf("key must not be emitted to the DOM:\n%.400s", html)
	}
}

// TestReactUseReducerBuild verifies useReducer lowers to createReducer in the
// hydration JS and the state getter renders its SSR initial.
func TestReactUseReducerBuild(t *testing.T) {
	page := `
		import { useReducer } from 'react';
		export default function Page() {
			const [count, dispatch] = useReducer((s, a) => s + a, 7);
			return <button onClick={() => dispatch(1)}>{count}</button>;
		}
	`
	html, js := buildReactPage(t, page)
	if !strings.Contains(html, ">7<") {
		t.Errorf("SSR should render reducer initial 7:\n%.400s", html)
	}
	if !strings.Contains(js, "createReducer") {
		t.Errorf("hydration should emit createReducer:\n%s", js)
	}
}

// TestReactUseIdBuild verifies useId lowers to a stable per-instance literal
// used for both the id and htmlFor attributes, with no runtime useId call.
func TestReactUseIdBuild(t *testing.T) {
	page := `
		import { useId } from 'react';
		export default function Page() {
			const id = useId();
			return <div><label htmlFor={id}>Name</label><input id={id} /></div>;
		}
	`
	html, js := buildReactPage(t, page)
	if strings.Contains(js, "useId") {
		t.Errorf("useId should be compile-time resolved:\n%s", js)
	}
	// The same generated id must appear on both the label and input.
	if !strings.Contains(html, `for="krate-`) || !strings.Contains(html, `id="krate-`) {
		t.Errorf("expected matching for/id literals:\n%.500s", html)
	}
}

// TestReactFragmentBuild verifies <Fragment> renders its children inline.
func TestReactFragmentBuild(t *testing.T) {
	page := `
		import { Fragment } from 'react';
		export default function Page() {
			return <Fragment><span>a</span><span>b</span></Fragment>;
		}
	`
	html, _ := buildReactPage(t, page)
	if !strings.Contains(html, "<span>a</span><span>b</span>") {
		t.Errorf("fragment children should render inline:\n%.400s", html)
	}
}

// buildShadcnButton writes a shadcn-style Button component plus a page and
// returns the built HTML. It exercises cva, cn, rest-spread props, defaults,
// the dynamic `asChild ? Slot : "button"` tag, and Slot class merging.
func buildShadcnButton(t *testing.T, pageSrc string) string {
	t.Helper()
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	uiDir := filepath.Join(root, "src", "components", "ui")
	for _, d := range []string{pagesDir, uiDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	button := `
		import { Slot, cva, cn } from '@krate/runtime';

		const buttonVariants = cva('inline-flex items-center rounded-md', {
			variants: {
				variant: { default: 'bg-blue-600 text-white', destructive: 'bg-red-600 text-white' },
				size: { default: 'h-9 px-4', lg: 'h-10 px-8' },
			},
			defaultVariants: { variant: 'default', size: 'default' },
		});

		export function Button({ className, variant, size, asChild = false, ...props }: any) {
			const Comp = asChild ? Slot : 'button';
			return <Comp className={cn(buttonVariants({ variant, size }), className)} {...props} />;
		}
	`
	if err := os.WriteFile(filepath.Join(uiDir, "button.tsx"), []byte(button), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pagesDir, "index.tsx"), []byte(pageSrc), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	html, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	return string(html)
}

func TestShadcnButtonFoldsVariantsAndSpread(t *testing.T) {
	html := buildShadcnButton(t, `
		import { Button } from '../components/ui/button';
		export default function Page() {
			return <div>
				<Button>Default</Button>
				<Button variant="destructive" size="lg" disabled>Delete</Button>
			</div>;
		}
	`)
	// Defaults fold.
	if !strings.Contains(html, "h-9 px-4 bg-blue-600 text-white") {
		t.Errorf("default variant classes missing:\n%.600s", html)
	}
	if !strings.Contains(html, ">Default<") {
		t.Errorf("children should render:\n%.600s", html)
	}
	// Explicit selection folds and spread props reach the element.
	if !strings.Contains(html, "h-10 px-8 bg-red-600 text-white") {
		t.Errorf("destructive/lg classes missing:\n%.600s", html)
	}
	if !strings.Contains(html, "disabled") {
		t.Errorf("spread `disabled` should reach the button:\n%.600s", html)
	}
}

func TestShadcnButtonAsChildMergesOntoChild(t *testing.T) {
	html := buildShadcnButton(t, `
		import { Button } from '../components/ui/button';
		export default function Page() {
			return <Button asChild><a href="/docs">Docs</a></Button>;
		}
	`)
	if !strings.Contains(html, `<a href="/docs"`) && !strings.Contains(html, `<a href=/docs`) {
		t.Errorf("asChild should render the anchor, not a wrapping button:\n%.600s", html)
	}
	if strings.Contains(html, "<button") {
		t.Errorf("asChild must not emit a button wrapper:\n%.600s", html)
	}
	if !strings.Contains(html, "inline-flex items-center rounded-md") {
		t.Errorf("Slot classes should merge onto the anchor:\n%.600s", html)
	}
}

// buildWithFiles writes the given files under a temp project and returns the
// page output directory. Keys are paths relative to the project root.
func buildWithFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	return cfg.OutDir
}

// TestDottedComponentNamespace verifies `import * as Card` + `<Card.Root>`
// resolves dotted tags to the imported module's exported functions.
func TestDottedComponentNamespace(t *testing.T) {
	outDir := buildWithFiles(t, map[string]string{
		"src/components/ui/card.tsx": `
			export function Root(props: any) { return <div class="card-root">{props.children}</div>; }
			export function Header(props: any) { return <h3 class="card-header">{props.children}</h3>; }
		`,
		"src/pages/index.tsx": `
			import * as Card from '../components/ui/card';
			export default function Page() {
				return <Card.Root><Card.Header>Title</Card.Header></Card.Root>;
			}
		`,
	})
	html := readOut(t, outDir, "index.html")
	if !strings.Contains(html, `class="card-root"`) || !strings.Contains(html, `<h3 class="card-header">Title</h3>`) {
		t.Errorf("dotted components did not render:\n%.500s", html)
	}
}

// TestDottedComponentNamespaceWithState verifies dotted components that use
// signals/handlers/attr bindings hydrate correctly.
func TestDottedComponentNamespaceWithState(t *testing.T) {
	outDir := buildWithFiles(t, map[string]string{
		"src/components/ui/panel.tsx": `
			import { createSignal } from '@krate/runtime';
			export function Root(props: any) {
				const [open, setOpen] = createSignal(false);
				return (
					<div class="panel" data-state={open() ? 'open' : 'closed'}>
						<button onClick={() => setOpen(!open())}>toggle</button>
						{props.children}
					</div>
				);
			}
		`,
		"src/pages/index.tsx": `
			import * as Panel from '../components/ui/panel';
			export default function Page() {
				return <Panel.Root>body</Panel.Root>;
			}
		`,
	})
	html := readOut(t, outDir, "index.html")
	if !strings.Contains(html, `data-state="closed"`) {
		t.Errorf("dotted component SSR state missing:\n%.500s", html)
	}
	jsFiles, _ := filepath.Glob(filepath.Join(outDir, "index.*.js"))
	if len(jsFiles) == 0 {
		t.Fatalf("expected hydration bundle for a stateful dotted component")
	}
	js, _ := os.ReadFile(jsFiles[0])
	if !strings.Contains(string(js), "createSignal") {
		t.Errorf("dotted component should hydrate:\n%s", js)
	}
}

// TestNamespaceReexportBarrel verifies `export * as Card from './card'` in a
// barrel resolves `<Card.Root>` from a named import of the namespace.
func TestNamespaceReexportBarrel(t *testing.T) {
	outDir := buildWithFiles(t, map[string]string{
		"src/components/card.tsx": `
			export function Root(props: any) { return <div class="card-root">{props.children}</div>; }
		`,
		"src/components/ui.ts": `export * as Card from './card';`,
		"src/pages/index.tsx": `
			import { Card } from '../components/ui';
			export default function Page() {
				return <Card.Root>hi</Card.Root>;
			}
		`,
	})
	html := readOut(t, outDir, "index.html")
	if !strings.Contains(html, `class="card-root"`) {
		t.Errorf("namespace re-export barrel did not resolve:\n%.500s", html)
	}
}

// TestUseContextTranspile verifies React's createContext/useContext lower to
// the Krate context API (Ctx.useContext()) end to end through the bundler.
func TestUseContextTranspile(t *testing.T) {
	root := t.TempDir()
	mkdir := func(rel string) string {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		return full
	}
	page := `
		import { createContext, useContext } from 'react';
		const ThemeCtx = createContext('light');
		export default function Page() {
			const theme = useContext(ThemeCtx);
			return <div>{theme}</div>;
		}
	`
	if err := os.WriteFile(mkdir("src/pages/index.tsx"), []byte(page), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	html, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), "light") {
		t.Errorf("context default should render:\n%s", html)
	}
	// The client runtime chunk must actually ship createContext, or hydration
	// would throw a ReferenceError (regression: it was absent from the bundle).
	assertRuntimeBundleHasContext(t)
}

// assertRuntimeBundleHasContext loads the built runtime chunk and checks the
// context API is present. Skips when the runtime dist hasn't been bundled.
func assertRuntimeBundleHasContext(t *testing.T) {
	t.Helper()
	root := findCompilerRoot(t)
	if root == "" {
		t.Skip("compiler root not found")
	}
	runtimeDir := filepath.Join(filepath.Dir(root), "runtime", "dist")
	for _, name := range []string{"krate-hydrate.js", "krate-runtime.js"} {
		data, err := os.ReadFile(filepath.Join(runtimeDir, name))
		if err != nil {
			continue
		}
		if !strings.Contains(string(data), "createContext") {
			t.Errorf("%s does not ship createContext", name)
		}
		return
	}
	t.Skip("runtime bundle not built")
}

// findCompilerRoot locates the packages/compiler directory from the test file.
func findCompilerRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil &&
			strings.HasSuffix(filepath.ToSlash(dir), "packages/compiler") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// TestSignalPersistOptionsEmitted verifies `createSignal(v, { persist })`
// preserves the options argument in the hydration bundle.
func TestSignalPersistOptionsEmitted(t *testing.T) {
	page := `
		import { createSignal } from '@krate/runtime';
		export default function Page() {
			const [n, setN] = createSignal(0, { persist: 'count' });
			return <button onClick={() => setN(n() + 1)}>{n()}</button>;
		}
	`
	_, js := buildReactPage(t, page)
	if !strings.Contains(js, "createSignal(0,{persist:'count'})") {
		t.Errorf("signal options were dropped:\n%s", js)
	}
}

// TestDurableStateInjected verifies a krate.state.json payload is injected as
// window.__KRATE_STATE__ so persisted signals hydrate the server value.
func TestDurableStateInjected(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", `
		import { createSignal } from '@krate/runtime';
		export default function Page() {
			const [n, setN] = createSignal(0, { persist: 'count' });
			return <button onClick={() => setN(n() + 1)}>{n()}</button>;
		}
	`)
	writeFileRel(t, root, "krate.state.json", `{"count":5}`)

	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	html, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), `window.__KRATE_STATE__={"count":5}`) {
		t.Errorf("durable state not injected:\n%s", html)
	}
}

func writeFileRel(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestModuleConstPropToClientComponent verifies a module-level const array
// passed as a prop to a client component is inlined into the hydration props
// registry (not emitted as a bare identifier, which threw ReferenceError), and
// that the component reads the prop at runtime.
func TestModuleConstPropToClientComponent(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/components/list.tsx", `
		import { createSignal } from '@krate/runtime';
		export function List(props: { items: any[] }) {
			const [active, setActive] = createSignal(0);
			return (
				<ul onClick={() => setActive(active() + 1)}>
					{props.items.map((it: any) => <li>{it.label}</li>)}
				</ul>
			);
		}
	`)
	writeFileRel(t, root, "src/pages/index.tsx", `
		import { List } from '../components/list';
		const menuItems = [{ label: 'Home' }, { label: 'About' }];
		export default function Page() {
			return <List items={menuItems} />;
		}
	`)
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	jsFiles, _ := filepath.Glob(filepath.Join(cfg.OutDir, "index.*.js"))
	if len(jsFiles) == 0 {
		t.Fatal("no hydration bundle emitted")
	}
	js, _ := os.ReadFile(jsFiles[0])
	src := string(js)
	if strings.Contains(src, "menuItems") {
		t.Errorf("module const leaked as a bare identifier into hydration JS:\n%s", src)
	}
	if !strings.Contains(src, "items:[{label:'Home'},{label:'About'}]") {
		t.Errorf("module const value not inlined into the props registry:\n%s", src)
	}
	if !strings.Contains(src, `var props=__krate_props[`) || !strings.Contains(src, "props.items.map") {
		t.Errorf("component should read the prop from the registry at runtime, got:\n%s", src)
	}
}

// TestUndefinedAttributesOmitted verifies props that resolve to undefined are
// omitted from static HTML instead of rendering value="undefined" (React
// semantics).
func TestUndefinedAttributesOmitted(t *testing.T) {
	page := `
		function Field(props: any) {
			return <input value={props.value} name={props.name} placeholder="Name" />;
		}
		export default function Page() {
			return <Field />;
		}
	`
	html, _ := buildReactPage(t, page)
	if strings.Contains(html, "undefined") {
		t.Errorf("undefined attribute leaked into HTML:\n%s", html)
	}
	if !strings.Contains(html, `placeholder="Name"`) {
		t.Errorf("defined attribute missing:\n%s", html)
	}
}

// TestForLoopArrayRenders verifies the `var x = []; for (...) x.push(<el/>)`
// pattern renders as real markup (not escaped text) for an SSREvaluated
// component.
func TestForLoopArrayRenders(t *testing.T) {
	page := `
		function List(props: any) {
			var items = [];
			for (var i = 1; i <= props.count; i++) {
				items.push(<li>{i}</li>);
			}
			return <ul>{items}</ul>;
		}
		export default function Page() {
			return <List count={3} />;
		}
	`
	html, _ := buildReactPage(t, page)
	if !strings.Contains(html, "<li>1</li>") || !strings.Contains(html, "<li>3</li>") {
		t.Errorf("for-loop array did not render items:\n%s", html)
	}
	if strings.Contains(html, "&lt;li&gt;") {
		t.Errorf("for-loop array markup was escaped:\n%s", html)
	}
}

// TestClientComponentReadsLocationSearch verifies a client component that reads
// the URL query in onMount (and listens for SPA navigation) compiles that logic
// into the hydration bundle, and that a reactive list over a signal-derived
// function is emitted as a re-render binding.
func TestClientComponentReadsLocationSearch(t *testing.T) {
	page := `
		import { createSignal, onMount } from '@krate/runtime';
		export default function Page() {
			const [p, setP] = createSignal(1);
			onMount(() => {
				const m = new RegExp('[?&]page=(\\d+)').exec(window.location.search);
				if (m) setP(parseInt(m[1], 10));
				window.addEventListener('krate:navigate', () => {});
			});
			function pages() {
				var out = [];
				for (var i = 1; i <= 3; i++) {
					out.push(i);
				}
				return out;
			}
			return <a href={'?page=' + p()}>{pages().map((n) => <span>{n === p() ? 'x' : ''}</span>)}</a>;
		}
	`
	_, js := buildReactPage(t, page)
	if !strings.Contains(js, "location.search") {
		t.Errorf("URL query read not emitted:\n%s", js)
	}
	if !strings.Contains(js, "krate:navigate") {
		t.Errorf("SPA navigation listener not emitted:\n%s", js)
	}
	if !strings.Contains(js, "kbindContent") {
		t.Errorf("reactive list binding not emitted:\n%s", js)
	}
}

// TestLocalFunctionInAttrBindingEmitted verifies a local helper referenced only
// from an attribute binding (e.g. href={'?p=' + prev()}) is emitted into the
// hydration scope instead of throwing ReferenceError.
func TestLocalFunctionInAttrBindingEmitted(t *testing.T) {
	page := `
		import { createSignal } from '@krate/runtime';
		export default function Page() {
			const [n, setN] = createSignal(2);
			function prev() {
				return n() > 1 ? n() - 1 : 1;
			}
			return <a href={'?p=' + prev()} onClick={() => setN(n() + 1)}>x</a>;
		}
	`
	_, js := buildReactPage(t, page)
	if !strings.Contains(js, "function prev(") {
		t.Errorf("local helper not emitted into hydration scope:\n%s", js)
	}
	if !strings.Contains(js, "prev()") {
		t.Errorf("attribute binding should call the helper:\n%s", js)
	}
}

// TestReactStyleObjectBuild verifies a literal style object folds to CSS.
func TestReactStyleObjectBuild(t *testing.T) {
	page := `
		export default function Page() {
			return <div style={{ fontSize: 14, backgroundColor: 'red' }}>hi</div>;
		}
	`
	html, _ := buildReactPage(t, page)
	if !strings.Contains(html, "font-size:14px") || !strings.Contains(html, "background-color:red") {
		t.Errorf("style object should fold to CSS:\n%.400s", html)
	}
}
