package caps

import (
	"net/url"
	"path"
	"strings"
)

// ParseURIList splits a text/uri-list body (RFC 2483): one URI per line,
// lines that start with "#" are comments.
func ParseURIList(s string) []string {
	var out []string
	for _, line := range strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' }) {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		out = append(out, line)
	}
	return out
}

// PathsFromURIs returns the local file paths named by file:// URIs, in order
// and without duplicates. A URI of another scheme, or of a file on another
// host, is left out.
func PathsFromURIs(uris []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range uris {
		u, err := url.Parse(strings.TrimSpace(s))
		if err != nil || !strings.EqualFold(u.Scheme, "file") || (u.Host != "" && !strings.EqualFold(u.Host, "localhost")) {
			continue
		}
		p := u.Path
		if p != "" {
			p = path.Clean(p)
		}
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}
