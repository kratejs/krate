package migrate

import (
	"regexp"
	"strings"
)

// codeMask marks, for each byte of a JavaScript/TypeScript source, whether it
// is code: false inside comments, string literals, regex literals and the
// text parts of template literals. The expressions of a `${...}` substitution
// are code.
func codeMask(src string) []bool {
	mask := make([]bool, len(src))
	type frame struct {
		template bool
		braces   int
	}
	stack := []frame{{}}
	var prev byte // last significant byte seen in code
	for i := 0; i < len(src); {
		top := &stack[len(stack)-1]
		c := src[i]
		if top.template {
			switch {
			case c == '\\':
				i += 2
			case c == '`':
				stack = stack[:len(stack)-1]
				i++
				prev = '`'
			case c == '$' && i+1 < len(src) && src[i+1] == '{':
				stack = append(stack, frame{})
				i += 2
			default:
				i++
			}
			continue
		}
		switch {
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				i = len(src)
			} else {
				i += end + 4
			}
		case c == '\'' || c == '"':
			i++
			for i < len(src) && src[i] != c && src[i] != '\n' {
				if src[i] == '\\' {
					i++
				}
				i++
			}
			i++
			prev = c
		case c == '`':
			stack = append(stack, frame{template: true})
			i++
		case c == '/' && regexAllowedAfter(prev):
			i = skipRegex(src, i)
			prev = '/'
		case c == '}' && top.braces == 0 && len(stack) > 1:
			stack = stack[:len(stack)-1]
			i++
		default:
			if c == '{' {
				top.braces++
			} else if c == '}' && top.braces > 0 {
				top.braces--
			}
			mask[i] = true
			if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
				prev = c
			}
			i++
		}
	}
	return mask
}

// regexAllowedAfter reports whether a `/` after the byte prev starts a regex
// literal rather than a division.
func regexAllowedAfter(prev byte) bool {
	return prev == 0 || strings.IndexByte("(,=:[!&|?{};+-*%<>~^", prev) >= 0
}

func skipRegex(src string, i int) int {
	i++
	inClass := false
	for i < len(src) && src[i] != '\n' {
		switch {
		case src[i] == '\\':
			i++
		case src[i] == '[':
			inClass = true
		case src[i] == ']':
			inClass = false
		case src[i] == '/' && !inClass:
			i++
			for i < len(src) && isIdentChar(src[i]) {
				i++
			}
			return i
		}
		i++
	}
	return i
}

func isIdentChar(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// codeOnly returns src with everything that is not code replaced by spaces.
func codeOnly(src string) string {
	mask := codeMask(src)
	b := []byte(src)
	for i := range b {
		if !mask[i] && b[i] != '\n' {
			b[i] = ' '
		}
	}
	return string(b)
}

var jsIdentRe = regexp.MustCompile(`[A-Za-z_$][\w$]*`)

// identifiers returns the set of identifier-like words in the code of src.
// Property names are included; callers use it to over-approximate references.
func identifiers(src string) map[string]bool {
	out := map[string]bool{}
	for _, m := range jsIdentRe.FindAllString(codeOnly(src), -1) {
		out[m] = true
	}
	return out
}

// splitTop splits the bracketed expression starting at src[open] (one of
// `[`, `{`, `(`) at its top-level commas. It returns the trimmed parts, in
// order; empty parts (a trailing comma) are dropped.
func splitTop(src string, open int) ([]string, bool) {
	if open < 0 || open >= len(src) || strings.IndexByte("[{(", src[open]) < 0 {
		return nil, false
	}
	mask := codeMask(src)
	depth := 0
	start := open + 1
	var parts []string
	add := func(end int) {
		if p := strings.TrimSpace(src[start:end]); p != "" {
			parts = append(parts, p)
		}
	}
	for i := open; i < len(src); i++ {
		if !mask[i] {
			continue
		}
		switch src[i] {
		case '[', '{', '(':
			depth++
		case ']', '}', ')':
			depth--
			if depth == 0 {
				add(i)
				return parts, true
			}
		case ',':
			if depth == 1 {
				add(i)
				start = i + 1
			}
		}
	}
	return nil, false
}

// dedent removes the indentation shared by the lines after the first, so a
// multi-line snippet can be re-indented where it is placed.
func dedent(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) < 2 {
		return s
	}
	min := -1
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		n := len(l) - len(strings.TrimLeft(l, " \t"))
		if min < 0 || n < min {
			min = n
		}
	}
	if min <= 0 {
		return s
	}
	for i := 1; i < len(lines); i++ {
		if len(lines[i]) >= min {
			lines[i] = lines[i][min:]
		} else {
			lines[i] = strings.TrimLeft(lines[i], " \t")
		}
	}
	return strings.Join(lines, "\n")
}

// indentLines prefixes every line after the first with indent.
func indentLines(s, indent string) string {
	lines := strings.Split(s, "\n")
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "" {
			lines[i] = indent + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}
