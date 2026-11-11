package windows

import (
	"net/url"
	"path/filepath"
	"strings"
)

// Origin is the app origin served by the Go handler.
const Origin = "https://app.localhost"

// originHost is the host part of Origin.
const originHost = "app.localhost"

// resourceFilter is the WebResourceRequested filter pattern.
const resourceFilter = Origin + "/*"

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

// userDataDir returns %LOCALAPPDATA%\<AppID>\WebView2 for a stable profile.
func userDataDir(localAppData, appID string) string {
	id := strings.Map(func(r rune) rune {
		switch r {
		case '\\', '/', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		if r < 0x20 {
			return '_'
		}
		return r
	}, appID)
	id = strings.Trim(id, " .")
	if id == "" || id == "_" {
		id = "krate-app"
	}
	return filepath.Join(localAppData, id, "WebView2")
}

// loaderDLLCandidates lists where EmbeddedBrowserWebView.dll may live for an
// installed Evergreen runtime, given the registry values. location is the
// "location" value (the Application directory, sometimes already including
// the version), pv the version and arch one of x64, arm64.
func loaderDLLCandidates(location, pv, arch string) []string {
	if location == "" {
		return nil
	}
	const dll = "EmbeddedBrowserWebView.dll"
	var out []string
	add := func(base string) {
		out = append(out, filepath.Join(base, "EBWebView", arch, dll))
	}
	add(location)
	if pv != "" && !strings.EqualFold(filepath.Base(location), pv) {
		add(filepath.Join(location, pv))
	}
	return out
}
