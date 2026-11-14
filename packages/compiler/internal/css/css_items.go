package css

import "strings"

// cssItem is one top-level unit of a CSS body: a statement ended by ';' (a
// declaration or an at-rule statement) or a block `head { body }`.
type cssItem struct {
	head   string // text before ';' or '{', including leading trivia
	body   string // block body (without braces)
	block  bool
	closed bool // statement had its ';', block had its '}'
}

// splitCSSItems splits a body into items. Strings, comments, backslash
// escapes and parenthesised groups are skipped, so a ';' or brace inside them
// never splits anything. Concatenating every item (head plus its terminator or
// braces) reproduces the input exactly.
func splitCSSItems(s string) []cssItem {
	var items []cssItem
	n := len(s)
	start, paren := 0, 0
	for i := 0; i < n; {
		switch s[i] {
		case '\\':
			i += 2
		case '"', '\'':
			i = cssSkipString(s, i)
		case '/':
			if i+1 < n && s[i+1] == '*' {
				i = skipCSSComment(s, i)
			} else {
				i++
			}
		case '(':
			paren++
			i++
		case ')':
			if paren > 0 {
				paren--
			}
			i++
		case ';':
			if paren == 0 {
				items = append(items, cssItem{head: s[start:i], closed: true})
				start = i + 1
			}
			i++
		case '{':
			if paren != 0 {
				i++
				continue
			}
			end := matchBlockEnd(s, i)
			if end < 0 {
				items = append(items, cssItem{head: s[start:i], body: s[i+1:], block: true})
				return items
			}
			items = append(items, cssItem{head: s[start:i], body: s[i+1 : end], block: true, closed: true})
			i = end + 1
			start = i
		default:
			i++
		}
	}
	if start < n {
		items = append(items, cssItem{head: s[start:]})
	}
	return items
}

// matchBlockEnd returns the index of the '}' closing the '{' at s[open], or -1.
func matchBlockEnd(s string, open int) int {
	n := len(s)
	depth, paren := 1, 0
	for i := open + 1; i < n; {
		switch s[i] {
		case '\\':
			i += 2
			continue
		case '"', '\'':
			i = cssSkipString(s, i)
			continue
		case '/':
			if i+1 < n && s[i+1] == '*' {
				i = skipCSSComment(s, i)
				continue
			}
		case '(':
			paren++
		case ')':
			if paren > 0 {
				paren--
			}
		case '{':
			if paren == 0 {
				depth++
			}
		case '}':
			if paren == 0 {
				depth--
				if depth == 0 {
					return i
				}
			}
		}
		i++
	}
	return -1
}

// declColon returns the index of the first top-level ':' of a declaration, or
// -1. Strings, comments, escapes and parentheses are skipped.
func declColon(s string) int {
	n := len(s)
	paren := 0
	for i := 0; i < n; {
		switch s[i] {
		case '\\':
			i += 2
			continue
		case '"', '\'':
			i = cssSkipString(s, i)
			continue
		case '/':
			if i+1 < n && s[i+1] == '*' {
				i = skipCSSComment(s, i)
				continue
			}
		case '(':
			paren++
		case ')':
			if paren > 0 {
				paren--
			}
		case ':':
			if paren == 0 {
				return i
			}
		}
		i++
	}
	return -1
}

// parseDecl splits one declaration at its first top-level ':', returning the
// property name, value, and whether a colon existed.
func parseDecl(s string) (prop, val string, hasColon bool) {
	i := declColon(s)
	if i < 0 {
		return strings.TrimSpace(s), "", false
	}
	return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:]), true
}

// declKey returns the dedupe key of a property: standard properties are
// case-insensitive, custom properties are case-sensitive.
func declKey(prop string) string {
	if strings.HasPrefix(prop, "--") {
		return prop
	}
	return strings.ToLower(prop)
}

// splitImportant strips a trailing `!important` (outside strings, comments and
// escapes) from a declaration value.
func splitImportant(val string) (core string, important bool) {
	last := -1
	n := len(val)
	for i := 0; i < n; {
		switch val[i] {
		case '\\':
			i += 2
			continue
		case '"', '\'':
			i = cssSkipString(val, i)
			continue
		case '/':
			if i+1 < n && val[i+1] == '*' {
				i = skipCSSComment(val, i)
				continue
			}
		case '!':
			last = i
		}
		i++
	}
	if last < 0 || !strings.EqualFold(strings.TrimSpace(val[last+1:]), "important") {
		return val, false
	}
	return strings.TrimSpace(val[:last]), true
}

// stripCSSComments removes /* ... */ comments outside string literals. It is
// used only to derive a declaration's property name/value for deduplication,
// never for the emitted text (which keeps comments verbatim).
func stripCSSComments(s string) string {
	if !strings.Contains(s, "/*") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			i = skipCSSComment(s, i)
			b.WriteByte(' ')
		case c == '"' || c == '\'':
			j := cssSkipString(s, i)
			b.WriteString(s[i:j])
			i = j
		case c == '\\' && i+1 < len(s):
			b.WriteString(s[i : i+2])
			i += 2
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// splitLeadingTrivia splits a segment into leading whitespace/comments and the
// declaration core that follows.
func splitLeadingTrivia(s string) (prefix, core string) {
	i := 0
	for i < len(s) {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' {
			i++
			continue
		}
		if c == '/' && i+1 < len(s) && s[i+1] == '*' {
			i = skipCSSComment(s, i)
			continue
		}
		break
	}
	return s[:i], s[i:]
}
