package css

import (
	"sort"
	"strings"
	"testing"
)

func TestUnsafeRuleReason(t *testing.T) {
	safe := []string{
		`.a{color: red;}`,
		`.bg-\[\#d81f26\]\/\[\.16\]{background-color: rgb(216 31 38 / .16);}`,
		`.a{background-image: url(data:image/svg+xml;base64,AAAA);}`,
		`.a{background-image: url("a]b.png");}`,
		`.a{content: "a}b;c(";}`,
		`.a{content: 'x[';}`,
		`.a{width: calc(100% - var(--x, calc(1px + 2px)));}`,
		`.a{font-family: "A B", var(--f, "C D"), sans-serif;}`,
		`@media (min-width: 48rem){.a:hover{color: red;}}`,
		`.a{color: red; @supports (color: color-mix(in lab, red, red)){color: blue;}}`,
		`.a{color: red}`,
		`.a{/* a ] b */color: red;}`,
		`.a{--x: [1,2];}`,
		`.a\:b::after{content: "\"";}`,
		`@layer properties;`,
	}
	for _, css := range safe {
		if why := unsafeRuleReason(css); why != "" {
			t.Errorf("rejected valid CSS (%s): %s", why, css)
		}
	}
	unsafe := map[string]string{
		`.a{background-color: #d81f26]/[.16;}`: "unbalanced",
		`.a{color: rgb(1 2 3;}`:                "unbalanced",
		`.a{color: red)}`:                      "unbalanced",
		`.a{color: red;`:                       "unbalanced",
		`.a{color: red;}}`:                     "unbalanced",
		`.a{color: red;} .b{`:                  "unbalanced",
		`.a{content: "abc;}`:                   "unterminated string",
		`.a{content: 'abc;}`:                   "unterminated string",
		"a{content: \"ab\ncd\";}":              "unterminated string",
		`.a{color: red; junk;}`:                "stray ;",
		`.a{color: red;;}`:                     "stray ;",
		`.a{color: red; junk}`:                 "stray text",
		`.a{width: [1;2];}`:                    "stray ;",
		`.a{color: red;} ; .b{color: blue;}`:   "stray ;",
		`.a{color: red; /* open}`:              "unterminated comment",
	}
	for css, want := range unsafe {
		why := unsafeRuleReason(css)
		if why == "" || !strings.Contains(why, want) {
			t.Errorf("%q: reason %q, want one containing %q", css, why, want)
		}
	}
}

func TestGenerateDropsUnsafeRule(t *testing.T) {
	g := NewTailwindGenerator()
	// The arbitrary value opens a paren that is never closed.
	out := g.Generate(map[string]bool{"w-[calc(1px]": true, "px-3": true, "w-[a)]": true})
	if strings.Contains(out, "calc(1px") || strings.Contains(out, "a)") {
		t.Fatalf("unsafe rule emitted:\n%s", out)
	}
	if !strings.Contains(out, ".px-3") {
		t.Errorf("valid rule lost:\n%s", out)
	}
	want := []string{"w-[a)]: unbalanced )", "w-[calc(1px]: unbalanced }"}
	sort.Strings(g.Dropped)
	if strings.Join(g.Dropped, "|") != strings.Join(want, "|") {
		t.Errorf("Dropped = %q, want %q", g.Dropped, want)
	}
}

func TestGenerateKeepsValidArbitraryValues(t *testing.T) {
	g := NewTailwindGenerator()
	classes := map[string]bool{
		"bg-[url(/a]b.png)]":                                 true,
		"w-[calc(100%-var(--a,calc(1px+2px)))]":              true,
		"font-['Open_Sans',var(--f,'Fira_Mono'),sans-serif]": true,
		"content-['a]b']":                                    true,
		"bg-[url(data:image/svg+xml;base64,AAAA)]":           true,
		"grid-cols-[repeat(2,minmax(0,1fr))]":                true,
		"[mask-image:linear-gradient(#000,transparent)]":     true,
		"bg-[#d81f26]/[.16]":                                 true,
		"hover:bg-[var(--x)]/[var(--a,0.5)]":                 true,
		"before:content-['\"']":                              true,
	}
	g.Generate(classes)
	if len(g.Dropped) != 0 {
		t.Errorf("valid classes dropped: %v", g.Dropped)
	}
}
