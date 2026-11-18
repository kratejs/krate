package routesource

import (
	"fmt"
	"regexp"
	"strings"
)

// The parser follows path-to-regexp 6.x (the version Next.js bundles): the
// same tokens, the same "./" prefix characters, the same default parameter
// pattern, and the same rules on what a custom pattern may contain.

const defaultPattern = `[^/#?]+?`

type token struct {
	lit     string // set for a literal run
	param   bool
	name    string // "" for an unnamed pattern or a bare group
	prefix  string
	suffix  string
	pattern string
	mod     byte
}

type lexTok struct {
	kind string // CHAR, ESCAPED, MODIFIER, OPEN, CLOSE, NAME, PATTERN, END
	val  string
}

func isWordByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '_'
}

func lex(s string) ([]lexTok, error) {
	var out []lexTok
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '*' || c == '+' || c == '?':
			out = append(out, lexTok{"MODIFIER", string(c)})
			i++
		case c == '\\':
			if i+1 >= len(s) {
				return nil, fmt.Errorf("a trailing backslash at %d", i)
			}
			out = append(out, lexTok{"ESCAPED", string(s[i+1])})
			i += 2
		case c == '{':
			out = append(out, lexTok{"OPEN", "{"})
			i++
		case c == '}':
			out = append(out, lexTok{"CLOSE", "}"})
			i++
		case c == ':':
			j := i + 1
			for j < len(s) && isWordByte(s[j]) {
				j++
			}
			if j == i+1 {
				return nil, fmt.Errorf("missing parameter name at %d", i)
			}
			out = append(out, lexTok{"NAME", s[i+1 : j]})
			i = j
		case c == '(':
			count := 1
			j := i + 1
			if j < len(s) && s[j] == '?' {
				return nil, fmt.Errorf("a pattern cannot start with \"?\" at %d (a lookahead belongs inside a parameter: :name((?!x$)[^/]+))", j)
			}
			var pat strings.Builder
			for j < len(s) {
				if s[j] == '\\' {
					if j+1 >= len(s) {
						break
					}
					pat.WriteByte(s[j])
					pat.WriteByte(s[j+1])
					j += 2
					continue
				}
				if s[j] == ')' {
					count--
					if count == 0 {
						j++
						break
					}
				} else if s[j] == '(' {
					count++
					if j+1 >= len(s) || s[j+1] != '?' {
						return nil, fmt.Errorf("capturing groups are not allowed at %d", j)
					}
				}
				pat.WriteByte(s[j])
				j++
			}
			if count != 0 {
				return nil, fmt.Errorf("unbalanced pattern at %d", i)
			}
			if pat.Len() == 0 {
				return nil, fmt.Errorf("missing pattern at %d", i)
			}
			out = append(out, lexTok{"PATTERN", pat.String()})
			i = j
		default:
			out = append(out, lexTok{"CHAR", string(c)})
			i++
		}
	}
	out = append(out, lexTok{"END", ""})
	return out, nil
}

func parsePattern(s string) ([]token, error) {
	lt, err := lex(s)
	if err != nil {
		return nil, err
	}
	i := 0
	try := func(kind string) (string, bool) {
		if lt[i].kind == kind {
			v := lt[i].val
			i++
			return v, true
		}
		return "", false
	}
	text := func() string {
		var sb strings.Builder
		for {
			if v, ok := try("CHAR"); ok {
				sb.WriteString(v)
			} else if v, ok := try("ESCAPED"); ok {
				sb.WriteString(v)
			} else {
				return sb.String()
			}
		}
	}
	var toks []token
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			toks = append(toks, token{lit: lit.String()})
			lit.Reset()
		}
	}
	for lt[i].kind != "END" {
		ch, hasChar := try("CHAR")
		name, hasName := try("NAME")
		pat, hasPat := try("PATTERN")
		if hasName || hasPat {
			prefix := ch
			if prefix != "." && prefix != "/" {
				lit.WriteString(prefix)
				prefix = ""
			}
			flush()
			t := token{param: true, name: name, prefix: prefix, pattern: pat}
			if !hasPat {
				t.pattern = defaultPattern
			}
			if m, ok := try("MODIFIER"); ok {
				t.mod = m[0]
			}
			toks = append(toks, t)
			continue
		}
		if hasChar {
			lit.WriteString(ch)
			continue
		}
		if v, ok := try("ESCAPED"); ok {
			lit.WriteString(v)
			continue
		}
		flush()
		if _, ok := try("OPEN"); ok {
			t := token{param: true}
			t.prefix = text()
			name, hasName := try("NAME")
			pat, hasPat := try("PATTERN")
			t.suffix = text()
			if _, ok := try("CLOSE"); !ok {
				return nil, fmt.Errorf("expected \"}\"")
			}
			t.name = name
			switch {
			case hasName && !hasPat:
				t.pattern = defaultPattern
			case hasPat:
				t.pattern = pat
			}
			if m, ok := try("MODIFIER"); ok {
				t.mod = m[0]
			}
			toks = append(toks, t)
			continue
		}
		return nil, fmt.Errorf("unexpected %s %q (a bare * is not valid here: use :name* or a trailing /*)", strings.ToLower(lt[i].kind), lt[i].val)
	}
	flush()
	return toks, nil
}

// splitLookaheads removes leading (?!...) groups from a parameter pattern and
// returns the bodies of those groups and the remaining pattern.
func splitLookaheads(p string) (bodies []string, rest string, err error) {
	for strings.HasPrefix(p, "(?!") {
		depth := 1
		i := 3
		inClass := false
		for ; i < len(p); i++ {
			c := p[i]
			switch {
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
			}
			if depth == 0 {
				break
			}
		}
		if depth != 0 {
			return nil, "", fmt.Errorf("unbalanced lookahead in %q", p)
		}
		bodies = append(bodies, p[3:i])
		p = p[i+1:]
	}
	if len(bodies) > 0 && p == "" {
		return nil, "", fmt.Errorf("a lookahead needs a pattern after it, for example (?!compare$)[^/]+")
	}
	return bodies, p, nil
}

func quote(s string) string { return regexp.QuoteMeta(s) }

// build emits the regular expression for toks into s. With js set the output
// is for a JavaScript engine: patterns are kept as written (lookaheads
// included), nothing is checked against RE2, and the splat group is
// JavaScript syntax.
func (s *Source) build(toks []token, splat, js bool) error {
	var re strings.Builder
	re.WriteString("^")
	group := 0
	for _, t := range toks {
		if !t.param {
			re.WriteString(quote(t.lit))
			continue
		}
		prefix, suffix := quote(t.prefix), quote(t.suffix)
		if t.pattern == "" {
			if t.mod == '+' || t.mod == '*' {
				return fmt.Errorf("cannot repeat a group without a parameter or pattern")
			}
			fmt.Fprintf(&re, "(?:%s%s)%s", prefix, suffix, modStr(t.mod))
			continue
		}
		negs, pat := []string(nil), t.pattern
		if !js {
			var err error
			negs, pat, err = splitLookaheads(t.pattern)
			if err != nil {
				return err
			}
			if err := checkPattern(pat); err != nil {
				return err
			}
		}
		pi := paramInfo{name: t.name, mod: t.mod}
		for _, b := range negs {
			n, err := regexp.Compile("^(?:" + b + ")")
			if err != nil {
				return unsupportedRegex(b, err)
			}
			pi.neg = append(pi.neg, n)
		}
		group++
		pi.group = group
		s.params = append(s.params, pi)
		switch {
		case (t.mod == '+' || t.mod == '*') && (prefix != "" || suffix != ""):
			opt := ""
			if t.mod == '*' {
				opt = "?"
			}
			fmt.Fprintf(&re, "(?:%s((?:%s)(?:%s%s(?:%s))*)%s)%s", prefix, pat, suffix, prefix, pat, suffix, opt)
		case prefix != "" || suffix != "":
			fmt.Fprintf(&re, "(?:%s(%s)%s)%s", prefix, pat, suffix, modStr(t.mod))
		case t.mod == '+' || t.mod == '*':
			fmt.Fprintf(&re, "((?:%s)%c)", pat, t.mod)
		default:
			fmt.Fprintf(&re, "(%s)%s", pat, modStr(t.mod))
		}
	}
	if splat {
		group++
		s.splatGroup = group
		if js {
			re.WriteString(`(?:/([\s\S]*))?$`)
		} else {
			re.WriteString(`(?:/((?s:.*)))?$`)
		}
	} else {
		re.WriteString(`/?$`)
	}
	if js {
		s.jsPattern = re.String()
		return nil
	}
	compiled, err := regexp.Compile(re.String())
	if err != nil {
		return unsupportedRegex(re.String(), err)
	}
	s.re = compiled
	return nil
}

func modStr(m byte) string {
	if m == 0 {
		return ""
	}
	return string(m)
}

// checkPattern verifies that a parameter pattern runs on RE2 and adds no
// capturing group of its own.
func checkPattern(p string) error {
	r, err := regexp.Compile(p)
	if err != nil {
		return unsupportedRegex(p, err)
	}
	if r.NumSubexp() != 0 {
		return fmt.Errorf("pattern %q has a capturing group, which is not allowed in a source", p)
	}
	return nil
}

func unsupportedRegex(p string, err error) error {
	msg := err.Error()
	msg = strings.TrimPrefix(msg, "error parsing regexp: ")
	return unsupported("the regex %q uses a feature Go's RE2 engine cannot run (%s); only a leading negative lookahead such as (?!compare$) is supported", p, msg)
}
