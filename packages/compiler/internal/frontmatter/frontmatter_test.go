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
