package css

import "strings"

// ExpandApply expands `@apply utility...;` directives in user CSS into the
// declarations those utilities produce, using the given theme. This is a v1
// implementation: it supports plain utilities. Variant utilities (`hover:*`,
// `md:*`), descendant-selector utilities (`space-x-*`), and unknown classes
// cannot be represented as plain declarations, so they are left unexpanded and
// returned in `unresolved` for the caller to warn about.
func ExpandApply(cssText string, theme TailwindTheme) (string, []string) {
	if !strings.Contains(cssText, "@apply") {
		return cssText, nil
	}
	var out strings.Builder
	var unresolved []string
	seen := map[string]bool{}
	note := func(util string) {
		if !seen[util] {
			seen[util] = true
			unresolved = append(unresolved, util)
		}
	}

	for _, line := range strings.Split(cssText, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "@apply") {
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(trimmed, "@apply"))
		body = strings.TrimSuffix(body, ";")

		var decls []string
		for _, util := range strings.Fields(body) {
			variants, base := parseVariants(util)
			b := strings.TrimPrefix(base, "!")
			if len(variants) > 0 || selectorSuffix(b) != "" {
				note(util)
				continue
			}
			text, _ := classToRule(util, theme)
			if text == "" {
				note(util)
				continue
			}
			open := strings.IndexByte(text, '{')
			close := strings.LastIndexByte(text, '}')
			if open < 0 || close <= open {
				note(util)
				continue
			}
			if d := strings.TrimSpace(text[open+1 : close]); d != "" {
				decls = append(decls, d)
			}
		}
		if len(decls) > 0 {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			out.WriteString(indent)
			out.WriteString(strings.Join(decls, " "))
			out.WriteByte('\n')
		}
	}
	return out.String(), unresolved
}
