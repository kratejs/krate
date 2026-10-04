package build

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestSecurityHeadersMiddleware(t *testing.T) {
	cfg := config.Default()
	cfg.SEO.BaseURL = "https://example.com"
	h := securityHeadersMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got == "" {
		t.Error("missing Referrer-Policy")
	}
	if got := rec.Header().Get("Strict-Transport-Security"); got == "" {
		t.Error("missing HSTS for https baseUrl")
	}
}

func TestSecurityHeadersNoHSTSForHTTP(t *testing.T) {
	cfg := config.Default()
	cfg.SEO.BaseURL = "http://localhost:3000"
	h := securityHeadersMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("unexpected HSTS for http baseUrl: %q", got)
	}
}

func TestCORSMiddlewareDisabledByDefault(t *testing.T) {
	cfg := config.Default()
	h := corsMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://a.example")
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("CORS header should be absent when disabled, got %q", got)
	}
}

func TestCORSMiddlewareAllowAllAndPreflight(t *testing.T) {
	cfg := config.Default()
	cfg.CORS.Enabled = true
	cfg.CORS.MaxAge = 600
	h := corsMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://a.example")
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Allow-Origin = %q, want *", got)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("OPTIONS", "/api/x", nil)
	req.Header.Set("Origin", "https://a.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("preflight code = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Max-Age"); got != "600" {
		t.Errorf("Max-Age = %q, want 600", got)
	}
}

func TestCORSMiddlewareExplicitOrigins(t *testing.T) {
	cfg := config.Default()
	cfg.CORS.Enabled = true
	cfg.CORS.Origins = []string{"https://ok.example"}
	cfg.CORS.Credentials = true
	h := corsMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://ok.example")
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://ok.example" {
		t.Errorf("Allow-Origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials = %q", got)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://evil.example")
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("disallowed origin got Allow-Origin %q", got)
	}
}

func TestBasePathMiddleware(t *testing.T) {
	var gotPath string
	h := basePathMiddleware("/docs", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))

	// Prefixed path is stripped before routing.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/docs/about/", nil))
	if gotPath != "/about/" {
		t.Errorf("stripped path = %q, want /about/", gotPath)
	}

	// The mount root maps to "/".
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/docs/", nil))
	if gotPath != "/" {
		t.Errorf("root path = %q, want /", gotPath)
	}

	// Non-prefixed GET redirects into the mount.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/about/", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTemporaryRedirect {
		t.Errorf("redirect code = %d, want 307", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/docs/about/" {
		t.Errorf("redirect location = %q, want /docs/about/", loc)
	}

	// Health stays at the root.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if gotPath != "/healthz" {
		t.Errorf("health path = %q, want /healthz", gotPath)
	}
}

func TestBodyLimitMiddleware(t *testing.T) {
	h := bodyLimitMiddleware(8, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", strings.NewReader("123456789"))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("over-limit body code = %d, want 413", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/", strings.NewReader("12345"))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("within-limit body code = %d, want 200", rec.Code)
	}
}

func TestCSPHeaderEmittedWhenEnabled(t *testing.T) {
	cfg := config.Default()
	cfg.CSP.Enabled = true
	h := securityHeadersMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if got := rec.Header().Get("Content-Security-Policy"); got != "frame-ancestors 'none'" {
		t.Errorf("CSP header = %q", got)
	}
}

func TestCSPHeaderCustomDirective(t *testing.T) {
	cfg := config.Default()
	cfg.CSP.Enabled = true
	cfg.CSP.Directive = "default-src 'self'"
	h := securityHeadersMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if got := rec.Header().Get("Content-Security-Policy"); got != "default-src 'self'" {
		t.Errorf("CSP header = %q", got)
	}
}

func TestPanicRecoveryReturns500(t *testing.T) {
	h := panicRecoveryMiddleware(Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestPanicRecoveryJSONForAPI(t *testing.T) {
	h := panicRecoveryMiddleware(Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/x", nil))
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "json") {
		t.Errorf("api panic Content-Type = %q, want json", ct)
	}
}

func TestRequestIDMiddleware(t *testing.T) {
	h := requestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestIDFrom(r.Context()) == "" {
			t.Error("request id missing from context")
		}
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Header().Get("X-Request-Id") == "" {
		t.Error("X-Request-Id header missing")
	}
}

func TestGzipMiddlewareCompressesText(t *testing.T) {
	h := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(strings.Repeat("hello ", 100)))
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected gzip encoding, got %q", rec.Header().Get("Content-Encoding"))
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	body, _ := io.ReadAll(zr)
	if !strings.Contains(string(body), "hello") {
		t.Errorf("decompressed body wrong: %q", body)
	}
}

func TestGzipMiddlewareSkipsBinary(t *testing.T) {
	h := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 0x50, 0x4e, 0x47})
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Content-Encoding") == "gzip" {
		t.Error("image/png should not be gzipped")
	}
}

func TestHashedAssetDetection(t *testing.T) {
	cases := map[string]bool{
		"/styles.abc123.css":        true,
		"/index.abc123.js":          true,
		"/chunks/runtime.x1y2z3.js": true,
		"/assets/logo-abc123.png":   true,
		"/about/index.html":         false,
		"/about/":                   false,
		"/main.js":                  false,
		"/site.css":                 false,
	}
	for p, want := range cases {
		if got := isHashedAsset(p); got != want {
			t.Errorf("isHashedAsset(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestNewHTTPServerHasTimeouts(t *testing.T) {
	s := newHTTPServer(http.NewServeMux())
	if s.ReadHeaderTimeout == 0 || s.ReadTimeout == 0 || s.WriteTimeout == 0 || s.IdleTimeout == 0 {
		t.Errorf("server missing timeouts: %+v", s)
	}
}
