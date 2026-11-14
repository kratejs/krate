package build

import (
	"encoding/json"
	"errors"
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
	data, err := os.ReadFile(ManifestPath(filepath.Join(root, "dist")))
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
	sm, err := os.ReadFile(ServerManifestPath(filepath.Join(root, "dist")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sm), `"serverComponents": true`) {
		t.Errorf("server manifest should flag the page:\n%s", sm)
	}
}

func TestAsyncLayoutBelowTemplateDiagnostic(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/pages/app/_layout.tsx":  `export default async function Layout({ children }: any) { await Promise.resolve(); return <div>{children}</div>; }`,
		"src/pages/app/template.tsx": `export default function Tpl({ children }: any) { return <section>{children}</section>; }`,
		"src/pages/app/index.tsx":    `export default function Page() { return <main>x</main>; }`,
	})
	_, err := b.asyncLayoutCut(filepath.Join(root, "src", "pages", "app", "index.tsx"))
	var d diag.Diagnostic
	if !errors.As(err, &d) {
		t.Fatalf("want a diagnostic, got %v", err)
	}
	if !strings.Contains(d.Message, "template.tsx sits below the async layout") {
		t.Errorf("message grammar: %q", d.Message)
	}
	if d.Line != 1 || d.Col != 1 {
		t.Errorf("position %d:%d, want 1:1", d.Line, d.Col)
	}
}

// TestServerBundleReactShimEffectsAndCache covers the server bundle's stand-in
// for `react`: useEffect (called in a server page and in a "use client" child)
// is a no-op while rendering, and cache() memoizes per request.
func TestServerBundleReactShimEffectsAndCache(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/components/Redirector.tsx": `"use client";
import { useEffect, useLayoutEffect } from "react";
export function Redirector() {
  useEffect(() => { document.title = "x"; }, []);
  useLayoutEffect(() => {}, []);
  return <p id="redir">redirecting</p>;
}`,
		"src/pages/signin/done.tsx": `import { useEffect } from "react";
import { Redirector } from "../../components/Redirector";
export default async function Done() {
  await Promise.resolve();
  useEffect(() => { (globalThis as any).__effectRan = true; }, []);
  return <main><Redirector /><p id="ran">{String((globalThis as any).__effectRan === true)}</p></main>;
}`,
		"src/pages/showcase/[id].tsx": `import { cache } from "react";
const getItem = cache(async (id: string) => {
  const g = globalThis as any;
  g.__runs = (g.__runs || 0) + 1;
  return { id, runs: g.__runs };
});
export default async function Show({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const a = await getItem(id);
  const b = await getItem(id);
  return <main><p id="r">runs:{a.runs}:{b.runs}</p></main>;
}`,
	})
	withPreact(t, root)
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
	_, _, html := getBody(t, base+"/signin/done/")
	if !strings.Contains(html, "redirecting") || !strings.Contains(html, `<p id="ran">false</p>`) {
		t.Fatalf("expected the page with a no-op server effect:\n%s", html)
	}
	// One run per request: the second request starts a new cache scope.
	for _, want := range []string{"runs:1:1", "runs:2:2"} {
		_, _, html = getBody(t, base+"/showcase/7/")
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q (cache() memoizes within a request only):\n%s", want, html)
		}
	}
}
