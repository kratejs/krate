package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"krate-compiler/internal/config"
)

// TestBuildErrorPages verifies src/pages/404.tsx and src/pages/500.tsx are
// emitted at the output root as 404.html/500.html: the friendly-fallback
// documents the dev server and client router serve on render failures.
func TestBuildErrorPages(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		marker := strings.TrimSuffix(name, ".tsx")
		src := "<div class='page'>" + marker + "-marker</div>"
		page := "export default function P() { return (" + src + "); }"
		if body != "" {
			page = body
		}
		if err := os.WriteFile(filepath.Join(pagesDir, name), []byte(page), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("404.tsx", "")
	write("500.tsx", "")

	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = filepath.Join(root, "dist")
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	for _, name := range []string{"404", "500"} {
		htmlPath := filepath.Join(cfg.OutDir, name+".html")
		data, err := os.ReadFile(htmlPath)
		if err != nil {
			t.Fatalf("%s.html not emitted: %v", name, err)
		}
		if !strings.Contains(string(data), name+"-marker") {
			t.Fatalf("%s.html missing page marker:\n%.600s", name, data)
		}
		// Error pages must not also produce a route directory.
		if _, err := os.Stat(filepath.Join(cfg.OutDir, name)); err == nil {
			t.Fatalf("unexpected route dir for error page %s", name)
		}
	}

	// Manifest must not list error pages.
	if data, err := os.ReadFile(filepath.Join(cfg.OutDir, "manifest.json")); err == nil {
		if strings.Contains(string(data), "\"404\"") || strings.Contains(string(data), "\"500\"") {
			t.Fatalf("error pages leaked into manifest:\n%.800s", data)
		}
	}
}