package css

import (
	"strconv"
	"strings"
)

// unwrapArbitrary returns the inner text of an arbitrary `[..]` value with
// underscores converted to spaces (Tailwind's convention), and whether v was a
// bracketed arbitrary value.
func unwrapArbitrary(v string) (string, bool) {
	if len(v) >= 2 && v[0] == '[' && v[len(v)-1] == ']' {
		return strings.ReplaceAll(v[1:len(v)-1], "_", " "), true
	}
	return "", false
}

// arbitraryValue returns the inner value of an arbitrary token, splitting off a
// leading CSS type hint (`[color:...]`, `[length:...]`, `[url:...]`, ...).
// ok is false when v is not bracketed.
func arbitraryValue(v string) (val, hint string, ok bool) {
	inner, isArb := unwrapArbitrary(v)
	if !isArb {
		return "", "", false
	}
	if i := strings.IndexByte(inner, ':'); i > 0 {
		h := inner[:i]
		switch h {
		case "color", "length", "size", "image", "position", "url", "number",
			"percentage", "family", "angle", "line-width", "shadow", "transform",
			"string", "integer", "custom":
			return strings.TrimSpace(inner[i+1:]), h, true
		}
	}
	return inner, "", true
}

// looksLikeColor reports whether a bare CSS value is most likely a color, used
// to disambiguate arbitrary values for utilities that accept multiple types
// (text-[14px] is a font-size, text-[#fff] is a color).
func looksLikeColor(v string) bool {
	lv := strings.ToLower(strings.TrimSpace(v))
	if lv == "" {
		return false
	}
	for _, p := range []string{
		"#", "rgb(", "rgba(", "hsl(", "hsla(", "hwb(", "oklch(", "oklab(",
		"lab(", "lch(", "color(", "color-mix(",
	} {
		if strings.HasPrefix(lv, p) {
			return true
		}
	}
	switch lv {
	case "transparent", "currentcolor", "white", "black", "inherit", "revert":
		return true
	}
	if strings.HasPrefix(lv, "var(") {
		return strings.Contains(lv, "color")
	}
	return false
}

// looksLikeLength reports whether a bare CSS value is most likely a length or
// number (as opposed to a color or keyword).
func looksLikeLength(v string) bool {
	lv := strings.ToLower(strings.TrimSpace(v))
	if lv == "" {
		return false
	}
	for _, p := range []string{"calc(", "min(", "max(", "clamp(", "length:"} {
		if strings.HasPrefix(lv, p) {
			return true
		}
	}
	if strings.HasPrefix(lv, "var(") {
		return !strings.Contains(lv, "color")
	}
	i := 0
	for i < len(lv) && (lv[i] >= '0' && lv[i] <= '9' || lv[i] == '.' || lv[i] == '-' || lv[i] == '+') {
		i++
	}
	if i == 0 {
		return false
	}
	rest := lv[i:]
	if rest == "" || rest == "%" {
		return true
	}
	for _, u := range []string{
		"px", "rem", "em", "ex", "ch", "vh", "vw", "vmin", "vmax", "svh", "lvh",
		"dvh", "svw", "lvw", "dvw", "pt", "pc", "in", "cm", "mm", "fr", "cqw",
		"cqh", "cqi", "cqb", "cqmin", "cqmax", "turn", "deg", "rad", "grad", "s", "ms",
	} {
		if rest == u {
			return true
		}
	}
	return false
}

// isDigits reports whether s is one or more ASCII digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// formatRem formats a numeric spacing multiple (n x 0.25rem) without float
// noise: 13 -> "3.25rem", 13.5 -> "3.375rem", 0.5 -> "0.125rem".
func formatRem(n float64) string {
	return strconv.FormatFloat(n/4, 'f', -1, 64) + "rem"
}

// angleValue resolves a rotate/skew value: arbitrary values pass through; bare
// numbers get a `deg` suffix.
func angleValue(key string) (string, bool) {
	if v, ok := unwrapArbitrary(key); ok {
		return v, true
	}
	if _, err := strconv.ParseFloat(key, 64); err == nil {
		return key + "deg", true
	}
	return "", false
}

// scaleFactor resolves a scale key to a unitless factor: "95" -> "0.95",
// "110" -> "1.1", "[1.7]" -> "1.7".
func scaleFactor(key string) string {
	if v, ok := unwrapArbitrary(key); ok {
		return v
	}
	if f, err := strconv.ParseFloat(key, 64); err == nil {
		return strconv.FormatFloat(f/100, 'f', -1, 64)
	}
	return key
}

// heightValue resolves a height utility key, mapping `screen` to 100vh (not
// 100vw) and supporting the dynamic viewport units.
func heightValue(key string, theme TailwindTheme) string {
	switch key {
	case "screen":
		return "100vh"
	case "svh":
		return "100svh"
	case "lvh":
		return "100lvh"
	case "dvh":
		return "100dvh"
	case "auto":
		return "auto"
	case "min":
		return "min-content"
	case "max":
		return "max-content"
	case "fit":
		return "fit-content"
	}
	if v, ok := unwrapArbitrary(key); ok {
		return v
	}
	return sizingValue(key, theme)
}

// gradientStop resolves a gradient color-stop utility (`from-*`, `via-*`,
// `to-*`) to a CSS color. It accepts named colors, shade colors, an optional
// alpha modifier (`/50`, `/[0.42]`), and arbitrary values. A bare percentage
// (`from-10%`) is a position, not a color, and is not handled here.
func gradientStop(cls, prefix string, theme TailwindTheme) (string, bool) {
	rest, ok := strings.CutPrefix(cls, prefix+"-")
	if !ok || rest == "" {
		return "", false
	}
	if v, hint, isArb := arbitraryValue(rest); isArb {
		if hint == "color" {
			return v, true
		}
		if looksLikeColor(v) || !looksLikeLength(v) {
			return v, true
		}
		return "", false
	}
	key, alpha := splitColorModifier(rest)
	if v, ok := colorValue(key, alpha, theme); ok {
		return v, true
	}
	return "", false
}

// widthValue resolves a width utility key, supporting the viewport-relative
// dynamic units in addition to the shared sizing scale.
func widthValue(key string, theme TailwindTheme) string {
	switch key {
	case "screen":
		return "100vw"
	case "svw":
		return "100svw"
	case "lvw":
		return "100lvw"
	case "dvw":
		return "100dvw"
	}
	if v, ok := unwrapArbitrary(key); ok {
		return v
	}
	return sizingValue(key, theme)
}
