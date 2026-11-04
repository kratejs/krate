package css

import (
	"strings"
	"testing"
)

func wantAll(t *testing.T, cls, out string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("%s: want %q in:\n%s", cls, w, out)
		}
	}
}

func wantNone(t *testing.T, cls, out string, bad ...string) {
	t.Helper()
	for _, w := range bad {
		if strings.Contains(out, w) {
			t.Errorf("%s: must not contain %q:\n%s", cls, w, out)
		}
	}
}

func TestAttributeVariantsTargetTheElement(t *testing.T) {
	tests := map[string]string{
		"data-[state=open]:p-4":           `.data-\[state\=open\]\:p-4[data-state="open"]{`,
		"data-foo:p-1":                    `.data-foo\:p-1[data-foo]{`,
		"aria-expanded:p-4":               `.aria-expanded\:p-4[aria-expanded="true"]{`,
		"aria-busy:p-1":                   `[aria-busy="true"]`,
		"aria-selected:p-1":               `[aria-selected="true"]`,
		"aria-[sort=ascending]:p-1":       `[aria-sort="ascending"]`,
		"has-[img]:p-4":                   `.has-\[img\]\:p-4:has(:is(img)){`,
		"has-checked:p-1":                 `:has(:checked){`,
		"has-[>img]:p-1":                  `:has(> img){`,
		"group-data-[x=y]:p-1":            `:is(:where(.group)[data-x="y"] *){`,
		"peer-data-[x=y]:p-1":             `:is(:where(.peer)[data-x="y"] ~ *){`,
		"group-aria-busy:p-1":             `:is(:where(.group)[aria-busy="true"] *){`,
		"group-has-[img]:p-1":             `:is(:where(.group):has(:is(img)) *){`,
		"peer-has-checked:p-1":            `:is(:where(.peer):has(:checked) ~ *){`,
		"in-focus:p-1":                    `:where(:focus) .in-focus\:p-1{`,
		"in-[.x]:p-1":                     `:where(:is(.x)) .in-\[\.x\]\:p-1{`,
		"not-first:p-1":                   `.not-first\:p-1:not(:first-child){`,
		"not-[.x]:p-1":                    `:not(:is(.x)){`,
		"not-data-[a]:p-1":                `:not([data-a]){`,
		"not-print:p-1":                   `@media not print{`,
		"not-supports-[display:grid]:p-1": `@supports not (display:grid){`,
	}
	for cls, want := range tests {
		out := gen(t, cls)
		wantAll(t, cls, out, want)
		wantNone(t, cls, out, "[data-state=open] .", "PLACEHOLDER")
	}
}

func TestNotHoverIsAUnion(t *testing.T) {
	out := gen(t, "not-hover:p-1")
	wantAll(t, "not-hover", out, `.not-hover\:p-1:not(:hover){`, `@media not (hover: hover){.not-hover\:p-1{`)
}

func TestHoverIsGatedByHoverMedia(t *testing.T) {
	out := gen(t, "hover:underline", "group-hover:underline")
	wantAll(t, "hover", out, `@media (hover: hover){.hover\:underline:hover{`,
		`@media (hover: hover){.group-hover\:underline:is(:where(.group):hover *){`)
}

func TestContainerQueryVariantsEmitContainerRules(t *testing.T) {
	tests := map[string]string{
		"@sm:p-4":           `@container (width >= 24rem){`,
		"@sm/main:p-4":      `@container main (width >= 24rem){`,
		"@max-md:p-4":       `@container (width < 28rem){`,
		"@min-md:p-4":       `@container (width >= 28rem){`,
		"@min-[400px]:p-4":  `@container (width >= 400px){`,
		"@max-[400px]:p-4":  `@container (width < 400px){`,
		"@[400px]:p-4":      `@container (width >= 400px){`,
		"@[400px]/main:p-4": `@container main (width >= 400px){`,
	}
	for cls, want := range tests {
		out := gen(t, cls)
		wantAll(t, cls, out, want)
		wantNone(t, cls, out, "@media")
	}
}

func TestContainerUtilities(t *testing.T) {
	tests := map[string]string{
		"@container":          `.\@container{container-type: inline-size;}`,
		"@container/main":     `container-type: inline-size; container-name: main;`,
		"@container-normal":   `container-type: normal;`,
		"@container-size/foo": `container-type: size; container-name: foo;`,
		"@container/[x]":      `container-name: x;`,
	}
	for cls, want := range tests {
		wantAll(t, cls, gen(t, cls), want)
	}
	if out := gen(t, "@container-bogus"); out != "" {
		t.Errorf("unknown container utility must produce nothing: %s", out)
	}
}

func TestContainerSizesFromTheme(t *testing.T) {
	g := NewTailwindGenerator()
	g.Theme.Containers = map[string]string{"card": "30rem"}
	out := g.Generate(map[string]bool{"@card:p-1": true, "@sm:p-1": true})
	wantAll(t, "containers", out, `@container (width >= 30rem){`, `@container (width >= 24rem){`)
}

// first reports whether a's rule is emitted before b's.
func first(t *testing.T, a, b string) bool {
	t.Helper()
	out := gen(t, a, b)
	ia, ib := indexOfRule(out, a), indexOfRule(out, b)
	if ia < 0 || ib < 0 {
		t.Fatalf("missing rule for %q or %q:\n%s", a, b, out)
	}
	return ia < ib
}

func TestVariantSortOrder(t *testing.T) {
	// The later pair member must win the cascade, so it is emitted later.
	pairs := [][2]string{
		{"hover:bg-blue-600", "disabled:bg-gray-300"},
		{"hover:p-1", "active:p-2"},
		{"hover:p-1", "focus:p-2"},
		{"focus:p-1", "focus-visible:p-2"},
		{"not-first:p-1", "first:p-2"},
		{"hover:p-1", "aria-expanded:p-2"},
		{"hover:p-1", "data-[x=y]:p-2"},
		{"md:p-1", "dark:p-2"},
		{"max-md:p-4", "max-sm:p-2"},
		{"motion-reduce:p-2", "md:p-1"},
		{"contrast-more:p-2", "md:p-1"},
		{"md:p-1", "print:p-2"},
		{"group-hover:p-1", "hover:p-2"},
		{"sm:p-1", "md:p-2"},
		{"disabled:p-1", "sm:hover:p-2"},
	}
	for _, p := range pairs {
		if !first(t, p[0], p[1]) {
			t.Errorf("%q must be emitted before %q", p[0], p[1])
		}
	}
}

func TestVariantSortBreakpointUnits(t *testing.T) {
	g := NewTailwindGenerator()
	g.Theme.Screens["3xl"] = "120rem"
	out := g.Generate(map[string]bool{"3xl:p-1": true, "sm:p-2": true, "2xl:p-3": true})
	i3, is, i2 := indexOfRule(out, "3xl:p-1"), indexOfRule(out, "sm:p-2"), indexOfRule(out, "2xl:p-3")
	if !(is < i2 && i2 < i3) {
		t.Errorf("breakpoints must sort by size across units (sm, 2xl, 3xl):\n%s", out)
	}
	// px and rem arbitrary values compare in px.
	out = gen(t, "min-[50rem]:p-1", "min-[700px]:p-2", "min-[10px]:p-3")
	a, b, c := indexOfRule(out, "min-[10px]:p-3"), indexOfRule(out, "min-[700px]:p-2"), indexOfRule(out, "min-[50rem]:p-1")
	if !(a < b && b < c) {
		t.Errorf("min-[10px] < min-[700px] < min-[50rem] expected:\n%s", out)
	}
}

func TestStackedVariantsApplyLeftToRight(t *testing.T) {
	tests := map[string]string{
		"hover:before:opacity-50": `.hover\:before\:opacity-50:hover::before{`,
		"before:hover:opacity-50": `.before\:hover\:opacity-50::before:hover{`,
		"*:hover:p-1":             `:is(.\*\:hover\:p-1 > *):hover{`,
		"hover:*:p-1":             `:is(.hover\:\*\:p-1:hover > *){`,
		"focus:hover:p-1":         `:focus:hover{`,
	}
	for cls, want := range tests {
		wantAll(t, cls, gen(t, cls), want)
	}
	out := gen(t, "sm:hover:dark:p-1")
	i1, i2, i3 := strings.Index(out, "min-width: 640px"), strings.Index(out, "hover: hover"), strings.Index(out, "prefers-color-scheme")
	if !(i1 >= 0 && i1 < i2 && i2 < i3) {
		t.Errorf("at-rules must nest in class order:\n%s", out)
	}
}

func TestVariantSelectorsAreValid(t *testing.T) {
	tests := map[string]string{
		"group-[.x]:p-1":              `:is(:where(.group):is(.x) *)`,
		"peer-[.x]:p-1":               `:is(:where(.peer):is(.x) ~ *)`,
		"nth-[2n+1]:p-1":              `:nth-child(2n+1)`,
		"nth-3:p-1":                   `:nth-child(3)`,
		"nth-last-2:p-1":              `:nth-last-child(2)`,
		"nth-of-type-2:p-1":           `:nth-of-type(2)`,
		"nth-last-of-type-[2n]:p-1":   `:nth-last-of-type(2n)`,
		"noscript:p-1":                `@media (scripting: none){`,
		"supports-[--x]:p-1":          `@supports (--x: var(--tw)){`,
		"supports-[display:grid]:p-1": `@supports (display:grid){`,
		"group-hover/item:p-1":        `:is(:where(.group\/item):hover *)`,
		"peer-checked/name:p-1":       `:is(:where(.peer\/name):checked ~ *)`,
	}
	for cls, want := range tests {
		wantAll(t, cls, gen(t, cls), want)
	}
	for _, bad := range []string{"group-foo:p-1", "peer-nonsense:p-1", "nth-abc:p-1", "sm:unknownvariant:p-1"} {
		if out := gen(t, bad); out != "" {
			t.Errorf("%s: unknown variant must produce nothing, got %s", bad, out)
		}
	}
}

func TestSmallVariantDifferences(t *testing.T) {
	tests := map[string]string{
		"starting:p-1":                  `@starting-style{.starting\:p-1{`,
		"open:p-1":                      `:is([open], :popover-open, :open)`,
		"rtl:p-1":                       `:where(:dir(rtl), [dir="rtl"], [dir="rtl"] *)`,
		"ltr:p-1":                       `:where(:dir(ltr), [dir="ltr"], [dir="ltr"] *)`,
		"inert:p-1":                     `:is([inert], [inert] *)`,
		"marker:p-1":                    `.marker\:p-1::marker`,
		"selection:p-1":                 `.selection\:p-1::selection`,
		"min-[900px]:p-1":               `@media (min-width: 900px){`,
		"max-[600px]:p-1":               `@media (width < 600px){`,
		"[&_p]:p-1":                     `.\[\&_p\]\:p-1 p{`,
		"[&:nth-child(3)]:p-1":          `:nth-child(3){`,
		"[@media(min-width:900px)]:p-1": `@media (min-width:900px){`,
		"optional:p-1":                  `:optional{`,
		"user-valid:p-1":                `:user-valid{`,
		"details-content:p-1":           `::details-content{`,
		"inverted-colors:p-1":           `@media (inverted-colors: inverted){`,
	}
	for cls, want := range tests {
		wantAll(t, cls, gen(t, cls), want)
	}
	// marker: also matches the list's children, selection: the descendants.
	wantAll(t, "marker", gen(t, "marker:p-1"), `.marker\:p-1 *::marker`)
	wantAll(t, "selection", gen(t, "selection:p-1"), `.selection\:p-1 *::selection`)
}

func TestArbitraryVariantUnderscoreIsSpace(t *testing.T) {
	out := gen(t, "[&_p]:p-1", `[&_a:hover]:p-1`)
	wantAll(t, "arbitrary", out, `.\[\&_p\]\:p-1 p{`, ` a:hover{`)
}

func TestDarkSelectorFromTheme(t *testing.T) {
	g := NewTailwindGenerator()
	g.Theme.DarkMode = "class"
	out := g.Generate(map[string]bool{"dark:p-1": true})
	wantAll(t, "dark default", out, `.dark\:p-1:where(.dark, .dark *){`)

	g.Theme.DarkSelector = "[data-mode=dark]"
	out = g.Generate(map[string]bool{"dark:p-1": true})
	wantAll(t, "dark selector", out, `:where([data-mode=dark], [data-mode=dark] *){`)
	wantNone(t, "dark selector", out, ".dark ")
}

func TestPrefixConfig(t *testing.T) {
	g := NewTailwindGenerator()
	g.Theme.Prefix = "tw"
	out := g.Generate(map[string]bool{"tw:flex": true, "tw:hover:p-1": true, "flex": true, "tw:group-hover:p-2": true})
	wantAll(t, "prefix", out, `.tw\:flex{display: flex;}`, `.tw\:hover\:p-1:hover{`,
		`:is(:where(.tw\:group):hover *)`)
	if strings.Contains(out, ".flex{") {
		t.Errorf("an unprefixed utility must not match when a prefix is set:\n%s", out)
	}
	g.Strict = true
	g.Generate(map[string]bool{"flex": true})
	if len(g.Unknown) != 1 || g.Unknown[0] != "flex" {
		t.Errorf("unprefixed class must be reported as unknown, got %v", g.Unknown)
	}
}

func TestImportantConfigAndSuffix(t *testing.T) {
	out := gen(t, "p-4!", "!p-2", "hover:m-1!")
	wantAll(t, "important", out, `.p-4\!{padding: 1rem !important;}`, `.\!p-2{padding: 0.5rem !important;}`,
		`.hover\:m-1\!:hover{margin: 0.25rem !important;}`)

	g := NewTailwindGenerator()
	g.Theme.Important = "true"
	wantAll(t, "important true", g.Generate(map[string]bool{"p-4": true}), `.p-4{padding: 1rem !important;}`)

	g.Theme.Important = "#app"
	out = g.Generate(map[string]bool{"p-4": true, "hover:p-1": true})
	wantAll(t, "important selector", out, `#app .p-4{padding: 1rem;}`, `#app .hover\:p-1:hover{`)
}

func TestSortKeyDeterministic(t *testing.T) {
	set := map[string]bool{}
	for _, c := range []string{"hover:p-1", "p-2", "md:p-3", "dark:p-4", "px-2", "pl-4", "not-hover:p-5"} {
		set[c] = true
	}
	a := NewTailwindGenerator().Generate(set)
	for i := 0; i < 20; i++ {
		if b := NewTailwindGenerator().Generate(set); a != b {
			t.Fatalf("non-deterministic output:\n%s\n---\n%s", a, b)
		}
	}
}
