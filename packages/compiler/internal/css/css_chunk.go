package css

import "strings"

// SplitCSSRules splits a stylesheet into top-level rule units (used by the
// build to prune unused CSS-module rules). Nested at-rules
// (`@media`, `@supports`, `@keyframes`) are kept whole; strings and comments are
// honoured so braces inside them do not split a rule.
func SplitCSSRules(cssText string) []string {
	var rules []string
	i, n := 0, len(cssText)
	start := 0
	depth := 0
	for i < n {
		c := cssText[i]
		switch {
		case c == '\\':
			i += 2
		case c == '/' && i+1 < n && cssText[i+1] == '*':
			j := strings.Index(cssText[i+2:], "*/")
			if j < 0 {
				i = n
				continue
			}
			i += 2 + j + 2
		case c == '"' || c == '\'':
			i = cssSkipString(cssText, i)
		case c == '{':
			depth++
			i++
		case c == '}':
			depth--
			i++
			if depth == 0 {
				if r := strings.TrimSpace(cssText[start:i]); r != "" {
					rules = append(rules, r)
				}
				start = i
			}
		default:
			i++
		}
	}
	if r := strings.TrimSpace(cssText[start:]); r != "" {
		rules = append(rules, r)
	}
	return rules
}
