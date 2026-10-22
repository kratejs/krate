package build

import (
	"encoding/json"
	"net/http"
	"strings"
)

// actionHandler guards POST /__krate/action/<id> before it reaches the action
// sidecar: POST only, same-origin (CSRF), a request body limit, no caching,
// and a sanitized same-origin return path for native (no-JS) form posts. The
// return path comes from a permalink (`__krate_return` query, set by
// useActionState), the client's X-Krate-Return header, or the Referer.
func actionHandler(next http.Handler, limit int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		fetchMode := strings.EqualFold(r.Header.Get("X-Krate-Action"), "fetch")
		fail := func(status int, msg string) {
			if fetchMode || strings.Contains(r.Header.Get("Accept"), "application/json") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": map[string]string{"message": msg}})
				return
			}
			http.Error(w, msg, status)
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			fail(http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !sameOriginActionRequest(r) {
			fail(http.StatusForbidden, "cross-site server action rejected")
			return
		}
		if limit > 0 && r.ContentLength > limit {
			fail(http.StatusRequestEntityTooLarge, "server action request body too large")
			return
		}
		if r.Body != nil && limit > 0 {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		ret := sanitizeActionReturn(r.URL.Query().Get("__krate_return"), r)
		if ret == "" {
			ret = sanitizeActionReturn(r.Header.Get("X-Krate-Return"), r)
		}
		if ret == "" && !fetchMode {
			ret = sanitizeActionReturn(r.Header.Get("Referer"), r)
		}
		if ret != "" {
			r.Header.Set("X-Krate-Return", ret)
		} else {
			r.Header.Del("X-Krate-Return")
		}
		next.ServeHTTP(w, r)
	}
}
