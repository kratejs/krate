package markdown

import (
	"regexp"
	"strings"
	"testing"
)

var (
	betweenTagsRe = regexp.MustCompile(`>\s+<`)
	tableWrapRe   = regexp.MustCompile(`</?div[^>]*table-wrap[^>]*>`)
)

func normGFM(s string) string {
	s = tableWrapRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "</div>", "")
	s = betweenTagsRe.ReplaceAllString(strings.TrimSpace(s), "><")
	s = strings.ReplaceAll(s, "\n<", "<")
	return voidSlashRe.ReplaceAllString(s, ">")
}

// GFM spec 0.29 examples for the extensions Krate enables (tables,
// strikethrough, task lists, extended autolinks).
func TestGFMSpecExamples(t *testing.T) {
	cases := []struct{ name, md, want string }{
		{"table basic", "| foo | bar |\n| --- | --- |\n| baz | bim |\n",
			`<table><thead><tr><th>foo</th><th>bar</th></tr></thead><tbody><tr><td>baz</td><td>bim</td></tr></tbody></table>`},
		{"table escaped pipe", "| f\\|oo  |\n| ------ |\n| b `\\|` az |\n| b **\\|** im |\n",
			`<table><thead><tr><th>f|oo</th></tr></thead><tbody><tr><td>b <code>|</code> az</td></tr><tr><td>b <strong>|</strong> im</td></tr></tbody></table>`},
		{"table ends at blockquote", "| abc | def |\n| --- | --- |\n| bar | baz |\n> bar\n",
			`<table><thead><tr><th>abc</th><th>def</th></tr></thead><tbody><tr><td>bar</td><td>baz</td></tr></tbody></table><blockquote><p>bar</p></blockquote>`},
		{"strikethrough", "~~Hi~~ Hello, ~there~ world!\n", `<p><del>Hi</del> Hello, <del>there</del> world!</p>`},
		{"strikethrough paragraph break", "This ~~has a\n\nnew paragraph~~.\n", `<p>This ~~has a</p><p>new paragraph~~.</p>`},
		{"task list", "- [ ] foo\n- [x] bar\n",
			`<ul><li><input disabled="" type="checkbox"> foo</li><li><input checked="" disabled="" type="checkbox"> bar</li></ul>`},
		{"task list nested", "- [x] foo\n  - [ ] bar\n  - [x] baz\n- [ ] bim\n",
			`<ul><li><input checked="" disabled="" type="checkbox"> foo<ul><li><input disabled="" type="checkbox"> bar</li><li><input checked="" disabled="" type="checkbox"> baz</li></ul></li><li><input disabled="" type="checkbox"> bim</li></ul>`},
		{"www autolink", "www.commonmark.org\n", `<p><a href="http://www.commonmark.org">www.commonmark.org</a></p>`},
		{"www autolink path", "Visit www.commonmark.org/help for more information.\n",
			`<p>Visit <a href="http://www.commonmark.org/help">www.commonmark.org/help</a> for more information.</p>`},
		{"url autolink", "http://commonmark.org\n", `<p><a href="http://commonmark.org">http://commonmark.org</a></p>`},
		{"autolink trailing punctuation", "Visit https://encrypted.google.com/search?q=Markup+(business)\n",
			`<p>Visit <a href="https://encrypted.google.com/search?q=Markup+(business)">https://encrypted.google.com/search?q=Markup+(business)</a></p>`},
		{"angle autolink", "<https://example.com/a>\n", `<p><a href="https://example.com/a">https://example.com/a</a></p>`},
		{"email autolink", "foo@bar.baz\n", `<p><a href="mailto:foo@bar.baz">foo@bar.baz</a></p>`},
	}
	for _, c := range cases {
		got := normGFM(RenderToHTML(c.md, Config{GFM: true}))
		if got != c.want {
			t.Errorf("%s\nmarkdown: %q\nwant: %s\ngot:  %s", c.name, c.md, c.want, got)
		}
	}
}

func TestGFMDisabled(t *testing.T) {
	got := RenderToHTML("~~x~~ and www.example.com\n\n| a |\n| - |\n| b |\n", Config{})
	for _, bad := range []string{"<del>", "<a ", "<table"} {
		if strings.Contains(got, bad) {
			t.Errorf("GFM off should not produce %s:\n%s", bad, got)
		}
	}
}
