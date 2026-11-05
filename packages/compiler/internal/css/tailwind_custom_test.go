package css

import (
	"strings"
	"testing"
)

func customGen(t *testing.T, css string) (*TailwindGenerator, *TailwindDirectives) {
	t.Helper()
	d := NewTailwindDirectives(t.TempDir())
	d.Add(css, "")
	g := NewTailwindGenerator()
	d.ApplyTo(&g.Theme)
	return g, d
}

func TestCustomUtilityNestedRulesDoNotWarn(t *testing.T) {
	g, d := customGen(t, `@utility card { color: red; &:hover { color: blue; } @media print { display: none; } @variant dark { color: white; } }
@utility ring-* { &:focus { outline: --value(integer)px solid; } }`)
	if w := d.Warnings(); len(w) != 0 {
		t.Fatalf("warnings: %v", w)
	}
	out := g.Generate(map[string]bool{"card": true, "ring-2": true, "md:card": true})
	for _, want := range []string{
		".card{color: red; &:hover{color: blue;} @media print{display: none;} @media (prefers-color-scheme: dark){color: white;}}",
		".ring-2{&:focus{outline: 2px solid;}}",
		"@media (min-width: 768px){.md\\:card{color: red;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestCustomUtilityCycleAndInjection(t *testing.T) {
	g, _ := customGen(t, `@utility a { @apply b; color: red; }
@utility b { @apply a; margin: 0; }
@utility inj-* { width: --value([*]); }`)
	out := g.Generate(map[string]bool{"a": true})
	if !strings.Contains(out, "color: red;") || strings.Count(out, "margin: 0;") > 1 {
		t.Errorf("@apply cycle: %q", out)
	}
	for _, cls := range []string{"inj-[red;}x{display:none]", "inj-[a;b]", "inj-[a}]", "w-[red;}x{display:none]", "w-[a}]"} {
		if out := g.Generate(map[string]bool{cls: true}); out != "" {
			t.Errorf("%s must not generate CSS, got %q", cls, out)
		}
	}
}

func TestCustomUtilityImportantAndPrefix(t *testing.T) {
	g, _ := customGen(t, `@utility card { color: red; &:hover { color: blue; } }
@utility pad-* { padding: --spacing(--value(integer)); }`)
	out := g.Generate(map[string]bool{"card!": true, "!pad-2": true})
	for _, want := range []string{"color: red !important;", "&:hover{color: blue !important;}", "padding: 0.5rem !important;"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	g.Theme.Important = "true"
	if out := g.Generate(map[string]bool{"card": true}); !strings.Contains(out, "color: blue !important;") {
		t.Errorf("important: true must reach nested rules:\n%s", out)
	}
	g.Theme.Important = ""
	g.Theme.Prefix = "tw"
	if out := g.Generate(map[string]bool{"tw:card": true, "card": true}); !strings.Contains(out, ".tw\\:card{") || strings.Contains(out, ".card{") {
		t.Errorf("prefix:\n%s", out)
	}
}

func TestCustomUtilityThemeNamespace(t *testing.T) {
	g, _ := customGen(t, `@theme { --tab-size-github: 8; --tab-size-wide: 12; }
@utility tabs-* { tab-size: --value(--tab-size-*, integer); }`)
	out := g.Generate(map[string]bool{"tabs-github": true, "tabs-3": true, "tabs-nope": true})
	if !strings.Contains(out, "tab-size: 8;") || !strings.Contains(out, "tab-size: 3;") || strings.Contains(out, "nope") {
		t.Errorf("custom namespace:\n%s", out)
	}
	// A namespace reset removes the variables again.
	g, _ = customGen(t, `@theme { --tab-size-github: 8; --tab-size-*: initial; }
@utility tabs-* { tab-size: --value(--tab-size-*); }`)
	if out := g.Generate(map[string]bool{"tabs-github": true}); out != "" {
		t.Errorf("reset namespace still resolves:\n%s", out)
	}
}

func TestCustomUtilityApplyUsesNestedUtility(t *testing.T) {
	g, _ := customGen(t, `@utility card { color: red; &:hover { color: blue; } }`)
	out, bad := g.ExpandApply(".k { @apply card md:card; }")
	if len(bad) != 0 || !strings.Contains(out, "&:hover{color: blue;}") {
		t.Errorf("@apply of a utility with nested rules: %q %v", out, bad)
	}
}

func TestInferDataType(t *testing.T) {
	yes := map[string][]string{
		"color":       {"#fff", "red", "RED", "rgb(0 0 0)", "oklch(0.5 0.1 20)", "currentColor", "ButtonText", "color-mix(in srgb,red,blue)"},
		"length":      {"10px", "-1px", ".5px", "1Q", "calc(1px + 2px)", "min(1px,2px)"},
		"percentage":  {"50%", "+5%", "calc(1% + 2%)"},
		"number":      {"1", "-1", ".5", "1e3", "calc(1 + 2)"},
		"integer":     {"0", "3"},
		"ratio":       {"3/2", "3 / 2", "1.5/2"},
		"angle":       {"45deg", "0.5turn"},
		"url":         {"url(x.png)", `url("x.png")`},
		"image":       {"url(x.png)", "linear-gradient(red,blue)", "url(a.png),repeating-linear-gradient(red,blue)"},
		"position":    {"center", "10px 20px", "left top", "50%"},
		"bg-size":     {"cover", "10px 20px", "auto 10px"},
		"line-width":  {"thin", "2px"},
		"family-name": {"Arial", "Arial,sans-serif"},
		"vector":      {"1 2 3"},
	}
	for typ, vals := range yes {
		for _, v := range vals {
			if !inferDataType(v, typ) {
				t.Errorf("%q should be a %s", v, typ)
			}
		}
	}
	no := map[string][]string{
		"color":       {"10px", "var(--c)", "inherit", "system-ui"},
		"length":      {"0", "10", "50%", "1PX", "red", "var(--x)"},
		"percentage":  {"0.5", "10px"},
		"number":      {"1px"},
		"integer":     {"007", "-3", "3.5"},
		"ratio":       {"3"},
		"angle":       {"45", "10px", "calc(1deg + 2deg)"},
		"position":    {"red", "var(--x) center"},
		"bg-size":     {"red", "10px 20px 30px"},
		"family-name": {"1abc", "var(--f)"},
		"vector":      {"1 2", "a b c", "1,2,3"},
	}
	for typ, vals := range no {
		for _, v := range vals {
			if inferDataType(v, typ) {
				t.Errorf("%q should not be a %s", v, typ)
			}
		}
	}
}
