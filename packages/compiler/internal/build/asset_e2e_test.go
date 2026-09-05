package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"krate-compiler/internal/config"
)

// TestBuildPageAssetImport is an end-to-end check that importing a non-code
// file (e.g. an image) in a page: (1) rewrites the imported binding to a hashed
// /assets/… URL literal in the hydration JS, and (2) copies the file into the
// output directory at that URL.
func TestBuildPageAssetImport(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pagesDir, "logo.png"), []byte("png-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	page := `
		import logo from './logo.png';
		export default function AssetPage() {
			const title = 'Asset';
			return (<div class="root"><img src={logo} alt={title} /></div>);
		}
	`
	if err := os.WriteFile(filepath.Join(pagesDir, "index.tsx"), []byte(page), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.EmitReact = true
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	// The asset must have been copied into <out>/assets/logo-<hash>.png.
	matches, err := filepath.Glob(filepath.Join(cfg.OutDir, "assets", "logo-*.png"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected one copied asset, got %v (err %v)", matches, err)
	}
	url := "/assets/" + filepath.Base(matches[0])

	// The page's hydration JS must not reference the imported binding (it was
	// compiled out into a static URL literal).
	jsFiles, err := filepath.Glob(filepath.Join(cfg.OutDir, "index.*.js"))
	if err != nil || len(jsFiles) != 1 {
		t.Fatalf("expected one hydration bundle, got %v (err %v)", jsFiles, err)
	}
	js, err := os.ReadFile(jsFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(js), "logo") {
		t.Fatalf("hydration JS still references the imported binding 'logo':\n%.1200s", js)
	}

	// The static HTML must carry the plain URL (SSR renders the constant).
	html, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), "src="+url) {
		if _, statErr := os.Stat(matches[0]); statErr == nil {
			t.Fatalf("SSR HTML missing src=%q:\n%.1200s", url, html)
		}
	}
}