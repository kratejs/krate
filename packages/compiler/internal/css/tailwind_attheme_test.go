package css

import (
	"strings"
	"testing"
)

func TestExtractAndMergeAtTheme(t *testing.T) {
	vars := ExtractAtTheme(`@theme { --color-brand-500: #ff0000; --color-accent: #00ff00; --spacing-7: 1.75rem; --radius-xl: 1rem; --breakpoint-3xl: 120rem; }`)
	if vars["color-brand-500"] != "#ff0000" || vars["spacing-7"] != "1.75rem" {
		t.Fatalf("extracted vars wrong: %v", vars)
	}

	theme := DefaultTailwindTheme()
	MergeAtTheme(&theme, vars)

	if v, ok := colorValue("brand-500", "", theme); !ok || v != "#ff0000" {
		t.Errorf("brand-500 = %q, %v", v, ok)
	}
	if v, ok := colorValue("accent", "", theme); !ok || v != "#00ff00" {
		t.Errorf("accent (DEFAULT) = %q, %v", v, ok)
	}
	if theme.Screens["3xl"] != "120rem" {
		t.Errorf("breakpoint 3xl = %q", theme.Screens["3xl"])
	}
	if got := generateCSS("p-7", theme); !strings.Contains(got, "1.75rem") {
		t.Errorf("p-7 from @theme spacing = %q", got)
	}
}
