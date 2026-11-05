package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func TestIsPagesEndpointFile(t *testing.T) {
	for name, want := range map[string]bool{
		"data.json.ts":        true,
		"feed.xml.js":         true,
		"robots.txt.ts":       true,
		"site.webmanifest.ts": true,
		"index.tsx":           false,
		"data.ts":             false,
		"[...slug].ts":        false,
		"page.name.ts":        false,
		"_private.json.ts":    false,
		"data.json.tsx":       false,
		".json.ts":            false,
	} {
		if got := isPagesEndpointFile(name); got != want {
			t.Errorf("isPagesEndpointFile(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestPagesStraysAreNotPages(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx":       `export default function Home() { return <main>home</main>; }`,
		"src/pages/api/hello.ts":    `export function GET() { return Response.json({ hi: 1 }); }`,
		"src/pages/api/docs.tsx":    `export default function Docs() { return <main>api docs page</main>; }`,
		"src/pages/data.json.ts":    `export function GET() { return Response.json({ a: 1 }); }`,
		"src/pages/blog/rss.xml.js": `export function GET() { return new Response("<rss/>"); }`,
	})
	pages, err := findPages(b.Cfg.PagesDir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range pages {
		rel, _ := filepath.Rel(b.Cfg.PagesDir, p)
		got = append(got, filepath.ToSlash(rel))
	}
	if strings.Join(got, ",") != "api/docs.tsx,index.tsx" && strings.Join(got, ",") != "index.tsx,api/docs.tsx" {
		t.Errorf("pages = %v, want only the two components (a .tsx page under api/ is a real page)", got)
	}
	strays := findPagesStrays(b.Cfg.PagesDir)
	if len(strays.endpoints) != 2 || len(strays.apiFiles) != 1 {
		t.Fatalf("strays = %+v", strays)
	}
	w := b.strayAPIWarnings(strays.apiFiles)
	if len(w) != 1 || w[0].Code != diag.CodeAPIRouteInPages || !w[0].IsWarning() ||
		!strings.Contains(w[0].Hint, "src/api/hello.ts") || w[0].File != "src/pages/api/hello.ts" {
		t.Errorf("warning = %+v", w)
	}
	_ = root
}

func TestPrerenderedEndpointBuildsAsFile(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node not available: %v", err)
		return
	}
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx":       `export default function Home() { return <main>home</main>; }`,
		"src/pages/data.json.ts":    `export async function GET() { return Response.json({ items: [1, 2, 3] }); }`,
		"src/pages/blog/rss.xml.ts": "const items = ['a', 'b'];\nexport function GET() { return new Response('<rss>' + items.join(',') + '</rss>', { headers: { 'content-type': 'application/xml' } }); }",
	})
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	out := filepath.Join(root, "dist")
	data, err := os.ReadFile(filepath.Join(out, "data.json"))
	if err != nil || strings.TrimSpace(string(data)) != `{"items":[1,2,3]}` {
		t.Fatalf("data.json = %q (%v)", data, err)
	}
	rss, err := os.ReadFile(filepath.Join(out, "blog", "rss.xml"))
	if err != nil || string(rss) != "<rss>a,b</rss>" {
		t.Fatalf("blog/rss.xml = %q (%v)", rss, err)
	}
	for _, stale := range []string{"data.json/index.html", "blog/rss.xml/index.html"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(stale))); err == nil {
			t.Errorf("%s: endpoints must not also build an empty HTML page", stale)
		}
	}
	m := readManifest(t, root)
	for _, p := range m.Pages {
		if strings.Contains(p.Route, "data.json") || strings.Contains(p.Route, "rss.xml") {
			t.Errorf("endpoint registered as page route %q", p.Route)
		}
	}
}

func TestPrerenderedEndpointErrors(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node not available: %v", err)
		return
	}
	cases := map[string]struct{ src, want string }{
		"src/pages/nogett.json.ts": {`export function POST() { return new Response("x"); }`, "no GET export"},
		"src/pages/status.txt.ts":  {`export function GET() { return new Response("no", { status: 500 }); }`, "status 500"},
		"src/pages/boom.json.ts":   {`export function GET() { throw new Error("kaput"); }`, "kaput"},
		"src/pages/[id].json.ts":   {`export function GET() { return Response.json({}); }`, "dynamic segment"},
	}
	for path, c := range cases {
		t.Run(filepath.Base(path), func(t *testing.T) {
			b, _ := scProject(t, map[string]string{
				"src/pages/index.tsx": `export default function Home() { return <main>home</main>; }`,
				path:                  c.src,
			})
			err := b.BuildAll()
			if err == nil || !strings.Contains(err.Error(), string(diag.CodePagesEndpoint)) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("expected %s mentioning %q, got %v", diag.CodePagesEndpoint, c.want, err)
			}
		})
	}
}
