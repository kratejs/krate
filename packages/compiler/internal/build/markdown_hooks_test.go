package build

import (
	"os"
	"path/filepath"
	"regexp"
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

// TestMarkdownPageHonoursMarkdownConfig covers page-level .md files using the
// project's markdown config instead of the defaults.
func TestMarkdownPageHonoursMarkdownConfig(t *testing.T) {
	const body = "## Heading\n\nInline $a+b$ math.\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n~~gone~~\n"
	build := func(t *testing.T, mutate func(*config.Config)) string {
		t.Helper()
		root := t.TempDir()
		writeFileRel(t, root, "src/pages/notes.md", body)
		cfg, err := config.Load(root)
		if err != nil {
			t.Fatalf("config.Load: %v", err)
		}
		cfg.Minify = false
		mutate(cfg)
		if err := New(root, cfg).BuildAll(); err != nil {
			t.Fatalf("BuildAll: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(cfg.OutDir, "notes", "index.html"))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	on := build(t, func(c *config.Config) { c.Markdown.Math = true })
	if !strings.Contains(on, `id="heading"`) || !strings.Contains(on, "<table") || !strings.Contains(on, "<del>") {
		t.Errorf("defaults not applied:\n%s", on)
	}
	if !strings.Contains(on, "krate-math") {
		t.Errorf("math: true should wrap $a+b$:\n%s", on)
	}

	off := build(t, func(c *config.Config) {
		c.Markdown.Math = false
		c.Markdown.GFM = false
		c.Markdown.HeadingAnchors = false
	})
	if strings.Contains(off, `id="heading"`) {
		t.Errorf("headingAnchors: false should drop heading ids:\n%s", off)
	}
	if strings.Contains(off, "<table") || strings.Contains(off, "<del>") {
		t.Errorf("gfm: false should not render tables or strikethrough:\n%s", off)
	}
	if strings.Contains(off, "krate-math") || !strings.Contains(off, "$a+b$") {
		t.Errorf("math: false should leave $a+b$ alone:\n%s", off)
	}
}

// A relative image in a page-level .md file is copied and linked at its hashed
// URL, under the site base path exactly once.
func TestMarkdownPageRelativeImages(t *testing.T) {
	for _, base := range []string{"", "/docs"} {
		root := t.TempDir()
		writeFileRel(t, root, "src/pages/notes.md", "# N\n\n![a cat](./cat.png)\n")
		writeFileRel(t, root, "src/pages/cat.png", "png-bytes")
		cfg, err := config.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Minify = false
		cfg.BasePath = base
		if err := New(root, cfg).BuildAll(); err != nil {
			t.Fatalf("BuildAll (base %q): %v", base, err)
		}
		data, err := os.ReadFile(filepath.Join(cfg.OutDir, "notes", "index.html"))
		if err != nil {
			t.Fatal(err)
		}
		m := regexp.MustCompile(`src="?(` + regexp.QuoteMeta(base) + `/assets/cat-[0-9a-f]+\.png)`).FindStringSubmatch(string(data))
		if m == nil {
			t.Fatalf("base %q: image not rewritten:\n%s", base, data)
		}
		file := filepath.Join(cfg.OutDir, filepath.FromSlash(strings.TrimPrefix(m[1], base)))
		if _, err := os.Stat(file); err != nil {
			t.Errorf("base %q: image not copied: %v", base, err)
		}
		if strings.Contains(string(data), "/docs/docs/") {
			t.Errorf("base path applied twice:\n%s", data)
		}
	}
}
