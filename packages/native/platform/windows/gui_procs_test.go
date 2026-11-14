//go:build windows

package windows

import (
	"io"
	"net/http"
)

// GUI checks for the process features: offscreen capture (CapturePreview),
// the DevTools protocol commands and browser extensions. They run with the
// other GUI tests under KRATE_NATIVE_GUI=1 in the CI native job.

// withProcPages wraps h with the pages these tests load.
func withProcPages(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redpage":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, `<!doctype html><title>red</title><body style="margin:0;background:#ff0000">red</body>`)
		case "/extpage":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, `<!doctype html><title>ext</title><body>ext</body>`)
		default:
			h.ServeHTTP(w, r)
		}
	})
}
