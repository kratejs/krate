package plugin

import (
	"net/url"
	"path"
	"strings"
)

// CanonicalRoutePath returns the URL path a statically generated page is
// served at: "/" for the root and "/x/y/" (trailing slash) for directory
// pages, which is what `krate serve` redirects to. A last segment with a file
// extension (feed.xml) keeps no trailing slash. Every canonical URL, og:url,
// sitemap entry and hreflang link uses this one rule.
func CanonicalRoutePath(route string) string {
	route = strings.TrimSpace(route)
	if route == "" || route == "." || route == "/" {
		return "/"
	}
	clean := path.Clean("/" + strings.Trim(route, "/"))
	if clean == "/" {
		return "/"
	}
	if strings.Contains(path.Base(clean), ".") {
		return clean
	}
	return clean + "/"
}

// CanonicalURL joins site origin, base path and route into an absolute,
// percent-encoded URL (the route is not XML/HTML escaped).
func CanonicalURL(origin, basePath, route string) string {
	origin = strings.TrimRight(origin, "/")
	basePath = strings.TrimRight(strings.TrimSpace(basePath), "/")
	if basePath != "" && !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	if basePath != "" && strings.HasSuffix(origin, basePath) {
		basePath = ""
	}
	p := CanonicalRoutePath(route)
	u := url.URL{Path: basePath + p}
	return origin + u.EscapedPath()
}
