package native

import "strings"

// matchBrace returns the offset just past the `}` that closes the `{` at
// open, or -1. It skips strings, template literals and comments. A `}` inside
// a regular expression literal is not recognized.
func matchBrace(src string, open int) int {
	depth := 0
	for i := open; i < len(src); i++ {
		switch c := src[i]; c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		case '"', '\'':
			i = skipQuoted(src, i)
		case '`':
			i = skipTemplate(src, i)
		case '/':
			if i+1 < len(src) {
				switch src[i+1] {
				case '/':
					if j := strings.IndexByte(src[i:], '\n'); j >= 0 {
						i += j
					} else {
						return -1
					}
				case '*':
					j := strings.Index(src[i+2:], "*/")
					if j < 0 {
						return -1
					}
					i += j + 3
				}
			}
		}
		if i < 0 {
			return -1
		}
	}
	return -1
}

// skipQuoted returns the index of the closing quote of the string at i.
func skipQuoted(src string, i int) int {
	q := src[i]
	for i++; i < len(src); i++ {
		switch src[i] {
		case '\\':
			i++
		case q:
			return i
		case '\n':
			return i
		}
	}
	return len(src)
}

// skipTemplate returns the index of the closing backtick of the template at i.
func skipTemplate(src string, i int) int {
	for i++; i < len(src); i++ {
		switch src[i] {
		case '\\':
			i++
		case '`':
			return i
		case '$':
			if i+1 < len(src) && src[i+1] == '{' {
				end := matchBrace(src, i+1)
				if end < 0 {
					return len(src)
				}
				i = end - 1
			}
		}
	}
	return len(src)
}

// bodyOpen finds the offset of the `{` that opens a function body whose first
// statement starts at firstStmt (or, for an empty body, after start).
func bodyOpen(src string, start, firstStmt int, hasStmt bool) int {
	if hasStmt && firstStmt > 0 && firstStmt <= len(src) {
		j := firstStmt - 1
		for j >= 0 && strings.ContainsRune(" \t\r\n", rune(src[j])) {
			j--
		}
		if j >= 0 && src[j] == '{' {
			return j
		}
		if k := strings.LastIndexByte(src[:firstStmt], '{'); k >= start {
			return k
		}
	}
	// Empty body: skip the parameter list, then take the next brace.
	if start < 0 || start > len(src) {
		return -1
	}
	p := strings.IndexByte(src[start:], '(')
	if p < 0 {
		return -1
	}
	depth := 0
	for i := start + p; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				if k := strings.IndexByte(src[i:], '{'); k >= 0 {
					return i + k
				}
				return -1
			}
		}
	}
	return -1
}

// siteRange returns the byte range of a function in src: from the start of its
// declaration (including a `@host` decorator line) to the closing brace.
func siteRange(src string, s *site) (start, end int) {
	start = s.pos.Offset
	if start < 0 || start > len(src) {
		return 0, 0
	}
	// Extend back over `export`, `export default` and a decorator above.
	cur := start
	back := func(kw string) {
		i := cur
		for i > 0 && strings.ContainsRune(" \t\r\n", rune(src[i-1])) {
			i--
		}
		if i >= len(kw) && src[i-len(kw):i] == kw && (i == len(kw) || !isIdentByte(src[i-len(kw)-1])) {
			cur = i - len(kw)
		}
	}
	back("default")
	back("export")
	back("@host")
	start = cur
	first, has := 0, len(s.body) > 0
	if has {
		first = s.body[0].Pos().Offset
	}
	open := bodyOpen(src, s.pos.Offset, first, has)
	if open < 0 {
		return start, start
	}
	end = matchBrace(src, open)
	if end < 0 {
		return start, start
	}
	// A const arrow ends at its trailing semicolon.
	if rest := src[end:]; strings.HasPrefix(rest, ";") {
		end++
	}
	return start, end
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
