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
		"@media (min-width: 768px){&{display: flex;}}",
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
