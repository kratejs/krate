package build

import (
	"sort"
	"strings"
)

// Route patterns are slash-separated OutNames ("blog/[slug]", "docs/[...rest]",
// "shop/[[...filter]]"). Matching is shared by the static server, the dev
// server and the SSR server so every path resolves the same way.

type routeSegKind int

const (
	segStatic routeSegKind = iota
	segParam
	segCatchAll
	segOptionalCatchAll
)

type routeSeg struct {
	kind routeSegKind
	// text is the literal for static segments and the param name otherwise.
	text string
}

// parseRoutePattern splits a route pattern into typed segments.
func parseRoutePattern(pattern string) []routeSeg {
	trimmed := strings.Trim(pattern, "/")
	if trimmed == "" || trimmed == "." {
		return nil
	}
	parts := strings.Split(trimmed, "/")
	segs := make([]routeSeg, 0, len(parts))
	for _, p := range parts {
		segs = append(segs, parseRouteSeg(p))
	}
	return segs
}

func parseRouteSeg(p string) routeSeg {
	switch {
	case strings.HasPrefix(p, "[[...") && strings.HasSuffix(p, "]]") && len(p) > 7:
		return routeSeg{kind: segOptionalCatchAll, text: p[5 : len(p)-2]}
	case strings.HasPrefix(p, "[...") && strings.HasSuffix(p, "]") && len(p) > 5:
		return routeSeg{kind: segCatchAll, text: p[4 : len(p)-1]}
	case strings.HasPrefix(p, "[") && strings.HasSuffix(p, "]") && len(p) > 2:
		return routeSeg{kind: segParam, text: p[1 : len(p)-1]}
	}
	return routeSeg{kind: segStatic, text: p}
}

// isDynamicPattern reports whether a route pattern has any dynamic segment.
func isDynamicPattern(pattern string) bool {
	for _, s := range parseRoutePattern(pattern) {
		if s.kind != segStatic {
			return true
		}
	}
	return false
}

// matchRoutePattern matches a URL path against a route pattern. A param
// segment matches exactly one non-empty segment; a catch-all matches one or
// more remaining segments and an optional catch-all zero or more. Catch-all
// values are the matched segments joined with "/" ("" for an empty optional
// catch-all). Catch-alls must be the last segment.
func matchRoutePattern(urlPath, pattern string) (map[string]string, bool) {
	segs := parseRoutePattern(pattern)
	trimmed := strings.Trim(urlPath, "/")
	var parts []string
	if trimmed != "" {
		parts = strings.Split(trimmed, "/")
	}
	params := map[string]string{}
	for i, s := range segs {
		switch s.kind {
		case segCatchAll, segOptionalCatchAll:
			if i != len(segs)-1 {
				return nil, false
			}
			rest := parts[min(i, len(parts)):]
			if s.kind == segCatchAll && len(rest) == 0 {
				return nil, false
			}
			for _, r := range rest {
				if r == "" {
					return nil, false
				}
			}
			params[s.text] = strings.Join(rest, "/")
			return params, true
		}
		if i >= len(parts) {
			return nil, false
		}
		switch s.kind {
		case segParam:
			if parts[i] == "" {
				return nil, false
			}
			params[s.text] = parts[i]
		default:
			if parts[i] != s.text {
				return nil, false
			}
		}
	}
	if len(parts) != len(segs) {
		return nil, false
	}
	return params, true
}

// compareRouteSpecificity orders two patterns: negative when a is more
// specific than b. Segments are compared left to right by kind (static >
// dynamic > catch-all > optional catch-all); at the first difference the
// more specific kind wins, then longer patterns win, then the pattern text
// breaks ties so the order is deterministic.
func compareRouteSpecificity(a, b string) int {
	sa, sb := parseRoutePattern(a), parseRoutePattern(b)
	for i := 0; i < len(sa) && i < len(sb); i++ {
		if sa[i].kind != sb[i].kind {
			return int(sa[i].kind) - int(sb[i].kind)
		}
		if sa[i].kind == segStatic && sa[i].text != sb[i].text {
			return strings.Compare(sa[i].text, sb[i].text)
		}
	}
	if len(sa) != len(sb) {
		return len(sb) - len(sa)
	}
	return strings.Compare(a, b)
}

// sortRoutePatterns sorts patterns most specific first (see
// compareRouteSpecificity).
func sortRoutePatterns(patterns []string) {
	sort.SliceStable(patterns, func(i, j int) bool { return compareRouteSpecificity(patterns[i], patterns[j]) < 0 })
}
