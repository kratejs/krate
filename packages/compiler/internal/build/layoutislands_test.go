package build

import (
	"path/filepath"
	"strings"
	"testing"
)

// toastFixture writes a stand-in for sonner: a Toaster component and a toast()
// function that share module-level state, both built on React.
func toastFixture(t *testing.T, root string) {
	t.Helper()
	writeProjectFile(t, root, "node_modules/sonner/package.json", `{"name":"sonner","version":"2.0.8","main":"index.js","peerDependencies":{"react":"*"}}`)
	writeProjectFile(t, root, "node_modules/sonner/index.js", `
		import React from 'react';
		const SHARED_TOAST_STORE = { subs: new Set(), items: [], tag: 'toast-store-instance' };
		export function toast(msg) {
			SHARED_TOAST_STORE.items.push(msg);
			SHARED_TOAST_STORE.subs.forEach((s) => s());
		}
		export function Toaster() {
			const [n, setN] = React.useState(0);
			React.useEffect(() => {
				const sub = () => setN(SHARED_TOAST_STORE.items.length);
				SHARED_TOAST_STORE.subs.add(sub);
				return () => SHARED_TOAST_STORE.subs.delete(sub);
			}, []);
			return React.createElement('section', { id: 'toaster', 'data-count': n });
		}
	`)
	writeProjectFile(t, root, "src/components/Save.tsx", `
		'use client';
		import { toast } from 'sonner';
		export default function Save() {
			return <button id="save" onClick={() => toast('Saved')}>save</button>;
		}
	`)
	writeProjectFile(t, root, "src/pages/app/_layout.tsx", `
		import { Toaster } from 'sonner';
		export default function Layout({ children }) {
			return <div id="shell"><nav><a href="/app/">one</a></nav>{children}<Toaster /></div>;
		}
	`)
	writeProjectFile(t, root, "src/pages/app/index.tsx", `
		import Save from '../../components/Save';
		export default function Page() { return <main><h1>one</h1><Save /></main>; }
	`)
	writeProjectFile(t, root, "src/pages/app/second.tsx", `
		import Save from '../../components/Save';
		export default function Page() { return <main><h1>two</h1><Save /></main>; }
	`)
}

// TestLayoutToasterSharesStoreWithPageIslands: a Toaster in a static layout and
// a client component that only calls toast() run as islands of one chunk
// graph, so the library's module state exists once.
func TestLayoutToasterSharesStoreWithPageIslands(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	toastFixture(t, root)
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	html := readBuilt(t, filepath.Join(cfg.OutDir, "app", "index.html"))
	if !strings.Contains(html, `id="shell"`) || !strings.Contains(html, `id="toaster"`) || !strings.Contains(html, `id="save"`) {
		t.Fatalf("the layout's Toaster and the page's button must both be server-rendered islands:\n%s", html)
	}
	if got := strings.Count(html, "data-krate-island-ssr"); got != 2 {
		t.Fatalf("want 2 server-rendered islands, got %d:\n%s", got, html)
	}
	// One copy of the module state across every page's chunks.
	if copies := strings.Count(islandCode(t, cfg.OutDir), "toast-store-instance"); copies != 1 {
		t.Fatalf("the toast store must be bundled once, found %d copies", copies)
	}
	if strings.Contains(readBuilt(t, mustGlobOne(t, filepath.Join(cfg.OutDir, "app", "index.*.js"))), "toast-store-instance") {
		t.Fatal("the library leaked into the page script")
	}
	// A layout that hosts islands is swapped, not kept, on navigation.
	if !strings.Contains(html, `"p":false`) {
		t.Fatalf("a layout with islands must not persist:\n%s", html)
	}
}

// TestVendorBundleResolvesReact: a plain function of a React package called
// from a module that is not an island bundles for the browser.
func TestVendorBundleResolvesReact(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	writeProjectFile(t, root, "node_modules/react-util/package.json", `{"name":"react-util","version":"1.0.0","main":"index.js","peerDependencies":{"react":"*"}}`)
	writeProjectFile(t, root, "node_modules/react-util/index.js", `
		import React from 'react';
		export function stamp(v) { return React.version ? 'VENDOR_STAMP:' + v : v; }
	`)
	writeProjectFile(t, root, "src/pages/index.tsx", `
		import { useState, useEffect } from 'react';
		import { stamp } from 'react-util';
		export default function Page() {
			const [s, setS] = useState('');
			useEffect(() => { setS(stamp('x')); }, []);
			return <main><p id="s">{s}</p></main>;
		}
	`)
	if out, err := captureOutput(func() error { return New(root, cfg).BuildAll() }); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	js := readBuilt(t, mustGlobOne(t, filepath.Join(cfg.OutDir, "index.*.js")))
	if !strings.Contains(js, "VENDOR_STAMP") {
		t.Fatalf("the vendor bundle is missing from the page script:\n%.400s", js)
	}
}

// TestLayoutProviderWithScriptIsLifted: a provider that adds a script next to
// its children (next-themes) is lifted into the islands instead of KR8016.
func TestLayoutProviderWithScriptIsLifted(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	writeProjectFile(t, root, "node_modules/theme-script/package.json", `{"name":"theme-script","version":"1.0.0","main":"index.js","peerDependencies":{"react":"*"}}`)
	writeProjectFile(t, root, "node_modules/theme-script/index.js", `
		import React from 'react';
		const Ctx = React.createContext('none');
		export function ThemeProvider({ theme, children }) {
			return React.createElement(React.Fragment, null,
				React.createElement('script', { dangerouslySetInnerHTML: { __html: 'window.__themeBoot=1' } }),
				React.createElement(Ctx.Provider, { value: theme }, children));
		}
		export function useTheme() { return React.useContext(Ctx); }
	`)
	writeProjectFile(t, root, "src/components/Toggle.tsx", `
		'use client';
		import { useTheme } from 'theme-script';
		export default function Toggle() { return <i id="t">{useTheme()}</i>; }
	`)
	writeProjectFile(t, root, "src/pages/app/_layout.tsx", `
		import { ThemeProvider } from 'theme-script';
		export default function Layout({ children }) {
			return <ThemeProvider theme="dark">{children}</ThemeProvider>;
		}
	`)
	writeProjectFile(t, root, "src/pages/app/index.tsx", `
		import Toggle from '../../components/Toggle';
		export default function Page() { return <main><Toggle /></main>; }
	`)
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	if strings.Contains(out, "KR8016") {
		t.Fatalf("the provider must be lifted:\n%s", out)
	}
	html := readBuilt(t, filepath.Join(cfg.OutDir, "app", "index.html"))
	if !strings.Contains(html, `<i id="t">dark</i>`) {
		t.Fatalf("the island did not see the layout's provider:\n%s", html)
	}
}

// TestAsyncLayoutRendersLibraryToaster: a package component with hooks that an
// async layout renders is a client boundary, not a server component (KR7110).
func TestAsyncLayoutRendersLibraryToaster(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	toastFixture(t, root)
	writeProjectFile(t, root, "src/pages/app/_layout.tsx", `
		import { Toaster } from 'sonner';
		export default async function Layout({ children }) {
			return <div id="shell">{children}<Toaster /></div>;
		}
	`)
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
}
