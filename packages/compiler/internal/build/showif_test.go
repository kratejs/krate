package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestBuildShowIfStaticAndReactive verifies the `showIf`/`visibleIf` sugar
// end-to-end: a statically-true test bakes the element, a statically-false test
// elides it, the attribute never leaks into HTML, and a signal-driven test
// produces hydration bindings that toggle visibility.
func TestBuildShowIfStaticAndReactive(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}

	src := `export default function Page() {
	const [show, setShow] = createSignal(false);
	return (
		<div>
			<p class="always" showIf={1 > 0}>always</p>
			<p class="never" showIf={1 > 2}>never</p>
			<p class="alias" visibleIf={1 > 0}>alias</p>
			<p class="toggle" showIf={show()}>toggle</p>
			<button onClick={() => setShow(!show())}>go</button>
		</div>
	);
}`
	if err := os.WriteFile(filepath.Join(pagesDir, "index.tsx"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = filepath.Join(root, "dist")
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	htmlPath := filepath.Join(cfg.OutDir, "index.html")
	data, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("index.html not emitted: %v", err)
	}
	html := string(data)

	if strings.Contains(html, "showIf") || strings.Contains(html, "visibleIf") {
		t.Errorf("showIf/visibleIf must not leak into HTML")
	}
	if !strings.Contains(html, "class=always") && !strings.Contains(html, `class="always"`) {
		t.Errorf("expected statically-true showIf element to render:\n%.600s", html)
	}
	if strings.Contains(html, "never") {
		t.Errorf("expected statically-false showIf element to be elided:\n%.600s", html)
	}
	if !strings.Contains(html, "class=alias") && !strings.Contains(html, `class="alias"`) {
		t.Errorf("expected visibleIf alias element to render:\n%.600s", html)
	}
	// The reactive element must be present (render-both ConditionalSlot) and
	// toggled by hydration rather than resolved at build time.
	if !strings.Contains(html, "toggle") {
		t.Errorf("expected reactive showIf element to render for hydration:\n%.600s", html)
	}
	if !strings.Contains(html, "data-krate-cond-w") {
		t.Errorf("expected reactive showIf to emit a conditional wrapper:\n%.600s", html)
	}

	// Hydration code must reference the reactive conditional.
	if !hasHydrationBinding(t, cfg.OutDir) {
		t.Errorf("expected hydration JS for the reactive showIf conditional")
	}
}

// hasHydrationBinding reports whether any emitted page JS references a
// conditional binding (kbindCond / data-kcond).
func hasHydrationBinding(t *testing.T, outDir string) bool {
	t.Helper()
	found := false
	_ = filepath.Walk(outDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".js") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if regexp.MustCompile(`kbindCond|kcond|data-kcond`).Match(data) {
			found = true
		}
		return nil
	})
	return found
}
