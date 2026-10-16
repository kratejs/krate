package css

import (
	"strconv"
	"strings"
)

// cssLiteralProtector replaces string literals, `url(...)` tokens, and `/*! ... */`
// license comments with opaque placeholders so the minifier's whitespace and
// value-level transforms cannot rewrite their contents. `restore` puts the
// originals back at the end.
type cssLiteralProtector struct {
	items []string
}

func (p *cssLiteralProtector) token(i int) string {
	return "\x00lit" + strconv.Itoa(i) + "\x00"
}

func (p *cssLiteralProtector) add(lit string) string {
	i := len(p.items)
	p.items = append(p.items, lit)
	return p.token(i)
}

func (p *cssLiteralProtector) protect(s string) string {
	var out strings.Builder
	i, n := 0, len(s)
	for i < n {
		c := s[i]
		switch {
		case c == '/' && strings.HasPrefix(s[i:], "/*!"):
			// License comment: tokenize so it survives comment stripping.
			j := strings.Index(s[i+2:], "*/")
			if j < 0 {
				out.WriteString(s[i:])
				i = n
				continue
			}
			end := i + 2 + j + 2
			out.WriteString(p.add(s[i:end]))
			i = end
		case c == '/' && i+1 < n && s[i+1] == '*':
			// Ordinary comment: copy verbatim and do NOT interpret its contents.
			// A quote inside a comment (e.g. "doesn't") must not be treated as a
			// string start, which would swallow the rest of the stylesheet.
			j := strings.Index(s[i+2:], "*/")
			if j < 0 {
				out.WriteString(s[i:])
				i = n
				continue
			}
			end := i + 2 + j + 2
			out.WriteString(s[i:end])
			i = end
		case c == '"' || c == '\'':
			j := cssSkipString(s, i)
			out.WriteString(p.add(s[i:j]))
			i = j
		case (c == 'u' || c == 'U') && hasPrefixFold(s[i:], "url("):
			k := i + 4
			depth := 1
			for k < n {
				if s[k] == '\\' {
					k += 2
					continue
				}
				if s[k] == '(' {
					depth++
				} else if s[k] == ')' {
					depth--
					if depth == 0 {
						k++
						break
					}
				}
				k++
			}
			out.WriteString(p.add(s[i:k]))
			i = k
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}

func (p *cssLiteralProtector) restore(s string) string {
	for i, lit := range p.items {
		s = strings.ReplaceAll(s, p.token(i), lit)
	}
	return s
}

// collapseCSS strips comments and collapses whitespace. It is only safe after
// literals have been protected (no strings/urls remain to be corrupted).
func collapseCSS(css string) string {
	var b strings.Builder
	b.Grow(len(css))
	i, n := 0, len(css)
	inBlock := false
	wasSpace := false
	for i < n {
		ch := css[i]
		switch {
		case ch == '/' && i+1 < n && css[i+1] == '*':
			inBlock = true
			i += 2
		case ch == '*' && i+1 < n && css[i+1] == '/':
			inBlock = false
			i += 2
		case inBlock:
			i++
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
			if !wasSpace {
				b.WriteByte(' ')
				wasSpace = true
			}
			i++
		case ch == '{' || ch == '}' || ch == ';' || ch == ',':
			b.WriteByte(ch)
			wasSpace = false
			i++
		default:
			b.WriteByte(ch)
			wasSpace = false
			i++
		}
	}
	return strings.TrimSpace(b.String())
}

// hasPrefixFold reports whether s begins with prefix, ASCII case-insensitively.
func hasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		a, b := s[i], prefix[i]
		if a >= 'A' && a <= 'Z' {
			a += 'a' - 'A'
		}
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}
