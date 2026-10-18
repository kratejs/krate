package build

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
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

// regionClient talks to the SSR renderer's /__krate/regions endpoint. Its
// transport keeps a large pool of idle keep-alive connections so a clustered
// renderer (ssr.rendererWorkers) actually receives traffic on every worker - the
// default transport's 2-per-host idle pool would pin requests to one or two.
var regionClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:        512,
		MaxIdleConnsPerHost: 256,
		IdleConnTimeout:     90 * time.Second,
	},
}

// newSidecarTransport builds a reverse-proxy transport with bounded dial and
// response-header timeouts, so a hung upstream cannot pin a request forever.
func newSidecarTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   128,
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

// request IDs + logging

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

// security headers (1.4)

// securityHeadersMiddleware applies safe defaults. Nothing here breaks static
// sites; HSTS is only emitted for https base URLs.
func securityHeadersMiddleware(cfg *config.Config, next http.Handler) http.Handler {
	sec := cfg.Security
	nosniff := sec.ContentTypeOptions == nil || *sec.ContentTypeOptions
	frame := headerOrDefault(sec.FrameOptions, "DENY")
	referrer := headerOrDefault(sec.ReferrerPolicy, "strict-origin-when-cross-origin")
	permissions := headerOrDefault(sec.PermissionsPolicy, "camera=(), microphone=(), geolocation=()")
	hsts := sec.HSTS
	if hsts == "" && strings.HasPrefix(strings.ToLower(cfg.SEO.BaseURL), "https://") {
		hsts = "max-age=31536000; includeSubDomains"
	}
	csp := buildCSPHeader(cfg)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		if nosniff {
			h.Set("X-Content-Type-Options", "nosniff")
		}
		setHeaderUnlessDisabled(h, "X-Frame-Options", frame)
		setHeaderUnlessDisabled(h, "Referrer-Policy", referrer)
		setHeaderUnlessDisabled(h, "Permissions-Policy", permissions)
		setHeaderUnlessDisabled(h, "Strict-Transport-Security", hsts)
		if csp != "" {
			h.Set("Content-Security-Policy", csp)
		}
		next.ServeHTTP(w, r)
	})
}

func headerOrDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func setHeaderUnlessDisabled(h http.Header, name, value string) {
	if value == "" || config.HeaderDisabled(value) {
		return
	}
	h.Set(name, value)
}

// customHeadersMiddleware applies user-configured response headers (config
// `headers: [{ source, headers }]`) for requests whose path matches. Runs just
// inside the security headers so a rule can override a default.
func customHeadersMiddleware(rules []config.HeaderRule, next http.Handler) http.Handler {
	if len(rules) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		for _, rule := range rules {
			if headerSourceMatches(rule.Source, r.URL.Path) {
				for k, v := range rule.Headers {
					h.Set(k, v)
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// headerSourceMatches reports whether a path matches a header rule's source: an
// exact path, a trailing `*` prefix ("/assets/*"), or a path.Match glob.
func headerSourceMatches(pattern, p string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	if !strings.Contains(pattern, "*") {
		return p == pattern
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(p, strings.TrimSuffix(pattern, "*"))
	}
	ok, _ := path.Match(pattern, p)
	return ok
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

// compression (4.3, static path)

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

// acceptsEncoding reports whether an Accept-Encoding header value allows the
// given content-coding (honoring q=0 to exclude it, and "*" as a wildcard).
func acceptsEncoding(header, coding string) bool {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		token, q := part, 1.0
		if i := strings.IndexByte(part, ';'); i >= 0 {
			token = strings.TrimSpace(part[:i])
			for _, param := range strings.Split(part[i+1:], ";") {
				param = strings.TrimSpace(param)
				if rest, ok := strings.CutPrefix(param, "q="); ok {
					if v, err := strconv.ParseFloat(strings.TrimSpace(rest), 64); err == nil {
						q = v
					}
				}
			}
		}
		if strings.EqualFold(token, coding) || token == "*" {
			return q > 0
		}
	}
	return false
}

// gzipMiddleware gzips compressible responses when the client advertises gzip
// support, and marks every compressible response `Vary: Accept-Encoding` so a
// cache never serves an uncompressed body to a client that accepts gzip.
// Because the Content-Type is set by the downstream handler immediately before
// WriteHeader, the decision is made on the first WriteHeader/Write.
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gw := &gzipResponseWriter{
			ResponseWriter: w,
			wantsGzip:      acceptsEncoding(r.Header.Get("Accept-Encoding"), "gzip"),
		}
		defer gw.close()
		next.ServeHTTP(gw, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	zw          *gzip.Writer
	decided     bool
	wantsGzip   bool
	useGzip     bool
	wroteHeader bool
}

func (g *gzipResponseWriter) decide() {
	if g.decided {
		return
	}
	g.decided = true
	if !compressibleType(g.Header().Get("Content-Type")) {
		return
	}
	g.Header().Add("Vary", "Accept-Encoding")
	if !g.wantsGzip {
		return
	}
	g.useGzip = true
	g.Header().Del("Content-Length")
	g.Header().Set("Content-Encoding", "gzip")
	g.zw = gzip.NewWriter(g.ResponseWriter)
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

// precompressedMiddleware serves a build-time precompressed sibling (`.br` or
// `.gz`, from `krate build --precompress`) for a static asset when the client
// accepts that coding, preferring brotli. Requests with no sibling fall through
// to the next handler, so dynamic responses still get on-the-fly gzip. The
// remote content-coding is fixed before ServeContent so Range/HEAD work on the
// compressed bytes.
func precompressedMiddleware(root string, next http.Handler) http.Handler {
	root = filepath.Clean(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		full, ok := precompressedBase(root, r.URL.Path)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		ae := r.Header.Get("Accept-Encoding")
		enc, base, ok := pickPrecompressed(full, acceptsEncoding(ae, "br"), acceptsEncoding(ae, "gzip"))
		if !ok {
			next.ServeHTTP(w, r)
			return
		}

		f, err := os.Open(base + precompressedSuffix(enc))
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		defer f.Close()
		fi, err := f.Stat()
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		h := w.Header()
		h.Set("Content-Encoding", enc)
		if ct := precompressedContentType(base); ct != "" {
			h.Set("Content-Type", ct)
		}
		h.Add("Vary", "Accept-Encoding")
		// The name drives ServeContent's Content-Type sniffing when unset, so
		// pass the original asset name (not the `.br`/`.gz` sibling).
		http.ServeContent(w, r, filepath.Base(base), fi.ModTime(), f)
	})
}

// precompressedBase maps a URL path to a file under root, rejecting traversal.
// The root and any directory path target their index.html.
func precompressedBase(root, urlPath string) (string, bool) {
	clean := path.Clean("/" + urlPath)
	if clean == "/" {
		return filepath.Join(root, "index.html"), true
	}
	full := filepath.Join(root, filepath.FromSlash(clean))
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	if fi, err := os.Stat(full); err == nil && fi.IsDir() {
		return filepath.Join(full, "index.html"), true
	}
	return full, true
}

// pickPrecompressed returns the preferred precompressed sibling of base that the
// client accepts (brotli before gzip).
func pickPrecompressed(base string, wantBr, wantGz bool) (enc, file string, ok bool) {
	if wantBr && isRegularFile(base+".br") {
		return "br", base, true
	}
	if wantGz && isRegularFile(base+".gz") {
		return "gzip", base, true
	}
	return "", "", false
}

func precompressedSuffix(enc string) string {
	if enc == "br" {
		return ".br"
	}
	return ".gz"
}

func isRegularFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// precompressedContentType returns a charset-qualified Content-Type for the
// asset kinds precompression produces, so the type is correct even where the
// platform mime table lacks an entry. Empty means "let ServeContent sniff".
func precompressedContentType(p string) string {
	switch strings.ToLower(filepathExt(p)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs", ".cjs":
		return "text/javascript; charset=utf-8"
	case ".json", ".map", ".webmanifest":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".xml", ".rss", ".atom":
		return "application/xml; charset=utf-8"
	case ".txt":
		return "text/plain; charset=utf-8"
	default:
		return ""
	}
}

// devProxyMiddleware forwards configured path prefixes to a backend during dev
// (config `devServer.proxy`). Longest prefix wins; unmatched requests fall
// through to the normal dev handler.
func devProxyMiddleware(proxy map[string]string, next http.Handler) http.Handler {
	if len(proxy) == 0 {
		return next
	}
	type rule struct {
		prefix  string
		handler http.Handler
	}
	var rules []rule
	for prefix, target := range proxy {
		u, err := url.Parse(target)
		if err != nil || u.Host == "" {
			continue
		}
		p := "/" + strings.Trim(prefix, "/")
		if p == "/" {
			p = ""
		}
		rules = append(rules, rule{p, httputil.NewSingleHostReverseProxy(u)})
	}
	sort.Slice(rules, func(i, j int) bool { return len(rules[i].prefix) > len(rules[j].prefix) })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, rt := range rules {
			if rt.prefix == "" || r.URL.Path == rt.prefix || strings.HasPrefix(r.URL.Path, rt.prefix+"/") {
				rt.handler.ServeHTTP(w, r)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// streaming helpers

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
	p = strings.TrimSuffix(p, ".map")
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

// readBodyLimited reads the request body. Callers must have wrapped r.Body with
// http.MaxBytesReader (the /api/ handler does) so this is bounded and returns an
// error on overflow instead of truncating silently.
func readBodyLimited(r *http.Request) ([]byte, error) {
	return io.ReadAll(r.Body)
}
