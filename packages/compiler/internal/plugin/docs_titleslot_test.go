package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/docs"
	"github.com/kratejs/krate/packages/compiler/internal/markdown"
)

func TestSplitLeadingTitle(t *testing.T) {
	tests := []struct {
		name, in, title, rest string
	}{
		{"plain", "<h1 id=\"a\">Title</h1>\n<p>x</p>", "<h1 id=\"a\">Title</h1>", "<p>x</p>"},
		{"anchor inside", `<h1 id="a"><a href="#a">#</a>Title <code>x</code></h1><p>y</p>`, `<h1 id="a"><a href="#a">#</a>Title <code>x</code></h1>`, "<p>y</p>"},
		{"leading space", "\n  <h1>T</h1><h2>S</h2>", "<h1>T</h1>", "<h2>S</h2>"},
		{"no title", "<p>x</p><h1>late</h1>", "", "<p>x</p><h1>late</h1>"},
		{"h2 first", "<h2>S</h2>", "", "<h2>S</h2>"},
		{"empty", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title, rest := splitLeadingTitle(tt.in)
			if title != tt.title || rest != tt.rest {
				t.Fatalf("splitLeadingTitle = (%q, %q), want (%q, %q)", title, rest, tt.title, tt.rest)
			}
		})
	}
}

func titleSlotPage(t *testing.T, md string) docs.Page {
	t.Helper()
	src := filepath.Join(t.TempDir(), "p.md")
	if err := os.WriteFile(src, []byte(md), 0644); err != nil {
		t.Fatal(err)
	}
	return docs.Page{Path: "p", Title: "P", URL: "/docs/p/", SourcePath: src, Description: "About P"}
}

// A theme that declares a title slot receives the leading h1 as titleHTML and
// the content without it; any other theme keeps the h1 in the content.
func TestDocsTitleSlot(t *testing.T) {
	page := titleSlotPage(t, "# Hello `x`\n\nBody text.\n")
	cfg := &config.Config{}
	gen := func(slot bool) string {
		return (&DocsPlugin{}).generateTSX(&BuildHookCtx{}, cfg, page, "", false, nil, nil, nil, "", "", "", "",
			docsLocaleInfo{}, &DocsPluginOptions{titleSlot: slot}, nil, markdown.Config{HeadingAnchors: true}, "/docs")
	}

	with := gen(true)
	if !strings.Contains(with, "titleHTML:") || !strings.Contains(with, "Hello") {
		t.Fatalf("title slot theme did not receive titleHTML:\n%s", with)
	}
	content := with[strings.Index(with, "<DocsLayout"):]
	if strings.Contains(content, "<h1") {
		t.Errorf("the h1 is still in the content of a title slot theme:\n%s", content)
	}
	if !strings.Contains(content, "Body text.") {
		t.Errorf("content lost its body:\n%s", content)
	}
	if !strings.Contains(with, "description:") {
		t.Errorf("description prop missing")
	}

	without := gen(false)
	if strings.Contains(without, "titleHTML:") {
		t.Errorf("a theme without a title slot got titleHTML:\n%s", without)
	}
	if !strings.Contains(without[strings.Index(without, "<DocsLayout"):], "<h1") {
		t.Errorf("the h1 was removed for a theme without a title slot:\n%s", without)
	}
}

func TestDocsThemeDescriptorTitleSlot(t *testing.T) {
	opts := &DocsPluginOptions{Theme: []byte(`{"name":"t","module":"./layout.tsx","titleSlot":true}`)}
	theme, err := (&DocsPlugin{}).resolveDocsTheme(t.TempDir(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !theme.titleSlot {
		t.Fatal("titleSlot was not read from the theme descriptor")
	}
	opts = &DocsPluginOptions{Theme: []byte(`{"name":"t","module":"./layout.tsx"}`)}
	theme, err = (&DocsPlugin{}).resolveDocsTheme(t.TempDir(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if theme.titleSlot {
		t.Fatal("titleSlot defaulted to true")
	}
}
