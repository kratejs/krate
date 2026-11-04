package css

import "strings"

// customUtilityCSS returns the declarations of a static `@utility` named base.
func customUtilityCSS(base string, theme TailwindTheme) string {
	if len(theme.CustomUtilities) == 0 {
		return ""
	}
	return theme.CustomUtilities[base]
}

// customVariant resolves a `@custom-variant` name. A selector template that
// starts with "&" and has no other "&" becomes a suffix on the utility
// selector; any other selector template wraps it; "@media"/"@supports" become
// at-rule variants.
func customVariant(name string, theme TailwindTheme) (variant, bool) {
	tmpl, ok := theme.CustomVariants[name]
	if !ok {
		return variant{}, false
	}
	switch {
	case strings.HasPrefix(tmpl, "@media"):
		return variant{kind: "media", at: strings.TrimSpace(tmpl[len("@media"):])}, true
	case strings.HasPrefix(tmpl, "@supports"):
		return variant{kind: "supports", at: strings.TrimSpace(tmpl[len("@supports"):])}, true
	case strings.HasPrefix(tmpl, "&") && strings.Count(tmpl, "&") == 1:
		return variant{kind: "pseudo", sel: tmpl[1:]}, true
	case strings.Count(tmpl, "&") == 1:
		return variant{kind: "ancestor", sel: strings.Replace(tmpl, "&", "PLACEHOLDER", 1)}, true
	}
	return variant{}, false
}
