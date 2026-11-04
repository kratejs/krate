package css

import (
	"fmt"
	"strings"
)

// customUtilityCSS returns the declarations of a static `@utility` named base.
func customUtilityCSS(base string, theme TailwindTheme) string {
	if len(theme.CustomUtilities) == 0 {
		return ""
	}
	return theme.CustomUtilities[base]
}

// customVariant resolves a `@custom-variant` name. A selector template with
// exactly one "&" becomes the variant's selector template; "@media",
// "@supports" and "@container" templates become at-rule variants. Custom
// variants sort after the built-in ones, like Tailwind v4 registers them.
func customVariant(name string, theme TailwindTheme) (variant, bool) {
	tmpl, ok := theme.CustomVariants[name]
	if !ok {
		return variant{}, false
	}
	tmpl = strings.TrimSpace(tmpl)
	rank := fmt.Sprintf("%02d:custom:%s", arbitraryVariantOrder-1, name)
	switch {
	case strings.HasPrefix(tmpl, "@media"), strings.HasPrefix(tmpl, "@supports"), strings.HasPrefix(tmpl, "@container"):
		return variant{ats: []string{tmpl}, rank: rank, comp: compAt}, true
	case strings.HasPrefix(tmpl, "@"):
		return variant{ats: []string{tmpl}, rank: rank}, true
	case strings.Count(tmpl, "&") == 1:
		return variant{sel: tmpl, rank: rank, comp: compSel}, true
	}
	return variant{}, false
}
