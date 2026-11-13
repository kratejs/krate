package build

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/native/jsruntime"
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
