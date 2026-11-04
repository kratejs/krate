package css

import "strings"

// v4Utility resolves the utility families that follow Tailwind v4 semantics.
// It runs before the legacy chain in generateCSS. handled is true when the
// class belongs to one of these families, even if its value is invalid (css
// is then empty), so the legacy chain cannot produce a wrong rule for it.
func v4Utility(cls string, theme TailwindTheme) (css string, handled bool) {
	neg := false
	base := cls
	if strings.HasPrefix(base, "-") && len(base) > 1 && base[1] != '-' {
		neg, base = true, base[1:]
	}
	if css, ok := transformUtility(base, neg, theme); ok {
		return css, true
	}
	if neg {
		return "", false
	}
	if css, ok := animationUtility(base, theme); ok {
		return css, true
	}
	if css, ok := transitionUtility(base); ok {
		return css, true
	}
	return "", false
}
