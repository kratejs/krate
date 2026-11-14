package build

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// realReactClientFiles adds the Next-style way of using a reconciler library:
// a "use client" Viewer that imports it next to ordinary hooks, imported from
// sync and async pages, an async layout and another "use client" component.
func realReactClientFiles(t *testing.T, root string) {
	t.Helper()
	writeProjectFile(t, root, "src/components/UViewer.tsx", `"use client";
		import { useState, useEffect } from 'react';
		import { Canvas, Rect } from 'fake-canvas';
		export default function UViewer({ color }: { color: string }) {
			const [n, setN] = useState(1);
			const [ready, setReady] = useState(false);
			useEffect(() => setReady(true), []);
			return (
				<div data-ready={ready ? 'yes' : 'no'}>
					<button id="grow" onClick={() => setN(n + 1)}>grow {n}</button>
					<Canvas id="cv" width={60} height={20}><Rect x={n * 5} y={0} w={5} h={5} fill={color} /></Canvas>
				</div>
			);
		}
	`)
	writeProjectFile(t, root, "src/components/UPanel.tsx", `"use client";
		import { useState } from 'react';
		import UViewer from './UViewer';
		export default function UPanel() {
			const [open, setOpen] = useState(true);
			return <section><button id="toggle" onClick={() => setOpen(!open)}>panel {open ? 'open' : 'closed'}</button>{open ? <UViewer color="rgb(255, 0, 0)" /> : null}</section>;
		}
	`)
	writeProjectFile(t, root, "src/pages/usync.tsx", `
		import UViewer from '../components/UViewer';
		export default function Page() { return <main><UViewer client:fallback="Loading usync" color="rgb(255, 0, 0)" /></main>; }
	`)
	writeProjectFile(t, root, "src/pages/upanelsync.tsx", `
		import UPanel from '../components/UPanel';
		export default function Page() { return <main><UPanel /></main>; }
	`)
	writeProjectFile(t, root, "src/pages/uasync.tsx", `
		import UViewer from '../components/UViewer';
		export default async function Page() {
			const color = await Promise.resolve('rgb(255, 0, 0)');
			return <main><UViewer client:fallback="Loading uasync" color={color} /></main>;
		}
	`)
	writeProjectFile(t, root, "src/pages/upanelasync.tsx", `
		import UPanel from '../components/UPanel';
		export default async function Page() {
			await Promise.resolve();
			return <main><UPanel /></main>;
		}
	`)
	writeProjectFile(t, root, "src/pages/ulay/_layout.tsx", `
		import UViewer from '../../components/UViewer';
		export default async function Layout({ children }: { children: any }) {
			const color = await Promise.resolve('rgb(255, 0, 0)');
			return <div><UViewer client:fallback="Loading ulay" color={color} />{children}</div>;
		}
	`)
	writeProjectFile(t, root, "src/pages/ulay/index.tsx", `
		export default function Page() { return <p id="in-layout">inside</p>; }
	`)
}

// realReactClientProject builds a project with the files above.
func realReactClientProject(t *testing.T) (*Builder, string) {
	t.Helper()
	b, root := scProject(t, nil)
	withPreact(t, root)
	realReactFixture(t, root)
	realReactClientFiles(t, root)
	e2eEnv(t)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	return b, root
}

// TestRealReactUseClientModule: a "use client" module that imports a
// reconciler library (and ordinary hooks) runs on real React wherever it is
// imported: from a sync page (static), an async page, an async layout, and a
// "use client" parent. The parent is a real-React island as a whole, so the
// Preact boundary path is never taken.
func TestRealReactUseClientModule(t *testing.T) {
	b, root := realReactClientProject(t)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	reactOf := func(entryURL string) string {
		entry := readBuilt(t, filepath.Join(b.Cfg.OutDir, filepath.FromSlash(strings.TrimPrefix(entryURL, "/"))))
		if strings.Contains(entry, "preact") {
			t.Fatalf("%s must not use Preact:\n%.400s", entryURL, entry)
		}
		all := entry
		shared, _ := filepath.Glob(filepath.Join(b.Cfg.OutDir, "chunks", "isr-*.js"))
		for _, f := range shared {
			all += readBuilt(t, f)
		}
		return all
	}
	// Sync pages are static: the island carries its fallback and a real chunk.
	srcRe := regexp.MustCompile(`chunks/(island_[0-9a-f]+)\.js`)
	for _, c := range []struct{ page, fallback string }{{"usync", "Loading usync"}, {"upanelsync", ""}} {
		html := readBuilt(t, filepath.Join(b.Cfg.OutDir, c.page, "index.html"))
		if strings.Contains(html, "<canvas") {
			t.Fatalf("%s: the island must not be rendered at build time:\n%s", c.page, html)
		}
		if c.fallback != "" && !strings.Contains(html, c.fallback) {
			t.Fatalf("%s: missing fallback %q:\n%s", c.page, c.fallback, html)
		}
		js, _ := filepath.Glob(filepath.Join(b.Cfg.OutDir, c.page, "*.js"))
		var entry string
		for _, f := range js {
			if m := srcRe.FindString(readBuilt(t, f)); m != "" {
				entry = m
			}
		}
		if entry == "" {
			t.Fatalf("%s: the page loads no island entry", c.page)
		}
		if !strings.Contains(reactOf("/"+entry), "__CLIENT_INTERNALS_DO_NOT_USE") {
			t.Fatalf("%s: the island must bundle real React", c.page)
		}
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	srcAttr := regexp.MustCompile(`data-src="([^"]+)"`)
	for _, c := range []struct{ path, fallback string }{
		{"/uasync/", "Loading uasync"}, {"/ulay/", "Loading ulay"}, {"/upanelasync/", ""},
	} {
		status, _, html := getBody(t, base+c.path)
		if status != 200 {
			t.Fatalf("%s: status %d:\n%s", c.path, status, html)
		}
		if strings.Contains(html, "<canvas") || strings.Contains(html, "[object Object]") {
			t.Fatalf("%s: the island must not be rendered on the server:\n%s", c.path, html)
		}
		if !strings.Contains(html, "<krate-island") || !strings.Contains(html, c.fallback) {
			t.Fatalf("%s: missing island or fallback %q:\n%s", c.path, c.fallback, html)
		}
		m := srcAttr.FindStringSubmatch(html)
		if m == nil || !strings.Contains(reactOf(m[1]), "__CLIENT_INTERNALS_DO_NOT_USE") {
			t.Fatalf("%s: the island chunk must bundle real React (%v)", c.path, m)
		}
	}
	// No Preact client chunk was made for a module that imports the library.
	files, _ := filepath.Glob(filepath.Join(b.Cfg.OutDir, "chunks", "client.*.js"))
	for _, f := range files {
		if strings.Contains(readBuilt(t, f), "createContainer") {
			t.Fatalf("%s: a reconciler library was bundled as a Preact boundary", f)
		}
	}
}
