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
	// A full declaration block (the built-in form) passes through unchanged.
	if got := generateCSS("text-sm", theme); got != theme.TextSizes["sm"] {
		t.Errorf("declaration-block font size = %q", got)
	}
}
