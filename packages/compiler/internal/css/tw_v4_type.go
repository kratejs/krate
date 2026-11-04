package css

import (
	"strings"
)

// Typography utilities with Tailwind v4 semantics: font size with a
// line-height that leading-* can override, letter spacing, font families and
// weights, text color and decoration.

var defaultTracking = map[string]string{
	"tighter": "-0.05em", "tight": "-0.025em", "normal": "0em",
	"wide": "0.025em", "wider": "0.05em", "widest": "0.1em",
}

// parseTextSize splits a TextSizes entry into font-size and line-height. A
// bare value is a font size with no line-height.
func parseTextSize(val string) (fs, lh string) {
	if !strings.Contains(val, ":") {
		return strings.TrimSpace(val), ""
	}
	for _, d := range strings.Split(val, ";") {
		prop, v, ok := strings.Cut(d, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(prop) {
		case "font-size":
			fs = strings.TrimSpace(v)
		case "line-height":
			lh = strings.TrimSpace(v)
		}
	}
	return fs, lh
}

// leadingFallback converts a rem line-height to a ratio of its font size like
// v4 (1.25rem at 0.875rem is calc(1.25 / 0.875)) so it tracks the font size.
func leadingFallback(fs, lh string) string {
	if strings.HasSuffix(fs, "rem") && strings.HasSuffix(lh, "rem") {
		f, l := strings.TrimSuffix(fs, "rem"), strings.TrimSuffix(lh, "rem")
		if plainNumber(f) && plainNumber(l) {
			if f == l {
				return "1"
			}
			return "calc(" + l + " / " + f + ")"
		}
	}
	return lh
}

// lineHeightValue resolves a leading modifier or leading-* key.
func lineHeightValue(key string, theme TailwindTheme) (string, bool) {
	if v, isArb := unwrapArbitrary(key); isArb {
		return v, true
	}
	if v, ok := theme.LineHeight[key]; ok {
		return v, true
	}
	if v, ok := namedSpacing(key, theme); ok {
		return v, true
	}
	return spacingMultiple(key, theme)
}

// textUtility resolves text-*: alignment, overflow, size (with /leading),
// arbitrary size/color and color (with /alpha).
func textUtility(cls string, theme TailwindTheme) (string, bool) {
	key, ok := strings.CutPrefix(cls, "text-")
	if !ok || key == "" {
		return "", false
	}
	switch key {
	case "left", "center", "right", "justify":
		return "text-align: " + key + ";", true
	case "ellipsis", "clip":
		return "text-overflow: " + key + ";", true
	}
	base, mod := splitColorModifier(key)
	sizeCSS := func(fs, lh string, named bool) (string, bool) {
		if mod != "" {
			v, ok := lineHeightValue(mod, theme)
			if !ok {
				return "", false
			}
			return "font-size: " + fs + "; line-height: " + v + ";", true
		}
		if lh != "" && named {
			return "font-size: " + fs + "; line-height: var(--tw-leading, " + leadingFallback(fs, lh) + ");", true
		}
		return "font-size: " + fs + ";", true
	}
	if val, ok := theme.TextSizes[base]; ok {
		fs, lh := parseTextSize(val)
		if fs == "" {
			return "", false
		}
		return sizeCSS(fs, lh, true)
	}
	if inner, hint, isArb := arbitraryValue(base); isArb {
		switch hint {
		case "length", "size", "percentage", "number":
			return sizeCSS(inner, "", false)
		case "family":
			return "font-family: " + inner + ";", true
		case "color":
			return colorDecl("color", inner, mod)
		}
		if isVarOnly(inner) || looksLikeColor(inner) {
			return colorDecl("color", inner, mod)
		}
		return sizeCSS(inner, "", false)
	}
	if v, ok := colorValue(base, mod, theme); ok {
		return "color: " + v + ";", true
	}
	return "", false
}

// colorDecl applies an optional alpha modifier to a literal color.
func colorDecl(prop, color, alpha string) (string, bool) {
	if alpha != "" {
		a, ok := resolveAlpha(alpha)
		if !ok {
			return "", false
		}
		color = withAlpha(color, a)
	}
	return prop + ": " + color + ";", true
}

// isVarOnly reports whether v is exactly one var() call.
func isVarOnly(v string) bool {
	if !strings.HasPrefix(v, "var(") || !strings.HasSuffix(v, ")") {
		return false
	}
	depth := 0
	for i := 0; i < len(v); i++ {
		switch v[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && i != len(v)-1 {
				return false
			}
		}
	}
	return depth == 0
}

// leadingUtility resolves leading-*; it also sets --tw-leading so text-*
// size utilities defer to it regardless of order.
func leadingUtility(cls string, theme TailwindTheme) (string, bool) {
	key, ok := strings.CutPrefix(cls, "leading-")
	if !ok {
		return "", false
	}
	if key == "none" {
		return "--tw-leading: 1; line-height: 1;", true
	}
	v, ok := lineHeightValue(key, theme)
	if !ok {
		return "", false
	}
	return "--tw-leading: " + v + "; line-height: " + v + ";", true
}

// trackingUtility resolves tracking-* from the theme or the defaults.
func trackingUtility(cls string, neg bool, theme TailwindTheme) (string, bool) {
	key, ok := strings.CutPrefix(cls, "tracking-")
	if !ok {
		return "", false
	}
	v, ok := theme.Tracking[key]
	if !ok {
		v, ok = defaultTracking[key]
	}
	if !ok {
		v, ok = unwrapArbitrary(key)
	}
	if !ok {
		return "", false
	}
	if neg {
		v = negateLength(v)
	}
	return "--tw-tracking: " + v + "; letter-spacing: " + v + ";", true
}

// fontUtility resolves font-<weight>, font-<family> and arbitrary values. The
// other font-* families (stretch, feature settings) are left to their own
// handlers.
func fontUtility(cls string, theme TailwindTheme) (string, bool) {
	key, ok := strings.CutPrefix(cls, "font-")
	if !ok || key == "" {
		return "", false
	}
	weight := func(v string) string { return "--tw-font-weight: " + v + "; font-weight: " + v + ";" }
	if v, ok := theme.FontWeights[key]; ok {
		return weight(v), true
	}
	if v, ok := theme.FontFamily[key]; ok && v != "" {
		return "font-family: " + v + ";", true
	}
	if inner, hint, isArb := arbitraryValue(key); isArb {
		switch hint {
		case "family":
			return "font-family: " + inner + ";", true
		case "number", "weight":
			return weight(inner), true
		}
		if plainNumber(inner) || isVarOnly(inner) {
			return weight(inner), true
		}
		return "font-family: " + inner + ";", true
	}
	if v, ok := defaultFontFamily()[key]; ok {
		return "font-family: " + v + ";", true
	}
	return "", false
}
