package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestServerOnlyLazyImportIsNotABrowserChunk builds a server page whose
// server-only helper lazily imports a Node module. No client component reaches
// the helper, so the lazy import must not become a browser chunk.
func TestServerOnlyLazyImportIsNotABrowserChunk(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/server/queues.ts": `import { createHash } from "node:crypto";
import { EventEmitter } from "events";
export async function enqueue(s: string) { return createHash("sha1").update(s).digest("hex") + typeof EventEmitter; }`,
		"src/server/log.ts": `export async function send(s: string) {
  const { enqueue } = await import("./queues");
  return enqueue(s);
}`,
		"src/components/Counter.tsx": `"use client";
import { useState } from "react";
export function Counter() { const [n, setN] = useState(0); return <button onClick={() => setN(n + 1)}>{n}</button>; }`,
		"src/pages/index.tsx": `import { send } from "../server/log";
import { Counter } from "../components/Counter";
export default async function Home() {
  const h = await send("x");
  return <main><i>{h}</i><Counter /></main>;
}`,
	})
	withPreact(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	out, err := captureOutput(b.BuildAll)
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	if strings.Contains(out, "queues") && strings.Contains(out, "chunk") {
		t.Errorf("unexpected chunk output:\n%s", out)
	}
}

// TestDirectivelessHookModuleBehindClientBoundaryBuilds: a module with no
// directive that calls a React library hook is client code when every module
// that imports it is "use client" (Next.js semantics), so an async page that
// renders those client components builds instead of reporting KR8013.
func TestDirectivelessHookModuleBehindClientBoundaryBuilds(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"node_modules/fake-hooks/package.json": `{"name":"fake-hooks","version":"1.0.0","main":"index.js","peerDependencies":{"react":"*"}}`,
		"node_modules/fake-hooks/index.js": `import React from 'react';
export function useStore(sel) { return React.useSyncExternalStore(function () { return function () {}; }, function () { return sel(1); }, function () { return sel(1); }); }`,
		"src/lib/path-ui.ts": `import { useStore } from "fake-hooks";
export function usePathValue() { return useStore((n: number) => n + 1); }`,
		"src/components/Tools.tsx": `"use client";
import { usePathValue } from "../lib/path-ui";
export function Tools() { return <b id="tools">{usePathValue()}</b>; }`,
		"src/pages/index.tsx": `import { Tools } from "../components/Tools";
export default async function Home() {
  await Promise.resolve();
  return <main><Tools /></main>;
}`,
	})
	withPreact(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	out, err := captureOutput(b.BuildAll)
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
}

// TestMetadataClosureLoadsNodeEntryOfPackages: the metadata closure runs under
// Node, so a package that ships a `browser` entry (one that throws without a
// window, like node-abort-controller) must load its Node entry.
func TestMetadataClosureLoadsNodeEntryOfPackages(t *testing.T) {
	b, root := hooksProject(t, map[string]string{
		"node_modules/dual-entry/package.json": `{"name":"dual-entry","version":"1.0.0","main":"index.js","browser":"browser.js"}`,
		"node_modules/dual-entry/index.js":     `module.exports = { origin: function () { return "https://node.example"; } };`,
		"node_modules/dual-entry/browser.js":   `throw new Error("Unable to find global scope. Are you sure this is running in the browser?");`,
		"src/lib/site.ts": `import { origin } from "dual-entry";
export function siteOrigin() { return origin(); }`,
		"src/pages/_layout.tsx": `import { siteOrigin } from "../lib/site";
export const metadata = { metadataBase: siteOrigin(), title: "Site", openGraph: { images: ["/og.png"] } };
export default function L({ children }: any) { return <div>{children}</div>; }`,
		"src/pages/index.tsx": `export default function Home() { return <p>home</p>; }`,
	})
	logs := captureStderrFile(t)
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	if out := logs(); strings.Contains(out, "KR7122") {
		t.Errorf("metadata evaluation warned:\n%s", out)
	}
	head := headOf(readMetaDist(t, root, "index.html"))
	if !strings.Contains(head, "https://node.example/og.png") {
		t.Errorf("head lacks the absolute og:image:\n%s", head)
	}
}

// TestExportClashIgnoresOneFileUnderTwoPaths: a workspace package reached both
// through its real path and through a symlink is one module, so its exports do
// not clash with themselves (KR7116).
func TestExportClashIgnoresOneFileUnderTwoPaths(t *testing.T) {
	b, root := hooksProject(t, map[string]string{
		"src/real/lib.ts": `export const X = 1;
export function twice(n: number) { return n * 2; }`,
		"src/barrel.ts": `export * from "./real/lib";
export * from "./link/lib";`,
		"src/pages/index.tsx": `import { X } from "../real/lib";
import { X as Y } from "../link/lib";
import * as all from "../barrel";
export default function Home() { return <p>{X + Y + Object.keys(all).length}</p>; }`,
	})
	if err := os.Symlink(filepath.Join(root, "src", "real"), filepath.Join(root, "src", "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	logs := captureStderrFile(t)
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	if out := logs(); strings.Contains(out, "KR7116") {
		t.Errorf("one file under two paths reported as an export clash:\n%s", out)
	}
}
