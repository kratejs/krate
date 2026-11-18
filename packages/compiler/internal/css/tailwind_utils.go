package css

import (
	"strconv"
	"strings"
)

// unwrapArbitrary returns the inner text of an arbitrary `[..]` value,
// normalized like Tailwind v4 (see normalizeArbitrary), and whether v was a
// bracketed arbitrary value.
func unwrapArbitrary(v string) (string, bool) {
	if len(v) >= 2 && v[0] == '[' && v[len(v)-1] == ']' {
		return normalizeArbitrary(v[1 : len(v)-1]), true
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
			"string", "integer", "custom", "bg-size", "family-name", "generic-name",
			"absolute-size", "relative-size", "ratio", "vector":
			return strings.TrimSpace(inner[i+1:]), h, true
		}
	}
	return inner, "", true
}

// isArbitrary reports whether key is a bracketed arbitrary value.
func isArbitrary(key string) bool {
	return len(key) >= 2 && key[0] == '[' && key[len(key)-1] == ']'
}

// arbType returns the value of an arbitrary key and its data type: the type
// hint when one is given, else the first of types the value can be read as
// (Tailwind's inferDataType), else "". ok is false when key is not bracketed.
func arbType(key string, types ...string) (val, typ string, ok bool) {
	val, hint, ok := arbitraryValue(key)
	if !ok {
		return "", "", false
	}
	if hint != "" {
		return val, hint, true
	}
	return val, twInfer(val, types...), true
}

// arbColor resolves an arbitrary key with an optional /alpha modifier
// ("[a_b]/50") as a color whatever its type, the way Tailwind's color
// utilities and the color fallback of mixed utilities treat it.
func arbColor(key string) (string, bool) {
	base, mod := splitColorModifier(key)
	inner, _, isArb := arbitraryValue(base)
	if !isArb {
		return "", false
	}
	if mod == "" {
		return inner, true
	}
	a, ok := resolveAlpha(mod)
	if !ok {
		return "", false
	}
	return withAlpha(inner, a), true
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
		return isVarOnly(lv)
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
