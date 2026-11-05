package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/migrate"
)

// TestNextMigrateMetadataPagesBuild migrates pages whose generateMetadata is
// dynamic (sync and async) and whose default export is async or a named
// reference, then builds them. The wrapper that carries the head tags must
// compile, keep the page's params, and render the page body.
func TestNextMigrateMetadataPagesBuild(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"package.json":   `{"name":"meta-ref","private":true,"dependencies":{"next":"15.0.0","react":"19.0.0"}}`,
		"app/layout.tsx": `export default function RootLayout({ children }) { return <html><body>{children}</body></html>; }`,
		"app/async/[slug]/page.tsx": `export async function generateStaticParams() { return [{ slug: "hello" }]; }
export async function generateMetadata({ params }: { params: { slug: string } }) { return { title: "Async " + params.slug }; }
export default async function Post({ params }: { params: { slug: string } }) {
  return <article><h1>async-body-{params.slug}</h1></article>;
}`,
		"app/sync/[slug]/page.tsx": `export function generateStaticParams() { return [{ slug: "world" }]; }
export function generateMetadata({ params }: { params: { slug: string } }) { return { title: "Sync " + params.slug }; }
export default function Post({ params }: { params: { slug: string } }) {
  return <article><h1>sync-body-{params.slug}</h1></article>;
}`,
		"app/named/[slug]/page.tsx": `export function generateStaticParams() { return [{ slug: "again" }]; }
export function generateMetadata({ params }: { params: { slug: string } }) { return { title: "Named " + params.slug }; }
function Post({ params }: { params: { slug: string } }) {
  return <article><h1>named-body-{params.slug}</h1></article>;
}
export default Post;
`,
	}
	for rel, content := range files {
		writeFileRel(t, root, rel, content)
	}
	rep, err := migrate.ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range rep.Files {
		if f.Kind != migrate.KindPage || !strings.Contains(f.Target, "[slug]") {
			continue
		}
		body := string(f.Content)
		// generateMetadata runs natively: the page is not wrapped and keeps its export.
		if strings.Contains(body, "KrateMetadata") || strings.Contains(body, "PageWithHead") || !strings.Contains(body, "generateMetadata") {
			t.Fatalf("%s: generateMetadata must stay as written:\n%s", f.Target, body)
		}
	}
	if _, err := migrate.Apply(rep, migrate.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	prepareMigratedRuntime(t, root)
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	b := New(root, cfg)
	b.SkipQualityChecks = true
	if err := b.BuildAll(); err != nil {
		t.Fatalf("build: %v", err)
	}
	// An async page renders per request, so it has no build-time body; it must
	// be an SSR route (its generateMetadata runs per request), with the sync
	// and named pages built statically with generateMetadata run at build time.
	manData, err := os.ReadFile(ManifestPath(filepath.Join(root, "dist")))
	if err != nil {
		t.Fatal(err)
	}
	var man Manifest
	if err := json.Unmarshal(manData, &man); err != nil {
		t.Fatal(err)
	}
	asyncMode := RenderMode(-1)
	for _, p := range man.Pages {
		if strings.HasPrefix(p.Route, "/async/") {
			asyncMode = p.Mode
		}
	}
	if asyncMode != RenderSSR {
		t.Errorf("async page mode = %v, want ssr; pages: %+v", asyncMode, man.Pages)
	}
	for route, want := range map[string][]string{
		"sync/world":  {"sync-body-world", "<title>Sync world</title>"},
		"named/again": {"named-body-again", "<title>Named again</title>"},
	} {
		data, err := os.ReadFile(filepath.Join(root, "dist", filepath.FromSlash(route), "index.html"))
		if err != nil {
			t.Fatalf("%s: %v", route, err)
		}
		for _, w := range want {
			if !strings.Contains(string(data), w) {
				t.Errorf("%s: missing %q in:\n%s", route, w, data)
			}
		}
	}
}
