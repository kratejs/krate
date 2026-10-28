package build

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func TestSegmentExportsRenderMode(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		mode   RenderMode
		reval  int
		issues int
	}{
		{"revalidate seconds", `export const revalidate = 60; export default function P(){ return <p/> }`, RenderISR, 60, 0},
		{"revalidate folded", `export const revalidate = 60 * 60; export default function P(){ return <p/> }`, RenderISR, 3600, 0},
		{"revalidate zero", `export const revalidate = 0; export default function P(){ return <p/> }`, RenderSSR, 0, 0},
		{"revalidate false", `export const revalidate = false; export default function P(){ return <p/> }`, RenderSSG, 0, 0},
		{"force-dynamic", `export const dynamic = "force-dynamic"; export default function P(){ return <p/> }`, RenderSSR, 0, 0},
		{"force-dynamic beats revalidate", `export const dynamic = "force-dynamic"; export const revalidate = 60; export default function P(){ return <p/> }`, RenderSSR, 0, 0},
		{"force-static", `export const dynamic = "force-static"; export default function P(){ return <p/> }`, RenderSSG, 0, 0},
		{"force-static with revalidate", `export const dynamic = "force-static"; export const revalidate = 30; export default function P(){ return <p/> }`, RenderISR, 30, 0},
		{"auto", `export const dynamic = "auto"; export default function P(){ return <p/> }`, RenderSSG, 0, 0},
		{"config wins", `export const config = { ssr: true }; export const revalidate = 60; export default function P(){ return <p/> }`, RenderSSR, 0, 0},
		{"config isr keeps working", `export const config = { isr: true, revalidate: 15 }; export default function P(){ return <p/> }`, RenderISR, 15, 0},
		{"unknown dynamic", `export const dynamic = "sometimes"; export default function P(){ return <p/> }`, RenderSSG, 0, 1},
		{"non-literal revalidate", `const n = 5; export const revalidate = n; export default function P(){ return <p/> }`, RenderSSG, 0, 1},
		{"revalidate true", `export const revalidate = true; export default function P(){ return <p/> }`, RenderSSG, 0, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prog := parseTest(t, c.src)
			mode, reval := detectRenderMode(prog)
			if mode != c.mode || reval != c.reval {
				t.Fatalf("got (%v, %d), want (%v, %d)", mode, reval, c.mode, c.reval)
			}
			if n := len(parsePageConfig(prog).issues); n != c.issues {
				t.Fatalf("issues = %d, want %d", n, c.issues)
			}
		})
	}
}

func TestDataCacheUsageInference(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		mode  RenderMode
		reval int
		ok    bool
	}{
		{"tagged fetch", `export default async function P(){ const r = await fetch("http://x", { next: { tags: ["posts"] } }); return <p/> }`, RenderISR, taggedOnlyISRRevalidate, true},
		{"timed fetch", `export default async function P(){ await fetch("http://x", { next: { revalidate: 30 } }); await fetch("http://y", { next: { revalidate: 90 } }); return <p/> }`, RenderISR, 30, true},
		{"no-store fetch", `export default async function P(){ await fetch("http://x", { cache: "no-store" }); return <p/> }`, RenderSSR, 0, true},
		{"revalidate zero fetch", `export default async function P(){ await globalThis.fetch("http://x", { next: { revalidate: 0 } }); return <p/> }`, RenderSSR, 0, true},
		{"unstable_cache tags", `import { unstable_cache as cache } from "next/cache";
const getPosts = cache(async () => [], ["posts"], { tags: ["posts"], revalidate: 120 });
export default function P(){ return <p/> }`, RenderISR, 120, true},
		{"unstable_noStore", `import { unstable_noStore } from "@krate/runtime/cache";
export default function P(){ unstable_noStore(); return <p/> }`, RenderSSR, 0, true},
		{"plain fetch", `export default async function P(){ await fetch("http://x"); return <p/> }`, RenderSSG, 0, false},
		{"string mention", `export default function P(){ return <p>{"fetch(url, { next: { tags: ['x'] } })"}</p> }`, RenderSSG, 0, false},
		{"unimported unstable_cache", `const unstable_cache = (f) => f; const g = unstable_cache(() => 1, [], { tags: ["a"] }); export default function P(){ return <p/> }`, RenderSSG, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := detectDataCacheUsage(parseTest(t, c.src))
			mode, reval, _, ok := inferRenderModeFromData(u)
			if ok != c.ok || mode != c.mode || reval != c.reval {
				t.Fatalf("got (%v, %d, %v), want (%v, %d, %v)", mode, reval, ok, c.mode, c.reval, c.ok)
			}
		})
	}
}

func TestResolvePageRenderModeDataInference(t *testing.T) {
	lib := &bundler.Module{Path: "/proj/src/lib/posts.ts", Program: parseTest(t,
		`export async function getPosts(){ return (await fetch("http://api/posts", { next: { tags: ["posts"], revalidate: 45 } })).json() }`)}
	vendor := &bundler.Module{Path: "/proj/node_modules/x/index.js", Program: parseTest(t,
		`export function f(){ return fetch("http://x", { cache: "no-store" }) }`)}

	src := `import { getPosts } from "../lib/posts"; export default async function P(){ await getPosts(); return <p/> }`
	entry := &bundler.Module{Path: "/proj/src/pages/index.tsx", Program: parseTest(t, src), SourceCode: src}
	mode, reval, usage := resolvePageRenderMode("src/pages/index.tsx", entry, []*bundler.Module{entry, lib, vendor})
	if mode != RenderISR || reval != 45 {
		t.Fatalf("got (%v, %d), want ISR 45 inferred from the imported module", mode, reval)
	}
	if usage.noStore {
		t.Fatal("node_modules reads must not drive the page mode")
	}

	// A page pinned static keeps SSG and gets the KR8602 warning.
	pinned := `export const dynamic = "force-static"; import { getPosts } from "../lib/posts"; export default async function P(){ await getPosts(); return <p/> }`
	entry2 := &bundler.Module{Path: "/proj/src/pages/pinned.tsx", Program: parseTest(t, pinned), SourceCode: pinned}
	mode, _, usage = resolvePageRenderMode("src/pages/pinned.tsx", entry2, []*bundler.Module{entry2, lib})
	if mode != RenderSSG {
		t.Fatalf("pinned page mode = %v, want SSG", mode)
	}
	d, ok := staticDataCacheWarning("src/pages/pinned.tsx", pinned, usage, "the page pins itself static")
	if !ok || d.Code != diag.CodeStaticDataCache || !d.IsWarning() || d.Hint == "" {
		t.Fatalf("want a KR8602 warning with a hint, got %+v ok=%v", d, ok)
	}
	if !strings.Contains(d.Message, "tagged") {
		t.Fatalf("warning should name the tagged read: %s", d.Message)
	}
}
