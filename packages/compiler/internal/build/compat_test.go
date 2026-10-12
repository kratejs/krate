package build

import (
	"os"
	"path/filepath"
	"regexp"
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

func TestCompatModernStaysZeroJS(t *testing.T) {
	html, outDir := buildCompatSrc(t, compatSrc, nil)
	if strings.Contains(html, shimMarker) {
		t.Errorf("modern mode must stay zero-JS:\n%.1200s", html)
	}
	css := readAllCSS(t, outDir)
	if !strings.Contains(css, ":has(") {
		t.Errorf("modern CSS should use :has():\n%s", css)
	}
	if strings.Contains(css, "@supports selector(:has(*)){") {
		t.Errorf("modern CSS should not wrap in @supports selector():\n%s", css)
	}
}

func TestCompatLegacyEmitsShimAndClassCSS(t *testing.T) {
	html, outDir := buildCompatSrc(t, compatSrc, func(c *config.Config) { c.Compat.Mode = "legacy" })
	if !strings.Contains(html, shimMarker) {
		t.Errorf("legacy mode should inject the shim:\n%.1200s", html)
	}
	css := readAllCSS(t, outDir)
	if strings.Contains(css, ":has(") {
		t.Errorf("legacy CSS must not use :has():\n%s", css)
	}
	if !strings.Contains(css, ".krc0.krc0-r-a-on") {
		t.Errorf("legacy CSS missing class selector:\n%s", css)
	}
}

func TestCompatHybridGatesShim(t *testing.T) {
	html, outDir := buildCompatSrc(t, compatSrc, func(c *config.Config) { c.Compat.Mode = "hybrid" })
	if !strings.Contains(html, "CSS.supports") {
		t.Errorf("hybrid should feature-gate the shim:\n%.1200s", html)
	}
	css := readAllCSS(t, outDir)
	if !strings.Contains(css, "@supports selector(:has(*)){") {
		t.Errorf("hybrid CSS missing the modern @supports block:\n%s", css)
	}
	if !strings.Contains(css, "@supports not selector(:has(*)){") {
		t.Errorf("hybrid CSS missing the legacy @supports block:\n%s", css)
	}
}

func TestCompatUserAgentEmitsLegacySibling(t *testing.T) {
	html, outDir := buildCompatSrc(t, compatSrc, func(c *config.Config) {
		c.Compat.Mode = "hybrid"
		c.Compat.UserAgent = true
	})
	// Default document stays the full hybrid: gated shim, works on any host.
	if !strings.Contains(html, "CSS.supports") || !strings.Contains(html, shimMarker) {
		t.Errorf("default hybrid+UA document should carry the gated shim:\n%.1200s", html)
	}
	legacy, err := os.ReadFile(filepath.Join(outDir, "index.legacy.html"))
	if err != nil {
		t.Fatalf("legacy sibling not written: %v", err)
	}
	if !strings.Contains(string(legacy), shimMarker) || strings.Contains(string(legacy), "CSS.supports") {
		t.Errorf("legacy sibling should include only the unconditional shim:\n%.1200s", legacy)
	}
	modern, err := os.ReadFile(filepath.Join(outDir, "index.modern.html"))
	if err != nil {
		t.Fatalf("modern sibling not written: %v", err)
	}
	if strings.Contains(string(modern), shimMarker) {
		t.Errorf("modern sibling should not carry the shim:\n%.1200s", modern)
	}
}

// TestCompatUserAgentPerVariantCSS verifies the UA siblings link variant-only
// stylesheets (modern-only / legacy-only) while the default document stays the
// full hybrid, so each visitor downloads only its variant.
func TestCompatUserAgentPerVariantCSS(t *testing.T) {
	html, outDir := buildCompatSrc(t, compatSrc, func(c *config.Config) {
		c.Compat.Mode = "hybrid"
		c.Compat.UserAgent = true
	})
	legacy, err := os.ReadFile(filepath.Join(outDir, "index.legacy.html"))
	if err != nil {
		t.Fatalf("legacy sibling not written: %v", err)
	}
	modern, err := os.ReadFile(filepath.Join(outDir, "index.modern.html"))
	if err != nil {
		t.Fatalf("modern sibling not written: %v", err)
	}

	hybridFile := regexp.MustCompile(`styles\.[0-9a-z]+\.css`).FindString(html)
	modernFile := regexp.MustCompile(`styles\.[0-9a-z]+\.modern\.css`).FindString(string(modern))
	legacyFile := regexp.MustCompile(`styles\.[0-9a-z]+\.legacy\.css`).FindString(string(legacy))
	if hybridFile == "" || modernFile == "" || legacyFile == "" {
		t.Fatalf("missing stylesheet links: hybrid=%q modern=%q legacy=%q", hybridFile, modernFile, legacyFile)
	}
	if hybridFile == modernFile || modernFile == legacyFile || hybridFile == legacyFile {
		t.Errorf("variants should be distinct files: %q %q %q", hybridFile, modernFile, legacyFile)
	}

	hybridCSS := readCSSFile(t, outDir, hybridFile)
	modernCSS := readCSSFile(t, outDir, modernFile)
	legacyCSS := readCSSFile(t, outDir, legacyFile)

	if !strings.Contains(hybridCSS, "@supports selector(:has(*)){") || !strings.Contains(hybridCSS, "@supports not selector(:has(*)){") {
		t.Errorf("default/hybrid stylesheet should carry both blocks:\n%s", hybridCSS)
	}
	if !strings.Contains(modernCSS, ":has(") || strings.Contains(modernCSS, "@supports not selector(:has(*))") || strings.Contains(modernCSS, ".krc0.krc0-r-a-on") {
		t.Errorf("modern stylesheet must be modern-only:\n%s", modernCSS)
	}
	if strings.Contains(legacyCSS, ":has(") || strings.Contains(legacyCSS, "@supports selector(:has(*))") {
		t.Errorf("legacy stylesheet must not carry the modern variant:\n%s", legacyCSS)
	}
	if !strings.Contains(legacyCSS, ".krc0.krc0-r-a-on") {
		t.Errorf("legacy stylesheet missing class selector:\n%s", legacyCSS)
	}
	// The sentinels must not leak into the split variants.
	if strings.Contains(modernCSS, "krate:modern") || strings.Contains(modernCSS, "krate:legacy") {
		t.Errorf("modern stylesheet leaked sentinels:\n%s", modernCSS)
	}
	if strings.Contains(legacyCSS, "krate:modern") || strings.Contains(legacyCSS, "krate:legacy") {
		t.Errorf("legacy stylesheet leaked sentinels:\n%s", legacyCSS)
	}
}

// TestCompatNoJSFallback verifies legacy/hybrid emit a <noscript> reveal-all so
// engines lacking :has() still show content when scripting is disabled. Hybrid
// gates it on :has() (so a modern browser with scripting off keeps native
// behavior); legacy is unconditional (its stylesheet never uses :has()).
func TestCompatNoJSFallback(t *testing.T) {
	hybrid, _ := buildCompatSrc(t, compatSrc, func(c *config.Config) { c.Compat.Mode = "hybrid" })
	if !strings.Contains(hybrid, "<noscript><style>@supports not selector(:has(*))") {
		t.Errorf("hybrid noscript should be :has()-gated:\n%.1200s", hybrid)
	}

	legacy, _ := buildCompatSrc(t, compatSrc, func(c *config.Config) { c.Compat.Mode = "legacy" })
	if !strings.Contains(legacy, "<noscript><style>") {
		t.Errorf("legacy missing noscript reveal:\n%.1200s", legacy)
	}
	if strings.Contains(legacy, "<noscript><style>@supports") {
		t.Errorf("legacy noscript must be unconditional:\n%.1200s", legacy)
	}
	if !strings.Contains(legacy, "display:contents") {
		t.Errorf("legacy noscript missing the reveal rule:\n%.1200s", legacy)
	}

	// modern mode keeps zero-JS inline behavior; no noscript is needed.
	modern, _ := buildCompatSrc(t, compatSrc, nil)
	if strings.Contains(modern, "<noscript>") {
		t.Errorf("modern mode should not add a noscript reveal:\n%.1200s", modern)
	}
}

// TestCompatSiblingsSurviveCacheReplay guards the disk-cache interaction: a
// second build wipes dist and replays cached pages, so the UA siblings and
// variant stylesheets must be re-emitted from the replayed PageResult.
func TestCompatSiblingsSurviveCacheReplay(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pagesDir, "index.tsx"), []byte(compatSrc), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Compat.Mode = "hybrid"
	cfg.Compat.UserAgent = true

	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("first build: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.OutDir, "index.legacy.html")); err != nil {
		t.Fatalf("first build missing legacy sibling: %v", err)
	}

	// Second build wipes dist and replays the disk cache.
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("second build: %v", err)
	}
	for _, name := range []string{"index.legacy.html", "index.modern.html"} {
		if _, err := os.Stat(filepath.Join(cfg.OutDir, name)); err != nil {
			t.Errorf("cache replay missing %s: %v", name, err)
		}
	}
	if m, _ := filepath.Glob(filepath.Join(cfg.OutDir, "styles.*.legacy.css")); len(m) == 0 {
		t.Errorf("cache replay missing legacy stylesheet")
	}
}

func readCSSFile(t *testing.T, outDir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outDir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}
