package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestContentConfigFromKrateConfig(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Resolve(root)
	cfg.Content = map[string]any{
		"blog": map[string]any{
			"dir": "src/content/blog",
			"schema": map[string]any{
				"title": "string",
				"order": map[string]any{"type": "number", "required": true},
				"tags":  "string[]",
			},
		},
	}

	b := New(root, cfg)
	cc := b.contentConfig()
	if cc == nil {
		t.Fatal("expected content config")
	}
	blog, ok := cc.Collections["blog"]
	if !ok {
		t.Fatalf("expected blog collection, got %v", cc.Collections)
	}
	if blog.Dir != "src/content/blog" {
		t.Errorf("dir = %q", blog.Dir)
	}
	if blog.Schema["title"].Type != "string" {
		t.Errorf("title schema = %+v", blog.Schema["title"])
	}
	if !blog.Schema["order"].Required {
		t.Errorf("order should be required: %+v", blog.Schema["order"])
	}
	if blog.Schema["tags"].Type != "string[]" {
		t.Errorf("tags schema = %+v", blog.Schema["tags"])
	}
}

func TestContentConfigDefaultDir(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Resolve(root)
	cfg.Content = map[string]any{
		"pages": map[string]any{"schema": map[string]any{"title": "string"}},
	}
	cc := New(root, cfg).contentConfig()
	if got := cc.Collections["pages"].Dir; got != "src/content/pages" {
		t.Errorf("default dir = %q, want src/content/pages", got)
	}
}

func TestContentConfigEmpty(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Resolve(root)
	if cc := New(root, cfg).contentConfig(); cc != nil {
		t.Fatalf("expected nil content config, got %+v", cc)
	}
}

func TestContentConfigMergesContributedDocs(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Resolve(root)
	cfg.Content = map[string]any{
		"blog": map[string]any{"dir": "src/content/blog", "schema": map[string]any{"title": "string"}},
	}
	cfg.Plugins = []config.PluginConfig{{Name: "docs", Options: map[string]interface{}{"contentDir": "src/content/docs"}}}

	cc := New(root, cfg).contentConfig()
	if cc == nil {
		t.Fatal("expected content config")
	}
	if _, ok := cc.Collections["blog"]; !ok {
		t.Errorf("expected blog collection, got %v", cc.Collections)
	}
	docs, ok := cc.Collections["docs"]
	if !ok {
		t.Fatalf("expected plugin-contributed docs collection, got %v", cc.Collections)
	}
	if docs.Dir != "src/content/docs" {
		t.Errorf("docs dir = %q, want src/content/docs", docs.Dir)
	}
	if docs.Schema["title"].Type != "string" {
		t.Errorf("docs schema missing title string: %+v", docs.Schema)
	}
}

// ContributedCollectionsOverrideOrder verifies the effective config keeps the
// configured collection when a plugin contributes a colliding name.
func TestContributedCollectionDoesNotOverrideConfigured(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Resolve(root)
	cfg.Content = map[string]any{
		"docs": map[string]any{"dir": "content/custom-docs", "schema": map[string]any{"title": "string"}},
	}
	cfg.Plugins = []config.PluginConfig{{Name: "docs", Options: map[string]interface{}{"contentDir": "src/content/docs"}}}

	cc := New(root, cfg).contentConfig()
	if cc.Collections["docs"].Dir != "content/custom-docs" {
		t.Errorf("configured docs should win, dir = %q", cc.Collections["docs"].Dir)
	}
}

func TestWriteContentTypesValidatesAndGenerates(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Resolve(root)
	cfg.Content = map[string]any{
		"blog": map[string]any{
			"dir":    "content/blog",
			"schema": map[string]any{"title": "string", "order": "number"},
		},
	}

	dir := filepath.Join(root, "content", "blog")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	good := "---\ntitle: Hello\norder: 2\n---\nbody"
	if err := os.WriteFile(filepath.Join(dir, "hello.md"), []byte(good), 0644); err != nil {
		t.Fatal(err)
	}
	bad := "---\norder: not-a-number\n---\nbody"
	if err := os.WriteFile(filepath.Join(dir, "bad.md"), []byte(bad), 0644); err != nil {
		t.Fatal(err)
	}

	b := New(root, cfg)
	res := b.prepareContent()
	if len(res.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", res.Warnings)
	}
	if len(res.Validation) != 1 { // bad.md has a non-numeric order
		t.Errorf("expected 1 validation error, got %d: %v", len(res.Validation), res.Validation)
	}

	data, err := os.ReadFile(filepath.Join(root, ".krate", "types", "content.d.ts"))
	if err != nil {
		t.Fatalf("content.d.ts not written: %v", err)
	}
	out := string(data)
	for _, want := range []string{"export interface BlogEntry", "title?: string", `| "hello"`, `| "bad"`} {
		if !contains(out, want) {
			t.Errorf("content.d.ts missing %q:\n%s", want, out)
		}
	}
}

// TestContentTypesInBuild drives a full BuildAll with a content collection and
// asserts both content.d.ts and the krate-env.d.ts bridge are emitted.
func TestContentTypesInBuild(t *testing.T) {
	root := t.TempDir()
	writePage(t, root, "index.tsx", "export default function App() { return <h1>Hi</h1>; }")
	writeContent(t, root, "content/blog/hello.md", "---\ntitle: Hello\norder: 1\n---\nbody")

	cfg := config.Default()
	cfg.Resolve(root)
	cfg.Minify = false
	cfg.Content = map[string]any{
		"blog": map[string]any{
			"dir":    "content/blog",
			"schema": map[string]any{"title": "string", "order": "number"},
		},
	}
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, ".krate", "types", "content.d.ts")); err != nil {
		t.Errorf("content.d.ts missing: %v", err)
	}
	bridge, err := os.ReadFile(filepath.Join(root, "src", "krate-env.d.ts"))
	if err != nil {
		t.Fatalf("krate-env.d.ts missing: %v", err)
	}
	if !strings.Contains(string(bridge), "ContentTypes") {
		t.Errorf("bridge should reference ContentTypes, got:\n%s", bridge)
	}
}

func writePage(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, "src", "pages", rel)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func writeContent(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
