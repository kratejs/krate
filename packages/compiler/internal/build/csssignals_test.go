package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// buildPageSrc writes a one-page project and builds it.
func buildPageSrc(t *testing.T, src string) (string, string, error) {
	t.Helper()
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pagesDir, "index.tsx"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = filepath.Join(root, "dist")
	err := New(root, cfg).BuildAll()
	html := ""
	if data, rerr := os.ReadFile(filepath.Join(cfg.OutDir, "index.html")); rerr == nil {
		html = string(data)
	}
	return html, cfg.OutDir, err
}

// TestBuildCSSSignalsZeroJS verifies the createCSSChoice transform end-to-end:
// radios, labels, and panels are emitted with no client hydration JS.
func TestBuildCSSSignalsZeroJS(t *testing.T) {
	src := `export default function Tabs() {
	const [tab, setTab] = createCSSChoice('overview');
	return (
		<div class="tabs">
			<button class="tab" onClick={() => setTab('overview')}>Overview</button>
			<button class="tab" onClick={() => setTab('features')}>Features</button>
			<div class="panel" showIf={tab() === 'overview'}>Overview content</div>
			<div class="panel" showIf={tab() === 'features'}>Features content</div>
		</div>
	);
}`
	html, outDir, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if strings.Contains(html, "createCSSChoice") || strings.Contains(html, "showIf") {
		t.Errorf("compiler sugar leaked:\n%.900s", html)
	}
	if !strings.Contains(html, "type=radio") && !strings.Contains(html, `type="radio"`) {
		t.Errorf("expected radio controllers:\n%.900s", html)
	}
	if !strings.Contains(html, "<label") || !strings.Contains(html, "for=") {
		t.Errorf("expected label triggers:\n%.900s", html)
	}
	if hasHydrationScript(html) {
		t.Errorf("CSS signal page should be zero-JS:\n%.900s", html)
	}
	css := readAllCSS(t, outDir)
	if !strings.Contains(css, ".krc0-r-overview:checked") {
		t.Errorf("expected compact radio rule:\n%s", css)
	}
	// Regression: base rules must not be joined to the next rule.
	if strings.Contains(css, "display:none},") {
		t.Errorf("dangling comma joining rules:\n%s", css)
	}
	if !strings.Contains(css, "display:contents") {
		t.Errorf("panels should toggle via display:contents wrapper:\n%s", css)
	}
	// Byte-savings check: no long legacy class prefix (the --krate-css-* theme
	// custom properties are intentional and excluded).
	if regexp.MustCompile(`\.krate-css-`).MatchString(css) {
		t.Errorf("legacy long class prefix present:\n%s", css)
	}
}

// hasHydrationScript reports whether the page references a page hydration script.
func hasHydrationScript(html string) bool {
	return regexp.MustCompile(`<script src="/index\.[A-Za-z0-9]+\.js"`).MatchString(html)
}

// readAllCSS concatenates every emitted stylesheet.
func readAllCSS(t *testing.T, outDir string) string {
	t.Helper()
	var sb strings.Builder
	_ = filepath.Walk(outDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".css") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err == nil {
			sb.Write(data)
		}
		return nil
	})
	return sb.String()
}
