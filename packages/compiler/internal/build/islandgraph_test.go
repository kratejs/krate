package build

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// islandEntryFiles lists the entry file of each island chunk of a build.
func islandEntryFiles(outDir string) []string {
	files, _ := filepath.Glob(filepath.Join(outDir, "chunks", "island_*.js"))
	sort.Strings(files)
	return files
}

// islandCode is the text of every island entry and shared chunk of a build.
func islandCode(t *testing.T, outDir string) string {
	t.Helper()
	var sb strings.Builder
	for _, pattern := range []string{"island_*.js", "isl-*.js"} {
		files, _ := filepath.Glob(filepath.Join(outDir, "chunks", pattern))
		sort.Strings(files)
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			sb.Write(data)
			sb.WriteByte('\n')
		}
	}
	if sb.Len() == 0 {
		t.Fatalf("no island chunks in %s", outDir)
	}
	return sb.String()
}

// islandFixture adds stand-ins for a query client library (a context provider
// whose hook throws without it) and a theme library, as React packages.
func islandProviderFixture(t *testing.T, root string) {
	t.Helper()
	lib := func(name, src string) {
		writeProjectFile(t, root, "node_modules/"+name+"/package.json", `{"name":"`+name+`","version":"1.0.0","main":"index.js","peerDependencies":{"react":"*"}}`)
		writeProjectFile(t, root, "node_modules/"+name+"/index.js", src)
	}
	lib("fake-query", `
		import React from 'react';
		const Ctx = React.createContext(null);
		export class QueryClient {
			constructor() { this.data = {}; this.subs = new Set(); (globalThis.__clients = globalThis.__clients || new Set()).add(this); }
		}
		export function QueryClientProvider({ client, children }) {
			return React.createElement(Ctx.Provider, { value: client }, children);
		}
		export function useQueryClient() {
			const c = React.useContext(Ctx);
			if (!c) throw new Error('No QueryClient set, use QueryClientProvider to set one');
			return c;
		}
		export function useQuery(key) {
			const c = useQueryClient();
			const [v, setV] = React.useState(c.data[key] === undefined ? null : c.data[key]);
			React.useEffect(() => {
				const sub = () => setV(c.data[key]);
				c.subs.add(sub);
				if (c.data[key] === undefined) { c.data[key] = 'loaded:' + key; c.subs.forEach((s) => s()); } else { setV(c.data[key]); }
				return () => c.subs.delete(sub);
			}, []);
			return v;
		}
	`)
	lib("fake-theme", `
		import React from 'react';
		const Ctx = React.createContext({ name: 'none' });
		export function ThemeProvider({ theme, children }) {
			return React.createElement(Ctx.Provider, { value: theme }, children);
		}
		export function useTheme() { return React.useContext(Ctx); }
	`)
	lib("fake-box", `
		import React from 'react';
		export function Box({ children }) { return React.createElement('div', { className: 'box' }, children); }
	`)
	writeProjectFile(t, root, "src/components/Queried.tsx", `
		import { useQuery } from 'fake-query';
		export default function Queried() { return <i className="q">{useQuery('k')}</i>; }
	`)
	writeProjectFile(t, root, "src/components/Themed.tsx", `
		import { useTheme } from 'fake-theme';
		export default function Themed() { return <i id="t">{useTheme().name}</i>; }
	`)
}

// providerSite writes the layouts and pages the provider tests share: an
// /app section whose layout holds a query client and a theme provider, a
// nested /app/nested section with another theme, a page without islands, and
// the store island from the other fixtures.
func providerSite(t *testing.T, root string) {
	t.Helper()
	reactLibFixture(t, root)
	islandProviderFixture(t, root)
	writeProjectFile(t, root, "src/store.ts", `
		import { create } from 'fake-store';
		export const useCount = create((set) => ({ n: 1, inc: () => set((s) => ({ n: s.n + 1 })) }));
	`)
	writeProjectFile(t, root, "src/components/Counter.tsx", `
		import { useCount } from '../store';
		export function Counter({ label }: { label: string }) {
			const n = useCount((s) => s.n);
			const inc = useCount((s) => s.inc);
			return <button id="inc" onClick={inc}>{label} {n}</button>;
		}
	`)
	providerPages(t, root)
}

// providerPages writes the layouts and pages of providerSite (they need the
// fixtures of reactLibFixture, islandProviderFixture and the store island).
func providerPages(t *testing.T, root string) {
	t.Helper()
	writeProjectFile(t, root, "src/pages/app/_layout.tsx", `
		import { useState } from 'react';
		import { QueryClient, QueryClientProvider } from 'fake-query';
		import { ThemeProvider } from 'fake-theme';
		const base = { name: 'light' };
		export default function Layout({ children }) {
			const [client] = useState(() => new QueryClient());
			return (
				<div id="app-shell">
					<nav><Link href="/app/">one</Link> <Link href="/app/second/">two</Link> <Link href="/app/plain/">plain</Link> <Link href="/app/nested/">nested</Link></nav>
					<QueryClientProvider client={client}>
						<ThemeProvider theme={base}>{children}</ThemeProvider>
					</QueryClientProvider>
				</div>
			);
		}
	`)
	writeProjectFile(t, root, "src/pages/app/nested/_layout.tsx", `
		import { ThemeProvider } from 'fake-theme';
		export default function Nested({ children }) {
			return <section id="nested"><ThemeProvider theme={{ name: 'dark' }}>{children}</ThemeProvider></section>;
		}
	`)
	writeProjectFile(t, root, "src/pages/app/index.tsx", `
		import { Counter } from '../../components/Counter';
		import Queried from '../../components/Queried';
		import Themed from '../../components/Themed';
		export default function Page() { return <main><h1>one</h1><Counter label="count" /><Queried /><Queried /><Themed /></main>; }
	`)
	writeProjectFile(t, root, "src/pages/app/second.tsx", `
		import { Counter } from '../../components/Counter';
		import Queried from '../../components/Queried';
		export default function Page() { return <main><h1>two</h1><Counter label="count" /><Queried /></main>; }
	`)
	writeProjectFile(t, root, "src/pages/app/plain.tsx", `
		export default function Page() { return <main><h1>plain</h1><p>no islands</p></main>; }
	`)
	writeProjectFile(t, root, "src/pages/app/nested/index.tsx", `
		import Queried from '../../../components/Queried';
		import Themed from '../../../components/Themed';
		export default function Page() { return <main><h1>nested</h1><Queried /><Themed /></main>; }
	`)
	writeProjectFile(t, root, "src/pages/solo.tsx", `
		export default function Page() { return <main><h1>solo</h1></main>; }
	`)
}

func islandSharedImports(t *testing.T, entry string) []string {
	t.Helper()
	data, err := os.ReadFile(entry)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range islandSharedRe.FindAllStringSubmatch(string(data), -1) {
		out = append(out, m[1])
	}
	sort.Strings(out)
	return out
}

// TestLayoutProvidersReachIslands: a provider a layout wraps around {children}
// renders around every island of the page, so the island sees its context at
// build time and in the browser, with one client for all of them.
func TestLayoutProvidersReachIslands(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	providerSite(t, root)
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	for _, bad := range []string{"No QueryClient", "client-only", "KR8015", "KR8016"} {
		if strings.Contains(out, bad) {
			t.Fatalf("build output mentions %q:\n%s", bad, out)
		}
	}
	html := readBuilt(t, filepath.Join(cfg.OutDir, "app", "index.html"))
	if !strings.Contains(html, `id="app-shell"`) || !strings.Contains(html, `<i id="t">light</i>`) {
		t.Fatalf("the island was not server-rendered inside the layout's provider:\n%s", html)
	}
	if got := strings.Count(html, "data-krate-island-ssr"); got != 4 {
		t.Fatalf("want 4 server-rendered islands, got %d:\n%s", got, html)
	}
	nested := readBuilt(t, filepath.Join(cfg.OutDir, "app", "nested", "index.html"))
	if !strings.Contains(nested, `<i id="t">dark</i>`) || !strings.Contains(nested, `id="nested"`) {
		t.Fatalf("the inner layout's provider must win:\n%s", nested)
	}
	// One copy of the library and of the generated provider module, shared by
	// the island chunks of every page.
	all := islandCode(t, cfg.OutDir)
	if n := strings.Count(all, "No QueryClient set"); n != 1 {
		t.Fatalf("the query library must be bundled once, found %d copies", n)
	}
	var shared []string
	for _, f := range islandEntryFiles(cfg.OutDir) {
		shared = append(shared, strings.Join(islandSharedImports(t, f), ","))
	}
	if len(shared) < 3 {
		t.Fatalf("want an island entry for each page, got %v", shared)
	}
	// The layout keeps the markup it owns and drops the lifted wrappers.
	if layoutJS := readBuilt(t, mustGlobOne(t, filepath.Join(cfg.OutDir, "app", "index.*.js"))); strings.Contains(layoutJS, "QueryClient") {
		t.Fatalf("the provider leaked into the page script:\n%.300s", layoutJS)
	}
	// A page without islands ships no island code.
	plain := readBuilt(t, filepath.Join(cfg.OutDir, "app", "plain", "index.html"))
	if strings.Contains(plain, "island_") || strings.Contains(plain, "isl-") {
		t.Fatalf("a page without islands references island chunks:\n%s", plain)
	}
	solo := readBuilt(t, filepath.Join(cfg.OutDir, "solo", "index.html"))
	if strings.Contains(solo, "<script src") || strings.Contains(solo, "island_") {
		t.Fatalf("a static page loads scripts:\n%s", solo)
	}
}

// TestIslandModulesAreSharedAcrossPages: a store module two pages import is
// one file, so a client-side navigation finds the module it already evaluated.
func TestIslandModulesAreSharedAcrossPages(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	providerSite(t, root)
	if out, err := captureOutput(func() error { return New(root, cfg).BuildAll() }); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	all := islandCode(t, cfg.OutDir)
	if n := strings.Count(all, "useSyncExternalStore"); n != 1 {
		t.Fatalf("the store library must be bundled once across pages, found %d copies", n)
	}
	one := islandSharedImports(t, islandEntryOf(t, cfg.OutDir, "app"))
	two := islandSharedImports(t, islandEntryOf(t, cfg.OutDir, "app/second"))
	if len(one) == 0 || strings.Join(one, ",") != strings.Join(two, ",") && !overlaps(one, two) {
		t.Fatalf("the two pages must import the same shared chunk, got %v and %v", one, two)
	}
}

func overlaps(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

// islandEntryOf finds the island entry the script of a page imports.
func islandEntryOf(t *testing.T, outDir, pageDir string) string {
	t.Helper()
	js := readBuilt(t, mustGlobOne(t, filepath.Join(outDir, filepath.FromSlash(pageDir), "index.*.js")))
	rels := islandChunkRe.FindAllString(js, -1)
	if len(rels) == 0 {
		t.Fatalf("the script of %s imports no island:\n%.300s", pageDir, js)
	}
	return filepath.Join(outDir, filepath.FromSlash(rels[0]))
}

// TestLayoutProviderDiagnostics: a wrapper that cannot move into the islands
// is reported with a precise warning instead of failing in the browser.
func TestLayoutProviderDiagnostics(t *testing.T) {
	cases := []struct {
		name   string
		layout string
		want   []string
	}{
		{
			name: "reads the layout params",
			layout: `
				import { ThemeProvider } from 'fake-theme';
				export default function Layout({ children, params }) {
					return <ThemeProvider theme={{ name: params.theme }}>{children}</ThemeProvider>;
				}`,
			want: []string{"[KR8015]", "ThemeProvider", "params"},
		},
		{
			name: "state with a setter",
			layout: `
				import { useState } from 'react';
				import { ThemeProvider } from 'fake-theme';
				export default function Layout({ children }) {
					const [theme, setTheme] = useState({ name: 'light' });
					return <div><button onClick={() => setTheme({ name: 'dark' })}>flip</button><ThemeProvider theme={theme}>{children}</ThemeProvider></div>;
				}`,
			want: []string{"[KR8015]", "ThemeProvider", "setTheme"},
		},
		{
			name: "renders markup of its own",
			layout: `
				import { Box } from 'fake-box';
				export default function Layout({ children }) {
					return <Box>{children}</Box>;
				}`,
			want: []string{"[KR8016]", "renders markup around its children"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			cfg := setupIslandProject(t, root, true)
			reactLibFixture(t, root)
			islandProviderFixture(t, root)
			writeProjectFile(t, root, "src/pages/app/_layout.tsx", tc.layout)
			writeProjectFile(t, root, "src/pages/app/index.tsx", `
				import Themed from '../../components/Themed';
				export default function Page() { return <main><Themed /></main>; }
			`)
			// A wrapper left in the layout may still fail to compile there; the
			// warning has to be in the output either way.
			out, _ := captureOutput(func() error { return New(root, cfg).BuildAll() })
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Fatalf("build output lacks %q:\n%s", want, out)
				}
			}
		})
	}
}

// TestIslandChunksSurviveDiskCache: a rebuild that replays pages from the disk
// cache still writes their island chunks and the Preact import map.
func TestIslandChunksSurviveDiskCache(t *testing.T) {
	t.Setenv("KRATE_NO_BUILD_CACHE", "")
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	providerSite(t, root)
	if out, err := captureOutput(func() error { return New(root, cfg).BuildAll() }); err != nil {
		t.Fatalf("first build: %v\n%s", err, out)
	}
	first := islandCode(t, cfg.OutDir)
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("second build: %v\n%s", err, out)
	}
	if second := islandCode(t, cfg.OutDir); second != first {
		t.Fatalf("the island chunks of a cached rebuild differ")
	}
	html := readBuilt(t, filepath.Join(cfg.OutDir, "app", "index.html"))
	if !strings.Contains(html, `type="importmap"`) {
		t.Fatalf("a cached rebuild lost the island import map:\n%s", html)
	}
	if chunks, _ := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "preact-preact.*.js")); len(chunks) == 0 {
		t.Fatalf("a cached rebuild lost the shared Preact chunks")
	}
}

// TestPerRequestPageIslandStubs: the server bundle of a page rendered per
// request does not run its islands in Krate's server renderer; it emits their
// placeholders with the markup rendered at build time.
func TestPerRequestPageIslandStubs(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	providerSite(t, root)
	for name, export := range map[string]string{"ssr": `export const dynamic = 'force-dynamic';`, "isr": `export const revalidate = 60;`} {
		writeProjectFile(t, root, "src/pages/app/"+name+".tsx", `
			import { Counter } from '../../components/Counter';
			import Themed from '../../components/Themed';
			`+export+`
			export default function Page() { return <main><h1>`+name+`</h1><p id="stamp">{String(Date.now())}</p><Counter label="count" /><Themed /></main>; }
		`)
	}
	if out, err := captureOutput(func() error { return New(root, cfg).BuildAll() }); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	for _, name := range []string{"ssr", "isr"} {
		bundle := readBuilt(t, filepath.Join(cfg.OutDir, ".krate", "server-bundles", "app", name+".server.mjs"))
		for _, bad := range []string{"useSyncExternalStore", "fake-store", "useContext"} {
			if strings.Contains(bundle, bad) {
				t.Fatalf("%s: the island code reached the server bundle (%q):\n%.600s", name, bad, bundle)
			}
		}
		for _, want := range []string{`"data-krate-island-id": "island-1"`, `"data-krate-island-id": "island-2"`, "count 1", `<i id="t">light</i>`} {
			if !strings.Contains(bundle, want) {
				t.Fatalf("%s: the server bundle lacks %q:\n%s", name, want, bundle)
			}
		}
	}
}
