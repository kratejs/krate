package darwin

import "strings"

// pathSanitizer drops characters that would make NSURL reject the string.
var pathSanitizer = strings.NewReplacer(" ", "%20", "\n", "", "\r", "", "\t", "", "\x00", "")

// appURL turns a route ("/", "/a?b=1#c", "a") into a full app URL. The input
// is taken to be in URL form already (percent-encoded where needed).
func appURL(path string) string {
	if path == "" {
		path = "/"
	}
	if path[0] != '/' {
		path = "/" + path
	}
	return OriginURL + pathSanitizer.Replace(path)
}
