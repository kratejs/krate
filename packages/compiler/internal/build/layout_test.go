package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestNestedLayoutComposition builds a project with a root _layout (nav shell)
// and a blog/_layout (section shell) and verifies a blog page is wrapped by
// BOTH - root wrapper outside the child layout - not just the nearest child.
// Regression for the "nested layout composition" report area: only the nearest
// layout was applied, so the root nav/footer vanished under a section layout.
func TestNestedLayoutComposition(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}

	write("src/pages/_layout.tsx", `export default function Root(children) {
  return <div class="root-shell"><nav class="root-nav"><a href="/">Home</a></nav><main>{children}</main></div>;
}
`)
	write("src/pages/blog/_layout.tsx", `export default function Blog(children) {
  return <section class="blog-shell"><h1>Blog</h1>{children}</section>;
}
`)
	write("src/pages/blog/index.tsx", `export default function Index() {
  return <p class="post-body">welcome to the blog</p>;
}
`)

	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(cfg.OutDir, "blog", "index.html"))
	if err != nil {
		t.Fatalf("blog page not emitted: %v", err)
	}
	html := string(data)
	for _, want := range []string{`class="root-shell"`, `class="root-nav"`, `class="blog-shell"`, `<h1>Blog</h1>`, `welcome to the blog`} {
		if !strings.Contains(html, want) {
			t.Errorf("emitted HTML missing %q:\n%s", want, html)
		}
	}
	// Nesting order: root nav before section shell before page body proves the
	// root layout wraps the child layout wraps the page.
	rootNav := strings.Index(html, `class="root-nav"`)
	blogShell := strings.Index(html, `class="blog-shell"`)
	body := strings.Index(html, `welcome to the blog`)
	if rootNav < 0 || blogShell < 0 || body < 0 {
		t.Fatalf("missing markers (nav %d, blog %d, body %d):\n%s", rootNav, blogShell, body, html)
	}
	if !(rootNav < blogShell && blogShell < body) {
		t.Errorf("expected root nav < blog shell < page body, got nav=%d blog=%d body=%d:\n%s", rootNav, blogShell, body, html)
	}
}
