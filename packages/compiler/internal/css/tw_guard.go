package css

import "strings"

// unsafeRuleReason reports why the generated rule text could break the
// stylesheet around it, or "" when it is safe to emit. It is the last gate
// before a rule is assembled: an unbalanced bracket or an unterminated string
// in one declaration makes the browser drop every later rule in the layer.
//
// The scan is one pass with a small bracket stack. Backslash escapes (class
// selectors are escaped), quoted strings and comments are skipped, so url(),
// quoted font names and content strings with brackets stay valid, and a
// ';' inside parentheses (url(data:...;base64,...)) is part of the value.
func unsafeRuleReason(text string) string {
	var stackBuf [24]byte
	stack := stackBuf[:0]
	segStart := 0 // start of the current declaration or prelude
	sawColon := false
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch c {
		case '\\':
			i++
		case '"', '\'':
			j := i + 1
			for ; j < len(text) && text[j] != c; j++ {
				switch text[j] {
				case '\\':
					j++
				case '\n':
					return "unterminated string"
				}
			}
			if j >= len(text) {
				return "unterminated string"
			}
			i = j
		case '/':
			if i+1 < len(text) && text[i+1] == '*' {
				end := indexFrom(text, "*/", i+2)
				if end < 0 {
					return "unterminated comment"
				}
				i = end + 1
			}
		case '(', '[', '{':
			if c == '(' && isURLOpen(text, i) {
				// An unquoted url() is one token that runs to the next ')'.
				j := i + 1
				for ; j < len(text) && text[j] != ')'; j++ {
					if text[j] == '\\' {
						j++
					}
				}
				if j >= len(text) {
					return "unbalanced ("
				}
				i = j
				continue
			}
			stack = append(stack, c)
			if c == '{' {
				segStart, sawColon = i+1, false
			}
		case ')', ']', '}':
			if len(stack) == 0 || stack[len(stack)-1] != closerOpen(c) {
				return "unbalanced " + string(c)
			}
			stack = stack[:len(stack)-1]
			if c == '}' {
				if hasContent(text[segStart:i]) && !sawColon {
					return "stray text before }"
				}
				segStart, sawColon = i+1, false
			}
		case ':':
			if len(stack) > 0 && stack[len(stack)-1] == '{' {
				sawColon = true
			}
		case ';':
			switch {
			case len(stack) == 0:
				if !hasContent(text[segStart:i]) || text[firstContent(text, segStart)] != '@' {
					return "stray ;"
				}
				segStart, sawColon = i+1, false
			case stack[len(stack)-1] == '{':
				if !sawColon {
					return "stray ;"
				}
				segStart, sawColon = i+1, false
			case stack[len(stack)-1] == '[':
				return "stray ; inside ["
			}
		}
	}
	if len(stack) > 0 {
		return "unbalanced " + string(stack[len(stack)-1])
	}
	return ""
}

// isURLOpen reports whether the '(' at i opens an unquoted url().
func isURLOpen(s string, i int) bool {
	if i < 3 || !strings.EqualFold(s[i-3:i], "url") {
		return false
	}
	j := i + 1
	for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n') {
		j++
	}
	return j < len(s) && s[j] != '"' && s[j] != '\''
}

func closerOpen(c byte) byte {
	switch c {
	case ')':
		return '('
	case ']':
		return '['
	}
	return '{'
}

func indexFrom(s, sub string, from int) int {
	for i := from; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func firstContent(s string, from int) int {
	for i := from; i < len(s); i++ {
		if s[i] != ' ' && s[i] != '\n' && s[i] != '\t' && s[i] != '\r' {
			return i
		}
	}
	return len(s) - 1
}

func hasContent(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' && s[i] != '\n' && s[i] != '\t' && s[i] != '\r' {
			return true
		}
	}
	return false
}
