package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestBuildPageDynamicImport verifies that `import('./widget.ts')` in a page is
// resolved: the hydration JS references a hashed /chunks/... URL and a real
// esbuild-bundled ES module is emitted (including the chunk's own imports).
func TestBuildPageDynamicImport(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	widgetsDir := filepath.Join(root, "src", "widgets")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(widgetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(widgetsDir, "helper.js"), []byte("export function greeting() { return 'hi'; }"), 0644); err != nil {
		t.Fatal(err)
	}
	widgetSrc := `
		import { greeting } from './helper.js';
		export function hello() { return greeting(); }
	`
	if err := os.WriteFile(filepath.Join(widgetsDir, "widget.ts"), []byte(widgetSrc), 0644); err != nil {
		t.Fatal(err)
	}
	page := `
		export default function Page() {
			createEffect(function () {
				import('../widgets/widget.ts').then(function (m) { console.log(m.hello()); });
			});
			return <div>page</div>;
		}
	`
	if err := os.WriteFile(filepath.Join(pagesDir, "index.tsx"), []byte(page), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = filepath.Join(root, "dist")
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	// The esbuild-bundled chunk must exist in <out>/chunks/.
	matches, err := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "widget-*.js"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected one emitted chunk, got %v (err %v)", matches, err)
	}
	url := "/chunks/" + filepath.Base(matches[0])

	chunkJS, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(chunkJS), "greeting") {
		t.Fatalf("chunk bundle missing bundled import:\n%.800s", chunkJS)
	}

	// Hydration JS must reference the hashed chunk URL, not the source path.
	jsFiles, err := filepath.Glob(filepath.Join(cfg.OutDir, "index.*.js"))
	if err != nil || len(jsFiles) != 1 {
		t.Fatalf("expected one hydration bundle, got %v (err %v)", jsFiles, err)
	}
	js, err := os.ReadFile(jsFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(js), url) {
		t.Fatalf("hydration JS missing chunk URL %q:\n%.1200s", url, js)
	}
	if strings.Contains(string(js), "widget.ts") {
		t.Fatalf("hydration JS still references chunk source:\n%.1200s", js)
	}
}

// TestBuildDynamicImportBareSpecifierUntouched verifies that a dynamic import
// which can't resolve to a project source file does not fail the build or emit
// spurious chunks.
func TestBuildDynamicImportBareSpecifierUntouched(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}
	page := `
		export default function Page() {
			createEffect(function () {
				import('third-party-library').then(function (m) { console.log(m); });
			});
			return <div>page</div>;
		}
	`
	if err := os.WriteFile(filepath.Join(pagesDir, "index.tsx"), []byte(page), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = filepath.Join(root, "dist")
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "*.js"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected no chunks for unresolvable specifier, got %v", matches)
	}
}
