package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/docs"
)

func TestFeedPluginGeneratesAllFormats(t *testing.T) {
	root := t.TempDir()
	docsDir := filepath.Join(root, "src", "content", "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(docsDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.md", "---\ntitle: Home\ndescription: Welcome home\n---\n# Home\n")
	write("post.md", "---\ntitle: My Post\ndescription: A post\ndate: 2024-03-01\ntags: [news]\n---\n# Post\n")

	out := filepath.Join(root, "dist")
	if err := os.MkdirAll(out, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.SEO.BaseURL = "https://example.com"
	cfg.Plugins = []config.PluginConfig{{Name: "feed", Options: map[string]interface{}{
		"title": "My Blog",
		"count": float64(10),
	}}}

	ctx := &BuildResultHookCtx{Root: root, OutDir: out, Config: cfg}
	if err := generateFeeds(ctx); err != nil {
		t.Fatalf("generateFeeds: %v", err)
	}

	for _, name := range []string{"feed.xml", "atom.xml", "feed.json"} {
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatalf("expected %s: %v", name, err)
		}
		s := string(data)
		if !strings.Contains(s, "My Post") || !strings.Contains(s, "My Blog") {
			t.Errorf("%s missing title/entry:\n%s", name, s)
		}
		if !strings.Contains(s, "https://example.com/docs/post/") {
			t.Errorf("%s missing absolute item URL:\n%s", name, s)
		}
	}

	// Newest-first ordering: the dated post should appear before the undated home.
	feed, _ := os.ReadFile(filepath.Join(out, "feed.xml"))
	if strings.Index(string(feed), "My Post") > strings.Index(string(feed), ">Home<") {
		t.Errorf("feed items not newest-first:\n%s", feed)
	}
}

func TestFeedPluginSkipsWhenNotConfigured(t *testing.T) {
	cfg := config.Default()
	ctx := &BuildResultHookCtx{Root: t.TempDir(), OutDir: t.TempDir(), Config: cfg}
	if err := generateFeeds(ctx); err != nil {
		t.Fatalf("unconfigured feed should be a no-op: %v", err)
	}
}

func TestToRFC3339(t *testing.T) {
	cases := map[string]string{
		"2024-03-01":                "2024-03-01T00:00:00Z",
		"2024-03-01T10:00:00Z":      "2024-03-01T10:00:00Z",
		"2024-03-01T10:00:00+01:00": "2024-03-01T09:00:00Z",
		"":                          "",
	}
	for in, want := range cases {
		if got := toRFC3339(in); got != want {
			t.Errorf("toRFC3339(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFeedBasePath(t *testing.T) {
	cases := []struct {
		name, seoBase, basePath string
	}{
		{"explicit basePath", "https://example.com", "/sub"},
		{"site url carries the path", "https://example.com/sub", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			docsDir := filepath.Join(root, "src", "content", "docs")
			if err := os.MkdirAll(docsDir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(docsDir, "post.md"), []byte("---\ntitle: My Post\ndate: 2024-03-01\n---\n# Post\n"), 0644); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(root, "dist")
			if err := os.MkdirAll(out, 0755); err != nil {
				t.Fatal(err)
			}
			cfg := config.Default()
			cfg.SEO.BaseURL = tc.seoBase
			cfg.BasePath = tc.basePath
			cfg.Plugins = []config.PluginConfig{{Name: "feed", Options: map[string]interface{}{"title": "T"}}}
			if err := generateFeeds(&BuildResultHookCtx{Root: root, OutDir: out, Config: cfg}); err != nil {
				t.Fatal(err)
			}
			read := func(n string) string {
				b, err := os.ReadFile(filepath.Join(out, n))
				if err != nil {
					t.Fatal(err)
				}
				return string(b)
			}
			rss, atom, js := read("feed.xml"), read("atom.xml"), read("feed.json")
			for name, want := range map[string][]string{
				"rss":  {"<link>https://example.com/sub/</link>", `href="https://example.com/sub/feed.xml"`, "https://example.com/sub/docs/post/"},
				"atom": {`href="https://example.com/sub/atom.xml"`, `href="https://example.com/sub/"`, "<id>https://example.com/sub/</id>"},
				"json": {`"home_page_url": "https://example.com/sub/"`, `"feed_url": "https://example.com/sub/feed.json"`},
			} {
				got := map[string]string{"rss": rss, "atom": atom, "json": js}[name]
				for _, w := range want {
					if !strings.Contains(got, w) {
						t.Errorf("%s missing %q:\n%s", name, w, got)
					}
				}
				if strings.Contains(got, "/sub/sub") {
					t.Errorf("%s doubles the base path:\n%s", name, got)
				}
			}

			head := docsHeadExtras(cfg, docs.Page{}, "")
			for _, w := range []string{`https://example.com/sub/feed.xml`, `https://example.com/sub/atom.xml`, `https://example.com/sub/feed.json`} {
				if !strings.Contains(head, w) {
					t.Errorf("head missing %q:\n%s", w, head)
				}
			}
			if strings.Contains(head, "/sub/sub") {
				t.Errorf("head doubles the base path:\n%s", head)
			}
		})
	}
}
