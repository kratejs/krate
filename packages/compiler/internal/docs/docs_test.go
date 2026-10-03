package docs

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/markdown"
)

func testMDConfig() markdown.Config {
	return markdown.DefaultConfig()
}

func TestParseMDFullFrontmatter(t *testing.T) {
	src := `---
title: Getting Decked
order: 42
description: A long intro.
badge:
  text: Hot
  variant: new
keywords: alpha,beta
tags:
  - omega
  - alpha
categories: [widgets, tools]
toc:
  minLevel: 2
  maxLevel: 4
  label: On this page
hero:
  title: Hero Title
  tagline: Tagline here
  actions:
    - link: /docs/x/
      text: Go
      variant: secondary
head:
  - - meta
    - name: robots
      content: noindex
prev:
  text: Back home
  link: /docs/
next: false
draft: true
editUrl: https://example.com/edit.md
sidebar:
  label: Custom Label
  order: 7
  hidden: true
  badge:
    text: v2
---

# Body
`

	html, fm := ParseMD(src, testMDConfig())
	if fm == nil {
		t.Fatal("expected frontmatter")
	}
	if fm.Title != "Getting Decked" {
		t.Errorf("title = %q", fm.Title)
	}
	if fm.Order != 42 {
		t.Errorf("order = %d", fm.Order)
	}
	if fm.Description != "A long intro." {
		t.Errorf("description = %q", fm.Description)
	}
	if fm.Badge == nil || fm.Badge.Text != "Hot" || fm.Badge.Variant != "new" {
		t.Errorf("badge = %+v", fm.Badge)
	}
	if !sliceEq(fm.Keywords, []string{"alpha", "beta"}) {
		t.Errorf("keywords = %v", fm.Keywords)
	}
	if !sliceEq(fm.Tags, []string{"omega", "alpha"}) {
		t.Errorf("tags = %v", fm.Tags)
	}
	if !sliceEq(fm.Categories, []string{"widgets", "tools"}) {
		t.Errorf("categories = %v", fm.Categories)
	}
	if fm.Toc.Disabled {
		t.Error("toc should be enabled")
	}
	if fm.Toc.MinLevel != 2 || fm.Toc.MaxLevel != 4 || fm.Toc.Label != "On this page" {
		t.Errorf("toc = %+v", fm.Toc)
	}
	if fm.Hero == nil || fm.Hero.Title != "Hero Title" || len(fm.Hero.Actions) != 1 {
		t.Fatalf("hero = %+v", fm.Hero)
	}
	if fm.Hero.Actions[0].Text != "Go" || fm.Hero.Actions[0].Link != "/docs/x/" || fm.Hero.Actions[0].Variant != "secondary" {
		t.Errorf("hero action = %+v", fm.Hero.Actions[0])
	}
	if len(fm.Head) != 1 || fm.Head[0].Tag != "meta" || fm.Head[0].Attrs["name"] != "robots" || fm.Head[0].Attrs["content"] != "noindex" {
		t.Errorf("head = %+v", fm.Head)
	}
	if fm.Prev == nil || fm.Prev.Text != "Back home" || fm.Prev.Link != "/docs/" {
		t.Errorf("prev = %+v", fm.Prev)
	}
	if fm.Next == nil || !fm.Next.Disabled {
		t.Errorf("next = %+v", fm.Next)
	}
	if !fm.Draft {
		t.Error("draft should be true")
	}
	if fm.EditURL != "https://example.com/edit.md" {
		t.Errorf("editUrl = %q", fm.EditURL)
	}
	if fm.SidebarConfig == nil || fm.SidebarConfig.Label != "Custom Label" || !fm.SidebarConfig.Hidden || fm.SidebarConfig.Order != 7 {
		t.Fatalf("sidebarConfig = %+v", fm.SidebarConfig)
	}
	if fm.SidebarConfig.Badge == nil || fm.SidebarConfig.Badge.Text != "v2" {
		t.Errorf("sidebar badge = %+v", fm.SidebarConfig.Badge)
	}
	if !strings.Contains(html, "<h1") {
		t.Error("expected rendered HTML with body heading")
	}
}

func TestParseMDLegacyCompat(t *testing.T) {
	// Legacy string sidebar + comma string keywords still work.
	src := `---
title: Legacy
sidebar: get-started
keywords: foo, bar
---

# Legacy
`
	_, fm := ParseMD(src, testMDConfig())
	if fm.Sidebar != "get-started" {
		t.Errorf("sidebar = %q", fm.Sidebar)
	}
	if !sliceEq(fm.Keywords, []string{"foo", "bar"}) {
		t.Errorf("keywords = %v", fm.Keywords)
	}

	// Custom sidebar as a JSON array string still parses.
	src2 := `---
title: Custom
sidebar: [{"title":"A","url":"/docs/a/"},{"title":"Group","children":[{"title":"B","url":"/docs/b/"}]}]
---
`
	_, fm2 := ParseMD(src2, testMDConfig())
	if len(fm2.CustomSidebar) != 2 {
		t.Fatalf("custom sidebar len = %d (%+v)", len(fm2.CustomSidebar), fm2.CustomSidebar)
	}
	if fm2.CustomSidebar[0].Title != "A" || fm2.CustomSidebar[0].URL != "/docs/a/" {
		t.Errorf("first item = %+v", fm2.CustomSidebar[0])
	}
	if len(fm2.CustomSidebar[1].Children) != 1 || fm2.CustomSidebar[1].Children[0].Title != "B" {
		t.Errorf("nested item = %+v", fm2.CustomSidebar[1])
	}

	// toc: false + prev/next booleans.
	src3 := `---
title: TOC off
toc: false
prev: false
---
`
	_, fm3 := ParseMD(src3, testMDConfig())
	if !fm3.Toc.Disabled {
		t.Error("toc: false should disable")
	}
	if fm3.Prev == nil || !fm3.Prev.Disabled {
		t.Errorf("prev: false should disable, got %+v", fm3.Prev)
	}

	// Bare string badge.
	src4 := `---
title: Badged
badge: Rocket
---
`
	_, fm4 := ParseMD(src4, testMDConfig())
	if fm4.Badge == nil || fm4.Badge.Text != "Rocket" || fm4.Badge.Variant != "" {
		t.Errorf("badge = %+v", fm4.Badge)
	}
}

func TestParseMDNoFrontmatter(t *testing.T) {
	html, fm := ParseMD("# Hi\n\nSome body.\n", testMDConfig())
	if fm == nil {
		t.Fatal("expected non-nil frontmatter")
	}
	if fm.Title != "" || fm.Order != 999 || fm.Template != "doc" {
		t.Errorf("defaults wrong: %+v", fm)
	}
	if fm.Toc.MinLevel != 2 || fm.Toc.MaxLevel != 3 {
		t.Errorf("default toc = %+v", fm.Toc)
	}
	if !strings.Contains(html, "<h1") {
		t.Error("expected h1 in html")
	}
}

func TestExtractTOCLevels(t *testing.T) {
	// Levels only extracts within the requested range, nearest match wins.
	items := ExtractTOCLevels("<h2 id=\"a\">A</h2>\n<h3 id=\"b\">B</h3>\n<h4 id=\"c\">C</h4>", 2, 3)
	if len(items) != 2 {
		t.Fatalf("len = %d", len(items))
	}
	if items[0].ID != "a" || items[0].Depth != 2 {
		t.Errorf("items[0] = %+v", items[0])
	}
	if items[1].ID != "b" || items[1].Depth != 3 {
		t.Errorf("items[1] = %+v", items[1])
	}

	items = ExtractTOCLevels("<h2 id=\"a\">A</h2>\n<h4 id=\"c\">C</h4>", 3, 4)
	if len(items) != 1 || items[0].ID != "c" || items[0].Depth != 4 {
		t.Errorf("h4-only range = %+v", items)
	}

	// Clamped defaults.
	items = ExtractTOCLevels("<h1 id=\"top\">T</h1>\n<h2 id=\"a\">A</h2>", 0, 0)
	if len(items) != 1 || items[0].ID != "a" {
		t.Errorf("clamped = %+v", items)
	}
}

func TestBuildSearchIndexMergesTags(t *testing.T) {
	pages := []Page{
		{
			Title: "Search", Path: "features/search",
			Content:    "<p>Body text.</p>",
			Tags:       []string{"docfind", "wasm"},
			Categories: []string{"features"},
		},
	}
	entries := BuildSearchIndex(pages)
	if len(entries) != 1 {
		t.Fatalf("len = %d", len(entries))
	}
	for _, term := range []string{"Body text.", "docfind", "wasm", "features"} {
		if !strings.Contains(entries[0].Content, term) {
			t.Errorf("content missing %q: %q", term, entries[0].Content)
		}
	}
	if !sliceEq(entries[0].Tags, []string{"docfind", "wasm"}) {
		t.Errorf("tags = %v", entries[0].Tags)
	}
}

func TestStripHTMLTagsSeparatesBlocks(t *testing.T) {
	if got := StripHTMLTags("<p>Hello</p><p>World</p>"); got != "Hello\nWorld" {
		t.Errorf("StripHTMLTags paragraphs = %q, want %q", got, "Hello\nWorld")
	}
	if got := StripHTMLTags("<td>File</td><td>When loaded</td>"); got != "File\nWhen loaded" {
		t.Errorf("table cells glued: %q", got)
	}
	if got := StripHTMLTags("  a   b  "); got != "a b" {
		t.Errorf("whitespace not collapsed: %q", got)
	}
	// Inline tags must not introduce breaks.
	if got := StripHTMLTags("a <code>b</code> c"); got != "a b c" {
		t.Errorf("inline tag broke the line: %q", got)
	}
}

func TestSidebarTreeNavConfig(t *testing.T) {
	pages := []Page{
		{Path: "overview", Title: "Overview", Dir: "", Order: 1},
		{Path: "concepts/index", Title: "Concepts", Dir: "concepts", Order: 1,
			SidebarCfg: &SidebarNavConfig{Label: "Concepts", Order: 1, Collapsible: true, DefaultOpen: true}},
		{Path: "concepts/signals", Title: "Signals", Dir: "concepts", Order: 2},
		{Path: "hidden/secret", Title: "Secret", Dir: "hidden", Order: 1,
			SidebarCfg: &SidebarNavConfig{Hidden: true}},
	}
	tree := BuildSidebarTree(pages)
	if len(tree) != 2 {
		t.Fatalf("tree len = %d (%+v)", len(tree), tree)
	}
	// overview first, conceptual section second
	if tree[0].Title != "Overview" || tree[0].URL != "/docs/overview/" {
		t.Errorf("tree[0] = %+v", tree[0])
	}
	sec := tree[1]
	if sec.Title != "Concepts" {
		t.Errorf("section title = %q", sec.Title)
	}
	if !sec.Collapsible {
		t.Error("section should be collapsible")
	}
	if !sec.Expanded {
		t.Error("section should be expanded via defaultOpen")
	}
	// The index page is a regular child of its own section before enrichment.
	if len(sec.Children) != 2 || sec.Children[0].Title != "Concepts" || sec.Children[1].Title != "Signals" {
		t.Errorf("children = %+v", sec.Children)
	}
	// EnrichSidebarItems hides the index leaf and promotes it to IndexURL,
	// leaving only the real section pages.
	enriched := EnrichSidebarItems(tree, "concepts/signals")
	eSec := enriched[1]
	if eSec.IndexURL != "/docs/concepts/" {
		t.Errorf("indexURL = %q", eSec.IndexURL)
	}
	if len(eSec.Children) != 1 || eSec.Children[0].Title != "Signals" {
		t.Errorf("enriched children = %+v", eSec.Children)
	}
	// hidden page must not appear anywhere in the tree
	if containsTitle(tree, "Secret") {
		t.Error("hidden page leaked into nav tree")
	}
}

// TestBuildBreadcrumbsAbsoluteURLs verifies breadcrumb URLs include the docs
// base path (like sidebar and prev/next links) rather than being base-relative.
func TestBuildBreadcrumbsAbsoluteURLs(t *testing.T) {
	crumbs := BuildBreadcrumbs("guides/react-compatibility")
	if len(crumbs) != 2 {
		t.Fatalf("crumbs = %+v", crumbs)
	}
	if crumbs[0].URL != "/docs/guides/" {
		t.Errorf("crumb[0].URL = %q, want /docs/guides/", crumbs[0].URL)
	}
	if crumbs[1].URL != "/docs/guides/react-compatibility/" {
		t.Errorf("crumb[1].URL = %q, want /docs/guides/react-compatibility/", crumbs[1].URL)
	}
	if !crumbs[1].IsLast {
		t.Error("final crumb should be last")
	}
}

func containsTitle(items []SidebarItem, title string) bool {
	for _, it := range items {
		if it.Title == title {
			return true
		}
		if len(it.Children) > 0 && containsTitle(it.Children, title) {
			return true
		}
	}
	return false
}

func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
