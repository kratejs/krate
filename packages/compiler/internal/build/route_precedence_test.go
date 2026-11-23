package build

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// precedenceProject mixes static and request-time pages that overlap at the
// same level. Each subtree isolates one rule.
func precedenceProject() map[string]string {
	return map[string]string{
		"src/pages/index.tsx": `export default function Home() { return <main>home-page</main>; }`,

		// Route group: static page against a request-time [slug] beside it.
		"src/pages/(site)/about.tsx":  `export default function About() { return <main>static-about</main>; }`,
		"src/pages/(site)/[slug].tsx": `export default async function Slug({ params }: any) { const p = await params; return <main>catch-all {p.slug}</main>; }`,

		// Nested: a static leaf against a request-time [id].
		"src/pages/blog/new.tsx":  `export default function New() { return <main>blog-new-static</main>; }`,
		"src/pages/blog/[id].tsx": `export default async function Post({ params }: any) { const p = await params; return <main>blog-post {p.id}</main>; }`,

		// A request-time static-path page against a static [slug] template.
		"src/pages/team/staff.tsx":  `export default async function Staff() { await Promise.resolve(); return <main>team-staff-dynamic</main>; }`,
		"src/pages/team/[slug].tsx": `export default function Member({ params }: any) { return <main>team-member {params.slug}</main>; }`,

		// Request-time [id] against a request-time catch-all.
		"src/pages/docs/[id].tsx":      `export default async function D({ params }: any) { const p = await params; return <main>docs-id {p.id}</main>; }`,
		"src/pages/docs/[...rest].tsx": `export default async function R({ params }: any) { const p = await params; return <main>docs-rest {p.rest}</main>; }`,

		// A static index against a request-time optional catch-all.
		"src/pages/shop/index.tsx":         `export default function S() { return <main>shop-index-static</main>; }`,
		"src/pages/shop/[[...filter]].tsx": `export default async function F({ params }: any) { const p = await params; return <main>shop-filter {p.filter}</main>; }`,

		// A prerendered dynamic page against a request-time catch-all.
		"src/pages/g/[id].tsx": `export function generateStaticParams() { return [{ id: "a" }]; }
export default function G({ params }: any) { return <main>gsp-page {params.id}</main>; }`,
		"src/pages/g/[...rest].tsx": `export default async function R({ params }: any) { const p = await params; return <main>g-rest {p.rest}</main>; }`,

		// A request-time (ISR) dynamic page with prerendered params must stay request-time.
		"src/pages/isr/[id].tsx": `export const config = { isr: true, revalidate: 60 };
export function generateStaticParams() { return [{ id: "p" }]; }
export default function P({ params }: any) { return <main>isr-page {params.id}</main>; }`,
		"src/pages/isr/[...rest].tsx": `export default async function R({ params }: any) { const p = await params; return <main>isr-rest {p.rest}</main>; }`,
	}
}

var precedenceCases = []struct{ path, want string }{
	{"/about/", "static-about"},
	{"/other/", "catch-all other"},
	{"/blog/new/", "blog-new-static"},
	{"/blog/42/", "blog-post 42"},
	{"/team/staff/", "team-staff-dynamic"},
	{"/team/ann/", "team-member ann"},
	{"/docs/1/", "docs-id 1"},
	{"/docs/1/2/", "docs-rest 1/2"},
	{"/shop/", "shop-index-static"},
	{"/shop/a/b/", "shop-filter a/b"},
	{"/g/a/", "gsp-page a"},
	{"/g/x/y/", "g-rest x/y"},
	{"/isr/p/", "isr-page p"},
	{"/isr/q/r/", "isr-rest q/r"},
}

func checkPrecedence(t *testing.T, base string) {
	t.Helper()
	client := &http.Client{Timeout: 20 * time.Second}
	for _, tc := range precedenceCases {
		resp, err := client.Get(base + tc.path)
		if err != nil {
			t.Errorf("GET %s: %v", tc.path, err)
			continue
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(string(data), tc.want) {
			t.Errorf("GET %s: status %d, want body containing %q\n%.300s", tc.path, resp.StatusCode, tc.want, data)
		}
	}
}

// TestServeRoutePrecedence: krate start/serve resolves overlapping routes the
// way Next.js does: a static page, then [param], then [...catchAll], then
// [[...optional]], whichever render mode each page has.
func TestServeRoutePrecedence(t *testing.T) {
	e2eEnv(t)
	b, root := scProject(t, precedenceProject())
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
	checkPrecedence(t, base)
}

// TestDevRoutePrecedence is the same table under krate dev.
func TestDevRoutePrecedence(t *testing.T) {
	d := startDevRun(t, precedenceProject())
	checkPrecedence(t, d.base)
}

func TestShadowedStaticPages(t *testing.T) {
	all := []serverAllPage{
		{Route: "/[slug]", Mode: "ssr"},
		{Route: "/about", Mode: "ssg"},
		{Route: "/", Mode: "ssg"},
		{Route: "/team/staff", Mode: "ssg"},
		{Route: "/team/[...rest]", Mode: "ssr"},
		{Route: "/g/[id]", Mode: "ssg"},
		{Route: "/g/[...rest]", Mode: "ssr"},
		{Route: "/shop", Mode: "ssg"},
		{Route: "/shop/[[...f]]", Mode: "isr"},
		{Route: "/lonely", Mode: "ssg"},
	}
	pages := map[string]string{
		"index.html":            "root",
		"about/index.html":      "about",
		"team/staff/index.html": "staff",
		"g/a/index.html":        "g-a",
		"shop/index.html":       "shop",
		"lonely/index.html":     "lonely",
	}
	got := shadowedStaticPages(all, pages)
	for _, want := range []string{"about/index.html", "team/staff/index.html", "g/a/index.html", "shop/index.html"} {
		if _, ok := got[want]; !ok {
			t.Errorf("%s must be answered by the function: %v", want, got)
		}
	}
	// "/" ranks below nothing that matches it here, and /lonely overlaps no
	// request-time route except /[slug], which it outranks.
	if _, ok := got["index.html"]; ok {
		t.Errorf("the root page overlaps no request-time route: %v", got)
	}
}

// A Vercel deploy without middleware sends a request-time page's glob to the
// function before the filesystem, so the function has to carry the prerendered
// pages that outrank it.
func TestVercelBundleCarriesShadowingStaticPages(t *testing.T) {
	t.Parallel()
	root, cfg := serverlessProject(t, "serverless")
	if err := os.Remove(filepath.Join(root, "middleware.ts")); err != nil {
		t.Fatal(err)
	}
	for rel, src := range map[string]string{
		"src/pages/zz/about.tsx":  `export default function A() { return <main>zz-static-about</main>; }`,
		"src/pages/zz/[slug].tsx": `export default async function S({ params }: any) { const p = await params; return <main>zz-dynamic {p.slug}</main>; }`,
	} {
		writeProjectFile(t, root, rel, src)
	}
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatal(err)
	}
	emitServerTarget(t, root, cfg, "vercel")
	bundle, err := os.ReadFile(filepath.Join(root, ".vercel", "output", "functions", "__krate.func", "index.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bundle), "zz-static-about") {
		t.Error("the function bundle must carry /zz/about, which outranks the request-time /zz/[slug]")
	}
	if strings.Contains(string(bundle), "static about page") {
		t.Error("only the pages a request-time route shadows belong in the bundle")
	}
}
