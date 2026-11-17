package plugin

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func runSitemap(t *testing.T, cfg *config.Config, pages []PageResult) string {
	t.Helper()
	out := t.TempDir()
	ctx := &BuildResultHookCtx{Root: out, OutDir: out, Config: cfg, Pages: pages}
	if err := generateSitemap(ctx); err != nil {
		t.Fatalf("generateSitemap: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(out, "sitemap.xml"))
	if err != nil {
		t.Fatalf("sitemap.xml not written: %v", err)
	}
	return string(data)
}

func TestSitemapFiltersEscapesAndPrefixes(t *testing.T) {
	cfg := &config.Config{
		BasePath: "/sub",
		SEO:      config.SEOConfig{BaseURL: "https://example.com"},
		Plugins: []config.PluginConfig{{
			Name:    "sitemap",
			Options: map[string]interface{}{"exclude": []interface{}{"/admin/*", "/private/"}},
		}},
	}
	got := runSitemap(t, cfg, []PageResult{
		{OutName: "."},
		{OutName: "."}, // the 404 page also has OutName "."
		{OutName: ".", IsErrorPage: true},
		{OutName: "blog/a&b"},
		{OutName: "blog/caf\u00e9"},
		{OutName: "blog/[slug]"},
		{OutName: "hidden", HeadHTML: `<meta name="robots" content="noindex, nofollow">`},
		{OutName: "admin/users"},
		{OutName: "private"},
		{OutName: "about"},
	})
	if err := xml.Unmarshal([]byte(got), new(struct{})); err != nil {
		t.Fatalf("sitemap is not well-formed XML: %v\n%s", err, got)
	}
	for _, want := range []string{
		"<loc>https://example.com/sub/</loc>",
		"<loc>https://example.com/sub/about/</loc>",
		"<loc>https://example.com/sub/blog/a&amp;b/</loc>",
		"<loc>https://example.com/sub/blog/caf%C3%A9/</loc>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("sitemap missing %s:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "<loc>https://example.com/sub/</loc>"); n != 1 {
		t.Errorf("root listed %d times, want 1:\n%s", n, got)
	}
	for _, bad := range []string{"[slug]", "hidden", "admin", "private"} {
		if strings.Contains(got, bad) {
			t.Errorf("sitemap should not list %q:\n%s", bad, got)
		}
	}
}

func TestSitemapLastmodOfGeneratedDocsUsesSourceDate(t *testing.T) {
	cfg := &config.Config{SEO: config.SEOConfig{BaseURL: "https://example.com"}}
	root := t.TempDir()
	gen := filepath.Join(root, ".krate", "gen", "docs")
	if err := os.MkdirAll(gen, 0o755); err != nil {
		t.Fatal(err)
	}
	page := "export default function DocPage() {\n  const docsProps = {\n    pageTitle: \"A\",\n    lastUpdated: \"2025-03-04T10:00:00Z\",\n    authors: [],\n  };\n}\n"
	if err := os.WriteFile(filepath.Join(gen, "a.tsx"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	noDate := "export default function DocPage() {\n  const docsProps = { pageTitle: \"B\" };\n}\n"
	if err := os.WriteFile(filepath.Join(gen, "b.tsx"), []byte(noDate), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := &BuildResultHookCtx{Root: root, OutDir: root, Config: cfg, Pages: []PageResult{
		{OutName: "docs/a", Page: ".krate/gen/docs/a.tsx"},
		{OutName: "docs/b", Page: ".krate/gen/docs/b.tsx"},
	}}
	if err := generateSitemap(ctx); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "sitemap.xml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "<loc>https://example.com/docs/a/</loc>\n    <lastmod>2025-03-04</lastmod>") {
		t.Errorf("docs page must carry its source date:\n%s", got)
	}
	if strings.Count(got, "<lastmod>2025-03-04</lastmod>") != 1 {
		t.Errorf("a page without a source date must not borrow one:\n%s", got)
	}
}
