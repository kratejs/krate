package css

import "testing"

func TestFlatAndDefaultCustomColors(t *testing.T) {
	theme := DefaultTailwindTheme()
	theme.Colors["brand"] = map[string]string{"DEFAULT": "#ff0000", "light": "#ffeeee"}

	if v, ok := colorValue("brand", "", theme); !ok || v != "#ff0000" {
		t.Errorf("bg-brand (DEFAULT) = %q, %v", v, ok)
	}
	if v, ok := colorValue("brand-light", "", theme); !ok || v != "#ffeeee" {
		t.Errorf("bg-brand-light = %q, %v", v, ok)
	}
}

func TestConfiguredFontSizeBareValue(t *testing.T) {
	theme := DefaultTailwindTheme()
	theme.TextSizes["huge"] = "4rem"
	if got := generateCSS("text-huge", theme); got != "font-size: 4rem;" {
		t.Errorf("bare configured font size = %q", got)
	}
	// A declaration block (the built-in form) keeps its size; the line-height
	// defers to --tw-leading so leading-* wins.
	if got := generateCSS("text-sm", theme); got != "font-size: 0.875rem; line-height: var(--tw-leading, calc(1.25 / 0.875));" {
		t.Errorf("declaration-block font size = %q", got)
	}
}
