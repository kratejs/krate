package css

import (
	"strings"
	"unicode/utf8"
)

// EscapeClass converts a Tailwind class name into a CSS-selector-safe
// identifier per the CSSOM `CSS.escape` algorithm, so selectors like
// `w-1/2`, `w-[100px]`, `bg-red-500/50`, `content-['a.b']` are valid.
// Rules (https://drafts.csswg.org/cssom/#serialize-an-identifier):
// - U+0000 -> U+FFFD
// - control chars (U+0001-U+001F, U+007F) -> \HEX
// - a leading digit -> \HEX
// - a digit in the second position when the first char is '-' -> \HEX
// - a lone "-" -> \-
//   - '-' '_' and ASCII alphanumerics are safe elsewhere
//   - everything else is backslash-escaped (with a trailing space when the
//     escaped char is a hex digit, to disambiguate)
func EscapeClass(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == 0:
			b.WriteRune('\uFFFD')
		case r >= 0x0001 && r <= 0x001F || r == 0x007F:
			writeHexEscape(&b, r)
		case i == 0 && r >= '0' && r <= '9':
			writeHexEscape(&b, r)
		case i == 1 && s[0] == '-' && r >= '0' && r <= '9':
			writeHexEscape(&b, r)
		case i == 0 && r == '-' && len(s) == 1:
			b.WriteString("\\-")
		case r >= 0x0080 || r == '-' || r == '_' ||
			(r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'):
			b.WriteRune(r)
		default:
			b.WriteByte('\\')
			b.WriteRune(r)
		}
		i += size
	}
	return b.String()
}

// writeHexEscape writes `\HEX ` (with the disambiguating trailing space).
func writeHexEscape(b *strings.Builder, r rune) {
	const hex = "0123456789abcdef"
	b.WriteByte('\\')
	// Emit the shortest hex form (max 6 digits).
	var buf [6]byte
	i := len(buf)
	v := int(r)
	if v == 0 {
		i--
		buf[i] = '0'
	}
	for v > 0 {
		i--
		buf[i] = hex[v&0xF]
		v >>= 4
	}
	b.Write(buf[i:])
	b.WriteByte(' ')
}
