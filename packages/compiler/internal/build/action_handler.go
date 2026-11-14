package build

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// actionHandler guards POST /__krate/action/<id> before it reaches the action
// sidecar: POST only, same-origin (CSRF), a request body limit, no caching,
// and a sanitized same-origin return path for native (no-JS) form posts. The
// return path comes from a permalink (`__krate_return` query, set by
// useActionState), the client's X-Krate-Return header, or the Referer.
func actionHandler(next http.Handler, limit int64) http.HandlerFunc {
	return actionHandlerSegments(next, limit, nil)
}

// actionHandlerSegments is actionHandler plus segment pages for actions that
// call notFound(), forbidden() or unauthorized() from a native form post.
func actionHandlerSegments(next http.Handler, limit int64, segs *segmentIndex) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// The sidecar sets no-store too; the proxy would copy it as a second line.
		w = &singleCacheControlWriter{ResponseWriter: w}
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
		if !fetchMode && segs != nil {
			// A native form post whose action called notFound(), forbidden() or
			// unauthorized() gets the nearest segment page of the page it came from.
			w = &actionFallbackWriter{ResponseWriter: w, segs: segs, from: ret, req: r}
		}
		next.ServeHTTP(w, r)
	}
}

// actionFallbackWriter replaces the plain-text answer the action sidecar gives
// for notFound(), forbidden() and unauthorized() (marked X-Krate-Fallback) with
// the segment page nearest to the page the form was posted from.
type actionFallbackWriter struct {
	http.ResponseWriter
	segs    *segmentIndex
	from    string
	req     *http.Request
	swapped bool
}

func (w *actionFallbackWriter) WriteHeader(code int) {
	h := w.Header()
	if h.Get("X-Krate-Fallback") != "" && (code == http.StatusUnauthorized || code == http.StatusForbidden || code == http.StatusNotFound) {
		kind := SegmentNotFound
		switch code {
		case http.StatusForbidden:
			kind = SegmentForbidden
		case http.StatusUnauthorized:
			kind = SegmentUnauthorized
		}
		urlPath := "/"
		if u, err := url.Parse(w.from); err == nil && u.Path != "" {
			urlPath = u.Path
		}
		if m, ok := w.segs.forURL(kind, urlPath, ""); ok {
			h.Del("X-Krate-Fallback")
			if respondSegment(w.ResponseWriter, w.req, w.segs, m, code, nil) {
				w.swapped = true
				return
			}
		}
		h.Del("X-Krate-Fallback")
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *actionFallbackWriter) Write(b []byte) (int, error) {
	if w.swapped {
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}

func (w *actionFallbackWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *actionFallbackWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// singleCacheControlWriter collapses repeated identical Cache-Control values
// into one when the headers are committed. The host sets no-store before the
// reverse proxy copies the sidecar's own no-store on top of it.
type singleCacheControlWriter struct {
	http.ResponseWriter
	wrote bool
}

func (w *singleCacheControlWriter) commit() {
	if w.wrote {
		return
	}
	w.wrote = true
	h := w.Header()
	vals := h.Values("Cache-Control")
	if len(vals) < 2 {
		return
	}
	var uniq []string
	for _, v := range vals {
		dup := false
		for _, u := range uniq {
			if strings.EqualFold(u, v) {
				dup = true
				break
			}
		}
		if !dup {
			uniq = append(uniq, v)
		}
	}
	if len(uniq) < len(vals) {
		h["Cache-Control"] = uniq
	}
}

func (w *singleCacheControlWriter) WriteHeader(code int) {
	w.commit()
	w.ResponseWriter.WriteHeader(code)
}

func (w *singleCacheControlWriter) Write(b []byte) (int, error) {
	w.commit()
	return w.ResponseWriter.Write(b)
}

func (w *singleCacheControlWriter) Flush() {
	w.commit()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *singleCacheControlWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
