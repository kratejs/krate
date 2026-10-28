package build

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// scProject writes files (paths relative to the project root) and returns a
// builder for it.
func scProject(t *testing.T, files map[string]string) (*Builder, string) {
	t.Helper()
	root := t.TempDir()
	for rel, src := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	return New(root, cfg), root
}

func readManifest(t *testing.T, root string) Manifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "dist", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func manifestPage(m Manifest, route string) (PageMeta, bool) {
	for _, p := range m.Pages {
		if p.Route == route {
			return p, true
		}
	}
	return PageMeta{}, false
}

func TestAsyncPageIsServerRendered(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/pages/post.tsx": `export default async function Post({ params }: any) {
  const { slug } = await params;
  const d = await fetch("http://example.invalid/" + slug).then((r: any) => r.json());
  return <article><h1>{d.title}</h1></article>;
}`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	m := readManifest(t, root)
	p, ok := manifestPage(m, "/post")
	if !ok {
		t.Fatalf("no /post page in manifest: %+v", m.Pages)
	}
	if p.Mode != RenderSSR || !p.ServerComponents {
		t.Fatalf("async page: mode=%v serverComponents=%v, want ssr + true", p.Mode, p.ServerComponents)
	}
	shell, err := os.ReadFile(filepath.Join(root, "dist", "post", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(shell), "<!--suspense:page--><!--/suspense:page-->") {
		t.Errorf("async page shell should carry an empty page region, got:\n%s", shell)
	}
	if strings.Contains(string(shell), "<script src=/post/index.") {
		t.Errorf("async page must not ship hydration code:\n%s", shell)
	}
	sm, err := os.ReadFile(filepath.Join(root, "dist", "server-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sm), `"serverComponents": true`) {
		t.Errorf("server manifest should flag the page:\n%s", sm)
	}
}

func TestAsyncPageRevalidateExportIsISR(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/pages/news.tsx": `export const revalidate = 30;
export default async function News() {
  const items = await fetch("http://example.invalid/news").then((r: any) => r.json());
  return <ul>{items.map((i: any) => <li>{i}</li>)}</ul>;
}`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	p, _ := manifestPage(readManifest(t, root), "/news")
	if p.Mode != RenderISR || p.Revalidate != 30 {
		t.Fatalf("mode=%v revalidate=%d, want isr/30", p.Mode, p.Revalidate)
	}
}

func TestAsyncChildBecomesServerRegion(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/pages/post.tsx": `import { Wrapper } from '../Wrapper';
async function Comments({ id }: any) {
  const list = await fetch("http://example.invalid/c/" + id).then((r: any) => r.json());
  return <ul>{list.map((c: any) => <li>{c}</li>)}</ul>;
}
export default function Post() {
  return <main><h1>Post</h1>
    <Suspense fallback={<p>loading comments</p>}><Comments id="7" /></Suspense>
    <Wrapper />
  </main>;
}`,
		// A private async component in another module, reached through a sync
		// wrapper that would otherwise be evaluated at build time.
		"src/Wrapper.tsx": `const Likes = async ({ n }: any) => {
  const r = await fetch("http://example.invalid/likes/" + n).then((x: any) => x.json());
  return <b>{r.count}</b>;
};
export function Wrapper() { return <section><Likes n="3" /></section>; }`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	m := readManifest(t, root)
	p, _ := manifestPage(m, "/post")
	if p.Mode != RenderStreaming || !p.ServerComponents {
		t.Fatalf("mode=%v serverComponents=%v, want streaming + true", p.Mode, p.ServerComponents)
	}
	exports := map[string]bool{}
	for _, r := range m.Regions["/post"] {
		exports[r.ServerExport] = true
	}
	if !exports["__krate_sc_Comments"] || !exports["__krate_sc_Likes"] {
		t.Fatalf("regions should render both server components from the page bundle: %+v", m.Regions["/post"])
	}
	shell, _ := os.ReadFile(filepath.Join(root, "dist", "post", "index.html"))
	s := string(shell)
	if !strings.Contains(s, "loading comments") || !strings.Contains(s, "<section><!--region:") {
		t.Errorf("shell should bake the Suspense fallback and a region inside the wrapper:\n%s", s)
	}
	bundle, err := os.ReadFile(filepath.Join(root, "dist", ".krate", "server-bundles", "post.server.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"__krate_sc_Comments", "__krate_sc_Likes"} {
		if !strings.Contains(string(bundle), want) {
			t.Errorf("page server bundle does not export %s", want)
		}
	}
}

func TestAsyncLayoutIsAnError(t *testing.T) {
	b, _ := scProject(t, map[string]string{
		"src/pages/_layout.tsx": `export default async function Layout({ children }: any) {
  const nav = await fetch("http://example.invalid/nav").then((r: any) => r.json());
  return <div><nav>{nav.title}</nav>{children}</div>;
}`,
		"src/pages/index.tsx": `export default function Home() { return <p>home</p>; }`,
	})
	err := b.BuildAll()
	if err == nil || !strings.Contains(err.Error(), string(diag.CodeAsyncLayout)) {
		t.Fatalf("expected %s for an async layout, got %v", diag.CodeAsyncLayout, err)
	}
}

func TestServerComponentStaticOutputIsAnError(t *testing.T) {
	b, _ := scProject(t, map[string]string{
		"src/pages/index.tsx": `export default async function Home() { const d = await fetch("x").then((r: any) => r.text()); return <p>{d}</p>; }`,
	})
	b.Cfg.Output = "static"
	err := b.BuildAll()
	if err == nil || !strings.Contains(err.Error(), string(diag.CodeServerComponentStaticOutput)) {
		t.Fatalf("expected %s, got %v", diag.CodeServerComponentStaticOutput, err)
	}
}

func TestServerComponentRuntimePropsMustBeConstant(t *testing.T) {
	b, _ := scProject(t, map[string]string{
		"src/pages/index.tsx": `async function Price({ sku }: any) { const p = await fetch("x/" + sku).then((r: any) => r.json()); return <b>{p.v}</b>; }
export default function Home(props: any) {
  return <main><Price sku={props.query.sku} /></main>;
}`,
	})
	err := b.BuildAll()
	if err == nil || !strings.Contains(err.Error(), string(diag.CodeServerComponentProps)) || !strings.Contains(err.Error(), "prop `sku`") {
		t.Fatalf("expected %s naming prop sku, got %v", diag.CodeServerComponentProps, err)
	}
}

func TestServerComponentChildrenFromStaticPageIsAnError(t *testing.T) {
	b, _ := scProject(t, map[string]string{
		"src/pages/index.tsx": `async function Panel({ children }: any) { await Promise.resolve(); return <div>{children}</div>; }
export default function Home() { return <main><Panel><p>inner</p></Panel></main>; }`,
	})
	err := b.BuildAll()
	if err == nil || !strings.Contains(err.Error(), string(diag.CodeServerComponentProps)) || !strings.Contains(err.Error(), "children") {
		t.Fatalf("expected %s about children, got %v", diag.CodeServerComponentProps, err)
	}
}

func TestBuildTimeAwaitHasSpecificDiagnostic(t *testing.T) {
	b, _ := scProject(t, map[string]string{
		"src/pages/index.tsx": `const greeting = await Promise.resolve("hi");
export default function Home() { return <main><p>{greeting}</p></main>; }`,
	})
	err := b.BuildAll()
	if err == nil || !strings.Contains(err.Error(), string(diag.CodeBuildTimeAwait)) {
		t.Fatalf("expected %s, got %v", diag.CodeBuildTimeAwait, err)
	}
}

// TestStreamRegionPageFatalServerComponentError: a fatal region error frame
// (an async server component threw) must not stream the shell: the caller
// gets fatalErr and answers 500 instead of an empty region.
func TestStreamRegionPageFatalServerComponentError(t *testing.T) {
	absOut := t.TempDir()
	shell := `<main><!--region:region-1--><!--/region:region-1--></main>`
	rel := filepath.Join(absOut, "p", "index.html")
	if err := os.MkdirAll(filepath.Dir(rel), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rel, []byte(shell), 0644); err != nil {
		t.Fatal(err)
	}
	sidecar := newFakeRegionsSidecar(t, []string{
		`{"type":"error","id":"region-1","status":500,"error":"db down","fatal":true}`,
		`{"type":"end","count":1}`,
	})
	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	rr := streamRegionPage(rec, rec, newRegionShellCache(), absOut, "/p", sidecar.port, nil, nil, nil, nil, nil)
	if !rr.served || rr.fatalErr != "db down" {
		t.Fatalf("served=%v fatalErr=%q, want served + db down", rr.served, rr.fatalErr)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("shell must not be written on a fatal error, got %q", rec.Body.String())
	}
}

// TestStreamRegionPageRegionRedirect: redirect() thrown by a server component
// region redirects the whole response before any body is written.
func TestStreamRegionPageRegionRedirect(t *testing.T) {
	absOut := t.TempDir()
	shell := `<main><!--region:region-1--><!--/region:region-1--></main>`
	rel := filepath.Join(absOut, "p", "index.html")
	if err := os.MkdirAll(filepath.Dir(rel), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rel, []byte(shell), 0644); err != nil {
		t.Fatal(err)
	}
	sidecar := newFakeRegionsSidecar(t, []string{
		`{"type":"region","id":"region-1","status":307,"html":"","redirect":"/login"}`,
		`{"type":"end","count":1}`,
	})
	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	rr := streamRegionPage(rec, rec, newRegionShellCache(), absOut, "/p", sidecar.port, nil, nil, nil, nil, nil)
	if rr.redirect != "/login" {
		t.Fatalf("redirect = %q, want /login", rr.redirect)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("shell must not be written before a redirect, got %q", rec.Body.String())
	}
}
