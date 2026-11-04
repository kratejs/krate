package css

import "strings"

// skipComment returns the index just past a /* */ comment starting at s[i].
func skipComment(s string, i int) int {
	j := strings.Index(s[i+2:], "*/")
	if j < 0 {
		return len(s)
	}
	return i + 2 + j + 2
}

// hasPrefixFold reports whether s[i:] starts with the lowercase ASCII word.
func hasFoldAt(s string, i int, word string) bool {
	if i+len(word) > len(s) {
		return false
	}
	return strings.EqualFold(s[i:i+len(word)], word)
}

// stmtEnd returns the index just past the ';' ending the at-rule statement
// that starts at s[i], honouring strings, comments and parentheses. It returns
// (-1, false) when a '{' block starts first (the at-rule is a block rule) and
// (len(s), true) when the statement runs to the end of the input.
func stmtEnd(s string, i int) (int, bool) {
	depth := 0
	for i < len(s) {
		switch c := s[i]; {
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			i = skipComment(s, i)
			continue
		case c == '"' || c == '\'':
			i = cssSkipString(s, i)
			continue
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			if depth > 0 {
				depth--
			}
		case c == '{' && depth == 0:
			return -1, false
		case c == ';' && depth == 0:
			return i + 1, true
		}
		i++
	}
	return len(s), true
}

// isWordEnd reports whether s[i] ends a keyword (end of input or a non-ident
// byte).
func isWordEnd(s string, i int) bool {
	return i >= len(s) || !isIdentChar(s[i])
}

// unquoteCSSString returns the contents of the quoted string literal that
// starts at s[i] and the index just past it. Backslash escapes other than a
// line continuation are kept as the escaped character.
func unquoteCSSString(s string, i int) (string, int) {
	end := cssSkipString(s, i)
	inner := s[i+1 : end]
	if end > i+1 && end <= len(s) && s[end-1] == s[i] {
		inner = s[i+1 : end-1]
	}
	if !strings.Contains(inner, "\\") {
		return inner, end
	}
	var b strings.Builder
	for k := 0; k < len(inner); k++ {
		if inner[k] == '\\' && k+1 < len(inner) {
			k++
			if inner[k] == '\n' {
				continue
			}
		}
		b.WriteByte(inner[k])
	}
	return b.String(), end
}

// parseURLToken parses a `url(...)` token whose "url" keyword starts at s[i].
// It returns the reference, the quote byte used (0 when unquoted) and the
// index just past the closing parenthesis. ok is false when s[i:] is not a
// well-formed url token.
func parseURLToken(s string, i int) (ref string, quote byte, end int, ok bool) {
	if !hasFoldAt(s, i, "url(") {
		return "", 0, 0, false
	}
	j := i + 4
	for j < len(s) && isCSSSpace(s[j]) {
		j++
	}
	if j >= len(s) {
		return "", 0, 0, false
	}
	if s[j] == '"' || s[j] == '\'' {
		quote = s[j]
		var k int
		ref, k = unquoteCSSString(s, j)
		for k < len(s) && isCSSSpace(s[k]) {
			k++
		}
		if k >= len(s) || s[k] != ')' {
			return "", 0, 0, false
		}
		return ref, quote, k + 1, true
	}
	k := j
	for k < len(s) && s[k] != ')' {
		if s[k] == '"' || s[k] == '\'' || s[k] == '(' {
			return "", 0, 0, false
		}
		k++
	}
	if k >= len(s) {
		return "", 0, 0, false
	}
	return strings.TrimSpace(s[j:k]), 0, k + 1, true
}

func isCSSSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

// RewriteURLs calls fn for every `url(...)` reference in css (quoted or bare,
// any case) and replaces the reference with fn's result when ok is true.
// Comments, other strings and `@import` statements are left untouched.
func RewriteURLs(css string, fn func(ref string) (string, bool)) string {
	if !strings.Contains(strings.ToLower(css), "url(") {
		return css
	}
	var out strings.Builder
	i, last := 0, 0
	for i < len(css) {
		c := css[i]
		switch {
		case c == '/' && i+1 < len(css) && css[i+1] == '*':
			i = skipComment(css, i)
		case c == '"' || c == '\'':
			i = cssSkipString(css, i)
		case c == '@' && hasFoldAt(css, i, "@import") && isWordEnd(css, i+len("@import")):
			end, _ := stmtEnd(css, i)
			if end < 0 {
				end = i + 1
			}
			i = end
		case (c == 'u' || c == 'U') && (i == 0 || !isIdentChar(css[i-1])):
			ref, quote, end, ok := parseURLToken(css, i)
			if !ok {
				i++
				continue
			}
			repl, changed := fn(ref)
			if changed {
				out.WriteString(css[last:i])
				out.WriteString("url(")
				if quote == 0 && strings.ContainsAny(repl, " \t\n()'\"\\") {
					quote = '"'
				}
				if quote != 0 {
					out.WriteByte(quote)
				}
				out.WriteString(repl)
				if quote != 0 {
					out.WriteByte(quote)
				}
				out.WriteByte(')')
				last = end
			}
			i = end
		default:
			i++
		}
	}
	out.WriteString(css[last:])
	return out.String()
}
