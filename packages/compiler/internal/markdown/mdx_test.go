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

	if fm["title"] != "Demo" {
		t.Errorf("frontmatter title = %q, want %q", fm["title"], "Demo")
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
