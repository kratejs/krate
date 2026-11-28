package build

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// linkedBig is a string only the shared module holds, to find where its code
// ended up.
var linkedBig = strings.Repeat("y", 4096)

func linkedPagesFiles() map[string]string {
	return map[string]string{
		"src/lib/store.ts": "export const hits = { n: 0 };\nexport const big = \"" + linkedBig + "\";\n",
		"src/components/Counter.tsx": `"use client";
import { useState } from "react";
export default function Counter({ start }: { start: number }) {
  const [n] = useState(start);
  return <button class="counter">count {n}</button>;
}`,
		"src/pages/add.tsx": `import { hits, big } from "../lib/store";
import Counter from "../components/Counter";
export const config = { ssr: true };
export default async function Add() {
  hits.n = await Promise.resolve(hits.n + 1);
  return <main><h1>add {hits.n} {big.length}</h1><Counter start={hits.n} /></main>;
}`,
		"src/pages/deep/read.tsx": `import { hits } from "../../lib/store";
import Counter from "../../components/Counter";
export const config = { ssr: true };
export default async function Read() {
  const n = await Promise.resolve(hits.n);
  return <main><h1>read {n}</h1><Counter start={7} /></main>;
}`,
	}
}

func linkedBundle(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "dist", ".krate", "server-bundles", filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func linkedChunks(root string) []string {
	chunks, _ := filepath.Glob(filepath.Join(root, "dist", ".krate", "server-bundles", pageLinkChunks, "*.mjs"))
	return chunks
}

// TestLinkedPageBundlesShareModules: a production build links the server
// bundles of its pages, so the app modules they share are one chunk, each
// page bundle stays at its path and imports it, the client boundary stubs
// still find their render modules from any directory, and the renderer holds
// one instance of the shared module for both pages.
func TestLinkedPageBundlesShareModules(t *testing.T) {
	t.Parallel()
	b, root := scProject(t, linkedPagesFiles())
	e2eEnv(t)
	withPreact(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	sm, err := os.ReadFile(ServerManifestPath(filepath.Join(root, "dist")))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`".krate/server-bundles/add.server.mjs"`, `".krate/server-bundles/deep/read.server.mjs"`} {
		if !strings.Contains(string(sm), want) {
			t.Errorf("server manifest lacks bundle path %s:\n%s", want, sm)
		}
	}
	chunks := linkedChunks(root)
	shared := 0
	for _, c := range chunks {
		data, _ := os.ReadFile(c)
		if strings.Contains(string(data), linkedBig) {
			shared++
		}
	}
	if shared != 1 {
		t.Fatalf("want the shared module in one chunk, found it in %d of %v", shared, chunks)
	}
	for _, name := range []string{"add.server.mjs", "deep/read.server.mjs"} {
		src := linkedBundle(t, root, name)
		if strings.Contains(src, linkedBig) || !strings.Contains(src, pageLinkChunks+"/") {
			t.Errorf("%s does not import the shared module from a chunk:\n%.600s", name, src)
		}
		if strings.Contains(src, ssrBundlesPlaceholder) || strings.Contains(src, ".krate-page-") {
			t.Errorf("%s keeps a placeholder of the linked build:\n%s", name, src)
		}
	}
	for _, c := range chunks {
		data, _ := os.ReadFile(c)
		if strings.Contains(string(data), ssrBundlesPlaceholder) {
			t.Errorf("%s keeps the render module placeholder", c)
		}
	}

	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	status, _, html := getWith(t, base+"/add/", nil)
	if status != 200 || !strings.Contains(html, "add 1 4096") || !strings.Contains(html, "count") {
		t.Fatalf("/add/: status %d:\n%s", status, html)
	}
	status, _, html = getWith(t, base+"/deep/read/", nil)
	if status != 200 || !strings.Contains(html, "count") {
		t.Fatalf("/deep/read/: status %d:\n%s", status, html)
	}
	if !strings.Contains(html, "read 1") {
		t.Errorf("/deep/read/ does not see the instance /add/ changed:\n%s", html)
	}
}

// TestLinkedPageBundlesLeaveUnsafePagesAlone: a page that imports a module an
// async layout's pages load as a layout (with the page globals) would load it
// differently if it were linked, so it keeps its own bundle; the layout's
// pages are still linked, and every page renders.
func TestLinkedPageBundlesLeaveUnsafePagesAlone(t *testing.T) {
	t.Parallel()
	files := linkedPagesFiles()
	files["src/pages/shop/_layout.tsx"] = `import { big } from "../../lib/store";
export const shopName = "Widget shop";
export default async function ShopLayout({ children }: any) {
  const name = await Promise.resolve(shopName);
  return <section><h2>{name} {big.length}</h2>{children}</section>;
}`
	files["src/pages/shop/one.tsx"] = `import { hits } from "../../lib/store";
export const config = { ssr: true };
export default async function One() { return <p>one {await Promise.resolve(hits.n)}</p>; }`
	files["src/pages/shop/two.tsx"] = `import { hits } from "../../lib/store";
export const config = { ssr: true };
export default async function Two() { return <p>two {await Promise.resolve(hits.n)}</p>; }`
	files["src/pages/outside.tsx"] = `import { shopName } from "./shop/_layout";
import { big } from "../lib/store";
export const config = { ssr: true };
export default async function Outside() { return <p>outside {await Promise.resolve(shopName)} {big.length}</p>; }`
	b, root := scProject(t, files)
	e2eEnv(t)
	withPreact(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	for _, name := range []string{"add.server.mjs", "shop/one.server.mjs", "shop/two.server.mjs"} {
		if src := linkedBundle(t, root, name); strings.Contains(src, linkedBig) || !strings.Contains(src, pageLinkChunks+"/") {
			t.Errorf("%s is not linked:\n%.600s", name, src)
		}
	}
	if src := linkedBundle(t, root, "outside.server.mjs"); !strings.Contains(src, linkedBig) || strings.Contains(src, pageLinkChunks+"/") {
		t.Errorf("outside.server.mjs is linked, but it loads the layout as a plain module:\n%.600s", src)
	}

	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	for path, want := range map[string]string{
		"/outside/":  "outside Widget shop 4096",
		"/shop/one/": "one 0",
		"/shop/two/": "two 0",
	} {
		status, _, html := getWith(t, base+path, nil)
		if status != 200 || !strings.Contains(html, want) {
			t.Errorf("%s: status %d, want %q:\n%s", path, status, want, html)
		}
		if strings.HasPrefix(path, "/shop/") && !strings.Contains(html, "Widget shop 4096") {
			t.Errorf("%s does not render inside its layout:\n%s", path, html)
		}
	}
}

// TestDevPageBundlesStayUnlinked: dev rebuilds page bundles one at a time, so
// they stay self-contained.
func TestDevPageBundlesStayUnlinked(t *testing.T) {
	t.Parallel()
	b, root := scProject(t, linkedPagesFiles())
	withPreact(t, root)
	b.DevMode = true
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if chunks := linkedChunks(root); len(chunks) != 0 {
		t.Errorf("dev build linked the page bundles: %v", chunks)
	}
	if src := linkedBundle(t, root, "add.server.mjs"); !strings.Contains(src, linkedBig) {
		t.Errorf("dev add.server.mjs does not bundle the shared module:\n%.600s", src)
	}
}

// linkedPackageFiles is a project whose pages render components of a local
// React-bound package (a package boundary for each page).
func linkedPackageFiles() map[string]string {
	files := linkedPagesFiles()
	files["node_modules/@acme/widgets/package.json"] = `{"name":"@acme/widgets","version":"1.0.0","type":"module","main":"index.js","peerDependencies":{"react":"*"}}`
	files["node_modules/@acme/widgets/index.js"] = `"use client";
import { h } from "preact";
import { useState } from "preact/hooks";
export const version = "w1";
export function Alpha() { const [n] = useState(1); return h("button", { class: "alpha" }, "alpha " + n); }
export function Beta() { const [n] = useState(2); return h("button", { class: "beta" }, "beta " + n); }
`
	files["src/components/Panel.tsx"] = `import { Alpha } from "@acme/widgets";
export function Panel() { return <aside><Alpha /></aside>; }`
	// Direct imports from the page: each page's own stub.
	files["src/pages/pkg/one.tsx"] = `import { Alpha } from "@acme/widgets";
import { big } from "../../lib/store";
export const config = { ssr: true };
export default async function One() { return <main><h1>one {await Promise.resolve(big.length)}</h1><Alpha /></main>; }`
	files["src/pages/pkg/two.tsx"] = `import { Alpha, Beta } from "@acme/widgets";
import { big } from "../../lib/store";
export const config = { ssr: true };
export default async function Two() { return <main><h1>two {await Promise.resolve(big.length)}</h1><Alpha /><Beta /></main>; }`
	// Through a shared module too: the shared stub, which the pages agree on.
	// (Each page also imports the package itself: a page that reaches it
	// only through a shared module may miss its boundary in its own build.)
	files["src/pages/panel/a.tsx"] = `import { Panel } from "../../components/Panel";
import { Alpha } from "@acme/widgets";
import { big } from "../../lib/store";
export const config = { ssr: true };
export default async function PanelA() { return <main><h1>panel a {await Promise.resolve(big.length)}</h1><Panel /><Alpha /></main>; }`
	files["src/pages/panel/b.tsx"] = `import { Panel } from "../../components/Panel";
import { Alpha } from "@acme/widgets";
import { big } from "../../lib/store";
export const config = { ssr: true };
export default async function PanelB() { return <main><h1>panel b {await Promise.resolve(big.length)}</h1><Panel /><Alpha /></main>; }`
	// Through the shared module as well, but its boundary for the package
	// also has Beta, so its Alpha is another boundary export than the shared
	// stub's.
	files["src/pages/panel/c.tsx"] = `import { Panel } from "../../components/Panel";
import { Alpha, Beta } from "@acme/widgets";
import { big } from "../../lib/store";
export const config = { ssr: true };
export default async function PanelC() { return <main><h1>panel c {await Promise.resolve(big.length)}</h1><Panel /><Alpha /><Beta /></main>; }`
	// Reaches the package without a boundary for it: a lowercase re-export
	// is not a component, so its own build imports the package.
	files["src/lib/widgetinfo.ts"] = `export { version } from "@acme/widgets";`
	files["src/pages/info.tsx"] = `import { version } from "../lib/widgetinfo";
import { big } from "../lib/store";
export const config = { ssr: true };
export default async function Info() { return <main><h1>info {version} {await Promise.resolve(big.length)}</h1></main>; }`
	// Reaches it through a side-effect import, which esbuild accepts from the
	// shared stub: only the import graph shows it.
	files["src/lib/widgetload.ts"] = `import "@acme/widgets";
export const loaded = "yes";`
	files["src/pages/load.tsx"] = `import { loaded } from "../lib/widgetload";
import { big } from "../lib/store";
export const config = { ssr: true };
export default async function Load() { return <main><h1>load {loaded} {await Promise.resolve(big.length)}</h1></main>; }`
	return files
}

var (
	islandMarkerRe   = regexp.MustCompile(`<!--krate-island:[^>]*-->`)
	linkedChunkURLRe = regexp.MustCompile(`"s":"[^"]*"`)
)

// TestLinkedPageBundlesWithPackageBoundaries: pages that render components of
// a React-bound package link: those importing it themselves keep their own
// stubs, those reaching it through a shared module share a stub they agree
// on. A page whose boundary disagrees with the shared stub, and pages that
// reach the package without a boundary for it, keep their own bundles. Every
// page renders the same island markers as an unlinked build.
func TestLinkedPageBundlesWithPackageBoundaries(t *testing.T) {
	t.Parallel()
	b, root := scProject(t, linkedPackageFiles())
	e2eEnv(t)
	withPreact(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	render := func(linked bool) map[string]string {
		t.Helper()
		nb := New(root, b.Cfg)
		nb.noLinkedServerBundles = !linked
		if err := nb.BuildAll(); err != nil {
			t.Fatalf("BuildAll (linked %v): %v", linked, err)
		}
		if linked {
			for name, want := range map[string]bool{
				"add.server.mjs": true, "pkg/one.server.mjs": true, "pkg/two.server.mjs": true,
				"panel/a.server.mjs": true, "panel/b.server.mjs": true,
				"panel/c.server.mjs": false, "info.server.mjs": false, "load.server.mjs": false,
			} {
				src := linkedBundle(t, root, name)
				if got := !strings.Contains(src, linkedBig) && strings.Contains(src, pageLinkChunks+"/"); got != want {
					t.Errorf("%s linked = %v, want %v:\n%.600s", name, got, want, src)
				}
			}
		} else if chunks := linkedChunks(root); len(chunks) != 0 {
			t.Errorf("unlinked build left chunks: %v", chunks)
		}
		cfg := *b.Cfg
		base, err := startKrateServer(t, root, &cfg)
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
		out := map[string]string{}
		for _, path := range []string{"/pkg/one/", "/pkg/two/", "/panel/a/", "/panel/b/", "/panel/c/", "/info/", "/load/"} {
			status, _, html := getWith(t, base+path, nil)
			if status != 200 {
				t.Fatalf("%s (linked %v): status %d:\n%s", path, linked, status, html)
			}
			out[path] = html
		}
		return out
	}
	linked := render(true)
	for path, want := range map[string][]string{
		"/pkg/one/": {"one 4096", "alpha 1"},
		"/pkg/two/": {"two 4096", "alpha 1", "beta 2"},
		"/panel/a/": {"panel a 4096", "alpha 1"},
		"/panel/b/": {"panel b 4096", "alpha 1"},
		"/panel/c/": {"panel c 4096", "alpha 1", "beta 2"},
		"/info/":    {"info w1 4096"},
		"/load/":    {"load yes 4096"},
	} {
		for _, w := range want {
			if !strings.Contains(linked[path], w) {
				t.Errorf("%s: missing %q:\n%s", path, w, linked[path])
			}
		}
		if path != "/info/" && path != "/load/" && !strings.Contains(linked[path], "<!--krate-island:") {
			t.Errorf("%s: the package components are not islands:\n%s", path, linked[path])
		}
	}
	alone := render(false)
	// The island chunk names are not stable from one build to the next; the
	// boundary module, export and instance of each island are.
	markers := func(html string) []string {
		out := islandMarkerRe.FindAllString(html, -1)
		for i := range out {
			out[i] = linkedChunkURLRe.ReplaceAllString(out[i], `"s":""`)
		}
		return out
	}
	for path, html := range linked {
		got, want := markers(html), markers(alone[path])
		if !slices.Equal(got, want) {
			t.Errorf("%s: island markers differ from the unlinked build:\nlinked %v\nalone  %v", path, got, want)
		}
	}
}
