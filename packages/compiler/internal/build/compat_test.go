package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// buildCompatSrc builds a one-page project with compat config applied.
func buildCompatSrc(t *testing.T, src string, cfgMut func(*config.Config)) (string, string) {
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
	if cfgMut != nil {
		cfgMut(cfg)
	}
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	return string(data), cfg.OutDir
}

const compatSrc = `export default function P(){const v=createCSSChoice('a',['a','b']);return <div><button {...v.trigger('a')}>A</button><button {...v.trigger('b')}>B</button><div {...v.panel('a')}>A</div><div {...v.panel('b')}>B</div></div>;}`

const shimMarker = "window.__krateHasShim"

func TestCompatLegacyEmitsShimAndClassCSS(t *testing.T) {
	html, outDir := buildCompatSrc(t, compatSrc, func(c *config.Config) { c.Compat.Mode = "legacy" })
	if !strings.Contains(html, shimMarker) {
		t.Errorf("legacy mode should inject the shim:\n%.1200s", html)
	}
	if !bootScriptHolds(html, shimMarker) {
		t.Errorf("the shim must carry data-krate-boot so soft navigations run it:\n%.1200s", html)
	}
	css := readAllCSS(t, outDir)
	if strings.Contains(css, ":has(") {
		t.Errorf("legacy CSS must not use :has():\n%s", css)
	}
	if !strings.Contains(css, ".krc0.krc0-r-a-on") {
		t.Errorf("legacy CSS missing class selector:\n%s", css)
	}
}
