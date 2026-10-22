package htmlspec

import (
	"strings"
)

// StyleEntry is one `key: value` pair of a JSX style object.
type StyleEntry struct {
	Key   string
	Value Value
}

// unitlessProps are CSS properties (hyphenated, vendor prefix removed) whose
// numeric values are not given a px unit (React isUnitlessNumber).
var unitlessProps = map[string]bool{
	"animation-iteration-count": true, "aspect-ratio": true,
	"border-image-outset": true, "border-image-slice": true,
	"border-image-width": true, "box-flex": true, "box-flex-group": true,
	"box-ordinal-group": true, "column-count": true, "columns": true,
	"flex": true, "flex-grow": true, "flex-positive": true, "flex-shrink": true,
	"flex-negative": true, "flex-order": true, "grid-area": true,
	"grid-row": true, "grid-row-end": true, "grid-row-span": true,
	"grid-row-start": true, "grid-column": true, "grid-column-end": true,
	"grid-column-span": true, "grid-column-start": true, "font-weight": true,
	"line-clamp": true, "line-height": true, "opacity": true, "order": true,
	"orphans": true, "scale": true, "tab-size": true, "widows": true,
	"z-index": true, "zoom": true,
	// SVG-related.
	"fill-opacity": true, "flood-opacity": true, "stop-opacity": true,
	"stroke-dasharray": true, "stroke-dashoffset": true,
	"stroke-miterlimit": true, "stroke-opacity": true, "stroke-width": true,
}

// CSSPropertyName converts a style-object key to its CSS property name:
// custom properties (--x) and already-hyphenated names are kept, camelCase is
// hyphenated, and vendor prefixes become leading-dash forms (WebkitTransition
// -> -webkit-transition, msTransition -> -ms-transition).
func CSSPropertyName(key string) string {
	if strings.HasPrefix(key, "--") {
		return key
	}
	var b strings.Builder
	b.Grow(len(key) + 4)
	for i := 0; i < len(key); i++ {
		c := key[i]
		if c >= 'A' && c <= 'Z' {
			b.WriteByte('-')
			b.WriteByte(c + ('a' - 'A'))
			continue
		}
		b.WriteByte(c)
	}
	name := b.String()
	if strings.HasPrefix(name, "ms-") {
		name = "-" + name
	}
	return name
}

// isUnitless reports whether a hyphenated CSS property takes bare numbers.
func isUnitless(prop string) bool {
	if strings.HasPrefix(prop, "--") {
		return true
	}
	for _, p := range []string{"-webkit-", "-moz-", "-ms-", "-o-"} {
		if strings.HasPrefix(prop, p) {
			prop = prop[len(p):]
			break
		}
	}
	return unitlessProps[prop]
}

// StyleValue renders one style value for the hyphenated property prop. ok is
// false when React would skip the declaration (null, undefined, booleans and
// empty strings).
func StyleValue(prop string, v Value) (string, bool) {
	if v.IsNullish() {
		return "", false
	}
	switch v.Kind {
	case Bool, Bare:
		return "", false
	case Loose:
		if v.Str == "true" || v.Str == "false" {
			return "", false
		}
	}
	s := strings.TrimSpace(v.Str)
	if s == "" {
		return "", false
	}
	isNum := v.Kind == Number || (v.Kind == Loose && isNumeric(s))
	if isNum && s != "0" && !isUnitless(prop) {
		return s + "px", true
	}
	return s, true
}

// Style serializes a style object to a CSS declaration list
// ("font-size:12px;margin-top:4px"). The result is unescaped; Attr-style
// callers escape it with the rest of the attribute. It returns "" when no
// declaration survives, in which case the style attribute is omitted.
func Style(entries []StyleEntry) string {
	var b strings.Builder
	for _, e := range entries {
		if e.Key == "" {
			continue
		}
		prop := CSSPropertyName(e.Key)
		val, ok := StyleValue(prop, e.Value)
		if !ok {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(';')
		}
		b.WriteString(prop)
		b.WriteByte(':')
		b.WriteString(val)
	}
	return b.String()
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	digits := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			digits = true
		case c == '-' || c == '+':
			if i != 0 && s[i-1] != 'e' && s[i-1] != 'E' {
				return false
			}
		case c == '.' || c == 'e' || c == 'E':
		default:
			return false
		}
	}
	return digits
}
