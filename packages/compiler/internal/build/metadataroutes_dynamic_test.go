package build

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestMetaRouteSchedule(t *testing.T) {
	cases := []struct {
		src     string
		dynamic bool
		reval   int
	}{
		{`export default function r() { return {}; }`, false, 0},
		{`export const dynamic = "force-dynamic";`, true, 0},
		{`export const dynamic = 'force-static';`, false, 0},
		{`export const revalidate = 3600;`, true, 3600},
		{`export const revalidate = 0;`, true, 0},
		{`export const revalidate = false;`, false, 0},
		{`export const dynamic = "force-dynamic";` + "\n" + `export const revalidate = 60;`, true, 60},
	}
	for _, c := range cases {
		dyn, reval := metaRouteSchedule(c.src)
		if dyn != c.dynamic || reval != c.reval {
			t.Errorf("%q: got (%v, %d), want (%v, %d)", c.src, dyn, reval, c.dynamic, c.reval)
		}
	}
}

func TestMetaRouteIndexMatch(t *testing.T) {
	idx := metaRouteIndex{
		{Kind: mrRobots, Path: "/robots.txt", Key: "__krate_meta/robots"},
		{Kind: mrSitemap, Prefix: "/sitemap/", Suffix: ".xml", Key: "__krate_meta/sitemap/[id]"},
	}
	for path, want := range map[string]string{
		"/robots.txt":      "__krate_meta/robots",
		"/sitemap/3.xml":   "__krate_meta/sitemap/3",
		"/sitemap/.xml":    "",
		"/sitemap/a/b.xml": "",
		"/robots.txt/x":    "",
		"/other":           "",
	} {
		got, ok := idx.match(path)
		if got != want || ok != (want != "") {
			t.Errorf("match(%q) = %q, %v; want %q", path, got, ok, want)
		}
	}
}

// dynamicMetaProject builds a project with a force-dynamic robots.ts, a
// revalidate sitemap.ts and a static manifest.ts.
func dynamicMetaProject(t *testing.T) (string, *config.Config) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
		t.Skip("node is not installed")
	}
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"src/pages/index.tsx": `export default function P() { return <main>home</main>; }`,
		"src/pages/robots.ts": `import "server-only";
import { headers } from "@krate/runtime/server";
export const dynamic = "force-dynamic";
let n = 0;
export default async function robots() {
  const origin = (await headers()).get("x-test-origin");
  n++;
  return { rules: [{ userAgent: "*", allow: "/" }], sitemap: origin + "/sitemap.xml?n=" + n };
}
`,
		"src/pages/sitemap.ts": `export const revalidate = 3600;
let n = 0;
export default function sitemap() { n++; return [{ url: "https://cached.example/" + n }]; }
`,
		"src/pages/manifest.ts": `export default function manifest() { return { name: "Static" }; }
`,
		"src/pages/docs/sitemap.ts": `export const dynamic = "force-dynamic";
export function generateSitemaps() { return [{ id: 0 }, { id: 1 }]; }
export default function sitemap({ id }: { id: string }) { return [{ url: "https://docs.example/part-" + id }]; }
`,
	})
	ensureHandlerDist(t)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	return root, cfg
}

// The node deploy target serves the same routes from its portable handler.
func TestDynamicMetadataRoutesOnNodeTarget(t *testing.T) {
	root, cfg := dynamicMetaProject(t)
	if _, err := BuildServerBundle(root, cfg, "node"); err != nil {
		t.Fatalf("BuildServerBundle: %v", err)
	}
	base := startNodeBundle(t, root, cfg)
	resp, body := getRaw(t, base, "/robots.txt", map[string]string{"X-Test-Origin": "https://node.example"})
	if resp.StatusCode != 200 || !strings.Contains(string(body), "Sitemap: https://node.example/sitemap.xml?n=1") {
		t.Fatalf("robots.txt: status %d body %q", resp.StatusCode, body)
	}
	_, body = getRaw(t, base, "/robots.txt", map[string]string{"X-Test-Origin": "https://node.example"})
	if !strings.Contains(string(body), "?n=2") {
		t.Errorf("a force-dynamic route must run on every request, got %q", body)
	}
	_, body = getRaw(t, base, "/sitemap.xml", nil)
	if !strings.Contains(string(body), "https://cached.example/1") {
		t.Errorf("sitemap.xml = %q", body)
	}
	_, body = getRaw(t, base, "/docs/sitemap/1.xml", nil)
	if !strings.Contains(string(body), "https://docs.example/part-1") {
		t.Errorf("/docs/sitemap/1.xml = %q", body)
	}
	if resp, _ := getRaw(t, base, "/docs/sitemap/9.xml", nil); resp.StatusCode != 404 {
		t.Errorf("an id generateSitemaps does not list: status %d, want 404", resp.StatusCode)
	}
}

// force-dynamic and revalidate metadata routes are not baked at build time:
// the build compiles them as Node routes, and `krate serve` answers the public
// URL from them per request (with headers() available), caching a revalidate
// route for its window. server-only imports work in them.
func TestDynamicMetadataRoutesServePerRequest(t *testing.T) {
	root, cfg := dynamicMetaProject(t)
	for _, rel := range []string{"robots.txt", "sitemap.xml"} {
		if _, err := os.Stat(filepath.Join(cfg.OutDir, rel)); err == nil {
			t.Errorf("dist/%s was written, want it served per request", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(cfg.OutDir, "manifest.webmanifest")); err != nil {
		t.Errorf("a static manifest.ts must still build: %v", err)
	}
	for _, rel := range []string{".krate/meta-routes.json", ".krate/api-node/__krate_meta/robots.js", ".krate/api-node/__krate_meta/sitemap.js"} {
		if _, err := os.Stat(filepath.Join(cfg.OutDir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("dist/%s missing: %v", rel, err)
		}
	}

	provideRuntime(t, root)
	base, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// The Node sidecar starts in the background; wait for it.
	for i := 0; i < 100; i++ {
		if r, _ := getRaw(t, base, "/api/__krate_meta/none", nil); r.StatusCode != http.StatusBadGateway {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	resp, body := getRaw(t, base, "/robots.txt", map[string]string{"X-Test-Origin": "https://one.example"})
	if resp.StatusCode != 200 || !strings.Contains(string(body), "Sitemap: https://one.example/sitemap.xml?n=1") {
		t.Fatalf("robots.txt: status %d body %q", resp.StatusCode, body)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") {
		t.Errorf("robots.txt Content-Type = %q", resp.Header.Get("Content-Type"))
	}
	_, body = getRaw(t, base, "/robots.txt", map[string]string{"X-Test-Origin": "https://two.example"})
	if !strings.Contains(string(body), "Sitemap: https://two.example/sitemap.xml?n=2") {
		t.Errorf("a force-dynamic route must run on every request, got %q", body)
	}
	resp, body = getRaw(t, base, "/sitemap.xml", nil)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "https://cached.example/1") {
		t.Fatalf("sitemap.xml: status %d body %q", resp.StatusCode, body)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/xml") {
		t.Errorf("sitemap.xml Content-Type = %q", resp.Header.Get("Content-Type"))
	}
	_, body = getRaw(t, base, "/sitemap.xml", nil)
	if !strings.Contains(string(body), "https://cached.example/1") {
		t.Errorf("a revalidate route must serve its cached result within the window, got %q", body)
	}
}
