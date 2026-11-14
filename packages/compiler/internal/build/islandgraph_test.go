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
