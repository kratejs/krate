package build

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
)

func newGate(t *testing.T, code string, dev bool) *middlewareGate {
	t.Helper()
	p := filepath.Join(t.TempDir(), "middleware.js")
	if err := os.WriteFile(p, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, err := jsruntime.NewMiddlewareRuntime(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Close)
	return &middlewareGate{rt: rt, dev: dev, file: p}
}

func TestMiddlewareGateBehaviors(t *testing.T) {
	var seen []string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.String())
		_, _ = io.WriteString(w, "page")
	})
	g := newGate(t, `
export function middleware(request) {
  const p = new URL(request.url).pathname;
  if (p === "/boom") throw new Error("kaboom detail");
  if (p === "/rewrite") return new Response(null, { headers: { "x-middleware-rewrite": "/other?x=1" } });
  if (p === "/cookies") { const h = new Headers({ "x-middleware-next": "1" }); h.append("set-cookie", "a=1"); h.append("set-cookie", "b=2"); return new Response(null, { headers: h }); }
  if (p === "/assets.css") throw new Error("must not run for static assets");
}`, false)
	g.staticExists = func(p string) bool { return p == "/assets.css" }
	h := g.wrap(next)

	do := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com"+path, nil))
		return rec
	}
	if rec := do("/assets.css"); rec.Code != 200 || rec.Body.String() != "page" {
		t.Errorf("static asset must bypass middleware without a matcher: %d %q", rec.Code, rec.Body)
	}
	rec := do("/boom")
	if rec.Code != 503 || strings.Contains(rec.Body.String(), "kaboom") {
		t.Errorf("prod middleware error: %d %q (must fail closed without leaking the message)", rec.Code, rec.Body)
	}
	g.dev = true
	if rec := do("/boom"); rec.Code != 503 || !strings.Contains(rec.Body.String(), "kaboom detail") {
		t.Errorf("dev middleware error should include the message: %d %q", rec.Code, rec.Body)
	}
	seen = nil
	if rec := do("/rewrite"); rec.Code != 200 || len(seen) != 1 || !strings.HasSuffix(seen[0], "/other/?x=1") {
		t.Errorf("rewrite: code=%d seen=%v", rec.Code, seen)
	}
	if rec := do("/cookies"); len(rec.Header().Values("Set-Cookie")) != 2 {
		t.Errorf("Set-Cookie headers: %v", rec.Header().Values("Set-Cookie"))
	}
	if rec := do("/__krate/status"); rec.Code != 200 {
		t.Errorf("internal endpoints must bypass the gate: %d", rec.Code)
	}
}

func TestRequestURLAndTrustProxy(t *testing.T) {
	defer proxyTrust.Store(false)
	r := httptest.NewRequest("GET", "http://internal:3000/a?b=1", nil)
	r.Host = "blog.example.com"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "public.example.org")
	r.Header.Set("X-Forwarded-For", "9.9.9.9, 10.0.0.1")
	r.RemoteAddr = "10.1.2.3:5555"

	proxyTrust.Store(false)
	if got := requestFullURL(r); got != "http://blog.example.com/a?b=1" {
		t.Errorf("untrusted URL = %q, want the Host header and http", got)
	}
	if got := clientIP(r); got != "10.1.2.3" {
		t.Errorf("untrusted client IP = %q, want the peer", got)
	}
	proxyTrust.Store(true)
	if got := requestFullURL(r); got != "https://public.example.org/a?b=1" {
		t.Errorf("trusted URL = %q", got)
	}
	if got := clientIP(r); got != "9.9.9.9" {
		t.Errorf("trusted client IP = %q", got)
	}
	r.TLS = &tls.ConnectionState{}
	r.Header.Del("X-Forwarded-Proto")
	if got := requestScheme(r); got != "https" {
		t.Errorf("TLS scheme = %q", got)
	}
}

func TestSameOriginActionHonorsTrustProxy(t *testing.T) {
	defer proxyTrust.Store(false)
	mk := func() *http.Request {
		r := httptest.NewRequest("POST", "http://127.0.0.1:3000/__krate/action/x", nil)
		r.Host = "127.0.0.1:3000"
		r.Header.Set("Origin", "https://app.example.com")
		r.Header.Set("X-Forwarded-Host", "app.example.com")
		return r
	}
	proxyTrust.Store(false)
	if sameOriginActionRequest(mk()) {
		t.Error("an Origin that only matches a client-supplied X-Forwarded-Host must be rejected without trustProxy")
	}
	proxyTrust.Store(true)
	if !sameOriginActionRequest(mk()) {
		t.Error("behind a trusted proxy the forwarded host is the origin to compare")
	}
}

func TestForwardedHeadersMiddlewareStripsSpoofing(t *testing.T) {
	var got http.Header
	capture := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.Header.Clone() })
	r := httptest.NewRequest("GET", "http://svc/", nil)
	r.RemoteAddr = "203.0.113.7:4000"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	r.Header.Set("X-Real-IP", "1.2.3.4")
	forwardedHeadersMiddleware(false, capture).ServeHTTP(httptest.NewRecorder(), r)
	if got.Get("X-Forwarded-For") != "203.0.113.7" || got.Get("X-Real-IP") != "" {
		t.Errorf("untrusted: XFF=%q X-Real-IP=%q", got.Get("X-Forwarded-For"), got.Get("X-Real-IP"))
	}
	r = httptest.NewRequest("GET", "http://svc/", nil)
	r.RemoteAddr = "10.0.0.9:4000"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	forwardedHeadersMiddleware(true, capture).ServeHTTP(httptest.NewRecorder(), r)
	if got.Get("X-Forwarded-For") != "1.2.3.4, 10.0.0.9" {
		t.Errorf("trusted: XFF=%q", got.Get("X-Forwarded-For"))
	}
}

func TestWriteAPIFailureBodies(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/x", nil)
	f := &jsruntime.APIFailure{Status: 500, Message: "db password is hunter2", Stack: "at x"}
	rec := httptest.NewRecorder()
	writeAPIFailure(rec, r, f, false)
	if rec.Code != 500 || strings.TrimSpace(rec.Body.String()) != `{"error":"Internal Server Error"}` {
		t.Errorf("prod: %d %q", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	writeAPIFailure(rec, r, f, true)
	if !strings.Contains(rec.Body.String(), "hunter2") {
		t.Errorf("dev body should include the message: %q", rec.Body)
	}
	rec = httptest.NewRecorder()
	writeAPIFailure(rec, r, &jsruntime.APIFailure{Status: 405, Allow: []string{"GET", "HEAD"}}, false)
	if rec.Code != 405 || rec.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("405: %d allow=%q", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestGoAPISocketPathStaysUnderLimit(t *testing.T) {
	short := t.TempDir()
	if p := goAPISocketPath(short); p == "" || len(p) > maxUnixSocketPath {
		t.Errorf("short project path: %q", p)
	}
	long := filepath.Join(t.TempDir(), strings.Repeat("deeply-nested-project-directory-", 5))
	if err := os.MkdirAll(long, 0o755); err != nil {
		t.Fatal(err)
	}
	p := goAPISocketPath(long)
	if p != "" && len(p) > maxUnixSocketPath {
		t.Errorf("long project path produced an over-long socket path (%d): %q", len(p), p)
	}
	if p == "" {
		t.Log("no short socket path available; TCP fallback in use")
	} else if strings.HasPrefix(p, long) {
		t.Errorf("long project path must not be used for the socket: %q", p)
	}
}

func TestWriteGoMethodResponse(t *testing.T) {
	allowed := []string{"GET", "HEAD", "OPTIONS"}
	rec := httptest.NewRecorder()
	if !writeGoMethodResponse(rec, httptest.NewRequest("DELETE", "/api/x", nil), allowed) || rec.Code != 405 || rec.Header().Get("Allow") != "GET, HEAD, OPTIONS" {
		t.Errorf("DELETE: %d allow=%q", rec.Code, rec.Header().Get("Allow"))
	}
	rec = httptest.NewRecorder()
	if !writeGoMethodResponse(rec, httptest.NewRequest("OPTIONS", "/api/x", nil), allowed) || rec.Code != 204 {
		t.Errorf("OPTIONS: %d", rec.Code)
	}
	if writeGoMethodResponse(httptest.NewRecorder(), httptest.NewRequest("DELETE", "/api/x", nil), []string{"*"}) {
		t.Error("an any-method route must not be answered here")
	}
}

// A matcher-less middleware must not be skipped just because the URL ends in a
// static extension: API routes and missing files still go through it.
func TestMiddlewareGateStaticExtensionBypass(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "secret") })
	g := newGate(t, `export function middleware(request) { return new Response("denied", { status: 401 }); }`, false)
	g.staticExists = func(p string) bool { return p == "/real.pdf" }
	h := g.wrap(next)
	do := func(path string) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com"+path, nil))
		return rec.Code
	}
	for _, p := range []string{"/api/files/report.pdf", "/api/files/a.txt", "/api/x.js", "/__krate/action/id.css", "/missing.pdf"} {
		if code := do(p); code != 401 {
			t.Errorf("%s: middleware skipped (status %d), want 401", p, code)
		}
	}
	if code := do("/real.pdf"); code != 200 {
		t.Errorf("real static file should skip middleware, got %d", code)
	}
	g.staticExists = nil
	if code := do("/real.pdf"); code != 401 {
		t.Errorf("with no static layer middleware must always run, got %d", code)
	}
}

// A client-supplied X-Forwarded-For entry must never decide the address: the
// proxy appends the real client, so the answer is that hop, not the first one.
func TestClientIPIgnoresClientSuppliedForwardedEntries(t *testing.T) {
	proxyTrust.Store(true)
	t.Cleanup(func() { proxyTrust.Store(false); proxyHops.Store(0) })
	var got string
	capture := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = clientIP(r) })
	do := func(xff, peer string) string {
		r := httptest.NewRequest("GET", "http://svc/", nil)
		r.RemoteAddr = peer + ":4000"
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		forwardedHeadersMiddleware(true, capture).ServeHTTP(httptest.NewRecorder(), r)
		return got
	}
	// One proxy (10.0.0.5) that appended the real client to a spoofed value.
	if ip := do("6.6.6.6, 203.0.113.9", "10.0.0.5"); ip != "203.0.113.9" {
		t.Errorf("one proxy: clientIP = %q, want 203.0.113.9", ip)
	}
	if ip := do("", "198.51.100.4"); ip != "198.51.100.4" {
		t.Errorf("no header: clientIP = %q, want the peer", ip)
	}
	// Two proxies in front: the client is two hops from the right.
	proxyHops.Store(2)
	if ip := do("6.6.6.6, 203.0.113.9, 10.0.0.2", "10.0.0.5"); ip != "203.0.113.9" {
		t.Errorf("two proxies: clientIP = %q, want 203.0.113.9", ip)
	}
}

// A rewrite to a page path without a trailing slash must be served in place,
// not answered with the canonical-slash 301 that changes the visible URL.
func TestRewriteTargetPathKeepsRewritesInvisible(t *testing.T) {
	cases := map[string]string{
		"/feedback":      "/feedback/",
		"/feedback/":     "/feedback/",
		"/":              "/",
		"/blog/post":     "/blog/post/",
		"/feed.xml":      "/feed.xml",
		"/api/users":     "/api/users",
		"/api":           "/api",
		"/__krate/x":     "/__krate/x",
		"/img/logo.webp": "/img/logo.webp",
	}
	for in, want := range cases {
		if got := rewriteTargetPath(in); got != want {
			t.Errorf("rewriteTargetPath(%q) = %q, want %q", in, got, want)
		}
	}
	r := httptest.NewRequest("GET", "http://example.com/old?x=1", nil)
	applyRewrite(r, "/feedback?src=mw")
	if r.URL.Path != "/feedback/" || r.URL.RawQuery != "src=mw" {
		t.Errorf("applyRewrite: path %q query %q", r.URL.Path, r.URL.RawQuery)
	}
	if redirectToCanonicalSlash(httptest.NewRecorder(), r) {
		t.Error("the rewritten request must not be redirected")
	}
}

func TestMiddlewareCompletionValueIsDiscarded(t *testing.T) {
	g := newGate(t, `
export function middleware(request) {
  return new Response("teapot", { status: 418 });
}
123456789012345678901234567890n;
`, false)
	h := g.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "page") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/x", nil))
	if rec.Code != 418 {
		t.Fatalf("a trailing BigInt expression must not break loading the middleware: %d %q", rec.Code, rec.Body)
	}
}
