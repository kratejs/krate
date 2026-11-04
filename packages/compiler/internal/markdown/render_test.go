package markdown

import (
	"strings"
	"testing"
)

func mustContain(t *testing.T, name, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%s: missing %q in:\n%s", name, w, got)
		}
	}
}

func mustNotContain(t *testing.T, name, got string, bads ...string) {
	t.Helper()
	for _, b := range bads {
		if strings.Contains(got, b) {
			t.Errorf("%s: unexpected %q in:\n%s", name, b, got)
		}
	}
}

func TestFenceMetaTildeAndSymbols(t *testing.T) {
	cfg := DefaultConfig()
	got := RenderToHTML("```ts title=\"x.ts\" {1,3}\nconst a = 1\n```\n\nafter\n", cfg)
	mustContain(t, "meta fence", got, `class="language-ts"`, `data-title="x.ts"`, `data-highlight-lines="1,3"`, "<p>after</p>")
	got = RenderToHTML("~~~js\nlet a\n~~~\n", cfg)
	mustContain(t, "tilde fence", got, `class="language-js"`)
	got = RenderToHTML("```c++\nint x;\n```\n", cfg)
	mustContain(t, "c++ fence", got, `language-c++`)
	got = RenderToHTML("```notalanguage\nplain & <b>\n```\n", cfg)
	mustContain(t, "unknown language", got, "plain &amp; &lt;b&gt;")
}

func TestHeadingIDs(t *testing.T) {
	cfg := DefaultConfig()
	got := RenderToHTML("## Setup\n\n## Setup\n\n## Setup\n", cfg)
	mustContain(t, "dedupe", got, `id="setup"`, `id="setup-1"`, `id="setup-2"`)
	got = RenderToHTML("## Ünïcödé\n\n## 你好世界\n\n## `code` heading\n\n## A *b* [c](http://x) d\n", cfg)
	mustContain(t, "unicode", got, `id="ünïcödé"`, `id="你好世界"`, `id="code-heading"`, `id="a-b-c-d"`)
	got = RenderToHTML("## Hi\n", Config{})
	mustNotContain(t, "anchors off", got, "id=")
}

func TestTaskListsAndNesting(t *testing.T) {
	cfg := DefaultConfig()
	got := RenderToHTML("- [x] done\n- [ ] todo\n", cfg)
	mustContain(t, "task", got, `checked=""`, `type="checkbox"`)
	mustNotContain(t, "task", got, "[x]", "[ ]")
	got = RenderToHTML("- a\n  - b\n    - c\n\n- d\n\n  para in d\n", cfg)
	mustContain(t, "nested", got, "<ul>", "<li>c</li>", "<p>para in d</p>")
	got = RenderToHTML("1) one\n2) two\n", cfg)
	mustContain(t, "paren ordered", got, "<ol>", "<li>one</li>")
}

func TestLinksTitlesAndParens(t *testing.T) {
	cfg := DefaultConfig()
	got := RenderToHTML("[Foo](https://en.wikipedia.org/wiki/Foo_(bar) \"The title\") ![img](/a.png \"t\")\n", cfg)
	mustContain(t, "link paren", got, `href="https://en.wikipedia.org/wiki/Foo_(bar)"`, `title="The title"`, `src="/a.png"`, `title="t"`)
	got = RenderToHTML("<https://x.com> and www.example.com and foo@bar.com\n", cfg)
	mustContain(t, "autolinks", got, `href="https://x.com"`, `href="http://www.example.com"`, `href="mailto:foo@bar.com"`)
	mustNotContain(t, "autolinks", got, "&gt;\"")
	got = RenderToHTML("[ref][r]\n\n[r]: /url \"T\"\n", cfg)
	mustContain(t, "reference link", got, `href="/url"`)
}

func TestTablesPipes(t *testing.T) {
	cfg := DefaultConfig()
	got := RenderToHTML("| a | b |\n|---|---|\n| x \\| y | `c\\|d` |\n", cfg)
	mustContain(t, "table", got, "table-wrap", "<td>x | y</td>", "<code>c|d</code>")
}

func TestDetailsFootnotesInlineHTML(t *testing.T) {
	cfg := DefaultConfig()
	got := RenderToHTML("<details>\n<summary>More</summary>\n\nInner **text**.\n\n</details>\n\nNote[^1].\n\n[^1]: The note.\n", cfg)
	mustContain(t, "details", got, "<details>", "</details>", "<strong>text</strong>", "krate-footnotes", `id="fn-1"`)
	got = RenderToHTML("Press <kbd>Ctrl</kbd> and <span class=\"x\">y</span>\n", cfg)
	mustContain(t, "inline html", got, "<kbd>Ctrl</kbd>", `<span class="x">y</span>`)
}

func TestMarkdownMiscSyntax(t *testing.T) {
	cfg := DefaultConfig()
	got := RenderToHTML("Title\n=====\n\nSub\n---\n\nline one  \nline two\n\ntext\n***\n\n&copy; \\* _em_ __strong__ `` x ` y ``\n\n    indented\n\n#NoSpace\n", cfg)
	mustContain(t, "misc", got, "<h1", "<h2", "<br>", "<hr>", "©", "*", "<em>em</em>", "<strong>strong</strong>", "<code>x ` y</code>", "<pre><code>indented", "#NoSpace")
}

func TestGFMAlerts(t *testing.T) {
	cfg := DefaultConfig()
	got := RenderToHTML("> [!NOTE]\n> Useful **info**.\n\n> plain quote\n", cfg)
	mustContain(t, "alert", got, "krate-aside krate-aside-note", "Useful <strong>info</strong>", "<blockquote>")
	mustNotContain(t, "alert", got, "[!NOTE]")
	_, segs := ParseMDXSegments("> [!WARNING]\n> Careful\n", cfg)
	if len(segs) != 1 || segs[0].Aside == nil || segs[0].Aside.Type != "warning" || !strings.Contains(segs[0].Aside.InnerHTML, "Careful") {
		t.Fatalf("alert not lowered to Aside segment: %#v", segs)
	}
}

func TestUnsafeLinksStillNeutralized(t *testing.T) {
	got := RenderToHTML("[x](javascript:alert(1)) ![y](data:text/html;base64,AA==)\n", DefaultConfig())
	mustNotContain(t, "unsafe", got, "javascript:", "data:", "<a ", "<img")
	mustContain(t, "unsafe", got, "x", "y")
}

func TestRawHTMLBlocksPassThrough(t *testing.T) {
	got := RenderToHTML("<div class=\"x\">\n\n**md**\n\n</div>\n", DefaultConfig())
	mustContain(t, "html block", got, `<div class="x">`, "<strong>md</strong>", "</div>")
}

func TestPlainMarkdownProseWithAngleBracketsIsNotJSX(t *testing.T) {
	r := Compile("<Image> is optimised at build time.\n\nThen a `Array<Foo>` here and <Bar> alone.\n", DefaultConfig(), Options{})
	all := segsText(r)
	// CommonMark passes the stray tag through as raw HTML; it must not be
	// mistaken for a component (which would swallow the rest of the page).
	mustContain(t, "prose", all, "is optimised at build time", "alone")
	for _, s := range r.Segments {
		if s.JSX != "" {
			t.Errorf("unclosed tag in prose became JSX: %q", s.JSX)
		}
	}
}
