package darwin

import (
	"net/http"
	"strings"
)

// hopByHop lists headers that describe a single transport hop and are
// meaningless (or harmful) on a response handed to WebKit.
var hopByHop = map[string]bool{
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Connection":    true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
	"Te":                  true,
	"Trailer":             true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
}

// responseFields flattens a Go header into the single-value dictionary that
// NSHTTPURLResponse takes. Repeated headers are joined with ", ".
func responseFields(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, vs := range h {
		ck := http.CanonicalHeaderKey(k)
		if hopByHop[ck] || len(vs) == 0 {
			continue
		}
		out[ck] = strings.ToValidUTF8(strings.Join(vs, ", "), "�")
	}
	return out
}

// requestHeader converts NSURLRequest.allHTTPHeaderFields into a Go header.
func requestHeader(fields map[string]string) http.Header {
	h := make(http.Header, len(fields))
	for k, v := range fields {
		h.Set(k, v)
	}
	return h
}
