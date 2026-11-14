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
