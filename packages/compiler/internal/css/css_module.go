package css

import (
	"regexp"
	"strings"
)

// ProcessModule scopes a CSS Module. Local class selectors are rewritten to
// `<name>_<hash>` and recorded in `mapping`; `:global(...)` is emitted unscoped;
// `composes:` local names are mapped. Strings, comments, `url(...)` and
// attribute selectors are left untouched (a whole-content regex pass corrupted
// them). Keyframe/animation names are not scoped (they do not collide in
// practice because the whole module shares one hash namespace via classes).
func ProcessModule(path, content string) (scopedCSS string, mapping map[string]string, err error) {
	hash := hashPath(path)
	mapping = make(map[string]string)

	const (
		ctxRules = iota // selectors are scoped here
		ctxDecls        // declaration bodies (keyframe frames, nested blocks)
	)
	stack := []int{ctxRules}

	var out strings.Builder
	i := 0
	n := len(content)
	preludeStart := 0

	for i < n {
		switch c := content[i]; {
		case c == '/' && i+1 < n && content[i+1] == '*':
			j := strings.Index(content[i+2:], "*/")
			if j < 0 {
				i = n
				continue
			}
			i += 2 + j + 2
		case c == '"' || c == '\'':
			i = cssSkipString(content, i)
		case c == '{':
			prelude := content[preludeStart:i]
			cur := stack[len(stack)-1]
			if cur == ctxRules {
				if at, isAt := atRuleName(prelude); isAt {
					out.WriteString(prelude)
					out.WriteByte('{')
					if declBodyAtRule(at) {
						stack = append(stack, ctxDecls)
					} else {
						stack = append(stack, ctxRules)
					}
				} else {
					out.WriteString(scopeSelector(prelude, hash, mapping))
					out.WriteByte('{')
					stack = append(stack, ctxDecls)
				}
			} else {
				out.WriteString(prelude)
				out.WriteByte('{')
				stack = append(stack, ctxDecls)
			}
			i++
			preludeStart = i
		case c == '}':
			body := content[preludeStart:i]
			if stack[len(stack)-1] == ctxDecls {
				out.WriteString(scopeComposes(body, mapping))
			} else {
				out.WriteString(body)
			}
			out.WriteByte('}')
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
			i++
			preludeStart = i
		default:
			i++
		}
	}
	tail := content[preludeStart:]
	if stack[len(stack)-1] == ctxDecls {
		out.WriteString(scopeComposes(tail, mapping))
	} else {
		out.WriteString(tail)
	}
	return out.String(), mapping, nil
}

// scopeSelector rewrites `.name` class tokens to their scoped form, skipping
// comments, strings, and anything inside `:global(...)` (whose wrapper is
// removed so the inner selector stays global).
func scopeSelector(sel, hash string, mapping map[string]string) string {
	var out strings.Builder
	i, n := 0, len(sel)
	for i < n {
		c := sel[i]
		switch {
		case c == '/' && i+1 < n && sel[i+1] == '*':
			j := strings.Index(sel[i+2:], "*/")
			if j < 0 {
				out.WriteString(sel[i:])
				return out.String()
			}
			out.WriteString(sel[i : i+2+j+2])
			i += 2 + j + 2
		case c == '"' || c == '\'':
			j := cssSkipString(sel, i)
			out.WriteString(sel[i:j])
			i = j
		case c == ':' && strings.HasPrefix(sel[i:], ":global("):
			open := i + len(":global")
			closeIdx := matchParen(sel, open)
			if closeIdx < 0 {
				out.WriteString(sel[i:])
				return out.String()
			}
			out.WriteString(sel[open+1 : closeIdx])
			i = closeIdx + 1
		case c == '.':
			k := i + 1
			for k < n && isIdentChar(sel[k]) {
				k++
			}
			if k > i+1 {
				name := sel[i+1 : k]
				scoped, ok := mapping[name]
				if !ok {
					scoped = name + "_" + hash
					mapping[name] = scoped
				}
				out.WriteByte('.')
				out.WriteString(scoped)
				i = k
				continue
			}
			out.WriteByte('.')
			i++
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}

// composesRe matches a `composes: a b from "..."` declaration.
var composesRe = regexp.MustCompile(`(composes\s*:\s*)([^;]+)(;)`)

// scopeComposes maps the local names in `composes:` declarations to their
// scoped form, leaving a `from "..."` import suffix untouched.
func scopeComposes(body string, mapping map[string]string) string {
	return composesRe.ReplaceAllStringFunc(body, func(m string) string {
		sub := composesRe.FindStringSubmatch(m)
		head, list, tail := sub[1], strings.TrimSpace(sub[2]), sub[3]
		from := ""
		if idx := strings.Index(list, " from "); idx >= 0 {
			from = list[idx:]
			list = strings.TrimSpace(list[:idx])
		}
		names := strings.Fields(list)
		for i, name := range names {
			if scoped, ok := mapping[name]; ok {
				names[i] = scoped
			}
		}
		return head + strings.Join(names, " ") + from + tail
	})
}

// atRuleName returns the lowercased at-rule keyword of a prelude, if it is one.
func atRuleName(prelude string) (string, bool) {
	s := strings.TrimSpace(prelude)
	if !strings.HasPrefix(s, "@") {
		return "", false
	}
	end := 1
	for end < len(s) && (isIdentChar(s[end]) || s[end] == '-') {
		end++
	}
	return strings.ToLower(s[1:end]), true
}

// declBodyAtRule reports whether an at-rule's block contains declarations
// (rather than nested rules).
func declBodyAtRule(name string) bool {
	switch name {
	case "keyframes", "-webkit-keyframes", "-moz-keyframes", "-o-keyframes",
		"font-face", "page", "counter-style", "property", "font-feature-values",
		"viewport", "-ms-viewport":
		return true
	}
	return false
}

func isIdentChar(c byte) bool {
	return c == '_' || c == '-' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// skipString returns the index just past a string literal starting at s[i]
// (a quote), honouring backslash escapes; unterminated strings run to the end.
func cssSkipString(s string, i int) int {
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
	return len(s)
}

// matchParen returns the index of the ')' matching the '(' at s[i], honouring
// strings and nesting, or -1.
func matchParen(s string, i int) int {
	if i >= len(s) || s[i] != '(' {
		return -1
	}
	depth := 0
	for ; i < len(s); i++ {
		switch s[i] {
		case '"', '\'':
			i = cssSkipString(s, i) - 1
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
