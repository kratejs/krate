package frontmatter

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckAcceptsValidFrontmatter(t *testing.T) {
	valid := map[string]string{
		"no frontmatter":     "# Hello\n",
		"empty block":        "---\n---\n# Hello\n",
		"blank block":        "---\n\n---\nbody",
		"plain":              "---\ntitle: Getting Started\norder: 3\ndraft: false\n---\nbody",
		"quoted colon":       "---\ntitle: \"Hello: World\"\n---\n",
		"single quoted":      "---\ntitle: 'It''s: fine'\n---\n",
		"url":                "---\nlink: https://example.com/a?b=c#d\n---\n",
		"inline comment":     "---\ntitle: \"A\" # note\nslug: b # note\n---\n",
		"multi-line quoted":  "---\ndescription: \"first\n  second\"\n---\n",
		"literal block":      "---\ndescription: |\n  line one\n  line: two\nnext: x\n---\n",
		"folded block":       "---\ndescription: >-\n  a\n  b\n---\n",
		"plain continuation": "---\ndescription: a long\n  description\n  here\n---\n",
		"plain block child":  "---\ndescription:\n  a long text\n  more\n---\n",
		"flow list":          "---\ntags: [a, \"b, c\", 'd']\n---\n",
		"flow apostrophe":    "---\ntags: [don't, stop]\n---\n",
		"flow object":        "---\nhero: {title: Hi, image: \"x\"}\n---\n",
		"multi-line flow":    "---\ntags: [a,\n  b]\n---\n",
		"block list":         "---\ntags:\n  - install\n  - \"cli: tool\"\n---\n",
		"nested":             "---\nhero:\n  title: Welcome\n  actions:\n    - text: Go\n      link: /x\n---\n",
		"crlf":               "---\r\ntitle: \"A\"\r\ntags: [a, b]\r\n---\r\nbody",
		"bom":                "\ufeff---\ntitle: A\n---\n",
		"comment lines":      "---\n# note\ntitle: A\n\n# more\n---\n",
		"unterminated block": "---\ntitle: \"A\n",
	}
	for name, src := range valid {
		t.Run(name, func(t *testing.T) {
			if is := Check(src); len(is) != 0 {
				t.Fatalf("valid frontmatter rejected: %+v", is)
			}
		})
	}
}

func TestCheckReportsFileLines(t *testing.T) {
	tests := []struct {
		name, src string
		line, col int
		contains  string
	}{
		{"unterminated quote", "---\ntitle: \"bad\ndescription: ok\n---\n# Hi", 2, 8, "never closed"},
		{"unterminated single quote", "---\nlayout: x\ntitle: 'bad\n---\n", 3, 8, "never closed"},
		{"text after quote", "---\ntitle: \"a\" b\n---\n", 2, 11, "after the closing quote"},
		{"unclosed list", "---\ntitle: A\ntags: [a, b\n---\n", 3, 7, "never closed"},
		{"unclosed object", "---\nhero: {title: Hi\n---\n", 2, 7, "never closed"},
		{"missing colon", "---\ntitle: A\nauthor Jane\n---\n", 3, 1, "key: value"},
		{"bad list item", "---\ntags:\n  - ok\n  - \"oops\n---\n", 4, 5, "never closed"},
		{"bad nested key", "---\nhero:\n  title: A\n  image\n---\n", 4, 3, "key: value"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			is := Check(tc.src)
			if len(is) != 1 {
				t.Fatalf("got %d issues, want 1: %+v", len(is), is)
			}
			g := is[0]
			if g.Line != tc.line || g.Col != tc.col || !strings.Contains(g.Msg, tc.contains) || g.Hint == "" {
				t.Fatalf("got %d:%d %q hint=%q, want %d:%d containing %q", g.Line, g.Col, g.Msg, g.Hint, tc.line, tc.col, tc.contains)
			}
		})
	}
}

// Frontmatter in this repository's own markdown must stay valid.
func TestCheckAcceptsRepositoryMarkdown(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	if _, err := os.Stat(filepath.Join(root, "packages", "web")); err != nil {
		t.Skip("repository layout not available")
	}
	checked := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", ".git", "dist", ".krate", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if ext := filepath.Ext(path); ext != ".md" && ext != ".mdx" {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		checked++
		for _, is := range Check(string(data)) {
			t.Errorf("%s:%d:%d: %s", path, is.Line, is.Col, is.Msg)
		}
		return nil
	})
	if checked == 0 {
		t.Skip("no markdown found")
	}
}
