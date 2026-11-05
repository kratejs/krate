package css

import (
	"strings"
	"testing"
)

func TestExpandApplyPlainUtilities(t *testing.T) {
	theme := NewTailwindGenerator().Theme
	out, unresolved := ExpandApply(".card {\n  @apply p-4 font-bold;\n}", theme)
	if len(unresolved) != 0 {
		t.Fatalf("unexpected unresolved: %v", unresolved)
	}
	if strings.Contains(out, "@apply") {
		t.Fatalf("@apply was not removed:\n%s", out)
	}
	if !strings.Contains(out, "padding") {
		t.Errorf("p-4 declaration missing:\n%s", out)
	}
	if !strings.Contains(out, "font-weight") {
		t.Errorf("font-bold declaration missing:\n%s", out)
	}
}

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

func TestExpandApplyDescendantAndImportant(t *testing.T) {
	theme := NewTailwindGenerator().Theme
	out, unresolved := ExpandApply(".row {\n  @apply space-x-2 focus:underline !important;\n}", theme)
	if len(unresolved) != 0 {
		t.Fatalf("unresolved: %v", unresolved)
	}
	for _, want := range []string{":where(& > :not(:last-child)){", "&:focus{text-decoration-line: underline !important;}"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestExpandApplyNestedInsideAtRule(t *testing.T) {
	theme := NewTailwindGenerator().Theme
	out, _ := ExpandApply("@media print {\n  .a { @apply dark:hidden; }\n  .b { @apply md:hover:flex; }\n}", theme)
	if strings.Count(out, "{") != strings.Count(out, "}") || !strings.Contains(out, "@media (prefers-color-scheme: dark){&{display: none;}} }") {
		t.Errorf("nested rules were not kept inside their own rule:\n%s", out)
	}
	if !strings.Contains(out, "@media (min-width: 768px){@media (hover: hover){&:hover{display: flex;}}}") {
		t.Errorf("stacked variants not nested:\n%s", out)
	}
}

func TestFunctionalUtility(t *testing.T) {
	g := NewTailwindGenerator()
	d := NewTailwindDirectives(t.TempDir())
	d.handleUtilityForTest("tab-*", "tab-size: --value(integer);")
	d.handleUtilityForTest("pad-*", "padding: --spacing(--value(integer));")
	d.handleUtilityForTest("card-bg-*", "background-color: --value(--color-red-*, [color]);")
	d.handleUtilityForTest("wide-*", "width: --value(\"full\", \"half\", [length], percentage);")
	d.ApplyTo(&g.Theme)
	out := g.Generate(map[string]bool{"tab-8": true, "pad-3": true, "card-bg-500": true, "card-bg-[#fff]": true, "wide-half": true, "wide-[7rem]": true, "wide-40%": true, "pad-x": true, "tab-1.5": true})
	for _, want := range []string{
		".tab-8{tab-size: 8;}",
		".pad-3{padding: 0.75rem;}",
		".card-bg-500{background-color:",
		"width: half;", "width: 7rem;", "width: 40%;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, not := range []string{"pad-x", "tab-1\\.5"} {
		if strings.Contains(out, not) {
			t.Errorf("%q must not match an integer-only value:\n%s", not, out)
		}
	}
	// A variant on a functional utility works like on any other.
	if v := g.Generate(map[string]bool{"md:tab-2": true}); !strings.Contains(v, "tab-size: 2;") {
		t.Errorf("variant of a functional utility:\n%s", v)
	}
	// And @apply can use it.
	applied, bad := g.ExpandApply(".c { @apply tab-4 pad-2; }")
	if len(bad) != 0 || !strings.Contains(applied, "tab-size: 4;") || !strings.Contains(applied, "padding: 0.5rem;") {
		t.Errorf("@apply of functional utilities: %q %v", applied, bad)
	}
}

func TestFunctionalUtilityModifier(t *testing.T) {
	g := NewTailwindGenerator()
	d := NewTailwindDirectives(t.TempDir())
	d.handleUtilityForTest("m-*", "margin: --value(integer); opacity: --modifier(number);")
	d.ApplyTo(&g.Theme)
	if len(d.Warnings()) != 0 {
		t.Fatalf("unexpected warnings: %v", d.Warnings())
	}
	out := g.Generate(map[string]bool{"m-4": true, "m-4/0.5": true, "m-4/x": true})
	for _, want := range []string{".m-4{margin: 4;}", "margin: 4; opacity: 0.5;"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "m-4\\/x") {
		t.Errorf("a modifier that does not resolve must not match:\n%s", out)
	}
}

func TestUtilityUnsupportedArgumentIsReported(t *testing.T) {
	d := NewTailwindDirectives(t.TempDir())
	d.handleUtilityForTest("c-*", "color: --value(color); width: --value([nope]);")
	if len(d.Warnings()) != 2 {
		t.Fatalf("warnings=%v", d.Warnings())
	}
}

func TestExpandApplyUnknownClass(t *testing.T) {
	theme := NewTailwindGenerator().Theme
	_, unresolved := ExpandApply(".x {\n  @apply zzz-nonexistent-utility;\n}", theme)
	if len(unresolved) != 1 {
		t.Fatalf("expected unknown class unresolved, got %v", unresolved)
	}
}

// The nested rules @apply writes survive the CSS minifier.
func TestApplyNestedRulesSurviveMinify(t *testing.T) {
	out, _ := ExpandApply(".btn { @apply hover:underline md:flex space-x-2; color: red; }", NewTailwindGenerator().Theme)
	min := Minify(out)
	// A rule that holds nested rules is kept as written; it must stay intact.
	if strings.Count(min, "{") != strings.Count(min, "}") {
		t.Errorf("unbalanced after minify:\n%s", min)
	}
	for _, want := range []string{"color: red;", "&:hover{text-decoration-line:underline}", "&{display:flex}", ":where(& > :not(:last-child)){"} {
		if !strings.Contains(min, want) {
			t.Errorf("minified output lost %q:\n%s", want, min)
		}
	}
}

// handleUtilityForTest feeds one `@utility name { body }` to handleUtility.
func (d *TailwindDirectives) handleUtilityForTest(name, body string) {
	d.handleUtility(cssAtRule{Name: "utility", Prelude: name, Body: body, HasBody: true})
}
