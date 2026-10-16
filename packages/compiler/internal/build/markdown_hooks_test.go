package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestMarkdownPageRouting verifies AfterMarkdownParse runs for markdown pages in
// src/pages. Markdown pages flow through the normal bundler/emitter pipeline (the
// bundler synthesizes an MDX-style TSX bundle), so the hook previously never ran -
// regression for the "AfterMarkdownParse wiring" report item.
func TestMarkdownPageRouting(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}
	mdPage := filepath.Join(pagesDir, "notes.md")
	if err := os.WriteFile(mdPage, []byte("# Notes\n\nHello **world**.\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg.Minify = false

	// Use a JS community plugin advertising an AfterMarkdownParse hook so it
	// runs through the same community-plugin path as the broken-Go test above.
	pluginDir := filepath.Join(root, "plugins", "markdown-wrapsub")
	_ = os.MkdirAll(pluginDir, 0755)
	src := `
export default {
  name: "markdown-wrapsub",
  order: 10,
  hooks: {
    AfterMarkdownParse(ctx, options, krate) { return { html: "<section class=\"amp\">" + ctx.html + "</section>" }; },
  },
};
`
	if err := os.WriteFile(filepath.Join(pluginDir, "index.js"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	cfg.Plugins = []config.PluginConfig{{Name: "markdown-wrapsub", Module: pluginDir}}

	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("build with .md page failed: %v", err)
	}

	out := filepath.Join(cfg.OutDir, "notes", "index.html")
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("markdown page not emitted: %v", err)
	}
	html := string(data)
	if !strings.Contains(html, "<section class=\"amp\"><div class=\"md-content\">") {
		t.Errorf("AfterMarkdownParse plugin output missing; page HTML:\n%s", html)
	}
	if !strings.Contains(html, "<strong>world</strong>") {
		t.Errorf("markdown body not rendered; page HTML:\n%s", html)
	}
}
