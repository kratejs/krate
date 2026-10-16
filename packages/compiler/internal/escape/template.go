package escape

import (
	"strconv"
	"strings"
)

// TemplateRaw converts a template-literal raw segment to its cooked value,
// processing JavaScript escape sequences (\n, \t, \\, \`, \$, \xNN, \uNNNN,
// line continuations) exactly as the language does. Interpolations are already
// split out into Parts by the parser, so a raw segment never contains "${".
//
// A backslash that is not a recognised escape is dropped (JS semantics: "\z"
// cooks to "z"), which is why callers must not blanket-replace "\n" in emitted
// output — a literal backslash-n in source is written "\n" (cooked to "\n") and
// a real newline escape is written "\n" (cooked to a newline).
func TemplateRaw(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'v':
			b.WriteByte('\v')
		case '0':
			b.WriteByte(0)
		case '\\':
			b.WriteByte('\\')
		case '`':
			b.WriteByte('`')
		case '\'':
			b.WriteByte('\'')
		case '"':
			b.WriteByte('"')
		case '$':
			b.WriteByte('$')
		case '\n':
			// Line continuation: backslash + newline is removed.
		case '\r':
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
		case 'x':
			if i+2 < len(s) {
				if v, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
					b.WriteByte(byte(v))
					i += 2
					continue
				}
			}
			b.WriteByte('x')
		case 'u':
			if i+1 < len(s) && s[i+1] == '{' {
				j := i + 2
				for j < len(s) && s[j] != '}' {
					j++
				}
				if j < len(s) {
					if v, err := strconv.ParseUint(s[i+2:j], 16, 32); err == nil {
						b.WriteRune(rune(v))
						i = j
						continue
					}
				}
			} else if i+4 < len(s) {
				if v, err := strconv.ParseUint(s[i+1:i+5], 16, 32); err == nil {
					b.WriteRune(rune(v))
					i += 4
					continue
				}
			}
			b.WriteByte('u')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
