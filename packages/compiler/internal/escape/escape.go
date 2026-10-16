// Package escape provides the canonical HTML, attribute, and JavaScript string
// escaping used across the compiler. Every emitter that writes user content into
// HTML or generated JS must route through these functions so escaping stays
// consistent (one shared implementation, no drift).
package escape

import "strings"

var htmlReplacer = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	"\"", "&quot;",
	"'", "&#39;",
	// The \x1f unit separator is the internal array-item delimiter used by
	// SSREval's array evaluation. It is not content and must never reach HTML.
	"\x1f", "",
)

// HTML escapes a string for safe inclusion in HTML text content or attribute
// values. The full set (& < > " ') is escaped and the internal \x1f array
// separator is stripped.
func HTML(s string) string {
	return htmlReplacer.Replace(s)
}

// HTMLAttr is an alias for HTML: both escape the full set, so the result is
// safe in double-quoted attributes, single-quoted attributes, and text nodes.
func HTMLAttr(s string) string {
	return HTML(s)
}

// JSString escapes a string for safe embedding in a JS single-quoted string
// literal. The caller is responsible for adding the surrounding quotes.
func JSString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	// \x00 (not \0) so a following digit cannot form a legacy octal escape.
	s = strings.ReplaceAll(s, "\x00", `\x00`)
	// U+2028/U+2029 are line terminators in older JS engines and would break a
	// single-line string literal.
	s = strings.ReplaceAll(s, "\u2028", `\u2028`)
	s = strings.ReplaceAll(s, "\u2029", `\u2029`)
	return s
}

// JSStringDQ escapes a string for embedding as a double-quoted JS string
// literal and returns it wrapped in double quotes. Used when interpolating a
// JSON payload (e.g. props) directly into generated JS.
func JSStringDQ(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return `"` + s + `"`
}

// UnescapeJSString decodes the escape sequences inside a JS/TS string literal's
// raw source text into their literal characters. The lexer stores the raw text
// between the quotes with escapes intact, so a token like `a\nb` becomes the
// two-character sequence backslash-n; this turns it into an actual newline.
// Handled: \n \r \t \b \f \v \0 \\ \' \" \` \xNN \uXXXX \u{...} \<newline>
// (line continuation). Unknown escapes keep the backslash so no information is
// lost on inputs the decoder does not understand.
func UnescapeJSString(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		c := s[i+1]
		switch c {
		case 'n':
			b.WriteByte('\n')
			i++
		case 't':
			b.WriteByte('\t')
			i++
		case 'r':
			b.WriteByte('\r')
			i++
		case 'b':
			b.WriteByte('\b')
			i++
		case 'f':
			b.WriteByte('\f')
			i++
		case 'v':
			b.WriteByte('\v')
			i++
		case '0':
			b.WriteByte(0)
			i++
		case '\\':
			b.WriteByte('\\')
			i++
		case '\'':
			b.WriteByte('\'')
			i++
		case '"':
			b.WriteByte('"')
			i++
		case '`':
			b.WriteByte('`')
			i++
		case '\n':
			// Line continuation: the newline is elided.
			i++
		case '\r':
			i++
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
		case 'x':
			if i+3 < len(s) {
				if v, ok := decodeHex(s[i+2 : i+4]); ok {
					b.WriteByte(byte(v))
					i += 3
					continue
				}
			}
			b.WriteByte('\\')
		case 'u':
			if i+2 < len(s) && s[i+2] == '{' {
				// \u{1F600} - consume up to the closing brace.
				end := strings.IndexByte(s[i+3:], '}')
				if end >= 0 {
					if v, ok := decodeHex(s[i+3 : i+3+end]); ok {
						b.WriteRune(rune(v))
						i += 3 + end
						continue
					}
				}
			} else if i+5 < len(s) {
				if v, ok := decodeHex(s[i+2 : i+6]); ok {
					r := rune(v)
					// Combine a UTF-16 surrogate pair (two \uXXXX escapes).
					if r >= 0xD800 && r <= 0xDBFF && i+11 < len(s) && s[i+6] == '\\' && s[i+7] == 'u' {
						if lo, ok := decodeHex(s[i+8 : i+12]); ok && lo >= 0xDC00 && lo <= 0xDFFF {
							combined := ((int(r) - 0xD800) << 10) + (lo - 0xDC00) + 0x10000
							b.WriteRune(rune(combined))
							i += 11
							continue
						}
					}
					b.WriteRune(r)
					i += 5
					continue
				}
			}
			b.WriteByte('\\')
		default:
			b.WriteByte('\\')
		}
	}
	return b.String()
}

// decodeHex decodes a run of hex digits (any length) to an integer.
func decodeHex(hex string) (int, bool) {
	if hex == "" {
		return 0, false
	}
	var v int
	for _, c := range hex {
		v <<= 4
		switch {
		case c >= '0' && c <= '9':
			v += int(c - '0')
		case c >= 'a' && c <= 'f':
			v += int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v += int(c-'A') + 10
		default:
			return 0, false
		}
	}
	return v, true
}
