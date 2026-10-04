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
