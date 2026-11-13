package krate

import "strings"

// WindowQuery is the query parameter that tells a page which window it is in.
// The shell adds it to the URL of a scoped window; a small script it injects
// into every HTML page stores the value for the browsing context and reports
// it as the x-krate-window header of the page's host calls. Native windows
// do not trust that header (the shell binds requests to the webview they came
// from); browser mode does, having nothing else.
const WindowQuery = "__krate_win"

// ScopedPath adds the window query to a route for the window with this id.
func ScopedPath(path, id string) string {
	if path == "" {
		path = "/"
	}
	frag := ""
	if i := strings.IndexByte(path, '#'); i >= 0 {
		path, frag = path[:i], path[i:]
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + WindowQuery + "=" + id + frag
}
