// Package routeconv lists the file names the build treats as route
// conventions (layouts, templates, loading and error boundaries, segment
// pages) instead of ordinary pages. Generators that emit page files use it
// to avoid colliding with them.
package routeconv

import (
	"path"
	"strings"
)

var (
	LayoutNames   = []string{"_layout.tsx", "_layout.ts", "_layout.jsx", "_layout.js"}
	TemplateNames = []string{"_template.tsx", "_template.ts", "_template.jsx", "_template.js", "template.tsx", "template.ts", "template.jsx", "template.js"}
	LoadingNames  = []string{"loading.tsx", "loading.ts", "loading.jsx", "loading.js"}
	// ErrorBoundaryNames become a client error boundary. 500.tsx is only the
	// server error page, so existing static 500 pages stay static.
	ErrorBoundaryNames = []string{"error.tsx", "error.ts", "error.jsx", "error.js"}
)

// SegmentBases maps a segment page base name (no extension) to its kind.
var SegmentBases = map[string]string{
	"404":       "notFound",
	"not-found": "notFound",
	"500":       "error",
	"error":     "error",

	"403":          "forbidden",
	"forbidden":    "forbidden",
	"401":          "unauthorized",
	"unauthorized": "unauthorized",
}

// ReservedBase reports whether a page file with this base name (no
// extension) would be treated as a route convention file, or skipped as a
// private file, rather than as a page.
func ReservedBase(base string) bool {
	if strings.HasPrefix(base, "_") {
		return true
	}
	if _, ok := SegmentBases[base]; ok {
		return true
	}
	for _, names := range [][]string{LayoutNames, TemplateNames, LoadingNames, ErrorBoundaryNames} {
		for _, n := range names {
			if strings.TrimSuffix(n, path.Ext(n)) == base {
				return true
			}
		}
	}
	return false
}
