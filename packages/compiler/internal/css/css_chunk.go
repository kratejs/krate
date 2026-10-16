package css

import (
	"sort"
	"strings"
)

// SplitCSSRules splits a stylesheet into top-level rule units. Nested at-rules
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

// canonicalRule normalizes a rule to a stable key so rules with different
// whitespace (or emitted on different pages) compare equal.
func canonicalRule(rule string) string {
	return strings.Join(strings.Fields(rule), " ")
}

// ChunkCSS splits page stylesheets into a shared chunk (rules used on more than
// one page) and a per-page remainder. The shared chunk is sorted by canonical
// key for deterministic hashing; per-page remnants preserve original order so
// page-specific overrides still follow the shared rules.
func ChunkCSS(pages []string) (shared string, perPage []string) {
	rules := make([][]string, len(pages))
	counts := map[string]int{}
	for i, p := range pages {
		for _, r := range SplitCSSRules(p) {
			k := canonicalRule(r)
			if k == "" {
				continue
			}
			rules[i] = append(rules[i], r)
			counts[k]++
		}
	}

	sharedSet := map[string]bool{}
	for k, c := range counts {
		if c >= 2 {
			sharedSet[k] = true
		}
	}

	seen := map[string]bool{}
	var sharedRules []string
	for _, rs := range rules {
		for _, r := range rs {
			k := canonicalRule(r)
			if sharedSet[k] && !seen[k] {
				seen[k] = true
				sharedRules = append(sharedRules, r)
			}
		}
	}
	sort.Slice(sharedRules, func(a, b int) bool {
		return canonicalRule(sharedRules[a]) < canonicalRule(sharedRules[b])
	})

	perPage = make([]string, len(pages))
	for i, rs := range rules {
		var b strings.Builder
		for _, r := range rs {
			if sharedSet[canonicalRule(r)] {
				continue
			}
			b.WriteString(r)
			b.WriteByte('\n')
		}
		perPage[i] = strings.TrimSpace(b.String())
	}
	return strings.Join(sharedRules, "\n"), perPage
}
