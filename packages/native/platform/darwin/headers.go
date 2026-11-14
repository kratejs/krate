package darwin

import (
	"net/http"
)

// requestHeader converts NSURLRequest.allHTTPHeaderFields into a Go header.
func requestHeader(fields map[string]string) http.Header {
	h := make(http.Header, len(fields))
	for k, v := range fields {
		h.Set(k, v)
	}
	return h
}
