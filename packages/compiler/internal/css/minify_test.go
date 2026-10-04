package css

import (
	"strings"
	"testing"
)

func TestMinifyPreservesStringContents(t *testing.T) {
	out := Minify(`.a{content:"a  b";}`)
	if !strings.Contains(out, `"a  b"`) {
		t.Errorf("whitespace inside a string must be preserved: %s", out)
	}
}

func TestMinifyPreservesDataURIAndUrl(t *testing.T) {
	in := `.a{background:url("data:image/svg+xml;utf8,<svg>#aabbcc</svg>") no-repeat}`
	out := Minify(in)
	if !strings.Contains(out, "data:image/svg+xml;utf8") || !strings.Contains(out, "#aabbcc") {
		t.Errorf("data URI must be untouched: %s", out)
	}
	if !strings.Contains(out, "no-repeat") {
		t.Errorf("declaration after url lost: %s", out)
	}
}

func TestMinifyPreservesLicenseComment(t *testing.T) {
	out := Minify("/*! (c) Krate */ .a{color:red}")
	if !strings.Contains(out, "/*! (c) Krate */") {
		t.Errorf("license comment must survive: %s", out)
	}
	if strings.Contains(out, "color:red;") {
		t.Errorf("unexpected content: %s", out)
	}
}

func TestMinifyImportantWins(t *testing.T) {
	out := Minify(".a{color:red !important;color:blue}")
	if !strings.Contains(out, "!important") {
		t.Errorf("!important declaration must not be dropped: %s", out)
	}
	if strings.Contains(out, "blue") {
		t.Errorf("later non-important value should be discarded: %s", out)
	}
}

func TestMinifyKeepsRulesAfterCommentWithApostrophe(t *testing.T) {
	// A quote inside a comment must not be treated as a string start: doing so
	// swallows the rest of the stylesheet (regression for the dev/docs theme
	// losing its later rules).
	in := "/* doesn't reintroduce the old size */\n.a { color: red; }\n.b { color: blue; }\n"
	out := Minify(in)
	if !strings.Contains(out, ".a") || !strings.Contains(out, ".b") {
		t.Fatalf("minify dropped rules after an apostrophe comment: %q", out)
	}
}

func TestMinifyKeepsRulesAfterUrlInComment(t *testing.T) {
	in := "/* see url(foo) for details */\n.a { color: red; }\n"
	out := Minify(in)
	if !strings.Contains(out, ".a") {
		t.Fatalf("minify dropped rule after a comment containing url(: %q", out)
	}
}
