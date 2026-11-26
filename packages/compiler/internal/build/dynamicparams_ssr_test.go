package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

const closedRouteLayout = `import { cookies } from "@krate/runtime/server";
export default async function AppLayout({ children }: any) {
  const c = await cookies();
  return <div data-theme={c.get("theme")?.value ?? "none"}>{children}</div>;
}`

const closedRoutePage = `const TOPICS = [{ slug: "alpha", title: "Alpha Topic" }, { slug: "beta", title: "Beta Topic" }];
export const dynamicParams = false;
export const generateStaticParams = () => TOPICS.map((t) => ({ slug: t.slug }));
export default async function TopicPage({ params }: any) {
  const { slug } = await params;
  const t = TOPICS.find((x) => x.slug === slug);
  return <main><h1>{t ? t.title : "missing"}</h1></main>;
}`

func closedRouteFiles(withAsyncLayout bool) map[string]string {
	files := map[string]string{
		"src/pages/index.tsx":             `export default function Home() { return <p>home</p>; }`,
		"src/pages/(app)/help/[slug].tsx": closedRoutePage,
	}
	if withAsyncLayout {
		files["src/pages/(app)/_layout.tsx"] = closedRouteLayout
	}
	return files
}

// A closed-params route under a request-time layout cannot be prerendered: the
// listed params stay server routes that carry their params, the rest 404.
func TestClosedParamsRouteUnderRequestTimeLayoutServes(t *testing.T) {
	t.Parallel()
	b, root := setupPrerenderProject(t, closedRouteFiles(true), false)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	m := readManifest(t, root)
	for _, slug := range []string{"alpha", "beta"} {
		p, ok := manifestPage(m, "/help/"+slug)
		if !ok || p.Mode != RenderSSR {
			t.Fatalf("/help/%s: ok=%v mode=%v, want an ssr page in %+v", slug, ok, p.Mode, m.Pages)
		}
		if p.Params["slug"] != slug {
			t.Errorf("/help/%s params = %v, want slug=%s", slug, p.Params, slug)
		}
	}
	sm, err := readServerManifest(filepath.Join(root, "dist"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range sm.Pages {
		if strings.HasPrefix(p.Route, "/help/") && p.Params["slug"] == "" {
			t.Errorf("server manifest page %s has no params: %+v", p.Route, p)
		}
	}

	cfg := b.Cfg
	base, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for slug, title := range map[string]string{"alpha": "Alpha Topic", "beta": "Beta Topic"} {
		code, body := getText(t, base+"/help/"+slug+"/")
		if code != 200 || !strings.Contains(body, "<h1>"+title+"</h1>") || !strings.Contains(body, `data-theme="none"`) {
			t.Errorf("/help/%s/ = %d, want 200 with %q and the layout:\n%s", slug, code, title, body)
		}
	}
	for _, path := range []string{"/help/gamma/", "/help/gamma", "/help/alpha/extra/"} {
		if code, _ := getText(t, base+path); code != 404 {
			t.Errorf("%s = %d, want 404", path, code)
		}
	}
}

// Without a request-time layout the same route is prerendered to files and
// needs no server route.
func TestClosedParamsRouteWithoutRequestTimeLayoutPrerenders(t *testing.T) {
	t.Parallel()
	b, root := setupPrerenderProject(t, closedRouteFiles(false), false)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	for slug, title := range map[string]string{"alpha": "Alpha Topic", "beta": "Beta Topic"} {
		raw, err := os.ReadFile(filepath.Join(root, "dist", "help", slug, "index.html"))
		if err != nil {
			t.Fatalf("read prerendered /help/%s: %v", slug, err)
		}
		if !strings.Contains(string(raw), "<h1>"+title+"</h1>") {
			t.Errorf("/help/%s HTML missing the title:\n%s", slug, raw)
		}
	}
	m := readManifest(t, root)
	if p, ok := manifestPage(m, "/help/alpha"); !ok || p.Mode != RenderSSG {
		t.Errorf("/help/alpha: ok=%v mode=%v, want ssg", ok, p.Mode)
	}
	if _, err := os.Stat(filepath.Join(root, "dist", ".krate", "server-manifest.json")); err == nil {
		t.Errorf("a fully prerendered route should leave no server manifest")
	}
}

func TestGenerateStaticParamsConstFormIsDetected(t *testing.T) {
	t.Parallel()
	for src, want := range map[string]bool{
		"export const generateStaticParams = () => [];":                   true,
		"export let generateStaticParams = async () => [];":               true,
		"export function generateStaticParams() { return []; }":           true,
		"export async function generateStaticParams() { return []; }":     true,
		"const generateStaticParams = () => [];":                          false,
		"export const generateStaticParamsX = () => [];":                  false,
		"export const other = () => [], generateStaticParams = () => [];": true,
	} {
		prog, err := parseSourceFile("p.tsx", []byte(src))
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		if got := hasGenerateStaticParams(prog); got != want {
			t.Errorf("hasGenerateStaticParams(%q) = %v, want %v", src, got, want)
		}
	}
	if !gspExportRe.MatchString("export const generateStaticParams = () => [];") || gspExportRe.MatchString("export const generateStaticParamsX = 1;") {
		t.Errorf("gspExportRe does not match the const form exactly")
	}
}

// A dynamicParams = false route with no generateStaticParams has no page and
// no server route: it is a build error, not a silent 404.
func TestClosedParamsRouteWithoutStaticParamsIsAnError(t *testing.T) {
	t.Parallel()
	b, _ := scProject(t, map[string]string{
		"src/pages/index.tsx":        `export default function Home() { return <p>home</p>; }`,
		"src/pages/items/[slug].tsx": "export const dynamicParams = false;\nexport default function Item() { return <p>item</p>; }",
	})
	err := b.BuildAll()
	if err == nil {
		t.Fatal("BuildAll succeeded, want a build error")
	}
	if !strings.Contains(err.Error(), string(diag.CodeDynamicRouteUnserved)) && !strings.Contains(diag.FormatDiagnostics([]error{err}), string(diag.CodeDynamicRouteUnserved)) {
		t.Fatalf("error does not carry %s: %v", diag.CodeDynamicRouteUnserved, err)
	}
}

// The dev server serves the listed params of the same route.
func TestDevServesClosedParamsRouteUnderRequestTimeLayout(t *testing.T) {
	t.Parallel()
	d := startDevRun(t, closedRouteFiles(true))
	d.waitBody(t, "/help/alpha/", "<h1>Alpha Topic</h1>")
	d.waitBody(t, "/help/beta/", "<h1>Beta Topic</h1>")
	d.waitStatus(t, "/help/gamma/", 404)
}
