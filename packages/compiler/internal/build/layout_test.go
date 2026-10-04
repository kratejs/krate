package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestFindLayoutGeneratedPageNestedFallback verifies that a page generated under
// .krate/gen resolves its nearest _layout within the gen tree, and that a gen
// page with no gen-tree layout stays unwrapped (returns "") rather than being
// forced into the app root layout — generated pages bring their own layout.
func TestFindLayoutGeneratedPageNestedFallback(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	genDir := filepath.Join(root, ".krate", "gen")
	if err := os.MkdirAll(filepath.Join(genDir, "docs", "guides"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}

	rootLayout := filepath.Join(pagesDir, "_layout.tsx")
	if err := os.WriteFile(rootLayout, []byte("root"), 0644); err != nil {
		t.Fatal(err)
	}
	nestedLayout := filepath.Join(genDir, "docs", "_layout.tsx")
	if err := os.WriteFile(nestedLayout, []byte("docs"), 0644); err != nil {
		t.Fatal(err)
	}

	deepPage := filepath.Join(genDir, "docs", "guides", "deep.tsx")
	if got := findLayout(deepPage, pagesDir); got != nestedLayout {
		t.Errorf("findLayout(deep) = %q, want nested %q", got, nestedLayout)
	}

	shallowPage := filepath.Join(genDir, "docs", "intro.tsx")
	if got := findLayout(shallowPage, pagesDir); got != nestedLayout {
		t.Errorf("findLayout(shallow) = %q, want nested %q", got, nestedLayout)
	}

	// A gen page with no gen-local layout must NOT fall back to pages/_layout.tsx
	// — it stays unwrapped (the app root layout belongs to regular pages only).
	orphan := filepath.Join(root, ".krate", "gen", "misc", "page.tsx")
	if err := os.MkdirAll(filepath.Dir(orphan), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphan, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := findLayout(orphan, pagesDir); got != "" {
		t.Errorf("findLayout(orphan) = %q, want empty (no app-root fallback for gen pages)", got)
	}
}

// TestFindLayoutRegularPage verifies normal pages under pagesDir still resolve
// their nearest layout and the pagesDir root layout.
func TestFindLayoutRegularPage(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "pages")
	blog := filepath.Join(pagesDir, "blog")
	if err := os.MkdirAll(blog, 0755); err != nil {
		t.Fatal(err)
	}
	rootLayout := filepath.Join(pagesDir, "_layout.tsx")
	if err := os.WriteFile(rootLayout, []byte("root"), 0644); err != nil {
		t.Fatal(err)
	}
	blogLayout := filepath.Join(blog, "_layout.tsx")
	if err := os.WriteFile(blogLayout, []byte("blog"), 0644); err != nil {
		t.Fatal(err)
	}

	if got := findLayout(filepath.Join(blog, "post.tsx"), pagesDir); got != blogLayout {
		t.Errorf("nested regular layout = %q, want %q", got, blogLayout)
	}
	if got := findLayout(filepath.Join(pagesDir, "home.tsx"), pagesDir); got != rootLayout {
		t.Errorf("root regular layout = %q, want %q", got, rootLayout)
	}
}

// TestNestedLayoutComposition builds a project with a root _layout (nav shell)
// and a blog/_layout (section shell) and verifies a blog page is wrapped by
// BOTH — root wrapper outside the child layout — not just the nearest child.
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

// TestFindLayoutStack verifies the innermost-first ordering used by page
// builds, and that plugin-generated (.krate/gen) pages never pick up the app
// root layout.
func TestFindLayoutStack(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	if err := os.MkdirAll(filepath.Join(pagesDir, "blog"), 0755); err != nil {
		t.Fatal(err)
	}
	rootLayout := filepath.Join(pagesDir, "_layout.tsx")
	_ = os.WriteFile(rootLayout, []byte("root"), 0644)
	blogLayout := filepath.Join(pagesDir, "blog", "_layout.tsx")
	_ = os.WriteFile(blogLayout, []byte("blog"), 0644)

	stack := findLayoutStack(filepath.Join(pagesDir, "blog", "post.tsx"), pagesDir)
	if len(stack) != 2 || stack[0] != blogLayout || stack[1] != rootLayout {
		t.Errorf("findLayoutStack = %v, want [%s %s]", stack, blogLayout, rootLayout)
	}

	// A page directly under pagesDir only gets the root layout.
	flat := findLayoutStack(filepath.Join(pagesDir, "home.tsx"), pagesDir)
	if len(flat) != 1 || flat[0] != rootLayout {
		t.Errorf("findLayoutStack(flat) = %v, want [%s]", flat, rootLayout)
	}
}

// TestPageToOutputNestedIndex verifies a nested index page maps to its parent
// route (blog/index.tsx → /blog) so /blog/ serves it instead of exposing the
// odd /blog/index URL (or a directory listing at /blog/).
func TestPageToOutputNestedIndex(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	blog := filepath.Join(pagesDir, "blog")
	if err := os.MkdirAll(blog, 0755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		page string
		want string
	}{
		{filepath.Join(pagesDir, "index.tsx"), "."},
		{filepath.Join(pagesDir, "about.tsx"), "about"},
		{filepath.Join(blog, "index.tsx"), "blog"},
		{filepath.Join(blog, "post.tsx"), "blog/post"},
	}
	for _, tc := range cases {
		if got := pageToOutput(tc.page, pagesDir); got != tc.want {
			t.Errorf("pageToOutput(%q) = %q, want %q", tc.page, got, tc.want)
		}
	}
}
