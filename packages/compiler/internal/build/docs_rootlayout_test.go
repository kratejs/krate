package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// docsRootLayoutBuild builds a small docs site next to an app root layout and
// returns the built /docs/ and /docs/deep/page/ HTML.
func docsRootLayoutBuild(t *testing.T, extra map[string]any) (index, deep string, root string) {
	t.Helper()
	root = t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("src/pages/index.tsx", `export default function Home() { return <main>Home</main>; }`)
	write("src/pages/_layout.tsx", `export default function Layout(props: any) {
  return (
    <div class="app-shell">
      <Head>
        <html lang="fr" class="app-root" /><body class="app-body" />
        <script data-krate-once="theme-prepaint">{"document.documentElement.setAttribute('data-theme','x')"}</script>
      </Head>
      {props.children}
    </div>
  );
}
`)
	write("components/theme/layout.tsx", `export default function Theme(props: any) {
  return <main class="theme-shell"><h1>{props.pageTitle}</h1>{props.children}</main>;
}
`)
	write("components/card.tsx", `export default function Card() { return <aside class="card-widget">card</aside>; }`)
	write("content/docs/index.mdx", "---\ntitle: Welcome\n---\n\nimport Card from \"../../components/card\"\n\n<Card />\n")
	write("content/docs/deep/page.mdx", "---\ntitle: Deep\n---\n\nimport Card from \"../../../components/card\"\n\n<Card />\n")

	opts := map[string]any{
		"contentDir": "content/docs",
		"search":     map[string]any{"enabled": false},
		"theme":      map[string]any{"name": "t", "module": "./components/theme/layout.tsx"},
	}
	for k, v := range extra {
		opts[k] = v
	}
	cfg := config.Default()
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.Tailwind.Enabled = false
	cfg.Minify = false
	cfg.Plugins = []config.PluginConfig{{Name: "docs", Options: opts}}
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	read := func(rel string) string {
		data, err := os.ReadFile(filepath.Join(root, "dist", filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	return read("docs/index.html"), read("docs/deep/page/index.html"), root
}

func TestDocsPagesDefaultSkipRootLayout(t *testing.T) {
	index, _, _ := docsRootLayoutBuild(t, nil)
	if strings.Contains(index, "app-shell") || strings.Contains(index, "app-root") {
		t.Errorf("docs pages must not use the root layout by default:\n%s", index)
	}
	if !strings.Contains(index, "theme-shell") || !strings.Contains(index, "card-widget") {
		t.Errorf("theme shell or MDX component missing:\n%s", index)
	}
}

func TestDocsRootLayoutWrapsGeneratedPages(t *testing.T) {
	index, deep, _ := docsRootLayoutBuild(t, map[string]any{"rootLayout": true})
	for name, html := range map[string]string{"index": index, "deep": deep} {
		for _, want := range []string{"app-shell", "theme-shell", "card-widget", `lang=fr`, "app-root", "app-body"} {
			if !strings.Contains(html, want) && !strings.Contains(html, strings.Replace(want, "lang=fr", `lang="fr"`, 1)) {
				t.Errorf("%s: missing %q:\n%s", name, want, html)
			}
		}
		if strings.Index(html, "app-shell") > strings.Index(html, "theme-shell") {
			t.Errorf("%s: the root layout must wrap the theme layout", name)
		}
		if n := len(regexp.MustCompile(`(?i)<html[\s>]`).FindAllString(html, -1)); n != 1 {
			t.Errorf("%s: %d <html> elements, want 1", name, n)
		}
		if n := len(regexp.MustCompile(`(?i)<body[\s>]`).FindAllString(html, -1)); n != 1 {
			t.Errorf("%s: %d <body> elements, want 1", name, n)
		}
	}
}

func TestDocsThemeScriptOnceWithRootLayoutScript(t *testing.T) {
	index, _, _ := docsRootLayoutBuild(t, map[string]any{"rootLayout": true, "defaultTheme": "dark"})
	if n := strings.Count(index, "setAttribute('data-theme'"); n != 1 {
		t.Errorf("pre-paint script emitted %d times, want 1:\n%s", n, index)
	}
	if !strings.Contains(index, "s='dark'") {
		t.Errorf("default dark not in the script:\n%s", index)
	}
	off, _, _ := docsRootLayoutBuild(t, map[string]any{"themeScript": false})
	if strings.Contains(off, "setAttribute('data-theme'") {
		t.Errorf("themeScript false still emitted the script:\n%s", off)
	}
}

func TestHeadDedupeKeepsOnePrePaintScript(t *testing.T) {
	t.Parallel()
	head := `<script data-krate-once="theme-prepaint">a()</script><script data-krate-once='theme-prepaint'>b()</script><script>c()</script><script>c()</script>`
	got := dedupeHead(head)
	if strings.Count(got, "theme-prepaint") != 1 || strings.Count(got, "c()") != 2 {
		t.Errorf("dedupeHead = %q", got)
	}
}
