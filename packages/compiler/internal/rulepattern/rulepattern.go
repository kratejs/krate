// Package rulepattern compiles redirect and rewrite sources that constrain a
// parameter with a regular expression, in path-to-regexp form as next.config
// writes them ("/store/:slug((?!compare$)[^/]+)", "/p/:id(\\d+)?", an unnamed
// "(a|b)"), to one anchored ECMAScript regex. Parameter i is the named group
// "p<i>" of the result, so groups inside a pattern do not shift the numbering.
package rulepattern

import (
	"regexp"
	"strconv"
	"strings"
)

// IsRegex reports whether a source uses a regex parameter.
func IsRegex(source string) bool {
	return strings.Contains(source, "(")
}

// Group is the regex group name of parameter i.
func Group(i int) string {
	return "p" + strconv.Itoa(i)
}

// ToRegex translates a path-to-regexp style source. A parameter is
// ":name", optionally followed by "(pattern)" (default: one segment) and a
// '?', '*' or '+' modifier; a bare "(pattern)" is an unnamed parameter. A
// trailing slash on the request is ignored, as for plain sources.
func ToRegex(source string) (string, []string, map[string]byte, bool) {
	var b strings.Builder
	var names []string
	mods := map[string]byte{}
	b.WriteString("^")
	unnamed := 0
	i := 0
	for i < len(source) {
		c := source[i]
		if c != ':' && c != '(' {
			b.WriteString(regexp.QuoteMeta(string(c)))
			i++
			continue
		}
		name := ""
		if c == ':' {
			j := i + 1
			for j < len(source) && isParamNameByte(source[j]) {
				j++
			}
			if j == i+1 {
				b.WriteString(":")
				i++
				continue
			}
			name = source[i+1 : j]
			i = j
		} else {
			name = strconv.Itoa(unnamed)
			unnamed++
		}
		pat := "[^/]+?"
		if i < len(source) && source[i] == '(' {
			end, ok := closingParen(source, i)
			if !ok {
				return "", nil, nil, false
			}
			pat = source[i+1 : end]
			if strings.HasPrefix(pat, "?") && !strings.HasPrefix(pat, "?:") && !strings.HasPrefix(pat, "?=") && !strings.HasPrefix(pat, "?!") && !strings.HasPrefix(pat, "?<") {
				return "", nil, nil, false
			}
			i = end + 1
		}
		var mod byte
		if i < len(source) && (source[i] == '?' || source[i] == '*' || source[i] == '+') {
			mod = source[i]
			i++
		}
		// A modified parameter right after a slash owns that slash, so
		// "/a/:b?" also matches "/a".
		prefixed := mod != 0 && strings.HasSuffix(b.String(), "/")
		if prefixed {
			s := strings.TrimSuffix(b.String(), "/")
			b.Reset()
			b.WriteString(s)
		}
		// Named groups keep the numbering independent of groups inside pat.
		gname := Group(len(names))
		group := "(?<" + gname + ">" + pat + ")"
		if mod == '*' || mod == '+' {
			group = "(?<" + gname + ">(?:" + pat + ")(?:/(?:" + pat + "))*)"
		}
		switch {
		case prefixed && mod == '+':
			b.WriteString("/" + group)
		case prefixed:
			b.WriteString("(?:/" + group + ")?")
		case mod == '+':
			b.WriteString(group)
		case mod != 0:
			b.WriteString(group + "?")
		default:
			b.WriteString(group)
		}
		names = append(names, name)
		mods[name] = mod
	}
	b.WriteString("/?$")
	return b.String(), names, mods, true
}

func isParamNameByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// closingParen finds the parenthesis closing the group opened at open,
// skipping escapes and character classes.
func closingParen(s string, open int) (int, bool) {
	depth := 0
	inClass := false
	for i := open; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\':
			i++
		case inClass:
			if c == ']' {
				inClass = false
			}
		case c == '[':
			inClass = true
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}
