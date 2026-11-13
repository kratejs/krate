package build

import (
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

func TestCORSMiddlewareRejectsCredentialedWildcard(t *testing.T) {
	cfg := config.Default()
	cfg.CORS.Enabled = true
	cfg.CORS.Credentials = true
	h := corsMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://evil.example")
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("credentialed wildcard reflected origin %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("credentialed wildcard emitted Allow-Credentials %q", got)
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
