package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func slashProject(t *testing.T) (root string, base config.Config) {
	t.Helper()
	root = t.TempDir()
	for name, body := range map[string]string{
		"src/content/docs/index.md":       "---\ntitle: Home\n---\nhi\n",
		"src/content/docs/guide/start.md": "---\ntitle: Start\ndescription: begin\n---\nbody\n",
	} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, config.Config{SEO: config.SEOConfig{BaseURL: "https://example.com"}}
}

func readOut(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLLMsAndFeedHonourTrailingSlashNever(t *testing.T) {
	for _, tc := range []struct{ mode, wantStart, wantDocs string }{
		{"never", "https://example.com/docs/guide/start)", "https://example.com/docs)"},
		{"always", "https://example.com/docs/guide/start/)", "https://example.com/docs/)"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			root, cfg := slashProject(t)
			cfg.TrailingSlash = tc.mode
			cfg.Plugins = []config.PluginConfig{{Name: "llms"}, {Name: "feed"}}
			out := t.TempDir()
			ctx := &BuildResultHookCtx{Root: root, OutDir: out, Config: &cfg}
			if err := generateLLMs(ctx); err != nil {
				t.Fatal(err)
			}
			if err := generateFeeds(ctx); err != nil {
				t.Fatal(err)
			}
			if got := readOut(t, out, "llms.txt"); !strings.Contains(got, tc.wantStart) {
				t.Errorf("llms.txt: want %q in:\n%s", tc.wantStart, got)
			}
			if got, want := readOut(t, out, "llms-full.txt"), "URL: "+strings.TrimSuffix(tc.wantStart, ")")+"\n"; !strings.Contains(got, want) {
				t.Errorf("llms-full.txt: want %q in:\n%s", want, got)
			}
			if got := readOut(t, out, "llms.txt"); !strings.Contains(got, tc.wantDocs) {
				t.Errorf("llms.txt: want %q in:\n%s", tc.wantDocs, got)
			}
			feed := readOut(t, out, "feed.json") + readOut(t, out, "feed.xml") + readOut(t, out, "atom.xml")
			want := strings.TrimSuffix(tc.wantStart, ")")
			if !strings.Contains(feed, want) {
				t.Errorf("feeds: want %q in:\n%s", want, feed)
			}
			if tc.mode == "never" && strings.Contains(feed, "/docs/guide/start/") {
				t.Errorf("feeds keep a trailing slash:\n%s", feed)
			}
		})
	}
}

func TestSitemapHonoursTrailingSlashNever(t *testing.T) {
	cfg := &config.Config{
		TrailingSlash: "never",
		BasePath:      "/sub",
		SEO:           config.SEOConfig{BaseURL: "https://example.com"},
	}
	got := runSitemap(t, cfg, []PageResult{{OutName: "."}, {OutName: "docs/guide/start"}, {OutName: "feed.xml"}})
	for _, want := range []string{"<loc>https://example.com/sub/</loc>", "<loc>https://example.com/sub/docs/guide/start</loc>", "/sub/feed.xml</loc>"} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}
}
