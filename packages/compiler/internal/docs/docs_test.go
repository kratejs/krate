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
