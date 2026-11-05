package css

import "strings"

// SyntaxProblem is a structural problem in authored CSS that browsers would
// silently recover from in a way that changes what the sheet means.
type SyntaxProblem struct {
	Line, Col int // 1-based
	Message   string
}

type cssScan struct {
	problems []SyntaxProblem
	// stray lists byte offsets of `}` with no matching `{`.
	stray []int
	// open counts `{` still unclosed at the end.
	open int
	// unterminatedComment is true when a comment runs to the end of input.
	unterminatedComment bool
}

// scanCSS walks src once, skipping comments, strings and unquoted url(...)
// bodies, and records unbalanced braces.
func scanCSS(src string) cssScan {
	var sc cssScan
	type pos struct{ off, line, col int }
	var stack []pos
	line, col := 1, 1
	at := func(i int) pos { return pos{i, line, col} }
	advance := func(r byte) {
		if r == '\n' {
			line++
			col = 1
		} else if r&0xC0 != 0x80 {
			col++
		}
	}
	n := len(src)
	for i := 0; i < n; {
		c := src[i]
		switch {
		case c == '/' && i+1 < n && src[i+1] == '*':
			start := at(i)
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				sc.unterminatedComment = true
				sc.problems = append(sc.problems, SyntaxProblem{start.line, start.col,
					"comment is never closed; everything after it, including later stylesheets, is ignored"})
				for ; i < n; i++ {
					advance(src[i])
				}
				continue
			}
			stop := i + 2 + end + 2
			for ; i < stop; i++ {
				advance(src[i])
			}
			continue
		case c == '"' || c == '\'':
			start := at(i)
			advance(c)
			i++
			closed := false
			for i < n {
				d := src[i]
				if d == '\\' && i+1 < n {
					advance(d)
					advance(src[i+1])
					i += 2
					continue
				}
				if d == '\n' {
					break
				}
				advance(d)
				i++
				if d == c {
					closed = true
					break
				}
			}
			if !closed {
				sc.problems = append(sc.problems, SyntaxProblem{start.line, start.col, "string is not closed before the end of the line"})
			}
			continue
		case c == '\\' && i+1 < n:
			advance(c)
			advance(src[i+1])
			i += 2
			continue
		case (c == 'u' || c == 'U') && i+4 <= n && strings.EqualFold(src[i:i+4], "url(") && isURLStart(src, i):
			// Unquoted url(...) bodies may contain characters that are not
			// structural; quoted ones are handled by the string case.
			j := i + 4
			for j < n && (src[j] == ' ' || src[j] == '\t') {
				j++
			}
			if j < n && src[j] != '"' && src[j] != '\'' {
				for ; i < n; i++ {
					advance(src[i])
					if src[i] == ')' {
						i++
						break
					}
				}
				continue
			}
		case c == '{':
			stack = append(stack, at(i))
		case c == '}':
			if len(stack) == 0 {
				p := at(i)
				sc.stray = append(sc.stray, i)
				sc.problems = append(sc.problems, SyntaxProblem{p.line, p.col, "`}` has no matching `{`; the rule after it is dropped by browsers"})
			} else {
				stack = stack[:len(stack)-1]
			}
		}
		advance(c)
		i++
	}
	sc.open = len(stack)
	for _, p := range stack {
		sc.problems = append(sc.problems, SyntaxProblem{p.line, p.col,
			"`{` is never closed; the rules after it are swallowed into this block"})
	}
	return sc
}

// isURLStart reports whether the `url(` at i starts a token rather than the end
// of a longer identifier such as `myurl(`.
func isURLStart(src string, i int) bool {
	if i == 0 {
		return true
	}
	p := src[i-1]
	return !(p == '-' || p == '_' || p >= '0' && p <= '9' || p >= 'a' && p <= 'z' || p >= 'A' && p <= 'Z' || p >= 0x80)
}

// CheckSyntax reports unbalanced braces, unclosed comments and strings in
// authored CSS, each with its line and column.
func CheckSyntax(src string) []SyntaxProblem {
	return scanCSS(src).problems
}

// Balance repairs the brace structure of one stylesheet so it cannot affect
// the sheets joined after it: stray `}` are removed, and unclosed comments and
// blocks are closed at the end. A balanced sheet is returned unchanged.
func Balance(src string) string {
	sc := scanCSS(src)
	if len(sc.stray) == 0 && sc.open == 0 && !sc.unterminatedComment {
		return src
	}
	var b strings.Builder
	last := 0
	for _, off := range sc.stray {
		b.WriteString(src[last:off])
		last = off + 1
	}
	b.WriteString(src[last:])
	if sc.unterminatedComment {
		b.WriteString("*/")
	}
	b.WriteString(strings.Repeat("}", sc.open))
	b.WriteString("\n")
	return b.String()
}
