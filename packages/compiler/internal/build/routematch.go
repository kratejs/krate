package build

import (
	"sort"
	"strings"
	"sync"
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

// routePattern is a route pattern parsed once. Matching it allocates only on a
// hit.
type routePattern struct {
	raw  string
	segs []routeSeg
	// valid is false when a catch-all is not the last segment; such a pattern
	// never matches.
	valid bool
}

func compileRoutePattern(pattern string) *routePattern {
	segs := parseRoutePattern(pattern)
	rp := &routePattern{raw: pattern, segs: segs, valid: true}
	for i, s := range segs {
		if (s.kind == segCatchAll || s.kind == segOptionalCatchAll) && i != len(segs)-1 {
			rp.valid = false
		}
	}
	return rp
}

var routePatternCache sync.Map // pattern string -> *routePattern

// cachedRoutePattern returns the compiled form of pattern, parsing it once per
// distinct pattern string.
func cachedRoutePattern(pattern string) *routePattern {
	if v, ok := routePatternCache.Load(pattern); ok {
		return v.(*routePattern)
	}
	v, _ := routePatternCache.LoadOrStore(pattern, compileRoutePattern(pattern))
	return v.(*routePattern)
}

// splitURLPath splits a URL path into its non-root segments. buf is scratch
// space for the result; pass a stack array to avoid a heap allocation.
func splitURLPath(urlPath string, buf []string) []string {
	trimmed := strings.Trim(urlPath, "/")
	if trimmed == "" {
		return buf[:0]
	}
	parts := buf[:0]
	for {
		i := strings.IndexByte(trimmed, '/')
		if i < 0 {
			return append(parts, trimmed)
		}
		parts = append(parts, trimmed[:i])
		trimmed = trimmed[i+1:]
	}
}

// matches reports whether the pre-split URL parts match the pattern. It does
// not allocate.
func (rp *routePattern) matches(parts []string) bool {
	if !rp.valid {
		return false
	}
	for i, s := range rp.segs {
		switch s.kind {
		case segCatchAll, segOptionalCatchAll:
			rest := parts[min(i, len(parts)):]
			if s.kind == segCatchAll && len(rest) == 0 {
				return false
			}
			for _, r := range rest {
				if r == "" {
					return false
				}
			}
			return true
		case segParam:
			if i >= len(parts) || parts[i] == "" {
				return false
			}
		default:
			if i >= len(parts) || parts[i] != s.text {
				return false
			}
		}
	}
	return len(parts) == len(rp.segs)
}

// params extracts the dynamic values from parts. It must only be called after
// matches returned true.
func (rp *routePattern) params(parts []string) map[string]string {
	params := make(map[string]string, len(rp.segs))
	for i, s := range rp.segs {
		switch s.kind {
		case segParam:
			params[s.text] = parts[i]
		case segCatchAll, segOptionalCatchAll:
			params[s.text] = strings.Join(parts[min(i, len(parts)):], "/")
		}
	}
	return params
}

// match matches pre-split URL parts, allocating the params map only on a hit.
func (rp *routePattern) match(parts []string) (map[string]string, bool) {
	if !rp.matches(parts) {
		return nil, false
	}
	return rp.params(parts), true
}

// matchRoutePattern matches a URL path against a route pattern. A param
// segment matches exactly one non-empty segment; a catch-all matches one or
// more remaining segments and an optional catch-all zero or more. Catch-all
// values are the matched segments joined with "/" ("" for an empty optional
// catch-all). Catch-alls must be the last segment.
func matchRoutePattern(urlPath, pattern string) (map[string]string, bool) {
	var buf [16]string
	return cachedRoutePattern(pattern).match(splitURLPath(urlPath, buf[:]))
}

// compareRouteSpecificity orders two patterns: negative when a is more
// specific than b. Segments are compared left to right by kind (static >
// dynamic > catch-all > optional catch-all); at the first difference the
// more specific kind wins, then longer patterns win, then the pattern text
// breaks ties so the order is deterministic. A pattern whose only extra
// segment is a trailing optional catch-all is the less specific one: "shop"
// answers /shop before "shop/[[...filter]]" does.
func compareRouteSpecificity(a, b string) int {
	sa, sb := cachedRoutePattern(a).segs, cachedRoutePattern(b).segs
	for i := 0; i < len(sa) && i < len(sb); i++ {
		if sa[i].kind != sb[i].kind {
			return int(sa[i].kind) - int(sb[i].kind)
		}
		if sa[i].kind == segStatic && sa[i].text != sb[i].text {
			return strings.Compare(sa[i].text, sb[i].text)
		}
	}
	if len(sa) != len(sb) {
		if len(sa) > len(sb) && sa[len(sb)].kind == segOptionalCatchAll {
			return 1
		}
		if len(sb) > len(sa) && sb[len(sa)].kind == segOptionalCatchAll {
			return -1
		}
		return len(sb) - len(sa)
	}
	return strings.Compare(a, b)
}

// sortRoutePatterns sorts patterns most specific first (see
// compareRouteSpecificity).
func sortRoutePatterns(patterns []string) {
	sort.SliceStable(patterns, func(i, j int) bool { return compareRouteSpecificity(patterns[i], patterns[j]) < 0 })
}
