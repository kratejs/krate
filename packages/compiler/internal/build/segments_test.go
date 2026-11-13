package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// A build writes the pages next to their directories, records the segment map
// and each route's source directory in the manifest, and keeps segment pages
// out of the route list.
func TestBuildWritesSegmentPagesAndManifest(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"src/pages/_layout.tsx":           `export default function L({ children }: any) { return <div id="root-layout">{children}</div>; }`,
		"src/pages/index.tsx":             devPage("home"),
		"src/pages/404.tsx":               devPage("ROOT404"),
		"src/pages/blog/_layout.tsx":      `export default function L({ children }: any) { return <section id="blog-layout">{children}</section>; }`,
		"src/pages/blog/index.tsx":        devPage("blog"),
		"src/pages/blog/not-found.tsx":    devPage("BLOG404"),
		"src/pages/blog/[slug]/index.tsx": devPage("post"),
		"src/pages/blog/[slug]/404.tsx":   devPage("POST404"),
		"src/pages/(shop)/cart/index.tsx": devPage("cart"),
		"src/pages/(shop)/cart/404.tsx":   devPage("CART404"),
		"src/pages/(shop)/cart/500.tsx":   devPage("CART500"),
	})
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	for rel, want := range map[string][]string{
		"404.html":             {"ROOT404", "root-layout"},
		"blog/404.html":        {"BLOG404", "root-layout", "blog-layout"},
		"blog/[slug]/404.html": {"POST404", "blog-layout"},
		"(shop)/cart/404.html": {"CART404"},
		"(shop)/cart/500.html": {"CART500"},
	} {
		data, err := os.ReadFile(filepath.Join(cfg.OutDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("%s not written: %v", rel, err)
			continue
		}
		for _, w := range want {
			if !strings.Contains(string(data), w) {
				t.Errorf("%s missing %q", rel, w)
			}
		}
		if !strings.Contains(string(data), "noindex") {
			t.Errorf("%s should be noindex", rel)
		}
		if strings.Contains(string(data), "canonical") {
			t.Errorf("%s must not carry a canonical link", rel)
		}
	}
	m := loadManifestForTest(t, cfg.OutDir)
	if len(m.Segments) != 4 {
		t.Fatalf("segments = %+v", m.Segments)
	}
	for _, p := range m.Pages {
		if isSegmentPageBase(strings.TrimSuffix(filepath.Base(p.Source), filepath.Ext(p.Source))) {
			t.Errorf("segment page %s leaked into the route list", p.Source)
		}
		if p.Route == "/blog/[slug]" && p.SegDir != "blog/[slug]" {
			t.Errorf("segDir of %s = %q", p.Route, p.SegDir)
		}
		if p.Route == "/cart" && p.SegDir != "(shop)/cart" {
			t.Errorf("segDir of %s = %q", p.Route, p.SegDir)
		}
	}
	// No route exists at the segment page URLs.
	for _, rel := range []string{"blog/404/index.html", "404/index.html"} {
		if _, err := os.Stat(filepath.Join(cfg.OutDir, filepath.FromSlash(rel))); err == nil {
			t.Errorf("unexpected route directory %s", rel)
		}
	}
}

func loadManifestForTest(t *testing.T, outDir string) *Manifest {
	t.Helper()
	data, err := os.ReadFile(ManifestPath(outDir))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return &m
}
