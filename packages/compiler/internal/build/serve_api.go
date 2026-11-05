package build

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
)

// proxyTrust is set once at startup from server.trustProxy. When false the
// server ignores client-supplied X-Forwarded-* headers; when true it honors
// them (request URLs, CSRF origin check, client IP).
var proxyTrust atomic.Bool

// proxyHops is server.trustedProxies: how many proxies sit in front of the
// server when proxyTrust is set.
var proxyHops atomic.Int32

// firstForwarded returns the first element of a comma-separated forwarded
// header value.
func firstForwarded(v string) string {
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// requestHost is the host the client addressed: the Host header, or
// X-Forwarded-Host behind a trusted proxy.
func requestHost(r *http.Request) string {
	if proxyTrust.Load() {
		if h := firstForwarded(r.Header.Get("X-Forwarded-Host")); h != "" {
			return h
		}
	}
	return r.Host
}

// requestScheme is "https" for TLS connections (or a trusted
// X-Forwarded-Proto), else "http".
func requestScheme(r *http.Request) string {
	if proxyTrust.Load() {
		switch p := strings.ToLower(firstForwarded(r.Header.Get("X-Forwarded-Proto"))); p {
		case "http", "https":
			return p
		}
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// requestBaseURL builds scheme://host for the request as the client saw it.
func requestBaseURL(r *http.Request) string {
	return requestScheme(r) + "://" + requestHost(r)
}

// requestFullURL is the absolute URL handed to JS handlers and middleware.
func requestFullURL(r *http.Request) string {
	return requestBaseURL(r) + r.URL.RequestURI()
}

// clientIP is the peer address, or behind a trusted proxy the X-Forwarded-For
// entry that trustedProxies hops (default 1) from the right. The leftmost entry
// is whatever the client sent, so it never decides the address.
// forwardedHeadersMiddleware has already appended the peer as the last entry.
func clientIP(r *http.Request) string {
	if proxyTrust.Load() {
		var chain []string
		for _, part := range strings.Split(r.Header.Get("X-Forwarded-For"), ",") {
			if part = strings.TrimSpace(part); part != "" {
				chain = append(chain, part)
			}
		}
		if n := len(chain); n > 0 {
			hops := int(proxyHops.Load())
			if hops < 1 {
				hops = 1
			}
			i := n - 1 - hops
			if i < 0 {
				i = 0
			}
			return chain[i]
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// forwardedHeadersMiddleware makes X-Forwarded-* reliable for everything
// downstream (JS handlers, sidecars, migrated code reading x-forwarded-for).
// Without a trusted proxy the client-supplied values are replaced by what the
// server observed; with one, the observed peer is appended to the chain.
func forwardedHeadersMiddleware(trust bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			peer = r.RemoteAddr
		}
		if !trust {
			r.Header.Del("Forwarded")
			r.Header.Del("X-Real-Ip")
			r.Header.Set("X-Forwarded-For", peer)
			r.Header.Set("X-Forwarded-Host", r.Host)
			if r.TLS != nil {
				r.Header.Set("X-Forwarded-Proto", "https")
			} else {
				r.Header.Set("X-Forwarded-Proto", "http")
			}
		} else if peer != "" {
			if prior := r.Header.Get("X-Forwarded-For"); prior != "" {
				r.Header.Set("X-Forwarded-For", prior+", "+peer)
			} else {
				r.Header.Set("X-Forwarded-For", peer)
			}
		}
		next.ServeHTTP(w, r)
	})
}

// requestHeaders returns the request headers for a JS handler, including the
// Host header Go hoists out of r.Header.
func requestHeaders(r *http.Request) map[string][]string {
	h := make(map[string][]string, len(r.Header)+1)
	for k, v := range r.Header {
		h[k] = v
	}
	if r.Host != "" {
		h["Host"] = []string{r.Host}
	}
	return h
}

// writeHeaderPairs copies JS response headers: repeated names (Set-Cookie)
// are appended, everything else replaces what earlier layers set.
func writeHeaderPairs(h http.Header, pairs [][2]string) {
	seen := map[string]bool{}
	for _, p := range pairs {
		k := http.CanonicalHeaderKey(p[0])
		if strings.EqualFold(k, "Set-Cookie") {
			h.Add(k, p[1])
			continue
		}
		if !seen[k] {
			h.Del(k)
			seen[k] = true
		}
		h.Add(k, p[1])
	}
}

// streamWriteIdle is how long a streaming API response may go without
// producing a chunk before the server's write deadline closes it.
const streamWriteIdle = 5 * time.Minute

// extendStreamDeadline pushes the connection write deadline out so the
// server-wide WriteTimeout does not cut long-lived streaming responses.
func extendStreamDeadline(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(streamWriteIdle))
}

// deadlineWriter extends the write deadline on every write and flushes, for
// proxied streaming responses (SSE) through the API sidecar.
type deadlineWriter struct {
	http.ResponseWriter
	rc *http.ResponseController
}

func newDeadlineWriter(w http.ResponseWriter) *deadlineWriter {
	return &deadlineWriter{ResponseWriter: w, rc: http.NewResponseController(w)}
}

func (d *deadlineWriter) Write(p []byte) (int, error) {
	_ = d.rc.SetWriteDeadline(time.Now().Add(streamWriteIdle))
	return d.ResponseWriter.Write(p)
}

func (d *deadlineWriter) Flush()                      { _ = d.rc.Flush() }
func (d *deadlineWriter) Unwrap() http.ResponseWriter { return d.ResponseWriter }

// httpSink adapts an http.ResponseWriter to the QuickJS API runtime.
type httpSink struct {
	w       http.ResponseWriter
	method  string
	started bool
	wrote   int
}

func (s *httpSink) Start(status int, headers [][2]string) error {
	if status < 100 || status > 999 {
		status = http.StatusBadGateway
	}
	writeHeaderPairs(s.w.Header(), headers)
	s.started = true
	s.w.WriteHeader(status)
	return nil
}

func (s *httpSink) Write(p []byte) error {
	if s.method == http.MethodHead {
		return jsruntime.ErrStopStream
	}
	if s.wrote > 0 {
		extendStreamDeadline(s.w)
	}
	s.wrote++
	if _, err := s.w.Write(p); err != nil {
		return err
	}
	if f, ok := s.w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

// genericAPIError is the production error body: no internals leak.
const genericAPIError = `{"error":"Internal Server Error"}`

// writeAPIFailure answers a request the API runtime could not serve and logs
// it. Dev responses include the message; production responses stay generic.
func writeAPIFailure(w http.ResponseWriter, r *http.Request, f *jsruntime.APIFailure, dev bool) {
	if f.Status == 499 {
		return
	}
	level := "API error"
	if f.Status == http.StatusNotFound || f.Status == http.StatusMethodNotAllowed {
		level = ""
	}
	if level != "" {
		msg := fmt.Sprintf("  %s%s%s %s %s -> %d: %s\n", cRed, level, cReset, r.Method, r.URL.Path, f.Status, f.Message)
		if dev && f.Stack != "" {
			msg += indentStack(f.Stack)
		}
		fmt.Fprint(os.Stderr, msg)
	}
	if f.Started {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	switch f.Status {
	case http.StatusNotFound:
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"API Route Not Found"}`)
	case http.StatusMethodNotAllowed:
		w.Header().Set("Allow", strings.Join(f.Allow, ", "))
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = io.WriteString(w, `{"error":"Method Not Allowed"}`)
	default:
		status := f.Status
		if status < 400 {
			status = http.StatusInternalServerError
		}
		w.WriteHeader(status)
		if dev {
			b, _ := json.Marshal(map[string]string{"error": "Internal Server Error", "details": f.Message})
			_, _ = w.Write(b)
		} else {
			_, _ = io.WriteString(w, genericAPIError)
		}
	}
}

func indentStack(stack string) string {
	var b strings.Builder
	for _, l := range strings.Split(strings.TrimSpace(stack), "\n") {
		b.WriteString("    ")
		b.WriteString(strings.TrimRight(l, "\r"))
		b.WriteString("\n")
	}
	return b.String()
}

// serveQuickJSAPI runs an /api request in the embedded runtime.
func serveQuickJSAPI(w http.ResponseWriter, r *http.Request, apiRT *jsruntime.APIRouteRuntime, dev bool) {
	var body []byte
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		b, err := readBodyLimited(r)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			_, _ = io.WriteString(w, `{"error":"Request Entity Too Large"}`)
			return
		}
		body = b
	}
	sink := &httpSink{w: w, method: r.Method}
	fail := apiRT.Serve(r.Context(), jsruntime.APIRequest{
		URL:        requestFullURL(r),
		Method:     r.Method,
		Path:       r.URL.Path,
		Headers:    requestHeaders(r),
		Body:       body,
		RemoteAddr: clientIP(r),
	}, sink)
	if fail != nil {
		writeAPIFailure(w, r, fail, dev)
	}
}

// staticAssetExts are skipped by middleware that declares no matcher, so a
// request for a stylesheet or image never pays for a JS call.
var staticAssetExts = map[string]bool{
	".css": true, ".js": true, ".mjs": true, ".map": true, ".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".svg": true, ".webp": true, ".avif": true, ".ico": true, ".woff": true, ".woff2": true, ".ttf": true, ".otf": true,
	".eot": true, ".mp4": true, ".webm": true, ".mp3": true, ".wav": true, ".ogg": true, ".wasm": true, ".txt": true,
	".xml": true, ".webmanifest": true, ".pdf": true, ".zip": true,
}

func isStaticAssetPath(p string) bool {
	return staticAssetExts[strings.ToLower(path.Ext(p))]
}

// skipsMiddleware reports whether a matcher-less middleware may be skipped for
// p. The extension alone is not enough: /api/files/x.pdf is a dynamic route
// that merely looks like a file, so the path must also resolve to a real file
// in the static layer and must not be an API route or server action.
func (g *middlewareGate) skipsMiddleware(p string) bool {
	if g.staticExists == nil || !isStaticAssetPath(p) {
		return false
	}
	if p == "/api" || strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/__krate/") {
		return false
	}
	return g.staticExists(p)
}

// middlewareGate runs the user middleware in front of next. api selects the
// request kinds it applies to: pages always; /api and server actions too.
type middlewareGate struct {
	rt  *jsruntime.MiddlewareRuntime
	dev bool
	// sidecar fallback (Node/Bun/Deno) used when the embedded runtime is off.
	sidecarURL string
	file       string
	// sidecarMatcher is what the sidecar last reported about the module's
	// matcher: 0 unknown, 1 none declared, 2 declared.
	sidecarMatcher atomic.Int32
	// staticExists reports whether a URL path is a file served by the static
	// layer. Nil means nothing is: middleware then runs for every request.
	staticExists func(urlPath string) bool
	// secret is sent to the sidecar as X-Krate-Internal; the sidecar refuses
	// requests without it.
	secret string
}

func (g *middlewareGate) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasPrefix(p, "/__krate/") && !strings.HasPrefix(p, "/__krate/action/") {
			next.ServeHTTP(w, r)
			return
		}
		if g.rt != nil {
			if !g.rt.HasMatcher() && g.skipsMiddleware(p) {
				next.ServeHTTP(w, r)
				return
			}
			res := g.rt.ExecuteContext(r.Context(), jsruntime.MiddlewareRequest{
				URL:        requestFullURL(r),
				Method:     r.Method,
				Path:       p,
				Headers:    requestHeaders(r),
				RemoteAddr: clientIP(r),
			})
			g.apply(w, r, next, res)
			return
		}
		if _, err := os.Stat(g.file); err != nil {
			next.ServeHTTP(w, r)
			return
		}
		// Until the sidecar reports a matcher, forward everything; once it says
		// there is none, static assets skip the round trip like the embedded
		// runtime does.
		if g.sidecarMatcher.Load() == 1 && g.skipsMiddleware(p) {
			next.ServeHTTP(w, r)
			return
		}
		g.viaSidecar(w, r, next)
	})
}

func (g *middlewareGate) apply(w http.ResponseWriter, r *http.Request, next http.Handler, res jsruntime.MiddlewareResult) {
	switch res.Action {
	case jsruntime.MiddlewareError:
		msg := res.Error
		fmt.Fprintf(os.Stderr, "  %sMiddleware error:%s %s %s: %s\n", cRed, cReset, r.Method, r.URL.Path, msg)
		if g.dev && res.Stack != "" {
			fmt.Fprint(os.Stderr, indentStack(res.Stack))
		}
		// Fail closed: continuing would turn a broken auth check into an
		// unauthenticated request.
		w.Header().Set("Cache-Control", "no-store")
		if g.dev {
			http.Error(w, "middleware failed: "+msg, http.StatusServiceUnavailable)
		} else {
			http.Error(w, "middleware failed", http.StatusServiceUnavailable)
		}
	case jsruntime.MiddlewareResponse:
		writeHeaderPairs(w.Header(), res.Headers)
		status := res.Status
		if status < 100 || status > 999 {
			status = http.StatusInternalServerError
		}
		w.WriteHeader(status)
		if len(res.Body) > 0 && r.Method != http.MethodHead {
			_, _ = w.Write(res.Body)
		}
	case jsruntime.MiddlewareContinue:
		writeHeaderPairs(w.Header(), res.Headers)
		if res.Rewrite != "" {
			applyRewrite(r, res.Rewrite)
		}
		next.ServeHTTP(w, r)
	default:
		next.ServeHTTP(w, r)
	}
}

// applyRewrite points the request at another path (x-middleware-rewrite).
func applyRewrite(r *http.Request, target string) {
	if i := strings.Index(target, "://"); i >= 0 {
		rest := target[i+3:]
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			target = rest[j:]
		} else {
			target = "/"
		}
	}
	p, q := target, ""
	if i := strings.IndexByte(target, '?'); i >= 0 {
		p, q = target[:i], target[i+1:]
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	r.URL.Path = p
	r.URL.RawPath = ""
	if q != "" {
		r.URL.RawQuery = q
	}
}

// viaSidecar asks the Node/Bun/Deno API sidecar to run the middleware.
func (g *middlewareGate) viaSidecar(w http.ResponseWriter, r *http.Request, next http.Handler) {
	hdrs := make(map[string][]string)
	for k, v := range requestHeaders(r) {
		hdrs[k] = v
	}
	payload, _ := json.Marshal(map[string]any{
		"url":     requestFullURL(r),
		"method":  r.Method,
		"path":    r.URL.Path,
		"headers": hdrs,
	})
	resp, err := sidecarPost(r.Context(), g.sidecarURL, payload, g.secret)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %sMiddleware unavailable:%s %v\n", cRed, cReset, err)
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "middleware unavailable", http.StatusServiceUnavailable)
		return
	}
	var out struct {
		Action  string      `json:"action"`
		Status  int         `json:"status"`
		Headers [][2]string `json:"headers"`
		Body    string      `json:"body"`
		Rewrite string      `json:"rewrite"`
		Error   string      `json:"error"`
		Matcher bool        `json:"matcher"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "middleware failed", http.StatusServiceUnavailable)
		return
	}
	if out.Matcher {
		g.sidecarMatcher.Store(2)
	} else if out.Error == "" {
		g.sidecarMatcher.Store(1)
	}
	res := jsruntime.MiddlewareResult{Status: out.Status, Headers: out.Headers, Rewrite: out.Rewrite, Error: out.Error, Body: []byte(out.Body)}
	switch {
	case out.Error != "" || out.Action == "error":
		res.Action = jsruntime.MiddlewareError
	case out.Action == "response":
		res.Action = jsruntime.MiddlewareResponse
	default:
		res.Action = jsruntime.MiddlewareContinue
	}
	g.apply(w, r, next, res)
}

func sidecarPost(ctx context.Context, url string, payload []byte, secret string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set(internalSecretHeader, secret)
	}
	resp, err := sidecarClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("sidecar returned " + resp.Status)
	}
	return b, nil
}

// writeGoMethodResponse answers a method a Go-only route does not define:
// automatic OPTIONS (204 + Allow) or 405 + Allow. It reports false when the
// route accepts every method (nothing to answer here).
func writeGoMethodResponse(w http.ResponseWriter, r *http.Request, allowed []string) bool {
	if len(allowed) == 1 && allowed[0] == "*" {
		return false
	}
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	w.WriteHeader(http.StatusMethodNotAllowed)
	_, _ = io.WriteString(w, `{"error":"Method Not Allowed"}`)
	return true
}
