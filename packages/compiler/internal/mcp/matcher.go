package mcp

import (
	"strings"

	"github.com/dlclark/regexp2/v2"
)

// compileMiddlewareMatcher is a Go port of the middleware matcher compiler the
// runtime uses (internal/jsruntime/matcher.js), so list_middleware can answer
// "does this path run the middleware?" exactly as the server would. It needs
// lookahead (the common /((?!api|_next).*) form), hence regexp2.
func compileMiddlewareMatcher(src string) (*regexp2.Regexp, error) {
	if src == "" || src[0] != '/' {
		return nil, &policyError{`middleware matcher "` + src + `" must start with "/"`}
	}
	esc := func(c string) string {
		var b strings.Builder
		for _, r := range c {
			if strings.ContainsRune(`.*+?^${}()|[]\/`, r) {
				b.WriteByte('\\')
			}
			b.WriteRune(r)
		}
		return b.String()
	}
	isWord := func(c byte) bool {
		return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	}
	// group returns the pattern inside the balanced parens starting at i and the
	// index after the closing paren.
	group := func(i int) (string, int) {
		depth, k := 0, i
		for ; k < len(src); k++ {
			ch := src[k]
			if ch == '\\' {
				k++
				continue
			}
			if ch == '(' {
				depth++
			} else if ch == ')' {
				depth--
				if depth == 0 {
					break
				}
			}
		}
		if k > len(src) {
			k = len(src)
		}
		end := k
		if end > len(src) {
			end = len(src)
		}
		if i+1 > end {
			return "", end + 1
		}
		return src[i+1 : end], k + 1
	}

	var out strings.Builder
	i, n := 0, len(src)
	for i < n {
		c := src[i]
		prefix := ""
		if (c == '/' || c == '.') && i+1 < n && (src[i+1] == ':' || src[i+1] == '(') {
			prefix = string(c)
			i++
			c = src[i]
		}
		if c == ':' || c == '(' {
			pattern := ""
			if c == ':' {
				j := i + 1
				for j < n && isWord(src[j]) {
					j++
				}
				i = j
				if i < n && src[i] == '(' {
					pattern, i = group(i)
				}
			} else {
				pattern, i = group(i)
			}
			mod := byte(0)
			if i < n && (src[i] == '*' || src[i] == '+' || src[i] == '?') {
				mod = src[i]
				i++
			}
			body := pattern
			if body == "" {
				body = `[^\/]+?`
			}
			p := ""
			if prefix != "" {
				p = esc(prefix)
			}
			sep := prefix
			if sep == "" {
				sep = "/"
			}
			switch mod {
			case '*':
				out.WriteString("(?:" + p + "(" + body + "(?:" + esc(sep) + body + ")*))?")
			case '+':
				out.WriteString(p + "(" + body + "(?:" + esc(sep) + body + ")*)")
			case '?':
				out.WriteString("(?:" + p + "(" + body + "))?")
			default:
				out.WriteString(p + "(" + body + ")")
			}
			continue
		}
		if c == '*' {
			out.WriteString(".*")
			i++
			continue
		}
		out.WriteString(esc(string(c)))
		i++
	}
	return regexp2.Compile("^"+out.String()+`\/?$`, regexp2.ECMAScript|regexp2.IgnoreCase)
}

// matcherMatches reports whether any matcher source matches the path. A nil
// list means no matcher: the middleware sees every forwarded request.
func matcherMatches(sources []string, p string) (bool, error) {
	if len(sources) == 0 {
		return true, nil
	}
	for _, src := range sources {
		re, err := compileMiddlewareMatcher(src)
		if err != nil {
			return false, err
		}
		ok, err := re.MatchString(p)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}
