package css

import (
	"regexp"
	"strings"
)

// v4Utility resolves the utility families that follow Tailwind v4 semantics.
// It runs before the legacy chain in generateCSS. handled is true when the
// class belongs to one of these families, even if its value is invalid (css
// is then empty), so the legacy chain cannot produce a wrong rule for it.
func v4Utility(cls string, theme TailwindTheme) (css string, handled bool) {
	css, handled = v4UtilityRaw(cls, theme)
	return addVarFallbacks(css), handled
}

func v4UtilityRaw(cls string, theme TailwindTheme) (css string, handled bool) {
	neg := false
	base := cls
	if strings.HasPrefix(base, "-") && len(base) > 1 && base[1] != '-' {
		neg, base = true, base[1:]
	}
	if css, ok := arbitraryProperty(cls); ok {
		return css, true
	}
	if css, ok := transformUtility(base, neg, theme); ok {
		return css, true
	}
	if css, ok := spacingUtility(base, neg, theme); ok {
		return css, true
	}
	if css, ok := spaceUtility(base, neg, theme); ok {
		return css, true
	}
	if css, ok := outlineUtility(base, neg, theme); ok {
		return css, true
	}
	if css, ok := trackingUtility(base, neg, theme); ok {
		return css, true
	}
	if css, ok := miscValueUtility(base, neg); ok {
		return css, true
	}
	if css, ok := gridLineUtility(base, neg); ok {
		return css, true
	}
	if neg {
		return "", false
	}
	if base == "container" {
		return containerMarker, true
	}
	if css, ok := staticUtilities[base]; ok {
		return css, true
	}
	if css, ok := alignmentUtility(base); ok {
		return css, true
	}
	if css, ok := sizeUtility(base, theme); ok {
		return css, true
	}
	if css, ok := gridAutoUtility(base); ok {
		return css, true
	}
	if css, ok := gridTemplateUtility(base); ok {
		return css, true
	}
	if css, ok := flexUtility(base); ok {
		return css, true
	}
	if css, ok := aspectUtility(base); ok {
		return css, true
	}
	if css, ok := shadowUtility(base, theme); ok {
		return css, true
	}
	if css, ok := ringUtility(base, theme); ok {
		return css, true
	}
	if css, ok := borderUtility(base, theme); ok {
		return css, true
	}
	if css, ok := divideUtility(base, theme); ok {
		return css, true
	}
	if css, ok := backgroundGradientUtility(base, false); ok {
		return css, true
	}
	if css, ok := gradientStopUtility(base, theme); ok {
		return css, true
	}
	if css, ok := textUtility(base, theme); ok {
		return css, true
	}
	if css, ok := leadingUtility(base, theme); ok {
		return css, true
	}
	if css, ok := fontUtility(base, theme); ok {
		return css, true
	}
	if css, ok := animationUtility(base, theme); ok {
		return css, true
	}
	if css, ok := transitionUtility(base); ok {
		return css, true
	}
	return "", false
}

// arbitraryProperty resolves `[property:value]` utilities.
func arbitraryProperty(cls string) (string, bool) {
	if len(cls) < 5 || cls[0] != '[' || cls[len(cls)-1] != ']' {
		return "", false
	}
	prop, val, ok := strings.Cut(cls[1:len(cls)-1], ":")
	if !ok || val == "" || !arbitraryPropRe.MatchString(prop) {
		return "", false
	}
	return prop + ": " + normalizeArbitrary(val) + ";", true
}

var arbitraryPropRe = regexp.MustCompile(`^(--[A-Za-z0-9_-]+|-?[a-z][a-z0-9-]*)$`)

var bareTwVarRe = regexp.MustCompile(`var\((--tw-[a-z0-9-]+)\)`)

// addVarFallbacks gives every bare var(--tw-x) whose registered initial value
// is known an inline fallback, so the utilities still work where @property is
// unsupported or its registrations were not included.
func addVarFallbacks(css string) string {
	if !strings.Contains(css, "var(--tw-") {
		return css
	}
	return bareTwVarRe.ReplaceAllStringFunc(css, func(m string) string {
		name := m[len("var(") : len(m)-1]
		if p, ok := twPropertyByName[name]; ok && p.hasInit {
			return "var(" + name + ", " + p.initial + ")"
		}
		return m
	})
}
