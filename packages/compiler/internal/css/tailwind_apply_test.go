package css

import (
	"strings"
	"testing"
)

func TestExpandApplyVariantsBecomeNestedRules(t *testing.T) {
	theme := NewTailwindGenerator().Theme
	out, unresolved := ExpandApply(".btn {\n  @apply px-4 hover:bg-red-500 md:flex;\n  color: red;\n}", theme)
	if len(unresolved) != 0 {
		t.Fatalf("nothing should be unresolved, got %v", unresolved)
	}
	for _, want := range []string{
		"padding-inline: 1rem",
		"@media (hover: hover){&:hover{background-color:",
		"@media (min-width: 48rem){&{display: flex;}}",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "@apply") {
		t.Errorf("@apply was left in:\n%s", out)
	}
	// Declarations stay ahead of nested rules, including one written after the
	// @apply line: older nesting implementations drop declarations that follow
	// a nested rule.
	if strings.Index(out, "color: red") > strings.Index(out, "@media") {
		t.Errorf("a declaration after @apply ended up behind the nested rules:\n%s", out)
	}
	if strings.Count(out, "{") != strings.Count(out, "}") {
		t.Errorf("unbalanced braces:\n%s", out)
	}
}

func TestExpandApplyKeepsSupportsFallbacksInline(t *testing.T) {
	theme := NewTailwindGenerator().Theme
	out, _ := ExpandApply(".card{@apply shadow-red-500 md:shadow-lg/20; color: red}", theme)
	// shadow-red-500 carries a nested @supports; it stays where @apply stood
	// instead of moving behind the declarations as a separate &{} rule.
	if !strings.HasPrefix(out, ".card{--tw-shadow-color: oklch(63.7% 0.237 25.331); @supports (color: color-mix(in lab, red, red)){") {
		t.Errorf("shadow-red-500 was not inlined:\n%s", out)
	}
	// The last declaration has no semicolon; the nested rule must not run into it.
	if !strings.Contains(out, "color: red; @media (min-width: 48rem){&{") {
		t.Errorf("nested rule glued onto the last declaration:\n%s", out)
	}
}
