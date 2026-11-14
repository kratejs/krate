package build

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func getWith(t *testing.T, url string, header map[string]string) (int, http.Header, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "text/html")
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(data)
}

func segmentSSRProject(t *testing.T) (*Builder, string) {
	t.Helper()
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx": `export default function Home() { return <h1>home</h1>; }`,
		"src/pages/404.tsx": `import { headers } from "@krate/runtime/server";
export const config = { ssr: true };
export default async function NotFound() {
  const h = await headers();
  return <main><Head><title>{"Missing " + h.get("x-site")}</title></Head><h1>missing for {h.get("x-site")}</h1></main>;
}`,
		"src/pages/blog/index.tsx": `export default function Blog() { return <h1>blog</h1>; }`,
		"src/pages/blog/[slug].tsx": `import { notFound } from "@krate/runtime";
export const config = { ssr: true };
export default async function Post({ params }: { params: { slug: string } }) {
  if (params.slug === "gone") notFound();
  return <h1>post {params.slug}</h1>;
}`,
		"src/pages/blog/404.tsx": `// blog's own 404 stays prerendered
export default function BlogNotFound() { return <h1>no such post</h1>; }`,
		"src/pages/error.tsx": `export const config = { ssr: true };
export default function Err() { return <h1>oops</h1>; }`,
	})
	e2eEnv(t)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	return b, root
}

// TestSegmentPageRendersPerRequest: a 404 page with `ssr` is rendered for every
// request that misses, with the 404 status, and reads request data.
func TestSegmentPageRendersPerRequest(t *testing.T) {
	b, root := segmentSSRProject(t)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	m := readManifest(t, root)
	var nf *SegmentEntry
	for i := range m.Segments {
		if m.Segments[i].Key == "" {
			nf = &m.Segments[i]
		}
	}
	if nf == nil || !nf.dynamic(SegmentNotFound) || nf.dynamic(SegmentError) {
		t.Fatalf("root segment entry = %+v", nf)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}

	// An unmatched URL.
	status, header, html := getWith(t, base+"/nope/", map[string]string{"x-site": "alpha"})
	if status != 404 || !strings.Contains(html, "missing for alpha") || !strings.Contains(html, "<title>Missing alpha</title>") {
		t.Fatalf("status %d (want 404 with the request's header):\n%s", status, html)
	}
	if header.Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", header.Get("Cache-Control"))
	}
	// Another request gets its own render.
	if _, _, html = getWith(t, base+"/nope/", map[string]string{"x-site": "beta"}); !strings.Contains(html, "missing for beta") {
		t.Errorf("each request renders on its own:\n%s", html)
	}
	// A route whose render calls notFound(): the nearest 404 page of its directory.
	status, _, html = getWith(t, base+"/blog/gone/", nil)
	if status != 404 || !strings.Contains(html, "no such post") {
		t.Errorf("blog/gone: status %d:\n%s", status, html)
	}
	// The shell of the page is not a URL of the site; its hydration directory
	// does not serve a document either.
	for _, p := range []string{"/__404", "/__404/", "/__404/index.html"} {
		if status, _, body := getWith(t, base+p, nil); status != 404 || strings.Contains(body, "missing for") {
			t.Errorf("%s answered %d:\n%s", p, status, body)
		}
	}
}

// TestSegmentPageRendersPerRequestOnNodeTarget: the node target answers a
// missed URL with the 404 page rendered for that request, like the Go host.
func TestSegmentPageRendersPerRequestOnNodeTarget(t *testing.T) {
	if !canStopInProcessServer {
		t.Skip("in-process Go server needs a unix host")
	}
	root, cfg := serverlessProject(t, "segmentssr")
	goBase, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatalf("krate start: %v", err)
	}
	if _, err := BuildServerBundle(root, cfg, "node"); err != nil {
		t.Fatalf("BuildServerBundle: %v", err)
	}
	nodeBase := startNodeBundle(t, root, cfg)
	for name, base := range map[string]string{"go": goBase, "node": nodeBase} {
		for _, path := range []string{"/nope/", "/gone/"} {
			status, header, html := getWith(t, base+path, map[string]string{"x-site": "alpha"})
			if status != 404 || !strings.Contains(html, "missing for alpha") || !strings.Contains(html, "<title>Missing alpha</title>") {
				t.Errorf("%s %s: status %d:\n%s", name, path, status, html)
			}
			if header.Get("Cache-Control") != "no-store" {
				t.Errorf("%s %s: Cache-Control = %q", name, path, header.Get("Cache-Control"))
			}
		}
		if status, _, body := getWith(t, base+"/__404/", nil); status != 404 || strings.Contains(body, "missing for") {
			t.Errorf("%s: the shell must not be a public URL, got %d:\n%s", name, status, body)
		}
	}
}

// TestSegmentErrorPageStaysPrerendered: only not-found, forbidden and
// unauthorized pages render per request. An error page answers while the app is
// failing, so its rendering mode is ignored with KR9003.
func TestSegmentErrorPageStaysPrerendered(t *testing.T) {
	b, root := segmentSSRProject(t)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	for _, e := range readManifest(t, root).Segments {
		if e.Key == "" && len(e.Dynamic) != 1 {
			t.Errorf("only the 404 page is dynamic: %+v", e)
		}
	}
}

// runtimeScriptProject builds files in a project that can load the built
// runtime, so a page that needs it shows a script (a project without the
// runtime would pass every "ships no script" check).
func runtimeScriptProject(t *testing.T, files map[string]string) string {
	t.Helper()
	b, root := scProject(t, files)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	return root
}

func readDistPage(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "dist", filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestRequestTimePagesShipNoRuntime: a page rendered per request that has
// nothing interactive, the SSR 404 page included, loads no runtime and no
// hydration script, and server-only code in it never reaches the browser. A
// page with state still hydrates.
func TestRequestTimePagesShipNoRuntime(t *testing.T) {
	root := runtimeScriptProject(t, map[string]string{
		"src/lib/env.ts": `export function readEnv() { const env = { A: process.env.A }; return env; }`,
		"src/pages/index.tsx": `import { createSignal } from "@krate/runtime";
export default function Home() { const [n, setN] = createSignal(0); return <button onClick={() => setN(n() + 1)}>{n()}</button>; }`,
		"src/pages/dyn.tsx": `import { readEnv } from "../lib/env";
export const dynamic = "force-dynamic";
export default function Dyn() { const e = readEnv(); return <p>{e.A}</p>; }`,
		"src/pages/who.tsx": `import { headers } from "@krate/runtime/server";
export default async function Who() { const h = await headers(); return <p>{h.get("x-who")}</p>; }`,
		"src/pages/404.tsx": `import { readEnv } from "../lib/env";
export const config = { ssr: true };
export default function NotFound() { const e = readEnv(); return <main><h1>404 {e.A}</h1></main>; }`,
	})
	if html := readDistPage(t, root, "index.html"); !strings.Contains(html, "<script src") {
		t.Fatalf("a page with state must hydrate:\n%s", html)
	}
	for _, name := range []string{"dyn/index.html", "who/index.html", "404.html"} {
		if html := readDistPage(t, root, name); strings.Contains(html, "<script") {
			t.Errorf("%s: a non-interactive page rendered per request must ship no script:\n%s", name, html)
		}
	}
	for _, dir := range []string{"dyn", "who", "__404"} {
		if files, _ := filepath.Glob(filepath.Join(root, "dist", dir, "*.js")); len(files) > 0 {
			t.Errorf("%s: no hydration script expected, got %v", dir, files)
		}
	}
}

// TestComponentLocalConstShipsNoRuntime: a local constant with a build-time
// value is baked into the HTML like a module-scope one.
func TestComponentLocalConstShipsNoRuntime(t *testing.T) {
	root := runtimeScriptProject(t, map[string]string{
		"src/pages/index.tsx": `export default function Page() {
  const label = 'x' + 'y';
  return <p>{label}</p>;
}`,
	})
	html := readDistPage(t, root, "index.html")
	if !strings.Contains(html, "<p>xy</p>") || strings.Contains(html, "<script") {
		t.Errorf("a component-local constant must leave the page static:\n%s", html)
	}
}
