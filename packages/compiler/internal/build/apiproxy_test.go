package build

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestAPISidecarProxyFirstAndFallthrough verifies a custom sidecar handles the
// routes it responds to and a 404 falls through (Handle returns false) so the
// built-in Go/TS routes can try.
func TestAPISidecarProxyFirstAndFallthrough(t *testing.T) {
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/echo":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("from-sidecar:" + r.URL.Path))
		case "/api/users/42":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("user-42"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer sidecar.Close()

	cfg := config.Default()
	cfg.API.Sidecar = &config.SidecarConfig{Target: sidecar.URL}

	sc, err := newAPISidecar(t.TempDir(), cfg)
	if err != nil {
		t.Fatalf("newAPISidecar: %v", err)
	}
	if sc.Prefix() != "/api" {
		t.Fatalf("prefix = %q, want /api", sc.Prefix())
	}

	// Handled response.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/echo", nil)
	if !sc.Handle(rec, req) {
		t.Fatal("expected sidecar to handle /api/echo")
	}
	if got := rec.Body.String(); got != "from-sidecar:/api/echo" {
		t.Errorf("body = %q", got)
	}

	// Dynamic segment is forwarded as-is (sidecar routes it itself).
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/users/42", nil)
	if !sc.Handle(rec, req) {
		t.Fatal("expected sidecar to handle /api/users/42")
	}
	if got := rec.Body.String(); got != "user-42" {
		t.Errorf("body = %q", got)
	}

	// 404 falls through.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/unknown", nil)
	if sc.Handle(rec, req) {
		t.Fatal("expected 404 to fall through, not be handled")
	}
	if rec.Body.Len() != 0 || rec.Code != 200 {
		t.Errorf("fallthrough should write nothing, got code=%d body=%q", rec.Code, rec.Body.String())
	}

	// Out-of-prefix paths are not matched.
	if sc.Matches("/other") {
		t.Error("Matches(/other) should be false")
	}
}

// TestAPISidecarUnreachableFallsThrough verifies an unreachable sidecar does not
// break the request; the caller falls through to built-ins.
func TestAPISidecarUnreachableFallsThrough(t *testing.T) {
	cfg := config.Default()
	// Port 1 is virtually never listening.
	cfg.API.Sidecar = &config.SidecarConfig{Target: "http://127.0.0.1:1"}

	sc, err := newAPISidecar(t.TempDir(), cfg)
	if err != nil {
		t.Fatalf("newAPISidecar: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	if sc.Handle(rec, req) {
		t.Fatal("unreachable sidecar should fall through")
	}
}

// TestAPISidecarCustomPrefix verifies a non-default prefix is recognized.
func TestAPISidecarCustomPrefix(t *testing.T) {
	cfg := config.Default()
	cfg.API.Sidecar = &config.SidecarConfig{Target: "http://127.0.0.1:1", Prefix: "/svc"}
	sc, err := newAPISidecar(t.TempDir(), cfg)
	if err != nil {
		t.Fatalf("newAPISidecar: %v", err)
	}
	if sc.Prefix() != "/svc" {
		t.Errorf("prefix = %q, want /svc", sc.Prefix())
	}
	if !sc.Matches("/svc/thing") || sc.Matches("/svcx") {
		t.Errorf("prefix matching wrong: /svc/thing=%v /svcx=%v", sc.Matches("/svc/thing"), sc.Matches("/svcx"))
	}
	if !strings.HasPrefix(sc.Prefix(), "/") {
		t.Error("prefix should start with /")
	}
}
