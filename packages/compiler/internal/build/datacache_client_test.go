package build

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/bundler"
)

func pageModeFor(t *testing.T, files map[string]string) RenderMode {
	t.Helper()
	b, root := scProject(t, files)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	p, ok := manifestPage(readManifest(t, root), "/")
	if !ok {
		t.Fatal("no / page in manifest")
	}
	return p.Mode
}

func TestClientNoStoreFetchKeepsPageStatic(t *testing.T) {
	mode := pageModeFor(t, map[string]string{
		"src/pages/index.tsx": `import { Live } from "../components/live";
export default function Home() {
  return <main><button onClick={() => fetch("/api/x", { cache: "no-store" })}>x</button><Live /></main>;
}`,
		"src/components/live.tsx": `"use client";
import { loadMore } from "../lib/client-api";
import { useEffect } from "react";
export function Live() {
  useEffect(() => { fetch("/api/live", { cache: "no-store" }); }, []);
  return <button onClick={() => loadMore()}>more</button>;
}`,
		"src/lib/client-api.ts": `export function loadMore() { return fetch("/api/more", { cache: "no-store" }); }`,
	})
	if mode != RenderSSG {
		t.Fatalf("client-side no-store fetch changed the page mode to %v, want ssg", mode)
	}
}

func TestServerNoStoreFetchStillMakesPageSSR(t *testing.T) {
	mode := pageModeFor(t, map[string]string{
		"src/pages/index.tsx": `import { load } from "../lib/data";
export default function Home() {
  const d = load();
  return <main>x</main>;
}`,
		"src/lib/data.ts": `export function load() { return fetch("http://example.invalid/x", { cache: "no-store" }); }`,
	})
	if mode != RenderSSR {
		t.Fatalf("server no-store fetch: mode %v, want ssr", mode)
	}
}

func TestServerReachableModulesStopsAtUseClient(t *testing.T) {
	entry := &bundler.Module{Path: "/p/index.tsx", Imports: []string{"./c"}}
	client := &bundler.Module{Path: "/p/c.tsx", SourceCode: "\"use client\";\n", Imports: []string{"./lib"}}
	lib := &bundler.Module{Path: "/p/lib.ts"}
	resolve := func(_, spec string) string {
		return map[string]string{"./c": "/p/c.tsx", "./lib": "/p/lib.ts"}[spec]
	}
	got := serverReachableModules([]*bundler.Module{entry, client, lib}, entry, resolve)
	if len(got) != 1 || got[0] != entry {
		t.Fatalf("got %d modules, want only the entry", len(got))
	}
}
