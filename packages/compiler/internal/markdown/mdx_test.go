package markdown

import (
	"strings"
	"testing"
)

func TestExtractImports(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name: "single import",
			input: `---
title: Hello
---

import { Card } from 'krate/components'

# Hello`,
			expected: []string{"import { Card } from 'krate/components'"},
		},
		{
			name: "multiple imports",
			input: `---
title: Demo
---

import { Card, CardGrid } from 'krate/components'
import MyComponent from './MyComponent'

# Demo`,
			expected: []string{
				"import { Card, CardGrid } from 'krate/components'",
				"import MyComponent from './MyComponent'",
			},
		},
		{
			name: "no imports",
			input: `---
title: Plain
---

# Just Markdown`,
			expected: nil,
		},
		{
			name: "prose starting with import is not an import",
			input: `# Themes

import it and pass every docs data field in as props.
import "./side-effect.css"
import type { Props } from "./types"
import * as Lib from "lib";
import Default, { named } from "./mod"`,
			expected: []string{
				`import "./side-effect.css"`,
				`import type { Props } from "./types"`,
				`import * as Lib from "lib";`,
				`import Default, { named } from "./mod"`,
			},
		},
		{
			name: "wrapped prose mentioning a warning is not an import",
			input: `# Lazy

a lazy package import is compiled like a static
import (warning KR4006).`,
			expected: nil,
		},
		{
			name: "imports in frontmatter ignored",
			input: `---
import: something
title: test
---

# Hello`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractImports(tt.input)
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d imports, got %d: %v", len(tt.expected), len(result), result)
			}
			for i, imp := range result {
				if imp != tt.expected[i] {
					t.Errorf("import[%d] = %q, want %q", i, imp, tt.expected[i])
				}
			}
		})
	}
}

func TestHTMLToJSX(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "br tag",
			input:    "<div>Hello<br></div>",
			expected: "<div>Hello<br /></div>",
		},
		{
			name:     "hr tag",
			input:    "<p>text</p><hr><p>more</p>",
			expected: "<p>text</p><hr /><p>more</p>",
		},
		{
			name:     "img tag",
			input:    `<img src="pic.png">`,
			expected: `<img src="pic.png" />`,
		},
		{
			name:     "already self-closing",
			input:    "<br />",
			expected: "<br />",
		},
		{
			name:     "no void elements",
			input:    "<div>Hello</div>",
			expected: "<div>Hello</div>",
		},
		{
			name:     "multiple void elements",
			input:    "<br><hr><img src=\"x.png\">",
			expected: "<br /><hr /><img src=\"x.png\" />",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HTMLToJSX(tt.input)
			if result != tt.expected {
				t.Errorf("HTMLToJSX(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseMDXSegmentsStripImports(t *testing.T) {
	src := `---
title: Demo
---

import { Card } from 'krate/components'

Some text here.

<Card title="Hello">content</Card>`

	cfg := DefaultConfig()
	fm, segments := ParseMDXSegments(src, cfg)

	if s, _ := fm["title"].(string); s != "Demo" {
		t.Errorf("frontmatter title = %#v, want %q", fm["title"], "Demo")
	}

	// First segment should be markdown HTML (not containing import line)
	if len(segments) == 0 {
		t.Fatal("expected at least 1 segment")
	}
	if strings.Contains(segments[0].HTML, "import") {
		t.Errorf("first segment should not contain import, got: %q", segments[0].HTML)
	}
	if !strings.Contains(segments[0].HTML, "Some text here") {
		t.Errorf("first segment should contain text content, got: %q", segments[0].HTML)
	}
	// Second segment should be JSX
	if len(segments) < 2 {
		t.Fatal("expected at least 2 segments (HTML + JSX)")
	}
	if segments[1].JSX == "" {
		t.Errorf("second segment should be JSX, got HTML: %q", segments[1].HTML)
	}
}

func TestParseMDXSegmentsNoFrontmatter(t *testing.T) {
	src := `import { Card } from 'krate/components'

Just text.

<Card title="Test">child</Card>`

	cfg := DefaultConfig()
	_, segments := ParseMDXSegments(src, cfg)

	// Should not have any HTML containing "import"
	for i, seg := range segments {
		if strings.Contains(seg.HTML, "import") {
			t.Errorf("segment[%d] HTML contains import: %q", i, seg.HTML)
		}
	}
}

func TestParseMDXSegmentsKeepsImportProse(t *testing.T) {
	src := "# Themes\n\nPages\nimport it and pass every field in as props.\n"
	_, segs := ParseMDXSegments(src, Config{})
	html := RenderSegmentsToHTML(segs)
	if !strings.Contains(html, "import it and pass every field") {
		t.Fatalf("prose line starting with import was dropped: %s", html)
	}
}

// A markdown list, quote or table directly inside a multi-line JSX element
// must close before the JSX closing tag instead of swallowing it.
func TestMDXBlocksInsideJSXElement(t *testing.T) {
	bodies := map[string]string{
		"bullet list":  "  - a\n  - b",
		"ordered list": "  1. a\n  2. b",
		"quote":        "> quote",
		"table":        "| a | b |\n|---|---|\n| 1 | 2 |",
		"nested list":  "- a\n  - b\n    - c",
	}
	for name, body := range bodies {
		src := "# T\n\n<Note>\n" + body + "\n</Note>\n\nafter\n"
		res := Compile(src, DefaultConfig(), Options{MDX: true})
		var jsx, htmlOut []string
		for _, s := range res.Segments {
			if s.JSX != "" {
				jsx = append(jsx, s.JSX)
			}
			htmlOut = append(htmlOut, s.HTML)
		}
		if len(jsx) != 2 || jsx[0] != "<Note>" || jsx[1] != "</Note>" {
			t.Errorf("%s: want <Note> and </Note> as their own segments, got %q", name, jsx)
		}
		if strings.Contains(strings.Join(htmlOut, ""), "Note") {
			t.Errorf("%s: closing tag leaked into markdown output: %q", name, htmlOut)
		}
	}
}

// Prose directly inside a component keeps its inline run (no <p> wrapper).
func TestMDXInlineTextInsideJSXElementStaysInline(t *testing.T) {
	res := Compile("<Button>\nclick me\n</Button>\n", DefaultConfig(), Options{MDX: true})
	var jsx string
	for _, s := range res.Segments {
		jsx += s.JSX
	}
	if strings.Contains(jsx, "<p>") || !strings.Contains(jsx, "click me") {
		t.Errorf("inline text should not gain a paragraph, got %q", jsx)
	}
}

func TestBuildCodeJSXMetaPassesTitleAndHighlight(t *testing.T) {
	got := BuildCodeJSXMeta("ts", `title="a.ts" {1,3-4}`, "x")
	for _, want := range []string{`lang={"ts"}`, `title={"a.ts"}`, `highlight={"1,3-4"}`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
}

func TestCompileWarnsAboutUnreadableFrontmatter(t *testing.T) {
	for name, src := range map[string]string{
		"toml":         "+++\ntitle = \"x\"\n+++\nbody\n",
		"unterminated": "---\ntitle: x\n\nbody\n",
	} {
		res := Compile(src, DefaultConfig(), Options{})
		var found bool
		for _, d := range res.Diags {
			if d.Warning && d.Line == 1 && d.Hint != "" {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: want a line-1 warning with a hint, got %+v", name, res.Diags)
		}
	}
	if res := Compile("---\ntitle: x\n---\nbody\n", DefaultConfig(), Options{}); len(res.Diags) != 0 {
		t.Errorf("valid frontmatter warned: %+v", res.Diags)
	}
}

func TestCompileReportsInvalidFrontmatterAsError(t *testing.T) {
	res := Compile("---\ntitle: \"bad\ndescription: ok\n---\n\n# Hello\n", Config{}, Options{})
	var found bool
	for _, d := range res.Diags {
		if !d.Warning && d.Line == 2 && d.Col == 8 && strings.Contains(d.Msg, "invalid frontmatter") && d.Hint != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no invalid-frontmatter error at 2:8: %+v", res.Diags)
	}
	ok := Compile("---\ntitle: \"Hello: World\"\n---\n\n# Hi\n", Config{}, Options{})
	for _, d := range ok.Diags {
		t.Errorf("valid frontmatter produced a diagnostic: %+v", d)
	}
	if got := ok.Frontmatter["title"]; got != "Hello: World" {
		t.Errorf("title = %v", got)
	}
}
