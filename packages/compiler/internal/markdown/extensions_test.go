package markdown

import (
	"strings"
	"testing"
)

func TestFootnotes(t *testing.T) {
	html := RenderToHTML("Hello[^1] and again[^1].\n\n[^1]: The note.\n", DefaultConfig())
	if !strings.Contains(html, `class="krate-footnote-ref"`) {
		t.Errorf("missing footnote reference:\n%s", html)
	}
	if !strings.Contains(html, `id="fn-1"`) || !strings.Contains(html, `class="krate-footnotes"`) {
		t.Errorf("missing footnote section:\n%s", html)
	}
	if strings.Contains(html, "[^1]:") {
		t.Errorf("footnote definition leaked into body:\n%s", html)
	}
}

// TestHeadingAnchorAmpersand verifies headings with `&` produce a clean slug
// (no `amp` entity remnant) so TOC links and hand-written anchors resolve.
func TestHeadingAnchorAmpersand(t *testing.T) {
	html := RenderToHTML("## Static output & dynamic params\n", DefaultConfig())
	if !strings.Contains(html, `id="static-output-dynamic-params"`) {
		t.Errorf("expected clean slug, got:\n%s", html)
	}
	if strings.Contains(html, `id="static-output-amp`) {
		t.Errorf("slug leaked an entity remnant:\n%s", html)
	}
	html = RenderToHTML("## Typed routes & content\n", DefaultConfig())
	if !strings.Contains(html, `id="typed-routes-content"`) {
		t.Errorf("expected clean slug, got:\n%s", html)
	}
}

func TestDefinitionList(t *testing.T) {
	html := RenderToHTML("Term\n: A definition\n", DefaultConfig())
	for _, want := range []string{"<dl>", "<dt>Term</dt>", "<dd>A definition</dd>"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
}

func TestEmojiShortcodes(t *testing.T) {
	html := RenderToHTML("Ship it :rocket: now", DefaultConfig())
	if !strings.Contains(html, "\U0001F680") {
		t.Errorf("emoji not replaced:\n%s", html)
	}
	// Inside inline code it must be left alone.
	code := RenderToHTML("Use `:rocket:` literally", DefaultConfig())
	if !strings.Contains(code, ":rocket:") {
		t.Errorf("emoji replaced inside code:\n%s", code)
	}
}

func allJSX(segs []MDXSegment) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.JSX)
		b.WriteByte('\n')
	}
	return b.String()
}

func TestContainerDirectives(t *testing.T) {
	cfg := DefaultConfig()

	_, segs := ParseMDXSegments(":::card Title\nBody text\n:::", cfg)
	if jsx := allJSX(segs); !strings.Contains(jsx, "<Card title=\"Title\">") {
		t.Fatalf("card directive not lowered: %q", jsx)
	}

	_, steps := ParseMDXSegments(":::steps\n::step One\nFirst\n::step Two\nSecond\n:::", cfg)
	jsx := allJSX(steps)
	if !strings.Contains(jsx, "<Steps>") || !strings.Contains(jsx, "<Step title=\"One\">") || !strings.Contains(jsx, "<Step title=\"Two\">") {
		t.Fatalf("steps directive not lowered: %q", jsx)
	}

	_, tabs := ParseMDXSegments(":::tabs\n::tab A\nAAA\n::tab B\nBBB\n:::", cfg)
	jsx = allJSX(tabs)
	if !strings.Contains(jsx, "<Tabs defaultValue=\"a\">") || !strings.Contains(jsx, "<TabsTrigger value=\"a\">A</TabsTrigger>") || !strings.Contains(jsx, "<TabsContent value=\"a\">") {
		t.Fatalf("tabs directive not lowered: %q", jsx)
	}

	_, cg := ParseMDXSegments(":::code-group\n```js\nlet x=1\n```\n```ts\nlet y:number=1\n```\n:::", cfg)
	jsx = allJSX(cg)
	if !strings.Contains(jsx, "<Code lang={") || !strings.Contains(jsx, "<TabsTrigger") {
		t.Fatalf("code-group directive not lowered: %q", jsx)
	}
}

func TestDirectiveComponents(t *testing.T) {
	got := DirectiveComponents(":::tabs\n::tab A\nx\n:::\n:::code-group\n```js\n1\n```\n:::")
	joined := strings.Join(got, ",")
	for _, want := range []string{"Tabs", "TabsTrigger", "Code"} {
		if !strings.Contains(joined, want) {
			t.Errorf("DirectiveComponents missing %s: %v", want, got)
		}
	}
}

func TestFootnoteInsideCodeBlockNotExtracted(t *testing.T) {
	html := RenderToHTML("```text\n[^1]: this is code, not a footnote\n```\n", DefaultConfig())
	if !strings.Contains(html, "[^1]: this is code") {
		t.Errorf("footnote def inside a code block was stripped:\n%s", html)
	}
	if strings.Contains(html, "krate-footnotes") {
		t.Errorf("code block should not produce a footnotes section:\n%s", html)
	}
}
