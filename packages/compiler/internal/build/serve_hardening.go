package build

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// Logger is the process-wide structured logger for the serving layer. Default
// level is Info; SetVerboseLogging raises it to Debug. Request logs render as a
// compact styled line (prettyHandler), while panics and diagnostics stay
// structured. Writes to stdout to match the CLI's build output.
var Logger = newLogger(false)

// SetVerboseLogging switches the serving logger between Info and Debug.
func SetVerboseLogging(verbose bool) {
	Logger = newLogger(verbose)
}

// HTTP server tuning. These bound how long a single request may take so a slow
// or malicious client cannot pin a goroutine (or the whole process) forever.
// SSE (live reload) and streaming regions explicitly extend their deadlines.
const (
	serverReadHeaderTimeout = 10 * time.Second
	serverReadTimeout       = 30 * time.Second
	// serverWriteTimeout must exceed the longest server-side render. Streaming
	// handlers extend the deadline themselves, so this only caps non-streaming
	// responses.
	serverWriteTimeout = 60 * time.Second
	serverIdleTimeout  = 120 * time.Second

	// shutdownGrace bounds graceful shutdown before the server is forced closed.
	shutdownGrace = 15 * time.Second
)

// sidecarClient is the shared HTTP client for talking to local sidecars (SSR
// renderer, API/middleware). It has a bounded timeout so a wedged sidecar can't
// hang a request goroutine forever.
var sidecarClient = &http.Client{Timeout: 30 * time.Second}

// newSidecarTransport builds a reverse-proxy transport with bounded dial and
// response-header timeouts, so a hung upstream cannot pin a request forever.
func newSidecarTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
}

// basePathMiddleware mounts the whole site under a URL prefix (e.g. "/docs").
// Requests under the prefix are stripped before routing; requests outside it are
// redirected (GET/HEAD) or 404'd. Internal endpoints and health probes stay at
// the root so orchestration is unaffected.
func basePathMiddleware(basePath string, next http.Handler) http.Handler {
	if basePath == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasPrefix(p, "/__krate/") || p == "/healthz" || p == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		switch {
		case p == basePath || p == basePath+"/":
			r.URL.Path = "/"
		case strings.HasPrefix(p, basePath+"/"):
			r.URL.Path = p[len(basePath):]
		default:
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				dest := basePath + p
				if !strings.HasPrefix(p, "/") {
					dest = basePath + "/" + p
				}
				http.Redirect(w, r, dest, http.StatusTemporaryRedirect)
				return
			}
			http.NotFound(w, r)
			return
		}
		r.URL.RawPath = ""
		next.ServeHTTP(w, r)
	})
}

// bodyLimitMiddleware caps the request body via http.MaxBytesReader, which also
// makes over-limit reads fail (a handler can return 413) instead of silently
// truncating.
func bodyLimitMiddleware(limit int64, next http.Handler) http.Handler {
	if limit <= 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}

// corsMiddleware applies the configured CORS policy. Disabled by default.
func corsMiddleware(cfg *config.Config, next http.Handler) http.Handler {
	c := cfg.CORS
	if !c.Enabled {
		return next
	}
	origins := c.Origins
	if len(origins) == 0 {
		origins = []string{"*"}
	}
	allowAll := len(origins) == 1 && origins[0] == "*"
	methods := strings.Join(nonEmpty(c.Methods, []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}), ", ")
	headers := strings.Join(nonEmpty(c.Headers, []string{"Content-Type", "Authorization"}), ", ")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := ""
		switch {
		case allowAll && !c.Credentials:
			allowed = "*"
		case origin != "" && originAllowed(origin, origins):
			allowed = origin
			w.Header().Add("Vary", "Origin")
		}
		if allowed != "" {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", allowed)
			h.Set("Access-Control-Allow-Methods", methods)
			h.Set("Access-Control-Allow-Headers", headers)
			if c.Credentials {
				h.Set("Access-Control-Allow-Credentials", "true")
			}
			if c.MaxAge > 0 {
				h.Set("Access-Control-Max-Age", strconv.Itoa(c.MaxAge))
			}
		}
		// Preflight: short-circuit (only when it is actually a preflight).
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// originAllowed reports whether origin matches any allowed pattern (exact, or a
// "*" entry).
func originAllowed(origin string, origins []string) bool {
	for _, o := range origins {
		if o == "*" || strings.EqualFold(o, origin) {
			return true
		}
	}
	return false
}

func nonEmpty(v, fallback []string) []string {
	if len(v) == 0 {
		return fallback
	}
	return v
}

// newHTTPServer builds the main server with hardened timeouts.
func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
	}
}

// ─── request IDs + logging ──────────────────────────────────────────────────

var requestSeq atomic.Uint64

type requestIDKey struct{}

// requestIDMiddleware assigns each request a short, unique ID and exposes it on
// the response and request context so logs and error responses can correlate.
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := fmt.Sprintf("req-%d", requestSeq.Add(1))
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

func requestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey{}).(string); ok {
		return v
	}
	return ""
}

// panicRecoveryMiddleware catches panics from downstream handlers so a single
// bad request cannot take the whole process down. It logs the panic and stack
// and returns 500 (JSON for /api/*, plain text otherwise).
func panicRecoveryMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("panic recovered",
					"id", requestIDFrom(r.Context()),
					"method", r.Method,
					"path", r.URL.Path,
					"panic", fmt.Sprint(rec),
					"stack", string(debug.Stack()),
				)
				if strings.HasPrefix(r.URL.Path, "/api/") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = io.WriteString(w, `{"error":"Internal Server Error"}`)
					return
				}
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ─── security headers (1.4) ─────────────────────────────────────────────────

// securityHeadersMiddleware applies safe defaults. Nothing here breaks static
// sites; HSTS is only emitted for https base URLs.
func securityHeadersMiddleware(cfg *config.Config, next http.Handler) http.Handler {
	hsts := ""
	if strings.HasPrefix(strings.ToLower(cfg.SEO.BaseURL), "https://") {
		hsts = "max-age=31536000; includeSubDomains"
	}
	csp := buildCSPHeader(cfg)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if hsts != "" {
			h.Set("Strict-Transport-Security", hsts)
		}
		if csp != "" {
			h.Set("Content-Security-Policy", csp)
		}
		next.ServeHTTP(w, r)
	})
}

// buildCSPHeader returns the CSP directive for the HTTP header. A user-supplied
// directive wins; otherwise, when enabled, a conservative default is emitted.
// Unlike the meta-tag CSP this can carry frame-ancestors.
func buildCSPHeader(cfg *config.Config) string {
	if !cfg.CSP.Enabled {
		return ""
	}
	if d := strings.TrimSpace(cfg.CSP.Directive); d != "" {
		return d
	}
	return "frame-ancestors 'none'"
}

// ─── compression (4.3, static path) ─────────────────────────────────────────

// compressibleType reports whether a content type benefits from gzip. Already
// compressed formats (images, fonts, wasm, zip) are excluded.
func compressibleType(ct string) bool {
	ct = strings.ToLower(ct)
	switch {
	case strings.HasPrefix(ct, "text/"):
		return true
	case strings.Contains(ct, "javascript"):
		return true
	case strings.Contains(ct, "json"):
		return true
	case strings.Contains(ct, "svg"):
		return true
	case strings.Contains(ct, "xml"):
		return true
	default:
		return false
	}
}

// gzipMiddleware gzips compressible responses when the client advertises gzip
// support. Because the Content-Type is set by the downstream handler immediately
// before WriteHeader, the decision is made on the first WriteHeader/Write.
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipResponseWriter{ResponseWriter: w}
		defer gw.close()
		next.ServeHTTP(gw, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	zw          *gzip.Writer
	decided     bool
	useGzip     bool
	wroteHeader bool
}

func (g *gzipResponseWriter) decide() {
	if g.decided {
		return
	}
	g.decided = true
	g.useGzip = compressibleType(g.Header().Get("Content-Type"))
	if g.useGzip {
		g.Header().Del("Content-Length")
		g.Header().Set("Content-Encoding", "gzip")
		g.Header().Add("Vary", "Accept-Encoding")
		g.zw = gzip.NewWriter(g.ResponseWriter)
	}
}

func (g *gzipResponseWriter) WriteHeader(code int) {
	if g.wroteHeader {
		return
	}
	g.decide()
	g.wroteHeader = true
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if !g.wroteHeader {
		g.WriteHeader(http.StatusOK)
	}
	if g.useGzip {
		return g.zw.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

func (g *gzipResponseWriter) Flush() {
	if g.useGzip && g.zw != nil {
		g.zw.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (g *gzipResponseWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }

func (g *gzipResponseWriter) close() {
	if g.useGzip && g.zw != nil {
		g.zw.Close()
	}
}

// ─── streaming helpers ──────────────────────────────────────────────────────

// hashedAssetRe matches content-hashed asset filenames: a `.<hash>.` segment
// before the extension (e.g. index.abc123.js, styles.abc123.css) or a
// `-<hash>.<ext>` suffix (e.g. logo-abc123.png). The build emits these names,
// so a one-year immutable cache is safe.
var hashedAssetRe = regexp.MustCompile(`\.[0-9a-z]{6,}\.[a-z0-9]+$|-[0-9a-z]{6,}\.[a-z0-9]+$`)

func isHashedAsset(urlPath string) bool {
	p := urlPath
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	if i := strings.IndexByte(p, '#'); i >= 0 {
		p = p[:i]
	}
	// A source map describes a content-hashed asset (index.<hash>.js.map), so
	// cache it under the same policy as its subject.
	if strings.HasSuffix(p, ".map") {
		p = strings.TrimSuffix(p, ".map")
	}
	switch strings.ToLower(filepathExt(p)) {
	case ".css", ".js", ".mjs", ".woff2", ".woff", ".png", ".jpg", ".jpeg",
		".gif", ".webp", ".avif", ".svg", ".ico", ".wasm":
		return hashedAssetRe.MatchString(p)
	}
	return false
}

func filepathExt(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '.' {
			return p[i:]
		}
		if p[i] == '/' {
			return ""
		}
	}
	return ""
}

// setStreamingDeadline extends the write deadline for long-lived SSE/streaming
// responses.
func setStreamingDeadline(w http.ResponseWriter, d time.Duration) {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(d))
}

// dialSidecar opens a bounded TCP connection for the region stream and sets a
// read deadline so a stalled sidecar can't block the request goroutine forever.
func dialSidecar(port int, readTimeout time.Duration) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 5*time.Second)
	if err != nil {
		return nil, err
	}
	_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
	return conn, nil
}

// readBodyLimited reads the request body. Callers must have wrapped r.Body with
// http.MaxBytesReader (the /api/ handler does) so this is bounded and returns an
// error on overflow instead of truncating silently.
func readBodyLimited(r *http.Request) ([]byte, error) {
	return io.ReadAll(r.Body)
}
