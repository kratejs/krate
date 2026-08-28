package markdown

import (
	"strings"
	"testing"
)

func TestParseMDXSegmentsCodeFences(t *testing.T) {
	src := `---
title: Demo
---

Some intro text.

` + "```tsx" + `
const x = 1;
` + "```" + `

Middle paragraph.

<Card title="Hi">content</Card>

` + "```go" + `
package main
` + "```" + `

Trailing.`

	cfg := DefaultConfig()
	_, segments := ParseMDXSegments(src, cfg)

	var kinds []string
	for _, seg := range segments {
		if seg.HTML != "" {
			kinds = append(kinds, "HTML")
		}
		if seg.JSX != "" {
			kinds = append(kinds, "JSX")
		}
		if seg.Code != nil {
			kinds = append(kinds, "Code("+seg.Code.Lang+")")
		}
	}

	want := []string{"HTML", "Code(tsx)", "HTML", "JSX", "HTML", "Code(go)", "HTML"}
	if len(kinds) != len(want) {
		t.Fatalf("segment order = %v, want %v", kinds, want)
	}
	for i := range kinds {
		if kinds[i] != want[i] {
			t.Errorf("segment[%d] = %v, want %v", i, kinds, want)
		}
	}

	// Verify Code segments contain the raw code and lang.
	var foundTSX, foundGo bool
	for _, seg := range segments {
		if seg.Code == nil {
			continue
		}
		switch seg.Code.Lang {
		case "tsx":
			if seg.Code.Code != "const x = 1;" {
				t.Errorf("tsx code = %q, want %q", seg.Code.Code, "const x = 1;")
			}
			foundTSX = true
		case "go":
			foundGo = true
		}
	}
	if !foundTSX {
		t.Error("no tsx code segment found")
	}
	if !foundGo {
		t.Error("no go code segment found")
	}

	// Verify BuildCodeJSX output.
	jsx := BuildCodeJSX("tsx", "const x = 1;\n// `backtick` and ${interp}")
	if !strings.HasPrefix(jsx, "<Code lang=\"tsx\">") {
		t.Errorf("BuildCodeJSX prefix wrong: %q", jsx)
	}
}

func TestHasCodeSegments(t *testing.T) {
	if HasCodeSegments([]MDXSegment{{HTML: "x"}}) {
		t.Error("expected false for no code segments")
	}
	if !HasCodeSegments([]MDXSegment{{Code: &CodeSegment{Lang: "js"}}}) {
		t.Error("expected true for code segment")
	}
}

func TestParseMDXSegmentsAdmonitions(t *testing.T) {
	src := `---
title: Admonitions
---

Intro text.

:::note
This is a note body with **bold**.
:::

:::tip Custom Tip Title
Custom title content.
:::

:::warning
Warning inner.
:::

Tail.`

	cfg := DefaultConfig()
	_, segments := ParseMDXSegments(src, cfg)

	var kinds []string
	for _, seg := range segments {
		if seg.HTML != "" {
			kinds = append(kinds, "HTML")
		}
		if seg.Aside != nil {
			kinds = append(kinds, "Aside("+seg.Aside.Type+")")
		}
	}

	want := []string{"HTML", "Aside(note)", "HTML", "Aside(tip)", "HTML", "Aside(warning)", "HTML"}
	if len(kinds) != len(want) {
		t.Fatalf("segment kinds = %v, want %v", kinds, want)
	}
	for i := range kinds {
		if kinds[i] != want[i] {
			t.Errorf("segment[%d] = %v, want %v", i, kinds, want)
		}
	}

	// Check asideTitle resolution: default per type, custom title passthrough.
	if asideTitle("note", "") != "Note" {
		t.Error("asideTitle(note) should default to Note")
	}
	if asideTitle("tip", "") != "Tip" {
		t.Error("asideTitle(tip) should default to Tip")
	}
	if asideTitle("warning", "") != "Warning" {
		t.Error("asideTitle(warning) should default to Warning")
	}
	if asideTitle("note", "Custom") != "Custom" {
		t.Error("asideTitle should keep explicit title")
	}

	// Verify the tip aside captured the custom title and raw rendered inner HTML.
	foundTip := false
	for _, seg := range segments {
		if seg.Aside != nil && seg.Aside.Type == "tip" {
			if seg.Aside.Title != "Custom Tip Title" {
				t.Errorf("tip title = %q, want %q", seg.Aside.Title, "Custom Tip Title")
			}
			if !strings.Contains(seg.Aside.InnerHTML, "Custom title content.") {
				t.Errorf("tip innerHTML missing content: %q", seg.Aside.InnerHTML)
			}
			foundTip = true
		}
	}
	if !foundTip {
		t.Error("no tip aside segment found")
	}

	// BuildAsideJSX emits an <Aside> with type/title and dangerouslySetInnerHTML.
	ad := &AsideSegment{Type: "warning", Title: "Warning", InnerHTML: "<p>hi</p>"}
	jsx := BuildAsideJSX(ad)
	if !strings.HasPrefix(jsx, "<Aside type=\"warning\" title=\"Warning\">") {
		t.Errorf("BuildAsideJSX prefix wrong: %q", jsx)
	}
	if !strings.Contains(jsx, "dangerouslySetInnerHTML") {
		t.Errorf("BuildAsideJSX missing dangerouslySetInnerHTML: %q", jsx)
	}

	if !HasAsideSegments(segments) {
		t.Error("HasAsideSegments expected true")
	}
	if HasAsideSegments([]MDXSegment{{HTML: "x"}}) {
		t.Error("HasAsideSegments expected false for plain HTML")
	}
}

func TestRenderSegmentsToHTML(t *testing.T) {
	segs := []MDXSegment{
		{HTML: "<p>intro</p>"},
		{Aside: &AsideSegment{Type: "note", Title: "Note", InnerHTML: "<p>body</p>"}},
		{Code: &CodeSegment{Lang: "", Code: "x < 1"}},
	}
	out := RenderSegmentsToHTML(segs)
	if !strings.Contains(out, "krate-aside krate-aside-note") {
		t.Errorf("aside markup missing: %q", out)
	}
	if !strings.Contains(out, "<pre>x &lt; 1</pre>") {
		t.Errorf("code escaped/pre missing: %q", out)
	}
}
