package build

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dlclark/regexp2/v2"
)

// Redirect and rewrite sources may constrain a parameter with a regular
// expression in path-to-regexp form, as next.config does:
// "/store/:slug((?!compare$)[^/]+)", "/p/:id(\\d+)?", or an unnamed "(a|b)".
// Such a source compiles to one anchored ECMAScript regex (lookaheads are
// allowed), matched with a timeout because the path comes from the request.

type sourceRegex struct {
	re    *regexp2.Regexp
	names []string
	mods  map[string]byte
}

var sourceRegexCache sync.Map // source -> *sourceRegex (nil when invalid)

const sourceRegexTimeout = 50 * time.Millisecond

// isRegexSource reports whether a redirect/rewrite source uses a regex
// parameter, which matchRedirectParamsMods handles with compileSourceRegex.
func isRegexSource(source string) bool {
	return strings.Contains(source, "(")
}

// compileSourceRegex returns the compiled form of a regex source, or nil when
// the source is not a valid pattern (it then matches nothing).
func compileSourceRegex(source string) *sourceRegex {
	if v, ok := sourceRegexCache.Load(source); ok {
		sr, _ := v.(*sourceRegex)
		return sr
	}
	pattern, names, mods, ok := sourceToRegex(source)
	var sr *sourceRegex
	if ok {
		if re, err := regexp2.Compile(pattern, regexp2.ECMAScript); err == nil {
			re.MatchTimeout = sourceRegexTimeout
			sr = &sourceRegex{re: re, names: names, mods: mods}
		}
	}
	sourceRegexCache.Store(source, sr)
	return sr
}

// sourceToRegex translates a path-to-regexp style source. A parameter is
// ":name", optionally followed by "(pattern)" (default: one segment) and a
// '?', '*' or '+' modifier; a bare "(pattern)" is an unnamed parameter. A
// trailing slash on the request is ignored, as for plain sources.
func sourceToRegex(source string) (string, []string, map[string]byte, bool) {
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
		gname := "p" + strconv.Itoa(len(names))
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

// matchRegexSource matches path against a regex source.
func matchRegexSource(path, source string) (map[string]string, map[string]byte, bool) {
	sr := compileSourceRegex(source)
	if sr == nil {
		return nil, nil, false
	}
	m, err := sr.re.FindStringMatch(path)
	if err != nil || m == nil {
		return nil, nil, false
	}
	params := map[string]string{}
	for i, name := range sr.names {
		if g := m.GroupByName("p" + strconv.Itoa(i)); g != nil {
			params[name] = g.String()
		}
	}
	mods := make(map[string]byte, len(sr.mods))
	for k, v := range sr.mods {
		mods[k] = v
	}
	return params, mods, true
}
