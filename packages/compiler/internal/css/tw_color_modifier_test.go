package css

import (
	"strings"
	"testing"
)

// colorFamilies maps each colour utility prefix to a property it must set.
var colorFamilies = []struct{ prefix, prop string }{
	{"bg", "background-color: "},
	{"text", "color: "},
	{"border", "border-color: "},
	{"border-t", "border-top-color: "},
	{"ring", "--tw-ring-color: "},
	{"ring-offset", "--tw-ring-offset-color: "},
	{"inset-ring", "--tw-inset-ring-color: "},
	{"outline", "outline-color: "},
	{"fill", "fill: "},
	{"stroke", "stroke: "},
	{"decoration", "text-decoration-color: "},
	{"divide", "border-color: "},
	{"placeholder", "color: "},
	{"accent", "accent-color: "},
	{"caret", "caret-color: "},
	{"from", "--tw-gradient-from: "},
	{"via", "--tw-gradient-via: "},
	{"to", "--tw-gradient-to: "},
	{"shadow", "--tw-shadow-color: "},
	{"inset-shadow", "--tw-inset-shadow-color: "},
}

// TestArbitraryColorModifierMatrix checks arbitrary colours with every kind
// of alpha modifier on every colour utility. The expectations are what
// Tailwind v4 computes; hex colours use the equivalent rgb() form, which
// Krate has always emitted for them, and other colours use color-mix().
func TestArbitraryColorModifierMatrix(t *testing.T) {
	colors := []struct{ key, css, rgb string }{
		{"[#d81f26]", "#d81f26", "216 31 38"},
		{"[rgb(1_2_3)]", "rgb(1 2 3)", ""},
		{"[var(--x)]", "var(--x)", ""},
		{"[color:var(--x)]", "var(--x)", ""},
	}
	mods := []struct{ mod, alpha, pct string }{
		{"/15", "0.15", "15%"},
		{"/[.16]", ".16", "16%"},
		{"/[16%]", "16%", "16%"},
		{"/[0.5]", "0.5", "50%"},
		{"/(--my-alpha)", "var(--my-alpha)", "var(--my-alpha)"},
		{"/[var(--a)]", "var(--a)", "var(--a)"},
	}
	for _, fam := range colorFamilies {
		for _, c := range colors {
			for _, m := range mods {
				want := "color-mix(in oklab, " + c.css + " " + m.pct + ", transparent)"
				if c.rgb != "" {
					want = "rgb(" + c.rgb + " / " + m.alpha + ")"
				}
				for _, variant := range []string{"", "hover:", "md:", "dark:hover:"} {
					cls := variant + fam.prefix + "-" + c.key + m.mod
					g := NewTailwindGenerator()
					out := g.Generate(map[string]bool{cls: true})
					if out == "" {
						t.Errorf("%s: no rule", cls)
						continue
					}
					if strings.Contains(out, "]/[") || strings.Contains(out, "rgb(1_") {
						t.Errorf("%s: raw modifier or underscore leaked: %s", cls, out)
					}
					if why := unsafeRuleReason(out); why != "" {
						t.Errorf("%s: unsafe output (%s): %s", cls, why, out)
					}
					if !strings.Contains(out, fam.prop+want) {
						t.Errorf("%s: want %q in\n%s", cls, fam.prop+want, out)
					}
					if len(g.Dropped) != 0 {
						t.Errorf("%s: dropped: %v", cls, g.Dropped)
					}
				}
			}
		}
	}
}

func TestArbitraryColorWithoutModifier(t *testing.T) {
	cases := map[string]string{
		"bg-[color:var(--x)]":          "background-color: var(--x);",
		"fill-[color:var(--x)]":        "fill: var(--x);",
		"placeholder-[color:var(--x)]": "color: var(--x);",
		"caret-[rgb(1_2_3)]":           "caret-color: rgb(1 2 3);",
		"bg-[#d81f26]/[.16]":           "background-color: rgb(216 31 38 / .16);",
		"bg-white/[.07]":               "background-color: rgb(255 255 255 / .07);",
		"bg-[#d81f26]/15":              "background-color: rgb(216 31 38 / 0.15);",
	}
	for cls, want := range cases {
		g := NewTailwindGenerator()
		if out := g.Generate(map[string]bool{cls: true}); !strings.Contains(out, want) {
			t.Errorf("%s: want %q in %q", cls, want, out)
		}
	}
}

func TestIsArbitraryRequiresMatchingBrackets(t *testing.T) {
	yes := []string{"[#fff]", "[a[b]c]", "[content:'a]b']", `[a\]b]`, "[var(--x)]"}
	no := []string{"[#fff]/[.5]", "[a]b[c]", "[a", "a]", "[]]", "x", ""}
	for _, s := range yes {
		if !isArbitrary(s) {
			t.Errorf("isArbitrary(%q) = false", s)
		}
	}
	for _, s := range no {
		if isArbitrary(s) {
			t.Errorf("isArbitrary(%q) = true", s)
		}
	}
}

func TestParenAlphaAfterBracketedColor(t *testing.T) {
	got := parenToBracket("border-[rgb(1_2_3)]/(--my-alpha)")
	if want := "border-[rgb(1_2_3)]/[var(--my-alpha)]"; got != want {
		t.Errorf("parenToBracket = %q, want %q", got, want)
	}
}
