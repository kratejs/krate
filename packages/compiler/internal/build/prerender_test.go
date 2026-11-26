package build

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func setupPrerenderProject(t *testing.T, files map[string]string, staticOut bool) (*Builder, string) {
	t.Helper()
	e2eEnv(t)
	b, root := scProject(t, files)
	if staticOut {
		b.Cfg.Output = "static"
	}
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	return b, root
}

func TestAsyncStaticParamsPrerender(t *testing.T) {
	t.Parallel()
	b, root := setupPrerenderProject(t, map[string]string{
		"src/pages/_layout.tsx": `export default function RootLayout({ children }: any) {
  return <div class="site-shell">{children}</div>;
}`,
		"src/pages/articles/[slug].tsx": `const CATALOG: Record<string, { title: string; summary: string }> = {
  alpha: { title: "Alpha Guide", summary: "First summary" },
  beta: { title: "Beta Guide", summary: "Second summary" },
};

export function generateStaticParams() {
  return [{ slug: "alpha" }, { slug: "beta" }];
}

export async function generateMetadata(props: { params: Promise<{ slug: string }> }) {
  const { slug } = await props.params;
  const item = CATALOG[slug];
  return { title: item.title, description: item.summary };
}

async function DetailNote({ text }: { text: string }) {
  await Promise.resolve();
  return <p class="note">{text}</p>;
}

export default async function ArticlePage(props: { params: Promise<{ slug: string }> }) {
  const { slug } = await props.params;
  const syncSlug = (props.params as any).slug;
  const item = CATALOG[slug];
  return (
    <article>
      <h1>async {item.title}</h1>
      <span class="sync">{syncSlug}</span>
      <Suspense fallback={<p>loading-note</p>}>
        <DetailNote text={item.summary} />
      </Suspense>
    </article>
  );
}`,
	}, false)

	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	m := readManifest(t, root)
	for _, tc := range []struct {
		slug, title, summary string
	}{
		{"alpha", "Alpha Guide", "First summary"},
		{"beta", "Beta Guide", "Second summary"},
	} {
		p, ok := manifestPage(m, "/articles/"+tc.slug)
		if !ok {
			t.Fatalf("manifest missing /articles/%s: %+v", tc.slug, m.Pages)
		}
		if p.Mode != RenderSSG || p.ServerComponents {
			t.Errorf("/articles/%s manifest: mode=%v serverComponents=%v, want ssg + false", tc.slug, p.Mode, p.ServerComponents)
		}
		raw, err := os.ReadFile(filepath.Join(root, "dist", "articles", tc.slug, "index.html"))
		if err != nil {
			t.Fatalf("read dist/articles/%s/index.html: %v", tc.slug, err)
		}
		html := string(raw)
		for _, want := range []string{
			`<title>` + tc.title + `</title>`,
			tc.summary,
			`<h1>async ` + tc.title + `</h1>`,
			`<span class=sync>` + tc.slug + `</span>`,
			`<p class=note>` + tc.summary + `</p>`,
			`site-shell`,
		} {
			if !strings.Contains(html, want) {
				t.Errorf("/articles/%s HTML missing %q:\n%s", tc.slug, want, html)
			}
		}
		for _, forbid := range []string{"<!--suspense:page-->", "loading-note", "<title>Krate App</title>"} {
			if strings.Contains(html, forbid) {
				t.Errorf("/articles/%s HTML should not contain %q:\n%s", tc.slug, forbid, html)
			}
		}
	}

	// Dynamic fallback template stays SSR for unknown slugs.
	tpl, ok := manifestPage(m, "/articles/[slug]")
	if !ok || tpl.Mode != RenderSSR {
		t.Errorf("/articles/[slug] template: ok=%v mode=%v, want ssr", ok, tpl.Mode)
	}
	smData, err := os.ReadFile(ServerManifestPath(filepath.Join(root, "dist")))
	if err != nil {
		t.Fatalf("read server-manifest.json: %v", err)
	}
	sm := string(smData)
	if !strings.Contains(sm, `"/articles/[slug]"`) {
		t.Errorf("server-manifest should keep /articles/[slug]:\n%s", sm)
	}
	if strings.Contains(sm, `"/articles/alpha"`) || strings.Contains(sm, `"/articles/beta"`) {
		t.Errorf("server-manifest must not list prerendered SSG pages:\n%s", sm)
	}
}

func TestAsyncPrerenderStaticOutput(t *testing.T) {
	t.Parallel()
	b, root := setupPrerenderProject(t, map[string]string{
		"src/pages/about.tsx": `export async function generateMetadata() {
  await Promise.resolve();
  return { title: "About Static Async" };
}
export default async function About() {
  const label = await Promise.resolve("Static About Body");
  return <main><h1>{label}</h1></main>;
}`,
		"src/pages/items/[slug].tsx": `const ITEMS: Record<string, { title: string }> = {
  one: { title: "Item One" },
  two: { title: "Item Two" },
};
export function generateStaticParams() {
  return [{ slug: "one" }, { slug: "two" }];
}
export async function generateMetadata({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  return { title: ITEMS[slug].title };
}
export default async function ItemPage({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  return <h1>static {ITEMS[slug].title}</h1>;
}`,
	}, true)

	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll with output: static failed: %v", err)
	}

	outDir := filepath.Join(root, "dist")
	for _, tc := range []struct {
		rel, title, body string
	}{
		{"about/index.html", "<title>About Static Async</title>", "<h1>Static About Body</h1>"},
		{"items/one/index.html", "<title>Item One</title>", "<h1>static Item One</h1>"},
		{"items/two/index.html", "<title>Item Two</title>", "<h1>static Item Two</h1>"},
	} {
		data, err := os.ReadFile(filepath.Join(outDir, filepath.FromSlash(tc.rel)))
		if err != nil {
			t.Fatalf("read %s: %v", tc.rel, err)
		}
		html := string(data)
		if !strings.Contains(html, tc.title) || !strings.Contains(html, tc.body) {
			t.Errorf("%s missing %q or %q:\n%s", tc.rel, tc.title, tc.body, html)
		}
	}

	// Static output must ship no SSR server or fallback [slug] template.
	for _, absent := range []string{
		filepath.Join(outDir, ".krate", "server-manifest.json"),
		filepath.Join(outDir, ".krate", "server-renderer.mjs"),
		filepath.Join(outDir, ".krate", "server-bundles"),
		filepath.Join(outDir, "items", "[slug]", "index.html"),
	} {
		if _, err := os.Stat(absent); !os.IsNotExist(err) {
			t.Errorf("static output must not emit %s", absent)
		}
	}

	m := readManifest(t, root)
	for _, p := range m.Pages {
		if p.Mode != RenderSSG {
			t.Errorf("manifest page %s has mode %v, want ssg", p.Route, p.Mode)
		}
	}
}

func TestAsyncPrerenderStaticOutputRejectsDynamic(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		files map[string]string
	}{
		{
			name: "dynamic async route without generateStaticParams",
			files: map[string]string{
				"src/pages/posts/[slug].tsx": `export default async function Post({ params }: any) {
  const { slug } = await params;
  return <h1>{slug}</h1>;
}`,
			},
		},
		{
			name: "force-dynamic opt-out",
			files: map[string]string{
				"src/pages/live.tsx": `export const dynamic = "force-dynamic";
export default async function Live() {
  await Promise.resolve();
  return <h1>live</h1>;
}`,
			},
		},
		{
			name: "headers() request API",
			files: map[string]string{
				"src/pages/hdr.tsx": `import { headers } from "@krate/runtime/server";
export default async function Hdr() {
  const h = await headers();
  return <p>{h.get("host")}</p>;
}`,
			},
		},
		{
			name: "cookies() request API",
			files: map[string]string{
				"src/pages/ck.tsx": `import { cookies } from "@krate/runtime/server";
export default async function Ck() {
  const c = await cookies();
  return <p>{c.get("sid")?.value}</p>;
}`,
			},
		},
		{
			name: "searchParams request API",
			files: map[string]string{
				"src/pages/sp.tsx": `export default async function Sp({ searchParams }: any) {
  const sp = await searchParams;
  return <p>{sp.q || "none"}</p>;
}`,
			},
		},
		{
			name: "async child in sync page",
			files: map[string]string{
				"src/pages/child.tsx": `async function Widget() {
  await Promise.resolve();
  return <span>w</span>;
}
export default function Page() {
  return <main><Widget /></main>;
}`,
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b, _ := setupPrerenderProject(t, tc.files, true)
			err := b.BuildAll()
			if err == nil {
				t.Fatalf("expected KR7103 build error for %s, got nil", tc.name)
			}
			var d diag.Diagnostic
			if !strings.Contains(err.Error(), string(diag.CodeServerComponentStaticOutput)) && !errors.As(err, &d) {
				t.Fatalf("expected KR7103 error for %s, got: %v", tc.name, err)
			}
			if !strings.Contains(err.Error(), string(diag.CodeServerComponentStaticOutput)) {
				t.Fatalf("expected %s in error, got: %v", diag.CodeServerComponentStaticOutput, err)
			}
		})
	}
}

func TestAsyncPrerenderOptOutsAndDynamicFallback(t *testing.T) {
	t.Parallel()
	b, root := setupPrerenderProject(t, map[string]string{
		"src/pages/dyn/[slug].tsx": `export const dynamic = "force-dynamic";
export function generateStaticParams() { return [{ slug: "a" }]; }
export default async function Dyn({ params }: any) {
  const { slug } = await params;
  return <h1>dyn {slug}</h1>;
}`,
		"src/pages/isr/[slug].tsx": `export const revalidate = 60;
const TITLES: Record<string, string> = { a: "ISR Alpha" };
export function generateStaticParams() { return [{ slug: "a" }]; }
export async function generateMetadata({ params }: any) {
  const { slug } = await params;
  return { title: TITLES[slug] };
}
export default async function IsrPage({ params }: any) {
  const { slug } = await params;
  return <h1>isr {TITLES[slug]}</h1>;
}`,
		"src/pages/req/[slug].tsx": `import { cookies } from "@krate/runtime/server";
export function generateStaticParams() { return [{ slug: "a" }]; }
export default async function ReqPage({ params }: any) {
  const { slug } = await params;
  const sid = (await cookies()).get("sid")?.value || "anon";
  return <h1>req {slug}:{sid}</h1>;
}`,
		"src/pages/query/[slug].tsx": `export function generateStaticParams() { return [{ slug: "a" }]; }
export default async function QueryPage({ params, searchParams }: any) {
  const { slug } = await params;
  const sp = await searchParams;
  return <h1>q {slug}:{sp.tab || "all"}</h1>;
}`,
	}, false)

	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll failed: %v", err)
	}

	m := readManifest(t, root)

	// force-dynamic stays SSR with empty page region shell.
	if p, ok := manifestPage(m, "/dyn/a"); !ok || p.Mode != RenderSSR {
		t.Errorf("/dyn/a: ok=%v mode=%v, want ssr", ok, p.Mode)
	}
	dynHTML, err := os.ReadFile(filepath.Join(root, "dist", "dyn", "a", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(dynHTML), "<!--suspense:page--><!--/suspense:page-->") {
		t.Errorf("/dyn/a should keep empty page shell:\n%s", dynHTML)
	}

	// ISR stays ISR with revalidate=60, and seeds its initial HTML and title.
	if p, ok := manifestPage(m, "/isr/a"); !ok || p.Mode != RenderISR || p.Revalidate != 60 {
		t.Errorf("/isr/a: ok=%v mode=%v revalidate=%d, want isr + 60", ok, p.Mode, p.Revalidate)
	}
	isrHTML, err := os.ReadFile(filepath.Join(root, "dist", "isr", "a", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<!--suspense:page--><h1>isr ISR Alpha</h1><!--/suspense:page-->", "<title>ISR Alpha</title>"} {
		if !strings.Contains(string(isrHTML), want) {
			t.Errorf("/isr/a missing seeded %q:\n%s", want, isrHTML)
		}
	}

	// Request-only APIs (cookies() and searchParams) fall back to SSR cleanly.
	for _, route := range []string{"/req/a", "/query/a"} {
		if p, ok := manifestPage(m, route); !ok || p.Mode != RenderSSR {
			t.Errorf("%s: ok=%v mode=%v, want ssr fallback", route, ok, p.Mode)
		}
	}
}

func TestAsyncPrerenderIslandHydration(t *testing.T) {
	t.Parallel()
	b, root := setupPrerenderProject(t, map[string]string{
		"src/components/Counter.tsx": `"use client";
import { useState } from "react";
export function Counter({ start }: { start: number }) {
  const [n, setN] = useState(start);
  return <button type="button" onClick={() => setN(n + 1)}>count:{n}</button>;
}`,
		"src/pages/widgets/[id].tsx": `import { Counter } from "../../components/Counter";
const COUNTS: Record<string, number> = { one: 3, two: 7 };
export function generateStaticParams() {
  return [{ id: "one" }, { id: "two" }];
}
export async function generateMetadata({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return { title: "Widget " + id };
}
export default async function WidgetPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <main><h1>Widget {id}</h1><Counter start={COUNTS[id]} /></main>;
}`,
	}, true)
	withPreact(t, root)

	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll failed: %v", err)
	}

	outDir := filepath.Join(root, "dist")
	raw, err := os.ReadFile(filepath.Join(outDir, "widgets", "one", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(raw)
	for _, want := range []string{
		"<title>Widget one</title>",
		"<h1>Widget one</h1>",
		"<!--krate-island:",
		">count:3</button>",
		"island-loader.",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("widgets/one HTML missing %q:\n%s", want, html)
		}
	}

	// Verify the hashed island chunk referenced inside the HTML exists on disk.
	chunkRe := regexp.MustCompile(`/chunks/island_[0-9a-f]+\.[0-9a-z]+\.js`)
	chunkURL := chunkRe.FindString(html)
	if chunkURL == "" {
		t.Fatalf("widgets/one HTML missing hashed island chunk reference:\n%s", html)
	}
	chunkFile := filepath.Join(outDir, filepath.FromSlash(strings.TrimPrefix(chunkURL, "/")))
	if _, err := os.Stat(chunkFile); err != nil {
		t.Errorf("referenced island chunk %s missing on disk: %v", chunkURL, err)
	}

	// Static output has no SSR server artifacts.
	if _, err := os.Stat(filepath.Join(outDir, ".krate", "server-renderer.mjs")); !os.IsNotExist(err) {
		t.Errorf("static output must not ship server-renderer.mjs")
	}
}
