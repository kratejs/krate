package frontmatter

import (
	"reflect"
	"testing"
)

func TestParseBasicFlatScalars(t *testing.T) {
	src := `---
title: Getting Started
order: 3
draft: true
rating: 4.5
empty:
quoted: "Hello, world"
single: 'it''s here'
null: null
---

# Body
`
	fm, body := Parse(src)
	if fm["title"] != "Getting Started" {
		t.Errorf("title = %#v, want %q", fm["title"], "Getting Started")
	}
	if fm["order"] != int64(3) {
		t.Errorf("order = %#v (%T), want int64(3)", fm["order"], fm["order"])
	}
	if fm["draft"] != true {
		t.Errorf("draft = %#v, want true", fm["draft"])
	}
	if fm["rating"] != 4.5 {
		t.Errorf("rating = %#v, want 4.5", fm["rating"])
	}
	if fm["empty"] != "" {
		t.Errorf("empty = %#v, want empty string", fm["empty"])
	}
	if fm["quoted"] != "Hello, world" {
		t.Errorf("quoted = %#v", fm["quoted"])
	}
	if fm["single"] != "it's here" {
		t.Errorf("single = %#v", fm["single"])
	}
	if fm["null"] != nil {
		t.Errorf("null = %#v, want nil", fm["null"])
	}
	if got := body; got != "\n# Body\n" {
		t.Errorf("body = %q", got)
	}
}

func TestParseInlineCollections(t *testing.T) {
	src := `---
keywords: [one, two, three]
map: {a: 1, b: two}
quotedList: ["x, y", z]
title: Demo
---
`
	fm, _ := Parse(src)
	if !reflect.DeepEqual(fm["keywords"], []any{"one", "two", "three"}) {
		t.Errorf("keywords = %#v", fm["keywords"])
	}
	m := fm["map"]
	mm, ok := m.(map[string]any)
	if !ok {
		t.Fatalf("map = %T, want map[string]any", m)
	}
	if mm["a"] != int64(1) || mm["b"] != "two" {
		t.Errorf("map = %#v", mm)
	}
	if !reflect.DeepEqual(fm["quotedList"], []any{"x, y", "z"}) {
		t.Errorf("quotedList = %#v", fm["quotedList"])
	}
}

func TestParseIndentedBlocksAndLists(t *testing.T) {
	src := `---
sidebar:
  label: Overview
  order: 1
  hidden: false
  badge: New
tags:
  - install
  - cli
hero:
  title: Welcome
  tagline: A static site generator
  actions:
    - text: Get Started
      link: /docs/getting-started
      variant: primary
    - text: GitHub
      link: https://github.com
keywords:
  - alpha
  - beta
title: Overview
---
`
	fm, _ := Parse(src)
	sidebar, ok := fm["sidebar"].(map[string]any)
	if !ok {
		t.Fatalf("sidebar = %T, want map[string]any", fm["sidebar"])
	}
	if sidebar["label"] != "Overview" || sidebar["order"] != int64(1) || sidebar["hidden"] != false {
		t.Errorf("sidebar = %#v", sidebar)
	}
	if sidebar["badge"] != "New" {
		t.Errorf("sidebar.badge = %#v", sidebar["badge"])
	}
	if !reflect.DeepEqual(fm["tags"], []any{"install", "cli"}) {
		t.Errorf("tags = %#v", fm["tags"])
	}
	hero := fm["hero"].(map[string]any)
	actions := hero["actions"].([]any)
	if len(actions) != 2 {
		t.Fatalf("hero.actions = %#v", actions)
	}
	first := actions[0].(map[string]any)
	if first["text"] != "Get Started" || first["link"] != "/docs/getting-started" || first["variant"] != "primary" {
		t.Errorf("hero.actions[0] = %#v", first)
	}
}

func TestParseNestedEmptyKey(t *testing.T) {
	src := `---
config:
  timeout: 5
  flags:
    - -v
    - --force
---
`
	fm, _ := Parse(src)
	cfg := fm["config"].(map[string]any)
	if cfg["timeout"] != int64(5) {
		t.Errorf("config.timeout = %#v", cfg["timeout"])
	}
	flags := cfg["flags"].([]any)
	if len(flags) != 2 || flags[0] != "-v" || flags[1] != "--force" {
		t.Errorf("config.flags = %#v", flags)
	}
}

func TestParseNoFrontmatter(t *testing.T) {
	src := "# Just a heading\n"
	fm, body := Parse(src)
	if fm != nil {
		t.Errorf("fm = %#v, want nil", fm)
	}
	if body != src {
		t.Errorf("body = %q, want unchanged src", body)
	}
}

func TestParseBOM(t *testing.T) {
	src := "\ufeff---\ntitle: BOM\n---\nbody"
	fm, body := Parse(src)
	if fm["title"] != "BOM" {
		t.Errorf("title = %#v", fm["title"])
	}
	if body != "body" {
		t.Errorf("body = %q", body)
	}
}

func TestParseCommentsAndInlineComment(t *testing.T) {
	src := `---
# a header comment
title: Page  # trailing comment
order: 2 # we like spaces
tags: [a, b] # list comment
---
`
	fm, _ := Parse(src)
	if fm["title"] != "Page" {
		t.Errorf("title = %#v", fm["title"])
	}
	if fm["order"] != int64(2) {
		t.Errorf("order = %#v", fm["order"])
	}
	if !reflect.DeepEqual(fm["tags"], []any{"a", "b"}) {
		t.Errorf("tags = %#v", fm["tags"])
	}
}

func TestParseUrlValue(t *testing.T) {
	src := `---
editUrl: https://example.com/guide/a.md
link: https://x.com/path?a=b:c
---
`
	fm, _ := Parse(src)
	if fm["editUrl"] != "https://example.com/guide/a.md" {
		t.Errorf("editUrl = %#v", fm["editUrl"])
	}
	if fm["link"] != "https://x.com/path?a=b:c" {
		t.Errorf("link = %#v", fm["link"])
	}
}

func TestParsePrevNextConfig(t *testing.T) {
	src := `---
prev:
  text: Previous Page
  link: /docs/prev/
next: false
---
`
	fm, _ := Parse(src)
	prev := fm["prev"].(map[string]any)
	if prev["text"] != "Previous Page" || prev["link"] != "/docs/prev/" {
		t.Errorf("prev = %#v", prev)
	}
	if fm["next"] != false {
		t.Errorf("next = %#v, want false", fm["next"])
	}
}

func TestParseHeadArrayOfPairs(t *testing.T) {
	src := `---
head:
  - [meta, {name: author, content: Krate}]
  - [link, {rel: canonical, href: https://example.com/}]
---
`
	fm, _ := Parse(src)
	head := fm["head"].([]any)
	if len(head) != 2 {
		t.Fatalf("head = %#v", head)
	}
	pair := head[0].([]any)
	if len(pair) != 2 {
		t.Fatalf("head[0] = %#v", pair)
	}
	if pair[0] != "meta" {
		t.Errorf("head[0][0] = %#v", pair[0])
	}
	attrs := pair[1].(map[string]any)
	if attrs["name"] != "author" || attrs["content"] != "Krate" {
		t.Errorf("head[0][1] = %#v", attrs)
	}
}

// Legacy flat inputs (as produced by old `key: value`-only docs) must keep
// parsing to scalar strings where the old parser returned strings.
func TestBackwardCompatFlatFrontmatter(t *testing.T) {
	src := `---
title: Demo
order: 999
sidebar: My Section
keywords: alpha,beta
---
`
	fm, _ := Parse(src)
	if fm["title"] != "Demo" {
		t.Errorf("title = %#v", fm["title"])
	}
	// order was a string in the old parser; now a number - docs decodes both.
	if fm["order"] != int64(999) {
		t.Errorf("order = %#v", fm["order"])
	}
	if fm["sidebar"] != "My Section" {
		t.Errorf("sidebar = %#v", fm["sidebar"])
	}
	if fm["keywords"] != "alpha,beta" {
		t.Errorf("keywords = %#v", fm["keywords"])
	}
}

func TestParseBlockTrivial(t *testing.T) {
	fm := ParseBlock("")
	if len(fm) != 0 {
		t.Errorf("fm = %#v, want empty", fm)
	}
	fm = ParseBlock("title: Only")
	if fm["title"] != "Only" {
		t.Errorf("fm = %#v", fm)
	}
}

// Block-form nested list on one line (`- - a` plus deeper `- b` continuations)
// must produce a nested list, e.g. the `head:` flow for meta tags:
//
//	head:
//	  - - meta
//	    - name: robots
//	      content: noindex
func TestParseNestedListOnSameLine(t *testing.T) {
	src := `---
head:
  - - meta
    - name: robots
      content: noindex
---
`
	fm, _ := Parse(src)
	head, ok := fm["head"].([]any)
	if !ok || len(head) != 1 {
		t.Fatalf("head = %#v", fm["head"])
	}
	pair, ok := head[0].([]any)
	if !ok || len(pair) != 2 {
		t.Fatalf("head[0] = %#v", head[0])
	}
	if pair[0] != "meta" {
		t.Errorf("pair[0] = %#v", pair[0])
	}
	attrs, ok := pair[1].(map[string]any)
	if !ok {
		t.Fatalf("pair[1] = %#v", pair[1])
	}
	if attrs["name"] != "robots" || attrs["content"] != "noindex" {
		t.Errorf("attrs = %#v", attrs)
	}
}

// JSON-style inline objects with quoted keys and nested arrays:
//
//	sidebar: [{"title":"A","url":"/docs/a/"},{"title":"B","children":[{"title":"C","url":"/docs/c/"}]}]
func TestParseInlineJSONStyleObjects(t *testing.T) {
	src := `---
sidebar: [{"title":"A","url":"/docs/a/"},{"title":"Group","children":[{"title":"B","url":"/docs/b/"}]}]
---
`
	fm, _ := Parse(src)
	list, ok := fm["sidebar"].([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("sidebar = %#v", fm["sidebar"])
	}
	first, ok := list[0].(map[string]any)
	if !ok || first["title"] != "A" || first["url"] != "/docs/a/" {
		t.Fatalf("list[0] = %#v", list[0])
	}
	second, ok := list[1].(map[string]any)
	if !ok || second["title"] != "Group" {
		t.Fatalf("list[1] = %#v", list[1])
	}
	children, ok := second["children"].([]any)
	if !ok || len(children) != 1 {
		t.Fatalf("children = %#v", second["children"])
	}
	child, ok := children[0].(map[string]any)
	if !ok || child["title"] != "B" {
		t.Fatalf("child = %#v", children[0])
	}
}
