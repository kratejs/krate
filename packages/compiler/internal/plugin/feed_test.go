package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
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
