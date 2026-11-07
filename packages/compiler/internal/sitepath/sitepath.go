// Package sitepath joins the site base path onto root-relative URLs. It is the
// one rule every emitter shares, so generated assets, links and stylesheets
// cannot disagree about what gets prefixed.
package sitepath

import "strings"

// IsRootRelative reports whether u is a path from the site root: "/x" but not
// "//host/x", which is protocol-relative.
func IsRootRelative(u string) bool {
	return strings.HasPrefix(u, "/") && !strings.HasPrefix(u, "//")
}

// Join prefixes a root-relative URL with base ("" or "/prefix"). Relative
// URLs, fragments, queries and absolute URLs are returned unchanged.
func Join(base, u string) string {
	if base == "" || !IsRootRelative(u) {
		return u
	}
	return base + u
}
