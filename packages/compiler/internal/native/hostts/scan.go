package hostts

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/native"
)

// mask returns src with comments, string literals, template text and regular
// expression literals blanked to spaces. Offsets and line breaks are kept, so
// positions in the result are positions in src. Code inside `${}` stays.
func mask(src string) string {
	b := []byte(src)
	blank := func(i, j int) {
		if j > len(b) {
			j = len(b)
		}
		for k := i; k < j; k++ {
			if b[k] != '\n' && b[k] != '\r' {
				b[k] = ' '
			}
		}
	}
	var stack []int
	inTpl := false
	prev := byte(0)
	i := 0
	for i < len(b) {
		if inTpl {
			start := i
			closed := false
			for i < len(b) {
				c := b[i]
				if c == '\\' {
					i += 2
					continue
				}
				if c == '`' {
					blank(start, i+1)
					i++
					inTpl, closed = false, true
					break
				}
				if c == '$' && i+1 < len(b) && b[i+1] == '{' {
					blank(start, i)
					i += 2
					stack = append(stack, 0)
					inTpl, closed = false, true
					break
				}
				i++
			}
			if !closed {
				blank(start, len(b))
				i = len(b)
			}
			prev = '"'
			continue
		}
		c := b[i]
		switch {
		case c == '/' && i+1 < len(b) && b[i+1] == '/':
			j := strings.IndexByte(src[i:], '\n')
			if j < 0 {
				j = len(src) - i
			}
			blank(i, i+j)
			i += j
		case c == '/' && i+1 < len(b) && b[i+1] == '*':
			j := strings.Index(src[i+2:], "*/")
			end := len(src)
			if j >= 0 {
				end = i + 2 + j + 2
			}
			blank(i, end)
			i = end
		case c == '"' || c == '\'':
			j := native.SkipQuoted(src, i)
			blank(i, j+1)
			i = j + 1
			prev = '"'
		case c == '`':
			blank(i, i+1)
			i++
			inTpl = true
		case c == '{':
			if len(stack) > 0 {
				stack[len(stack)-1]++
			}
			prev = c
			i++
		case c == '}':
			if len(stack) > 0 {
				if stack[len(stack)-1] == 0 {
					stack = stack[:len(stack)-1]
					inTpl = true
					i++
					continue
				}
				stack[len(stack)-1]--
			}
			prev = c
			i++
		case c == '/' && strings.IndexByte("(,=:[!&|?{};", prev) >= 0 || (c == '/' && prev == 0):
			if j := regexEnd(src, i); j > 0 {
				blank(i, j)
				i = j
				prev = '"'
			} else {
				prev = c
				i++
			}
		default:
			if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
				prev = c
			}
			i++
		}
	}
	return string(b)
}

// regexEnd returns the offset just past the regular expression literal that
// starts at i, or -1 when the text there is not one.
func regexEnd(src string, i int) int {
	inClass := false
	for j := i + 1; j < len(src); j++ {
		switch src[j] {
		case '\n', '\r':
			return -1
		case '\\':
			j++
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '/':
			if !inClass {
				j++
				for j < len(src) && isIdent(src[j]) {
					j++
				}
				return j
			}
		}
	}
	return -1
}

func isIdent(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

// words returns the set of identifier-like words in s.
func words(s string) map[string]bool {
	out := map[string]bool{}
	for i := 0; i < len(s); {
		if !isIdent(s[i]) {
			i++
			continue
		}
		j := i
		for j < len(s) && isIdent(s[j]) {
			j++
		}
		out[s[i:j]] = true
		i = j
	}
	return out
}

// lineCol converts a byte offset to a 1-based line and column.
func lineCol(src string, off int) (int, int) {
	if off < 0 {
		off = 0
	}
	if off > len(src) {
		off = len(src)
	}
	line := 1 + strings.Count(src[:off], "\n")
	col := off - strings.LastIndexByte(src[:off], '\n')
	return line, col
}

func lineText(src string, line int) string {
	lines := strings.Split(src, "\n")
	if line >= 1 && line <= len(lines) {
		return strings.TrimRight(lines[line-1], "\r")
	}
	return ""
}

// findWord returns the offset of the first whole-word occurrence of w in m at
// or after from, or -1.
func findWord(m, w string, from int) int {
	for from < len(m) {
		k := strings.Index(m[from:], w)
		if k < 0 {
			return -1
		}
		k += from
		end := k + len(w)
		if (k == 0 || !isIdent(m[k-1])) && (end >= len(m) || !isIdent(m[end])) {
			return k
		}
		from = k + 1
	}
	return -1
}
