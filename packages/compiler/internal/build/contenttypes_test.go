package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

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
