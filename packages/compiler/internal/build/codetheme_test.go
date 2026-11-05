package build

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

const codeThemePage = "export default function Page() {\n\treturn <main><SyntaxHighlight lang=\"ts\">{`const a: number = 1;`}</SyntaxHighlight></main>;\n}\n"

func buildCodeThemeProject(t *testing.T, mutate func(*config.Config)) (css, html string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src", "pages"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "pages", "index.tsx"), []byte(codeThemePage), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if mutate != nil {
		mutate(cfg)
	}
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	h, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(cfg.OutDir, "styles.*.css"))
	if len(files) != 1 {
		t.Fatalf("want one stylesheet, got %v", files)
	}
	c, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(c), string(h)
}

func TestCodeThemeCSSHasLightAndDarkByDefault(t *testing.T) {
	css, html := buildCodeThemeProject(t, nil)
	for _, want := range []string{"--chroma-bg", "prefers-color-scheme", "data-theme"} {
		if !strings.Contains(css, want) {
			t.Errorf("stylesheet missing %q:\n%.600s", want, css)
		}
	}
	// One span per token: a theme pair must not duplicate the markup.
	if n := len(regexp.MustCompile(`<span class="?kr"?>const</span>`).FindAllString(html, -1)); n != 1 {
		t.Errorf("const keyword span appears %d times:\n%s", n, html)
	}
	if strings.Count(html, "const") != 1 || strings.Contains(html, "data-theme") {
		t.Errorf("markup must not carry per-mode copies:\n%s", html)
	}
}

func TestCodeThemeStringAppliesToBothModes(t *testing.T) {
	css, _ := buildCodeThemeProject(t, func(c *config.Config) {
		c.Markdown.SetCodeTheme("monokai")
	})
	if strings.Contains(css, "prefers-color-scheme") && strings.Contains(css, "--chroma-k:") {
		t.Errorf("a single theme must not emit per-mode token variables:\n%.800s", css)
	}
	if !strings.Contains(css, "#272822") {
		t.Errorf("monokai background missing:\n%.800s", css)
	}
}

func TestCodeThemeObjectPicksPerMode(t *testing.T) {
	css, _ := buildCodeThemeProject(t, func(c *config.Config) {
		c.Markdown.SetCodeTheme(map[string]interface{}{"light": "github", "dark": "dracula"})
	})
	for _, want := range []string{"#f7f7f7", "#282a36"} {
		if !strings.Contains(css, want) {
			t.Errorf("stylesheet missing %q:\n%.800s", want, css)
		}
	}
}

func TestUnknownCodeThemeIsADiagnostic(t *testing.T) {
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	css, _ := buildCodeThemeProject(t, func(c *config.Config) {
		c.Markdown.SetCodeTheme("githb-dark")
	})
	w.Close()
	os.Stderr = old
	out, _ := io.ReadAll(r)
	if !regexp.MustCompile(`KR7150`).Match(out) || !strings.Contains(string(out), "githb-dark") {
		t.Errorf("expected a KR7150 warning naming the theme, got:\n%s", out)
	}
	if !strings.Contains(css, "--chroma-bg") {
		t.Errorf("an unknown theme must fall back to the defaults, not drop the stylesheet:\n%.400s", css)
	}
}
