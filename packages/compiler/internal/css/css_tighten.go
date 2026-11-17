package css

import "strings"

// Selectors whose argument is itself a selector list, where combinator spacing
// can be tightened like at the top level. Other pseudo-class arguments (such as
// :nth-child(2n + 1)) keep their text.
var selectorArgFuncs = map[string]bool{
	"is": true, "where": true, "not": true, "has": true, "matches": true, "any": true,
}

// tightenSelector removes the optional spaces of a selector list: around the
// combinators > + ~, after and before commas, and inside parentheses. Strings
// are already protected tokens; attribute selectors and escapes are copied
// verbatim.
func tightenSelector(sel string) string {
	sel = strings.TrimSpace(sel)
	var b strings.Builder
	b.Grow(len(sel))
	n := len(sel)
	var stack []bool // per open paren: combinators may be tightened inside
	allow := true
	bracket := 0
	pending := ""
	trimOut := func() {
		s := b.String()
		if t := strings.TrimRight(s, " "); len(t) != len(s) {
			b.Reset()
			b.WriteString(t)
		}
	}
	for i := 0; i < n; {
		c := sel[i]
		switch {
		case c == '\\':
			b.WriteByte(c)
			if i+1 < n {
				b.WriteByte(sel[i+1])
			}
			i += 2
			pending = ""
		case c == '[':
			bracket++
			b.WriteByte(c)
			i++
		case c == ']':
			if bracket > 0 {
				bracket--
			}
			b.WriteByte(c)
			i++
		case bracket > 0:
			b.WriteByte(c)
			i++
		case c == '(':
			stack = append(stack, allow)
			allow = allow && selectorArgFuncs[stripVendorPrefix(strings.ToLower(pending))]
			b.WriteByte(c)
			i++
			for i < n && sel[i] == ' ' {
				i++
			}
			pending = ""
		case c == ')':
			trimOut()
			if len(stack) > 0 {
				allow = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
			b.WriteByte(c)
			i++
			pending = ""
		case c == ',':
			trimOut()
			b.WriteByte(c)
			i++
			for i < n && sel[i] == ' ' {
				i++
			}
			pending = ""
		case (c == '>' || c == '+' || c == '~') && allow:
			trimOut()
			b.WriteByte(c)
			i++
			for i < n && sel[i] == ' ' {
				i++
			}
			pending = ""
		case isIdentByte(c) || c == '-':
			j := i
			for j < n && (isIdentByte(sel[j]) || sel[j] == '-') {
				j++
			}
			b.WriteString(sel[i:j])
			pending = sel[i:j]
			i = j
		default:
			b.WriteByte(c)
			i++
			pending = ""
		}
	}
	return b.String()
}

// tightenAtPrelude removes optional spaces from the prelude of @media,
// @supports, @container and @layer: after commas, inside parentheses and after
// the colon of a feature. Spaces between words ("screen and (x)") stay. A
// prelude with selector() or style() is left alone.
func tightenAtPrelude(head string) string {
	head = strings.TrimSpace(head)
	switch preludeAtName(head) {
	case "media", "supports", "container", "layer":
	default:
		return head
	}
	if strings.Contains(head, "selector(") || strings.Contains(head, "style(") {
		return head
	}
	var b strings.Builder
	b.Grow(len(head))
	n := len(head)
	depth := 0
	trimOut := func() {
		s := b.String()
		if t := strings.TrimRight(s, " "); len(t) != len(s) {
			b.Reset()
			b.WriteString(t)
		}
	}
	skipSpaces := func(i int) int {
		for i < n && head[i] == ' ' {
			i++
		}
		return i
	}
	for i := 0; i < n; {
		c := head[i]
		switch {
		case c == '\\':
			b.WriteByte(c)
			if i+1 < n {
				b.WriteByte(head[i+1])
			}
			i += 2
		case c == '(':
			depth++
			b.WriteByte(c)
			i = skipSpaces(i + 1)
		case c == ')':
			if depth > 0 {
				depth--
			}
			trimOut()
			b.WriteByte(c)
			i++
		case c == ',':
			trimOut()
			b.WriteByte(c)
			i = skipSpaces(i + 1)
		case c == ':' && depth > 0:
			trimOut()
			b.WriteByte(c)
			i = skipSpaces(i + 1)
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// tightenHead minifies the text before a '{' or ';': an at-rule prelude or a
// selector list.
func tightenHead(head string) string {
	if strings.HasPrefix(strings.TrimSpace(head), "@") {
		return tightenAtPrelude(head)
	}
	return tightenSelector(head)
}

// tightenValueSpaces removes optional spaces from a declaration value that is
// not a custom property: before and after commas, just inside parentheses, and
// before !important. Strings and url() are protected tokens by now. Operators
// are untouched, so the required spaces around + and - in calc() stay.
func tightenValueSpaces(val string) string {
	if !strings.ContainsAny(val, ", ()!") {
		return val
	}
	var b strings.Builder
	b.Grow(len(val))
	n := len(val)
	trimOut := func() {
		s := b.String()
		if t := strings.TrimRight(s, " "); len(t) != len(s) {
			b.Reset()
			b.WriteString(t)
		}
	}
	for i := 0; i < n; {
		c := val[i]
		switch {
		case c == '\\':
			b.WriteByte(c)
			if i+1 < n {
				b.WriteByte(val[i+1])
			}
			i += 2
		case c == 0:
			end := n
			if j := strings.IndexByte(val[i+1:], 0); j >= 0 {
				end = i + 1 + j + 1
			}
			b.WriteString(val[i:end])
			i = end
		case c == '(':
			b.WriteByte(c)
			i++
			for i < n && val[i] == ' ' {
				i++
			}
		case c == ')':
			trimOut()
			b.WriteByte(c)
			i++
		case c == ',':
			trimOut()
			b.WriteByte(c)
			i++
			for i < n && val[i] == ' ' {
				i++
			}
		case c == '!':
			trimOut()
			b.WriteByte(c)
			i++
			for i < n && val[i] == ' ' {
				i++
			}
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}
