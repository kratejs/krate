package css

import "strings"

// cssAtRule is a top-level at-rule found by topLevelAtRules.
type cssAtRule struct {
	Name    string // without "@", lower-case
	Prelude string // text between the name and the body or semicolon, trimmed
	Body    string // inner text of the { } block (without braces)
	HasBody bool
	Start   int // index of "@"
	End     int // index just past the rule
}

// matchCSSBlock returns the index of the "}" matching the "{" at open, or -1.
func matchCSSBlock(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		case '"', '\'':
			i = skipCSSString(s, i) - 1
		case '/':
			if i+1 < len(s) && s[i+1] == '*' {
				i = skipCSSComment(s, i) - 1
			}
		}
	}
	return -1
}

// topLevelAtRules lists the at-rules that sit at brace depth 0.
func topLevelAtRules(s string) []cssAtRule {
	var out []cssAtRule
	i := 0
	for i < len(s) {
		switch s[i] {
		case '/':
			if i+1 < len(s) && s[i+1] == '*' {
				i = skipCSSComment(s, i)
				continue
			}
		case '"', '\'':
			i = skipCSSString(s, i)
			continue
		case '{':
			// A style rule block: skip it whole.
			if end := matchCSSBlock(s, i); end >= 0 {
				i = end + 1
			} else {
				i = len(s)
			}
			continue
		case '@':
			r, ok := readAtRule(s, i)
			if ok {
				out = append(out, r)
				i = r.End
				continue
			}
		}
		i++
	}
	return out
}

func readAtRule(s string, at int) (cssAtRule, bool) {
	j := at + 1
	for j < len(s) && (s[j] == '-' || s[j] == '_' || s[j] >= 'a' && s[j] <= 'z' || s[j] >= 'A' && s[j] <= 'Z' || s[j] >= '0' && s[j] <= '9') {
		j++
	}
	if j == at+1 {
		return cssAtRule{}, false
	}
	r := cssAtRule{Name: strings.ToLower(s[at+1 : j]), Start: at}
	k := j
	paren := 0
	for k < len(s) {
		c := s[k]
		if c == '"' || c == '\'' {
			k = skipCSSString(s, k)
			continue
		}
		if c == '/' && k+1 < len(s) && s[k+1] == '*' {
			k = skipCSSComment(s, k)
			continue
		}
		if c == '(' {
			paren++
		} else if c == ')' && paren > 0 {
			paren--
		}
		if paren == 0 && (c == ';' || c == '{') {
			break
		}
		k++
	}
	r.Prelude = strings.TrimSpace(s[j:min(k, len(s))])
	if k >= len(s) {
		r.End = len(s)
		return r, true
	}
	if s[k] == ';' {
		r.End = k + 1
		return r, true
	}
	end := matchCSSBlock(s, k)
	if end < 0 {
		r.Body = s[k+1:]
		r.HasBody = true
		r.End = len(s)
		return r, true
	}
	r.Body = s[k+1 : end]
	r.HasBody = true
	r.End = end + 1
	return r, true
}

// splitCSSDecls splits a block body into top-level declarations and nested
// blocks. Declarations are "name: value" pairs; nested holds raw text of nested
// rules and at-rules.
type cssDecl struct{ Name, Value string }

func splitCSSBody(body string) (decls []cssDecl, nested []string) {
	for _, n := range splitCSSNodes(body) {
		if n.Block {
			nested = append(nested, n.Text)
			continue
		}
		if c := strings.IndexByte(n.Text, ':'); c > 0 {
			decls = append(decls, cssDecl{Name: strings.TrimSpace(n.Text[:c]), Value: strings.TrimSpace(n.Text[c+1:])})
		} else if n.Text != "" {
			nested = append(nested, n.Text)
		}
	}
	return decls, nested
}

// cssNode is one top-level entry of a block body, in source order.
type cssNode struct {
	// Block is true for `prelude { body }`; Text is then the whole block text
	// with its braces. Otherwise Text is a statement: a declaration or a
	// semicolon-terminated at-rule such as `@apply flex`.
	Block         bool
	Text          string
	Prelude, Body string
}

// splitCSSNodes splits a block body into its declarations, statements and
// nested blocks without losing their relative order.
func splitCSSNodes(body string) []cssNode {
	var out []cssNode
	i := 0
	for i < len(body) {
		// Skip whitespace and stray semicolons.
		for i < len(body) && (body[i] == ' ' || body[i] == '\t' || body[i] == '\n' || body[i] == '\r' || body[i] == ';') {
			i++
		}
		if i >= len(body) {
			break
		}
		if body[i] == '/' && i+1 < len(body) && body[i+1] == '*' {
			i = skipCSSComment(body, i)
			continue
		}
		start := i
		paren := 0
		for i < len(body) {
			c := body[i]
			if c == '"' || c == '\'' {
				i = skipCSSString(body, i)
				continue
			}
			if c == '(' {
				paren++
			} else if c == ')' && paren > 0 {
				paren--
			}
			if paren == 0 && (c == ';' || c == '{') {
				break
			}
			i++
		}
		if i < len(body) && body[i] == '{' {
			end := matchCSSBlock(body, i)
			if end < 0 {
				end = len(body) - 1
			}
			inner := ""
			if end > i {
				inner = body[i+1 : end]
			}
			out = append(out, cssNode{Block: true, Text: strings.TrimSpace(body[start : end+1]), Prelude: strings.TrimSpace(body[start:i]), Body: inner})
			i = end + 1
			continue
		}
		if text := strings.TrimSpace(body[start:min(i, len(body))]); text != "" {
			out = append(out, cssNode{Text: text})
		}
		i++
	}
	return out
}
