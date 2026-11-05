package css

import (
	"strings"
)

// ExpandApply expands `@apply utility...;` directives anywhere in user CSS
// into the declarations those utilities produce, using the given theme. Plain
// utilities become declarations. Variant utilities (`hover:*`, `md:*`) and
// descendant-selector utilities (`space-x-*`) become CSS nested rules inside
// the rule that holds the @apply (`&:hover { ... }`, `@media (...) { ... }`),
// placed after the rule's declarations. Unknown classes cannot be expressed,
// so they are dropped from the output and returned in `unresolved` for the
// caller to warn about.
func ExpandApply(cssText string, theme TailwindTheme) (string, []string) {
	g := NewTailwindGenerator()
	g.Theme = theme
	return g.ExpandApply(cssText)
}

// ExpandApply is the package-level ExpandApply running through this generator,
// so animations applied this way are recorded for KeyframesCSS.
func (g *TailwindGenerator) ExpandApply(cssText string) (string, []string) {
	if !strings.Contains(cssText, "@apply") {
		return cssText, nil
	}
	var out strings.Builder
	var unresolved []string
	seen := map[string]bool{}
	// pending[d] holds the nested rules @apply produced inside the block that
	// is open at depth d; they are written just before its closing brace so
	// declarations that follow an @apply stay ahead of nested rules.
	var pending [][]string

	i := 0
	for i < len(cssText) {
		c := cssText[i]
		switch {
		case c == '/' && i+1 < len(cssText) && cssText[i+1] == '*':
			end := skipCSSComment(cssText, i)
			out.WriteString(cssText[i:end])
			i = end
			continue
		case c == '"' || c == '\'':
			end := skipCSSString(cssText, i)
			out.WriteString(cssText[i:end])
			i = end
			continue
		case c == '@' && strings.HasPrefix(cssText[i:], "@apply") && i+6 < len(cssText) && isCSSSpace(cssText[i+6]):
			end := applyEnd(cssText, i)
			body := cssText[i+len("@apply") : end]
			decls, nested, bad := g.applyRules(strings.Fields(body))
			for _, u := range bad {
				if !seen[u] {
					seen[u] = true
					unresolved = append(unresolved, u)
				}
			}
			out.WriteString(decls)
			if nested != "" {
				if len(pending) > 0 {
					pending[len(pending)-1] = append(pending[len(pending)-1], nested)
				} else {
					out.WriteString(" " + nested)
				}
			}
			i = end
			if i < len(cssText) && cssText[i] == ';' {
				i++
			}
			continue
		case c == '{':
			pending = append(pending, nil)
		case c == '}' && len(pending) > 0:
			if rules := pending[len(pending)-1]; len(rules) > 0 {
				out.WriteString(" " + strings.Join(rules, " ") + " ")
			}
			pending = pending[:len(pending)-1]
		}
		out.WriteByte(c)
		i++
	}
	return out.String(), unresolved
}

// applyEnd returns the index of the ";" or "}" ending the @apply at start.
func applyEnd(s string, start int) int {
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '(', '[':
			depth++
		case ')', ']':
			if depth > 0 {
				depth--
			}
		case ';':
			if depth == 0 {
				return i
			}
		case '}':
			if depth == 0 {
				return i
			}
		case '"', '\'':
			i = skipCSSString(s, i) - 1
		}
	}
	return len(s)
}

// applyRules resolves utilities through the generator. It returns the joined
// declarations of the plain utilities, the nested rules of the variant ones,
// and the utilities it could not express.
func (g *TailwindGenerator) applyRules(utils []string) (string, string, []string) {
	important := false
	var plain []string
	for _, u := range utils {
		if u == "!important" {
			important = true
			continue
		}
		plain = append(plain, u)
	}
	var unresolved []string
	want := map[string]string{} // selector -> util
	set := map[string]bool{}
	var nested []string
	for _, u := range plain {
		variants, base := parseVariants(u)
		if len(variants) > 0 || selectorSuffix(strings.TrimPrefix(base, "!")) != "" {
			if rules, ok := g.nestedApplyRules(u, important); ok {
				nested = append(nested, rules)
			} else {
				unresolved = append(unresolved, u)
			}
			continue
		}
		set[u] = true
		want["."+EscapeClass(u)] = u
	}
	nestedText := strings.Join(nested, " ")
	if len(set) == 0 {
		return "", nestedText, unresolved
	}
	// Generate through a throwaway copy so strict bookkeeping is unaffected,
	// while keyframes used by animation utilities stay recorded on g.
	text := g.Generate(set)
	got := map[string]bool{}
	var decls []string
	for _, r := range topLevelRules(text) {
		util, ok := want[r.selector]
		if !ok {
			continue
		}
		body := strings.TrimSpace(r.body)
		if body == "" || strings.Contains(body, "{") {
			continue
		}
		got[util] = true
		if important {
			body = addImportant(body)
		}
		decls = append(decls, body)
	}
	for _, u := range plain {
		if set[u] && !got[u] {
			// Not a plain declaration list (`space-x-2` targets descendants).
			if rules, ok := g.nestedApplyRules(u, important); ok {
				nested = append(nested, rules)
			} else {
				unresolved = append(unresolved, u)
			}
		}
	}
	nestedText = strings.Join(nested, " ")
	return strings.Join(decls, " "), nestedText, unresolved
}

// nestedApplyRules expresses one utility that is not a plain declaration list
// (it carries variants, or targets descendants) as CSS nesting: the generated
// rule's own class becomes `&`, so `hover:underline` is
// `@media (hover: hover){&:hover{...}}` and `space-x-2` is
// `:where(& > :not(:last-child)){...}`. It reports false when the generator
// does not know the utility or its rule does not mention the class.
func (g *TailwindGenerator) nestedApplyRules(util string, important bool) (string, bool) {
	text := strings.TrimSpace(g.Generate(map[string]bool{util: true}))
	if text == "" {
		return "", false
	}
	out, ok := nestGeneratedRules(text, "."+EscapeClass(util), important)
	if !ok || out == "" {
		return "", false
	}
	return out, true
}

// nestGeneratedRules rewrites generator output for one class so it can sit
// inside the rule that applies it: at-rules are kept, the class selector
// becomes `&`.
func nestGeneratedRules(text, classSel string, important bool) (string, bool) {
	var b strings.Builder
	found := false
	i := 0
	for i < len(text) {
		open := strings.IndexByte(text[i:], '{')
		if open < 0 {
			break
		}
		open += i
		end := matchCSSBlock(text, open)
		if end < 0 {
			return "", false
		}
		head := strings.TrimSpace(text[i:open])
		body := text[open+1 : end]
		i = end + 1
		if strings.HasPrefix(head, "@") {
			inner, ok := nestGeneratedRules(body, classSel, important)
			if !ok {
				return "", false
			}
			if inner != "" {
				b.WriteString(head + "{" + inner + "}")
				found = true
			}
			continue
		}
		if !strings.Contains(head, classSel) {
			// A rule for something else (a helper the utility pulls in).
			return "", false
		}
		sel := strings.ReplaceAll(head, classSel, "&")
		body = strings.TrimSpace(body)
		if important && !strings.Contains(body, "{") {
			body = addImportant(body)
		}
		b.WriteString(sel + "{" + body + "}")
		found = true
	}
	return b.String(), found
}

type simpleRule struct{ selector, body string }

// topLevelRules splits generator output into its top-level rules.
func topLevelRules(text string) []simpleRule {
	var out []simpleRule
	i := 0
	for i < len(text) {
		open := strings.IndexByte(text[i:], '{')
		if open < 0 {
			break
		}
		open += i
		end := matchCSSBlock(text, open)
		if end < 0 {
			break
		}
		out = append(out, simpleRule{selector: strings.TrimSpace(text[i:open]), body: text[open+1 : end]})
		i = end + 1
	}
	return out
}
