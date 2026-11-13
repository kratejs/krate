package windows

import (
	"net/url"
	"strings"
)

// Origin is the app origin served by the Go handler.
const Origin = "https://app.localhost"

// originHost is the host part of Origin.
const originHost = "app.localhost"

// navURL returns the absolute URL for a route path on the app origin.
func navURL(path string) string {
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return Origin + path
}

// allowedNavigation reports whether a top-level navigation may proceed: only
// https://app.localhost (default port) is allowed.
func allowedNavigation(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "https" && u.Host == originHost && u.User == nil
}

// allowedFrameNavigation is allowedNavigation plus the empty documents a page
// creates for its own iframes (about:blank, about:srcdoc).
func allowedFrameNavigation(raw string) bool {
	return raw == "about:blank" || raw == "about:srcdoc" || allowedNavigation(raw)
}
