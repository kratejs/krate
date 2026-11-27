package build

import (
	"os"
	"path/filepath"
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
