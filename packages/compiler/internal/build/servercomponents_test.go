package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// scProject writes files (paths relative to the project root) and returns a
// builder for it.
func scProject(t *testing.T, files map[string]string) (*Builder, string) {
	t.Helper()
	root := t.TempDir()
	for rel, src := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	return New(root, cfg), root
}

func readManifest(t *testing.T, root string) Manifest {
	t.Helper()
	data, err := os.ReadFile(ManifestPath(filepath.Join(root, "dist")))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func manifestPage(m Manifest, route string) (PageMeta, bool) {
	for _, p := range m.Pages {
		if p.Route == route {
			return p, true
		}
	}
	return PageMeta{}, false
}

func TestAsyncPageIsServerRendered(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/pages/post.tsx": `export default async function Post({ params }: any) {
  const { slug } = await params;
  const d = await fetch("http://example.invalid/" + slug).then((r: any) => r.json());
  return <article><h1>{d.title}</h1></article>;
}`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	m := readManifest(t, root)
	p, ok := manifestPage(m, "/post")
	if !ok {
		t.Fatalf("no /post page in manifest: %+v", m.Pages)
	}
	if p.Mode != RenderSSR || !p.ServerComponents {
		t.Fatalf("async page: mode=%v serverComponents=%v, want ssr + true", p.Mode, p.ServerComponents)
	}
	shell, err := os.ReadFile(filepath.Join(root, "dist", "post", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(shell), "<!--suspense:page--><!--/suspense:page-->") {
		t.Errorf("async page shell should carry an empty page region, got:\n%s", shell)
	}
	if strings.Contains(string(shell), "<script src=/post/index.") {
		t.Errorf("async page must not ship hydration code:\n%s", shell)
	}
	sm, err := os.ReadFile(ServerManifestPath(filepath.Join(root, "dist")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sm), `"serverComponents": true`) {
		t.Errorf("server manifest should flag the page:\n%s", sm)
	}
}
