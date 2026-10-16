package css

import "strings"

// The hybrid CSS-signals stylesheet wraps its two variants in sentinel-carrying
// @supports blocks (see csssignals.CompatModernPrefix / CompatLegacyPrefix).
// These helpers split it into per-variant stylesheets for UA-based serving.
// They match the exact prefix (which includes the opening brace and a
// `/*krate:*​/` sentinel comment), so an author's own
// `@supports selector(:has(*))` is never touched.

// RemoveAtRuleBlock removes every `prefix … }` block (prefix ends just after the
// opening brace and sentinel) from s, brace-balanced and string/comment-aware.
func RemoveAtRuleBlock(s, prefix string) string {
	var b strings.Builder
	pos := 0
	for {
		start, _, close, ok := findPrefixBlock(s, prefix, pos)
		if !ok {
			b.WriteString(s[pos:])
			break
		}
		b.WriteString(s[pos:start])
		pos = close
	}
	return b.String()
}

// UnwrapAtRuleBlock replaces every `prefix … }` block with its inner content.
func UnwrapAtRuleBlock(s, prefix string) string {
	var b strings.Builder
	pos := 0
	for {
		start, contentStart, close, ok := findPrefixBlock(s, prefix, pos)
		if !ok {
			b.WriteString(s[pos:])
			break
		}
		b.WriteString(s[pos:start])
		b.WriteString(s[contentStart : close-1])
		pos = close
	}
	return b.String()
}

// findPrefixBlock locates the next block introduced by prefix (which includes
// the opening brace) at or after from, returning the block start, the index of
// the first byte after the prefix, and the index just past the closing brace.
// Braces inside string literals and comments are ignored.
func findPrefixBlock(s, prefix string, from int) (start, contentStart, close int, ok bool) {
	idx := strings.Index(s[from:], prefix)
	if idx < 0 {
		return 0, 0, 0, false
	}
	start = from + idx
	contentStart = start + len(prefix)
	brace := strings.LastIndexByte(prefix, '{')
	if brace < 0 {
		return 0, 0, 0, false
	}
	open := start + brace
	depth := 0
	i := open
	for i < len(s) {
		switch s[i] {
		case '"', '\'':
			i = skipCSSString(s, i)
			continue
		case '/':
			if i+1 < len(s) && s[i+1] == '*' {
				i = skipCSSComment(s, i)
				continue
			}
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return start, contentStart, i + 1, true
			}
		}
		i++
	}
	return 0, 0, 0, false
}

// skipCSSString returns the index just past the string literal starting at i
// (which points at the opening quote).
func skipCSSString(s string, i int) int {
	quote := s[i]
	i++
	for i < len(s) {
		if s[i] == '\\' {
			i += 2
			continue
		}
		if s[i] == quote {
			return i + 1
		}
		i++
	}
	return i
}

// skipCSSComment returns the index just past the /* */ comment starting at i.
func skipCSSComment(s string, i int) int {
	end := strings.Index(s[i+2:], "*/")
	if end < 0 {
		return len(s)
	}
	return i + 2 + end + 2
}
