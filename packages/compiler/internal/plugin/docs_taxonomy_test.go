package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/docs"
	"github.com/kratejs/krate/packages/compiler/internal/markdown"
)

func taxonomyTestPages() []docs.Page {
	return []docs.Page{
		{Title: "Getting Started", Path: "getting-started", URL: "/docs/getting-started/", Tags: []string{"Alpha Tag"}, Categories: []string{"Guides"}},
		{Title: "Configuration", Path: "configuration", URL: "/docs/configuration/", Tags: []string{"Alpha Tag"}},
	}
}

func TestGenerateTaxonomyPagesUsesDocsShell(t *testing.T) {
	root := t.TempDir()
	genRoot := filepath.Join(root, ".krate", "gen")
	ctx := &BuildHookCtx{Root: root}
	var generated []GeneratedPage
	ctx.GeneratedPages = &generated

	p := &DocsPlugin{}
	opts := &DocsPluginOptions{
		Title:  "Krate Docs",
		Search: &DocsSearchOptions{Placeholder: "Search the docs", Hotkey: "mod+k"},
	}
	theme := &resolvedDocsTheme{spec: "@krate/base-docs-theme/layout"}
	themeOptions := json.RawMessage(`{"themeStorageKey":"krate-theme"}`)
	sections := []docs.SidebarItem{{Title: "Guides", Children: []docs.SidebarItem{
		{Title: "Guides", URL: "/docs/guides/"},
		{Title: "Advanced", URL: "/docs/guides/advanced/"},
	}}}

	if err := p.generateTaxonomyPages(ctx, &config.Config{Markdown: markdown.Config{}}, genRoot, theme, opts, "/docs", true, themeOptions, taxonomyTestPages(), sections); err != nil {
		t.Fatalf("generateTaxonomyPages: %v", err)
	}
	if len(generated) != 2 {
		t.Fatalf("generated pages = %d, want 2", len(generated))
	}

	tagSrc, err := os.ReadFile(filepath.Join(genRoot, "docs", "tags", "alpha-tag.tsx"))
	if err != nil {
		t.Fatalf("reading tag page: %v", err)
	}
	tag := string(tagSrc)
	for _, want := range []string{
		`import DocsLayout from "@krate/base-docs-theme/layout"`,
		`search: {"hotkey":"mod+k","placeholder":"Search the docs"}`,
		`options: {"themeStorageKey":"krate-theme"}`,
		`currentPath: "tags/alpha-tag"`,
		`routeBase: "/docs"`,
		`<script src="/docs/search/search.js" defer={true}></script>`,
		`localStorage.getItem`,
		`data-theme`,
		`Documentation pages tagged Alpha Tag.`,
		`<li><a href="/docs/getting-started/">Getting Started</a></li>`,
	} {
		if !strings.Contains(tag, want) {
			t.Errorf("tag page missing %q:\n%s", want, tag)
		}
	}
	if !strings.Contains(tag, `"indexURL":"/docs/guides/"`) {
		t.Errorf("taxonomy sidebar was not enriched:\n%s", tag)
	}

	catSrc, err := os.ReadFile(filepath.Join(genRoot, "docs", "categories", "guides.tsx"))
	if err != nil {
		t.Fatalf("reading category page: %v", err)
	}
	cat := string(catSrc)
	for _, want := range []string{
		`currentPath: "categories/guides"`,
		`Category: Guides`,
		`Documentation pages in the Guides category.`,
	} {
		if !strings.Contains(cat, want) {
			t.Errorf("category page missing %q:\n%s", want, cat)
		}
	}
}

func TestTaxonomyHonorsRouteBase(t *testing.T) {
	root := t.TempDir()
	genRoot := filepath.Join(root, ".krate", "gen")
	var generated []GeneratedPage
	ctx := &BuildHookCtx{Root: root, GeneratedPages: &generated}

	pages := []docs.Page{{Title: "Intro", Path: "intro", Tags: []string{"Docs"}}}
	opts := &DocsPluginOptions{Title: "Docs"}
	if err := (&DocsPlugin{}).generateTaxonomyPages(ctx, &config.Config{Markdown: markdown.Config{}}, genRoot, nil, opts, "/reference", false, nil, pages, nil); err != nil {
		t.Fatalf("generateTaxonomyPages: %v", err)
	}

	src, err := os.ReadFile(filepath.Join(genRoot, "docs", "tags", "docs.tsx"))
	if err != nil {
		t.Fatalf("reading tag page: %v", err)
	}
	got := string(src)
	for _, want := range []string{
		`routeBase: "/reference"`,
		`<li><a href="/reference/intro/">Intro</a></li>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("custom route base missing %q:\n%s", want, got)
		}
	}
}
