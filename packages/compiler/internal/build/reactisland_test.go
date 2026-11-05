package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// reactLibFixture adds small stand-ins for the npm React libraries that the
// island path has to run: a zustand-style store hook (default `import React`),
// a react-hook-form-style hook (namespace import), a CommonJS hook library
// (`require("react")`, as swr's shim does) and a styled-components-style
// factory. Each declares react as a peer dependency, as the real ones do.
func reactLibFixture(t *testing.T, root string) {
	t.Helper()
	lib := func(name, peer, src string) {
		dir := filepath.Join(root, "node_modules", name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		pkg := `{"name":"` + name + `","version":"1.0.0","main":"index.js","peerDependencies":{"react":"*"}}`
		if peer == "" {
			pkg = `{"name":"` + name + `","version":"1.0.0","main":"index.js"}`
		}
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
	lib("fake-store", "react", `
		import React from 'react';
		export function create(init) {
			const listeners = new Set();
			let state;
			const set = (p) => { state = { ...state, ...(typeof p === 'function' ? p(state) : p) }; listeners.forEach((l) => l()); };
			state = init(set);
			const subscribe = (cb) => { listeners.add(cb); return () => listeners.delete(cb); };
			return function useStore(sel) {
				return React.useSyncExternalStore(subscribe, () => sel(state), () => sel(state));
			};
		}
	`)
	lib("fake-form", "react", `
		import * as React from 'react';
		export function useForm() {
			const ref = React.useRef({ name: '' });
			const [, bump] = React.useState(0);
			return { value: () => ref.current.name, set: (v) => { ref.current.name = v; bump((n) => n + 1); } };
		}
	`)
	lib("fake-cjs", "react", `
		var React = require('react');
		exports.useGreeting = function (who) { return React.useMemo(function () { return 'hello ' + who; }, [who]); };
	`)
	lib("styled-components", "react", `
		import React from 'react';
		function make(tag) {
			return function (strings) {
				var css = strings.join('');
				return function Styled(props) {
					React.useEffect(function () {
						var s = document.createElement('style');
						s.setAttribute('data-fake-styled', '');
						s.textContent = '.fs-' + tag + '{' + css + '}';
						document.head.appendChild(s);
					}, []);
					return React.createElement(tag, { id: props.id, className: 'fs-' + tag }, props.children);
				};
			};
		}
		var styled = { div: make('div') };
		export default styled;
	`)
}

func readBuilt(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeProjectFile(t *testing.T, root, rel, src string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestPreactCompatChunkHasDefaultExport: libraries written as
// `import React from "react"` (recharts, headlessui) need the shared
// preact/compat module to export a default.
func TestPreactCompatChunkHasDefaultExport(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	cfg.Minify = true
	if _, err := captureOutput(func() error { return New(root, cfg).BuildAll() }); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	chunks, _ := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "preact-preact-compat.*.js"))
	if len(chunks) != 1 {
		t.Fatalf("expected one preact/compat chunk, got %v", chunks)
	}
	if code := readBuilt(t, chunks[0]); !regexp.MustCompile(`\bas default\b|export default`).MatchString(code) {
		t.Fatalf("preact/compat chunk has no default export:\n%.600s", code)
	}
}

// TestReactHookLibraryPageIsland: a page whose component calls a hook of an npm
// React library runs as a Preact island (rendered on the server, hydrated in
// place) instead of crashing in the browser.
func TestReactHookLibraryPageIsland(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	reactLibFixture(t, root)
	writeProjectFile(t, root, "src/pages/index.tsx", `
		import { create } from 'fake-store';
		import { useState } from 'react';
		const useCount = create((set) => ({ n: 4, inc: () => set((s) => ({ n: s.n + 1 })) }));
		export default function Page() {
			const n = useCount((s) => s.n);
			const inc = useCount((s) => s.inc);
			const [label] = useState('clicks');
			return <button id="b" onClick={inc}>{label} {n}</button>;
		}
	`)
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	html := readBuilt(t, filepath.Join(cfg.OutDir, "index.html"))
	if !strings.Contains(html, "data-krate-island-ssr") || !strings.Contains(html, "clicks 4") {
		t.Fatalf("page island was not server-rendered:\n%s", html)
	}
	if strings.Contains(out, "KR8008") || strings.Contains(out, "KR2002") {
		t.Fatalf("unexpected warnings:\n%s", out)
	}
	jsFiles, _ := filepath.Glob(filepath.Join(cfg.OutDir, "index.*.js"))
	for _, f := range jsFiles {
		if js := readBuilt(t, f); strings.Contains(js, "useSyncExternalStore") {
			t.Fatalf("the store library leaked into the page script:\n%.400s", js)
		}
	}
	chunk := islandCode(t, cfg.OutDir)
	if !strings.Contains(chunk, "preact/compat") || strings.Contains(chunk, "react.production") {
		t.Fatalf("island chunk must run on preact/compat:\n%.600s", chunk)
	}
	if !strings.Contains(chunk, "export") || !strings.Contains(chunk, "mount") {
		t.Fatalf("island chunk must export mount():\n%.600s", chunk)
	}
}

func mustGlobOne(t *testing.T, pattern string) string {
	t.Helper()
	m, err := filepath.Glob(pattern)
	if err != nil || len(m) != 1 {
		t.Fatalf("glob %s: want one match, got %v (%v)", pattern, m, err)
	}
	return m[0]
}

// TestReactHookLibraryComponentIslands: components in their own modules become
// islands wherever they are rendered (also through a path alias), islands that
// share a store module share one chunk (so one store), custom hooks carry the
// requirement to the modules that call them, and a signal prop stays live.
func TestReactHookLibraryComponentIslands(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	reactLibFixture(t, root)
	writeProjectFile(t, root, "src/store.ts", `
		import { create } from 'fake-store';
		export const useCount = create((set) => ({ n: 1, inc: () => set((s) => ({ n: s.n + 1 })) }));
	`)
	writeProjectFile(t, root, "src/components/Counter.tsx", `
		import { useCount } from '../store';
		export function Counter({ label }: { label: string }) {
			const n = useCount((s) => s.n);
			return <button id="inc">{label} {n}</button>;
		}
	`)
	writeProjectFile(t, root, "src/components/Display.tsx", `
		import { useCount } from '../store';
		export default function Display() { return <p id="disp">shown {useCount((s) => s.n)}</p>; }
	`)
	writeProjectFile(t, root, "src/hooks.ts", `
		import { useGreeting } from 'fake-cjs';
		export function useHello(who: string) { return useGreeting(who); }
	`)
	writeProjectFile(t, root, "src/components/Hello.tsx", `
		import { useHello } from '../hooks';
		export default function Hello() { return <i id="hello">{useHello('krate')}</i>; }
	`)
	writeProjectFile(t, root, "src/pages/index.tsx", `
		import { createSignal } from '@krate/runtime';
		import { Counter } from '../components/Counter';
		import Display from '../components/Display';
		import Hello from '../components/Hello';
		export default function Page() {
			const [label, setLabel] = createSignal('count');
			return (
				<main>
					<button id="rename" onClick={() => setLabel('clicks')}>rename</button>
					<Counter label={label()} />
					<Display />
					<Hello />
				</main>
			);
		}
	`)
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	html := readBuilt(t, filepath.Join(cfg.OutDir, "index.html"))
	for _, want := range []string{"count 1", "shown 1", "hello krate"} {
		if !strings.Contains(html, want) {
			t.Fatalf("server render lacks %q:\n%s", want, html)
		}
	}
	chunks := islandEntryFiles(cfg.OutDir)
	if len(chunks) != 1 {
		t.Fatalf("islands that run the project's own modules must share one chunk, got %v", chunks)
	}
	chunk := islandCode(t, cfg.OutDir)
	if n := strings.Count(chunk, "useSyncExternalStore"); n != 1 {
		t.Fatalf("the store library must be bundled once (shared store state), found %d copies", n)
	}
	if strings.Contains(chunk, "Dynamic require") {
		t.Fatalf("CommonJS require of react must not reach the browser:\n%.400s", chunk)
	}
	if !strings.Contains(html, "island-1") || !strings.Contains(html, "island-3") {
		t.Fatalf("expected three islands:\n%s", html)
	}
}

// TestReactHookLibraryInLayoutDiagnostic: a layout cannot host an island, and
// that must be a build error naming the package, not a silent browser failure.
func TestReactHookLibraryInLayoutDiagnostic(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	reactLibFixture(t, root)
	writeProjectFile(t, root, "src/components/Cart.tsx", `
		import { create } from 'fake-store';
		const useCart = create(() => ({ n: 0 }));
		export default function Cart() { return <b>{useCart((s) => s.n)}</b>; }
	`)
	writeProjectFile(t, root, "src/pages/_layout.tsx", `
		import Cart from '../components/Cart';
		export default function Layout({ children }) { return <div><Cart />{children}</div>; }
	`)
	_, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err == nil {
		t.Fatal("expected a build error for a React hook library used in a layout")
	}
	for _, want := range []string{"[KR8013]", "fake-store", "useCart", "layout"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("diagnostic lacks %q: %v", want, err)
		}
	}
}

// TestReactCSSInJSIsland: a component built by a CSS-in-JS factory runs as a
// client-only island (the styles are created in the browser), and a css prop
// without emotion's JSX runtime is reported.
func TestReactCSSInJSIsland(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	reactLibFixture(t, root)
	writeProjectFile(t, root, "src/pages/index.tsx", `
		import styled from 'styled-components';
		const Box = styled.div`+"`color: red;`"+`;
		export default function Page() { return <Box id="o">hi</Box>; }
	`)
	writeProjectFile(t, root, "src/pages/css.tsx", `
		export default function Page() { return <div css={{ color: 'red' }}>x</div>; }
	`)
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	html := readBuilt(t, filepath.Join(cfg.OutDir, "index.html"))
	if strings.Contains(html, "data-krate-island-ssr") || !strings.Contains(html, "data-krate-island=") {
		t.Fatalf("CSS-in-JS island must be an empty client-only placeholder:\n%s", html)
	}
	if !strings.Contains(out, "[KR8008]") || !strings.Contains(out, "CSS-in-JS") {
		t.Fatalf("expected the client-only notice:\n%s", out)
	}
	if !strings.Contains(out, "[KR8014]") || !strings.Contains(out, "css/") && !strings.Contains(out, "css.tsx") {
		t.Fatalf("expected a css prop warning:\n%s", out)
	}
}

// TestIslandClientDirectives: client:visible and client:idle defer the island
// chunk (no modulepreload, no fetch at load) and never reach the component.
func TestIslandClientDirectives(t *testing.T) {
	for _, tc := range []struct {
		directive, marker string
	}{
		{"client:visible", "IntersectionObserver"},
		{"client:idle", "requestIdleCallback"},
	} {
		t.Run(tc.directive, func(t *testing.T) {
			root := t.TempDir()
			cfg := setupIslandProject(t, root, true)
			cfg.Minify = true
			writeProjectFile(t, root, "src/pages/index.tsx", `
				import { Badge } from 'fake-lib';
				export default function Page() {
					return <div><Badge `+tc.directive+` label="deferred" /></div>;
				}
			`)
			out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
			if err != nil {
				t.Fatalf("BuildAll: %v\n%s", err, out)
			}
			html := readBuilt(t, filepath.Join(cfg.OutDir, "index.html"))
			if strings.Contains(html, "modulepreload") {
				t.Fatalf("a deferred island chunk must not be preloaded:\n%s", html)
			}
			if !strings.Contains(html, "deferred") {
				t.Fatalf("the island is still server-rendered:\n%s", html)
			}
			js := readBuilt(t, mustGlobOne(t, filepath.Join(cfg.OutDir, "index.*.js")))
			if !strings.Contains(js, tc.marker) {
				t.Fatalf("page script lacks %s:\n%s", tc.marker, js)
			}
			if regexp.MustCompile(`["']/chunks/island_[a-z0-9]+\.js["']`).MatchString(js) {
				t.Fatalf("the island URL must not appear as a preloadable literal:\n%s", js)
			}
			chunk := islandCode(t, cfg.OutDir)
			if strings.Contains(chunk, "client:") {
				t.Fatalf("the directive leaked into the island:\n%s", chunk)
			}
		})
	}
}

// TestClientDirectiveOnNonIslandWarns: a directive on a Krate component does
// nothing, and says so.
func TestClientDirectiveOnNonIslandWarns(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	writeProjectFile(t, root, "src/pages/index.tsx", `
		function Local() { return <b>local</b>; }
		export default function Page() { return <Local client:visible />; }
	`)
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	if !strings.Contains(out, "[KR8012]") || !strings.Contains(out, "client:visible") {
		t.Fatalf("expected KR8012:\n%s", out)
	}
	if html := readBuilt(t, filepath.Join(cfg.OutDir, "index.html")); strings.Contains(html, "client:visible") {
		t.Fatalf("the directive leaked into the HTML:\n%s", html)
	}
}

// TestReactHookLibraryPageForms: the island page works for an arrow function
// default export, and a page that takes route props is refused with a reason
// instead of rendering an island that reads undefined params.
func TestReactHookLibraryPageForms(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	reactLibFixture(t, root)
	writeProjectFile(t, root, "src/pages/index.tsx", `
		import { create } from 'fake-store';
		const useCount = create(() => ({ n: 7 }));
		const Page = () => <b id="n">{useCount((s) => s.n)}</b>;
		export default Page;
	`)
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	if html := readBuilt(t, filepath.Join(cfg.OutDir, "index.html")); !strings.Contains(html, `id="n">7`) {
		t.Fatalf("arrow page was not rendered as an island:\n%s", html)
	}

	writeProjectFile(t, root, "src/pages/index.tsx", `
		import { create } from 'fake-store';
		const useCount = create(() => ({ n: 7 }));
		export default function Page({ params }) { return <b>{useCount((s) => s.n)}{params.id}</b>; }
	`)
	_, err = captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err == nil || !strings.Contains(err.Error(), "[KR8013]") || !strings.Contains(err.Error(), "params") {
		t.Fatalf("expected KR8013 about route props, got: %v", err)
	}
}

// TestReactIslandModulesAreNotKrateSource: the store and library an island
// imports are bundled by the island build only. Krate must not try to compile
// their initializers into the page script (a library function that declares a
// class inside a function, as immer does, failed the whole page build).
func TestReactIslandModulesAreNotKrateSource(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	reactLibFixture(t, root)
	dir := filepath.Join(root, "node_modules", "fake-redux")
	writeProjectFile(t, root, "node_modules/fake-redux/package.json", `{"name":"fake-redux","version":"1.0.0","main":"index.js","peerDependencies":{"react":"*"}}`)
	_ = dir
	writeProjectFile(t, root, "node_modules/fake-redux/index.js", `
		import React from 'react';
		export function createStore(initial) {
			function enableDrafts() { class Draft { constructor(v) { this.v = v; } } return new Draft(initial); }
			return enableDrafts();
		}
		export function useSelector(store, pick) {
			return React.useMemo(function () { return pick(store.v); }, [store, pick]);
		}
	`)
	writeProjectFile(t, root, "src/store.ts", `
		import { createStore } from 'fake-redux';
		export const store = createStore({ n: 5 });
	`)
	writeProjectFile(t, root, "src/pages/index.tsx", `
		import { useSelector } from 'fake-redux';
		import { store } from '../store';
		export default function Page() { return <b id="n">{useSelector(store, (s) => s.n)}</b>; }
	`)
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	if html := readBuilt(t, filepath.Join(cfg.OutDir, "index.html")); !strings.Contains(html, `id="n">5`) {
		t.Fatalf("island was not rendered:\n%s", html)
	}
}

// TestReactLibraryValueInPageMakesIsland: `const client = new Client()` handed
// to a provider tag in the page (the standard TanStack Query setup) cannot
// cross into a separate island as a free variable, so the page module itself
// runs as the island and the child component is bundled with it.
func TestReactLibraryValueInPageMakesIsland(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	reactLibFixture(t, root)
	writeProjectFile(t, root, "node_modules/fake-store/index.js", `
		import React from 'react';
		const Ctx = React.createContext(null);
		export class Client { constructor() { this.n = 3; } }
		export function Provider(props) { return React.createElement(Ctx.Provider, { value: props.client }, props.children); }
		export function useClient() { return React.useContext(Ctx); }
	`)
	writeProjectFile(t, root, "src/components/Todos.tsx", `
		import { useClient } from 'fake-store';
		export default function Todos() { return <p id="todos">n={useClient().n}</p>; }
	`)
	writeProjectFile(t, root, "src/pages/index.tsx", `
		import { Client, Provider } from 'fake-store';
		import Todos from '../components/Todos';
		const client = new Client();
		export default function App() { return <Provider client={client}><Todos /></Provider>; }
	`)
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	if strings.Contains(out, "KR8008") || strings.Contains(out, "KR8007") {
		t.Fatalf("unexpected island warnings:\n%s", out)
	}
	if html := readBuilt(t, filepath.Join(cfg.OutDir, "index.html")); !strings.Contains(html, "n=3") {
		t.Fatalf("provider and child were not rendered together:\n%s", html)
	}
}

// A page built per generateStaticParams entry hosts islands like any page: the
// build-time render keeps their placeholders, each entry's chunk is written,
// and a library imported by the page's islands does not break the params
// evaluation.
func TestGenerateStaticParamsPagesBuildIslands(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	reactLibFixture(t, root)
	// The params evaluation resolves the library's react import to the runtime's
	// React compatibility module.
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "@krate"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repo, "packages", "runtime"), filepath.Join(root, "node_modules", "@krate", "runtime")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	writeProjectFile(t, root, "src/store.ts", `
		import { create } from 'fake-store';
		export const useCount = create((set) => ({ n: 1, inc: () => set((s) => ({ n: s.n + 1 })) }));
	`)
	writeProjectFile(t, root, "src/components/Counter.tsx", `
		import { useCount } from '../store';
		export function Counter({ label }: { label: string }) {
			const n = useCount((s) => s.n);
			return <button id="inc">{label} {n}</button>;
		}
	`)
	writeProjectFile(t, root, "src/pages/gsp/[id].tsx", `
		import { Counter } from '../../components/Counter';
		export function generateStaticParams() { return [{ id: 'a' }, { id: 'b' }]; }
		export default function Page({ params }: any) {
			return <main><h1>entry {params.id}</h1><Counter label={'gsp-' + params.id} /></main>;
		}
	`)
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	for _, id := range []string{"a", "b"} {
		html := readBuilt(t, filepath.Join(cfg.OutDir, "gsp", id, "index.html"))
		if !strings.Contains(html, "data-krate-island") || !strings.Contains(html, "gsp-"+id+" 1") {
			t.Errorf("gsp/%s lacks its server-rendered island:\n%.1500s", id, html)
		}
		m := regexp.MustCompile(`chunks/(island_[0-9a-f]+\.js)`).FindStringSubmatch(html)
		if m == nil {
			t.Errorf("gsp/%s does not load an island chunk", id)
			continue
		}
		if _, err := os.Stat(filepath.Join(cfg.OutDir, "chunks", m[1])); err != nil {
			t.Errorf("gsp/%s names the island chunk %s, which was not written", id, m[1])
		}
	}
}
