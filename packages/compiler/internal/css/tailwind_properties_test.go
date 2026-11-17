package css

import (
	"strings"
	"testing"
)

func TestPropertiesFallbackLayer(t *testing.T) {
	g := NewTailwindGenerator()
	utilities := g.Generate(map[string]bool{"shadow-lg": true, "translate-x-4": true})
	sheet := assembleTailwindSheet(&g.Theme, &AtTheme{}, nil, utilities, g.KeyframesCSS(), false)

	if !strings.HasPrefix(sheet, "@layer properties, theme, base, components, utilities;") {
		t.Fatalf("properties must be declared as the first (weakest) layer:\n%s", sheet)
	}
	if n := strings.Count(sheet, "@layer properties{"); n != 1 {
		t.Fatalf("want one properties block, got %d:\n%s", n, sheet)
	}
	block := sheet[strings.Index(sheet, "@layer properties{"):]
	for _, want := range []string{
		propertiesFallbackSupports,
		"*,::before,::after,::backdrop{",
		"--tw-shadow:0 0 #0000;",
		"--tw-shadow-color:initial;",
		"--tw-translate-x:0;",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("properties block lacks %q:\n%s", want, block)
		}
	}

	// Rebuilding after appending more registrations (as the build does for
	// @apply) keeps a single block that covers the new variable.
	again := WithPropertiesFallback(sheet + "@property --tw-blur{syntax:\"*\";inherits:false}\n")
	if n := strings.Count(again, "@layer properties{"); n != 1 {
		t.Fatalf("rebuild left %d properties blocks:\n%s", n, again)
	}
	if !strings.Contains(again, "--tw-blur:initial;") {
		t.Errorf("rebuilt block lacks the appended registration:\n%s", again)
	}
	if strings.Count(again, "@layer properties, theme") != 1 {
		t.Errorf("layer order statement duplicated:\n%s", again)
	}
	if WithPropertiesFallback(again) != again {
		t.Error("WithPropertiesFallback is not idempotent")
	}
}

func TestPropertiesFallbackWithoutRegistrations(t *testing.T) {
	g := NewTailwindGenerator()
	utilities := g.Generate(map[string]bool{"flex": true})
	sheet := assembleTailwindSheet(&g.Theme, &AtTheme{}, nil, utilities, g.KeyframesCSS(), false)
	if strings.Contains(sheet, "properties") {
		t.Fatalf("no --tw-* variables are used, so no properties layer is needed:\n%s", sheet)
	}
	if got := WithPropertiesFallback("@property --tw-blur{syntax:\"*\";inherits:false}\n"); !strings.HasPrefix(got, "@layer properties;\n") {
		t.Fatalf("a sheet without the layer statement must still declare properties first:\n%s", got)
	}
}

func TestTypedPropertyRegistrations(t *testing.T) {
	g := NewTailwindGenerator()
	g.Generate(map[string]bool{"from-red-500": true, "shadow-lg/20": true})
	kf := g.KeyframesCSS()
	for _, want := range []string{
		`@property --tw-gradient-from{syntax:"<color>";inherits:false;initial-value:#0000}`,
		`@property --tw-gradient-from-position{syntax:"<length-percentage>";inherits:false;initial-value:0%}`,
		`@property --tw-shadow-alpha{syntax:"<percentage>";inherits:false;initial-value:100%}`,
		`@property --tw-shadow{syntax:"*";inherits:false;initial-value:0 0 #0000}`,
	} {
		if !strings.Contains(kf, want) {
			t.Errorf("missing %s in:\n%s", want, kf)
		}
	}
}

func TestPreflightPlaceholderFallback(t *testing.T) {
	pf := TailwindPreflight(DefaultTailwindTheme())
	want := "::placeholder{color:currentcolor}@supports (color:color-mix(in lab,red,red)){::placeholder{color:color-mix(in oklab,currentcolor 50%,transparent)}}"
	if !strings.Contains(pf, want) {
		t.Fatalf("preflight lacks the placeholder colour fallback:\n%s", pf)
	}
}
