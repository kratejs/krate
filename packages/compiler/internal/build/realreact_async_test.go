package build

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// realReactAsyncFiles adds pages that render real-React components inside the
// server tree of request-time pages: a reconciler library used directly in an
// async page, through a user wrapper module, and in an async layout.
func realReactAsyncFiles(t *testing.T, root string) {
	t.Helper()
	writeProjectFile(t, root, "src/pages/adirect.tsx", `
		import { Canvas } from 'fake-canvas';
		export default async function Page() {
			const fill = await Promise.resolve('rgb(255, 0, 0)');
			return (
				<main>
					<Canvas id="cv" width={60} height={20} client:fallback="Loading direct"><rect x={5} y={0} w={5} h={5} fill={fill} /></Canvas>
				</main>
			);
		}
	`)
	writeProjectFile(t, root, "src/pages/awrapped.tsx", `
		import Scene from '../components/Scene';
		export default async function Page() {
			const color = await Promise.resolve('rgb(255, 0, 0)');
			return <main><Scene client:fallback="Loading wrapped" color={color} /></main>;
		}
	`)
	writeProjectFile(t, root, "src/pages/alay/_layout.tsx", `
		import Scene from '../../components/Scene';
		export default async function Layout({ children }: { children: any }) {
			const color = await Promise.resolve('rgb(255, 0, 0)');
			return <div><Scene client:fallback="Loading layout" color={color} />{children}</div>;
		}
	`)
	writeProjectFile(t, root, "src/pages/alay/index.tsx", `
		export default function Page() { return <p id="in-layout">inside</p>; }
	`)
}

// TestRealReactInAsyncPages: a real-React component in an async page (used
// directly, through a wrapper module, and in an async layout) becomes a
// client-rendered real-React island. The server answers with the fallback
// placeholder, never renders it with Preact, and the chunk it points at holds
// real React.
func TestRealReactInAsyncPages(t *testing.T) {
	b, root := scProject(t, nil)
	withPreact(t, root)
	realReactFixture(t, root)
	realReactSceneModule(t, root)
	realReactAsyncFiles(t, root)
	e2eEnv(t)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	chunkRe := regexp.MustCompile(`data-src="([^"]+)"`)
	cases := []struct{ path, export, fallback string }{
		{"/adirect/", "Canvas", "Loading direct"},
		{"/awrapped/", "default", "Loading wrapped"},
		{"/alay/", "default", "Loading layout"},
	}
	for _, c := range cases {
		status, _, html := getBody(t, base+c.path)
		if status != 200 {
			t.Fatalf("%s: status %d:\n%s", c.path, status, html)
		}
		if strings.Contains(html, "[object Object]") || strings.Contains(html, "<canvas") {
			t.Fatalf("%s: the island must not be rendered on the server:\n%s", c.path, html)
		}
		for _, want := range []string{"<krate-island", `data-export="` + c.export + `"`, c.fallback} {
			if !strings.Contains(html, want) {
				t.Fatalf("%s: response is missing %q:\n%s", c.path, want, html)
			}
		}
		m := chunkRe.FindStringSubmatch(html)
		if m == nil {
			t.Fatalf("%s: no island chunk:\n%s", c.path, html)
		}
		entry := readBuilt(t, filepath.Join(b.Cfg.OutDir, filepath.FromSlash(strings.TrimPrefix(m[1], "/"))))
		if strings.Contains(entry, "preact") {
			t.Fatalf("%s: the chunk must not use Preact:\n%.400s", c.path, entry)
		}
		all := entry
		shared, _ := filepath.Glob(filepath.Join(b.Cfg.OutDir, "chunks", "isr-*.js"))
		for _, f := range shared {
			all += readBuilt(t, f)
		}
		if !strings.Contains(all, "__CLIENT_INTERNALS_DO_NOT_USE") {
			t.Fatalf("%s: the chunk must bundle real React", c.path)
		}
	}
	// No Preact client boundary chunk holds the reconciler library.
	files, _ := filepath.Glob(filepath.Join(b.Cfg.OutDir, "chunks", "client.*.js"))
	for _, f := range files {
		if strings.Contains(readBuilt(t, f), "createContainer") {
			t.Fatalf("%s: a reconciler library was bundled as a Preact boundary", f)
		}
	}
}
