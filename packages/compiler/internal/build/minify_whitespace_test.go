package build

import (
	"strings"
	"testing"
)

func TestMinifyHTMLKeepsWhiteSpaceWhereStyleSaysSo(t *testing.T) {
	const text = "a\n\nb   c"
	cases := []struct {
		name  string
		html  string
		rules *whiteSpaceRules
	}{
		{"tailwind class", `<div class="p-2 whitespace-pre-wrap">` + text + `</div>`, nil},
		{"tailwind variant", `<div class="md:whitespace-pre-line">` + text + `</div>`, nil},
		{"inline style", `<div style="white-space:pre-line">` + text + `</div>`, nil},
		{"inline style spaced", `<div style="color:red; white-space : pre-wrap ">` + text + `</div>`, nil},
		{"nested child", `<div class="whitespace-pre"><span><b>` + text + `</b></span></div>`, nil},
		{"stylesheet class", `<div class="log">` + text + `</div>`, parseWhiteSpaceRules(`.x{color:red}@media (min-width:1px){.log,.other{white-space:pre-wrap}}`)},
		{"stylesheet tag", `<p>` + text + `</p>`, parseWhiteSpaceRules(`p { white-space: break-spaces }`)},
		{"inline style element", `<style>.log{white-space:pre}</style><div class="log">` + text + `</div>`, nil},
		{"pre", `<pre>` + text + `</pre>`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := minifyHTMLWith(tc.html, tc.rules)
			if !strings.Contains(got, text) {
				t.Errorf("white space was collapsed:\n in: %q\nout: %q", tc.html, got)
			}
		})
	}
}

func TestMinifyHTMLStillCollapsesElsewhere(t *testing.T) {
	rules := parseWhiteSpaceRules(`.log{white-space:pre-wrap}.n{white-space:normal}`)
	html := `<div class="log">a  b</div><div class="n">c   d</div><p>e   f</p><div class="whitespace-pre">x</div><span>g    h</span>`
	got := minifyHTMLWith(html, rules)
	for _, want := range []string{"a  b", "c d", "e f", "g h"} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q: %s", want, got)
		}
	}
	// The end of a preserving element ends the exemption.
	if strings.Contains(got, "g    h") || strings.Contains(got, "e   f") {
		t.Errorf("white space outside the preserving elements was kept: %s", got)
	}
}

func TestParseWhiteSpaceRules(t *testing.T) {
	r := parseWhiteSpaceRules(`.a{white-space:nowrap}.b{white-space-collapse:preserve}.c .d,#e{white-space:pre !important}.md\:f{white-space:pre-line}`)
	if r == nil {
		t.Fatal("no rules")
	}
	for _, c := range []string{"c", "d", "md:f"} {
		if !r.classes[c] {
			t.Errorf("class %q missing: %v", c, r.classes)
		}
	}
	if !r.ids["e"] {
		t.Errorf("id e missing: %v", r.ids)
	}
	if r.classes["a"] || r.classes["b"] {
		t.Errorf("non-preserving rules picked up: %v", r.classes)
	}
	if parseWhiteSpaceRules(`.a{color:red}`) != nil {
		t.Error("rules reported for a stylesheet without any")
	}
}
