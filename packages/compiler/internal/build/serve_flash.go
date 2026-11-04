package build

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/escape"
)

// flashCookie is the cookie the action endpoint sets after a native form post
// that returned state (see action_sidecar.go).
const flashCookie = "krate_flash"

type flashPayload struct {
	A string          `json:"a"`
	R json.RawMessage `json:"r"`
}

// requestFlash returns the decoded flash cookie of a request.
func requestFlash(r *http.Request) (string, flashPayload, bool) {
	c, err := r.Cookie(flashCookie)
	if err != nil || c.Value == "" {
		return "", flashPayload{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(c.Value, "="))
	if err != nil {
		return "", flashPayload{}, false
	}
	var p flashPayload
	if json.Unmarshal(raw, &p) != nil || p.A == "" {
		return "", flashPayload{}, false
	}
	return c.Value, p, true
}

// hasFlashCookie reports a page request that carries flash state, which must
// not be answered from a build-time snapshot.
func hasFlashCookie(r *http.Request) bool {
	_, _, ok := requestFlash(r)
	return ok
}

// flashMessages extracts the human-readable strings of an action's returned
// state: a string, or the string values of an object (error, message,
// errors: {field: text}, lists of strings) in a stable order.
func flashMessages(raw json.RawMessage) []string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	var out []string
	var walk func(v any, depth int)
	walk = func(v any, depth int) {
		if depth > 3 {
			return
		}
		switch t := v.(type) {
		case string:
			if s := strings.TrimSpace(t); s != "" {
				out = append(out, s)
			}
		case []any:
			for _, x := range t {
				walk(x, depth+1)
			}
		case map[string]any:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				switch t[k].(type) {
				case string, []any, map[string]any:
					walk(t[k], depth+1)
				}
			}
		}
	}
	walk(v, 0)
	return out
}

type bufferedResponse struct {
	h      http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header { return b.h }
func (b *bufferedResponse) WriteHeader(code int) {
	if b.status == 0 {
		b.status = code
	}
}
func (b *bufferedResponse) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(p)
}
func (b *bufferedResponse) Flush() {}

// flashNoticeMiddleware serves no-JS form feedback. After a native form post
// the action endpoint redirects back with the returned state in a krate_flash
// cookie; pages are pre-rendered, so a browser that never runs the script
// would otherwise not see it. For a page request carrying a flash cookie, the
// strings of that state are rendered into a notice at the top of the form that
// posts to the action, and the cookie is cleared (single use). The notice
// carries the payload in data-krate-flash so the hydrated form can still
// start from it and remove the notice.
func flashNoticeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || (path.Ext(r.URL.Path) != "" && !isHTMLDocRequest(r.URL.Path)) {
			next.ServeHTTP(w, r)
			return
		}
		raw, payload, ok := requestFlash(r)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		// The page is rendered fresh with the notice, so a cached copy must not
		// be answered with 304 or a partial range.
		r = r.Clone(r.Context())
		for _, h := range []string{"If-None-Match", "If-Modified-Since", "If-Match", "If-Unmodified-Since", "If-Range", "Range"} {
			r.Header.Del(h)
		}
		buf := &bufferedResponse{h: http.Header{}}
		next.ServeHTTP(buf, r)
		body := buf.body.Bytes()
		if buf.status == 0 {
			buf.status = http.StatusOK
		}
		if buf.status == http.StatusOK && strings.HasPrefix(buf.h.Get("Content-Type"), "text/html") && buf.h.Get("Content-Encoding") == "" {
			if injected, ok := injectFlashNotice(body, raw, payload); ok {
				body = injected
				buf.h.Set("Cache-Control", "no-store")
				buf.h.Del("Etag")
				buf.h.Del("Last-Modified")
				buf.h.Add("Set-Cookie", flashCookie+"=; Path=/; Max-Age=0; SameSite=Lax")
			}
		}
		for k, v := range buf.h {
			w.Header()[k] = v
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(buf.status)
		_, _ = w.Write(body)
	})
}

// injectFlashNotice inserts the notice after the opening tag of the form whose
// action is the one that produced the flash. It reports false when the page has
// no such form or the state has nothing to show.
func injectFlashNotice(page []byte, cookieValue string, p flashPayload) ([]byte, bool) {
	msgs := flashMessages(p.R)
	if len(msgs) == 0 {
		return nil, false
	}
	re := regexp.MustCompile(`(?is)<form\b[^>]*?\baction\s*=\s*["']?[^"'\s>]*/__krate/action/` + regexp.QuoteMeta(p.A) + `[^>]*>`)
	loc := re.FindIndex(page)
	if loc == nil {
		return nil, false
	}
	var n strings.Builder
	n.WriteString(`<div class="krate-flash" role="alert" data-krate-action="`)
	n.WriteString(escape.HTMLAttr(p.A))
	n.WriteString(`" data-krate-flash="`)
	n.WriteString(escape.HTMLAttr(cookieValue))
	n.WriteString(`">`)
	for _, m := range msgs {
		n.WriteString("<p>")
		n.WriteString(escape.HTML(m))
		n.WriteString("</p>")
	}
	n.WriteString("</div>")
	out := make([]byte, 0, len(page)+n.Len())
	out = append(out, page[:loc[1]]...)
	out = append(out, n.String()...)
	out = append(out, page[loc[1]:]...)
	return out, true
}
