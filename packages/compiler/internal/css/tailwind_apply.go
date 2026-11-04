package css

import (
	"strings"
)

// ExpandApply expands `@apply utility...;` directives anywhere in user CSS
// into the declarations those utilities produce, using the given theme. It
// supports plain utilities. Variant utilities (`hover:*`, `md:*`),
// descendant-selector utilities (`space-x-*`), and unknown classes cannot be
// represented as plain declarations, so they are dropped from the output and
// returned in `unresolved` for the caller to warn about.
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
			decls, bad := g.applyDecls(strings.Fields(body))
			for _, u := range bad {
				if !seen[u] {
					seen[u] = true
					unresolved = append(unresolved, u)
				}
			}
			out.WriteString(decls)
			i = end
			if i < len(cssText) && cssText[i] == ';' {
				i++
			}
			continue
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

// applyDecls resolves utilities through the generator in its sort order and
// returns the joined declarations plus the utilities it could not express.
func (g *TailwindGenerator) applyDecls(utils []string) (string, []string) {
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
	for _, u := range plain {
		variants, base := parseVariants(u)
		if len(variants) > 0 || selectorSuffix(strings.TrimPrefix(base, "!")) != "" {
			unresolved = append(unresolved, u)
			continue
		}
		set[u] = true
		want["."+EscapeClass(u)] = u
	}
	if len(set) == 0 {
		return "", unresolved
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
			unresolved = append(unresolved, u)
		}
	}
	return strings.Join(decls, " "), unresolved
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
