package jsruntime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAPIRouteExecute(t *testing.T) {
	tmpDir := t.TempDir()
	apiDir := filepath.Join(tmpDir, "api")
	_ = os.MkdirAll(apiDir, 0755)

	// Create a simple API route
	routeCode := `
function GET(request) {
	return {
		status: 200,
		headers: { 'Content-Type': 'application/json' },
		text: function() { return JSON.stringify({ message: 'hello' }); }
	};
}
`
	_ = os.WriteFile(filepath.Join(apiDir, "test.js"), []byte(routeCode), 0644)

	rt := NewAPIRouteRuntime(apiDir)
	result := rt.Execute(APIRequest{
		URL:     "http://localhost:3000/api/test",
		Method:  "GET",
		Path:    "/api/test",
		Headers: map[string][]string{},
	})

	if result.Status != 200 {
		t.Errorf("Expected 200, got %d", result.Status)
	}
	if string(result.Body) != `{"message":"hello"}` {
		t.Errorf("Expected {message:hello}, got %s", result.Body)
	}
}

// TestAPIRouteProcessEnv verifies environment variables configured on the
// runtime are visible inside an API route via process.env.
func TestAPIRouteProcessEnv(t *testing.T) {
	tmpDir := t.TempDir()
	apiDir := filepath.Join(tmpDir, "api")
	_ = os.MkdirAll(apiDir, 0755)

	routeCode := `
function GET(request) {
	return { status: 200, text: function() { return process.env.KRATE_TEST_ENV || 'missing'; } };
}
`
	_ = os.WriteFile(filepath.Join(apiDir, "env.js"), []byte(routeCode), 0644)

	rt := NewAPIRouteRuntime(apiDir)
	rt.SetEnv(map[string]string{"KRATE_TEST_ENV": "api-env"})
	result := rt.Execute(APIRequest{Method: "GET", Path: "/api/env"})

	if result.Status != 200 {
		t.Fatalf("Expected 200, got %d (%s)", result.Status, result.Error)
	}
	if string(result.Body) != "api-env" {
		t.Errorf("Body = %q, want %q (process.env not visible in route)", result.Body, "api-env")
	}
}

func TestAPIRoutePathTraversalRejected(t *testing.T) {
	tmpDir := t.TempDir()
	apiDir := filepath.Join(tmpDir, "api")
	_ = os.MkdirAll(apiDir, 0755)

	// A legit route inside the API dir.
	_ = os.WriteFile(filepath.Join(apiDir, "secret.js"), []byte("function GET() { return 'secret'; }"), 0644)
	// A decoy outside the API dir that must never be reached.
	_ = os.WriteFile(filepath.Join(tmpDir, "outside.js"), []byte("function GET() { return 'outside'; }"), 0644)

	rt := NewAPIRouteRuntime(apiDir)

	// The real route still resolves.
	ok := rt.Execute(APIRequest{Method: "GET", Path: "/api/secret"})
	if ok.Status != 200 {
		t.Fatalf("expected legit route to resolve, got %d", ok.Status)
	}

	for _, path := range []string{
		"/api/../secret",        // traverse up a level
		"/api/..%2F..%2Fsecret", // encoded traversal (decoded by resolver)
		"/api/..%2fsecret",      // lowercase encoded traversal
		"/api/%2e%2e/secret",    // dotted-encoded segments
		"/api/../outside",       // escape to the decoy
		"/api/..\\outside",      // backslash separator (Windows)
		"/api/x/../../secret",   // deep traversal back in
	} {
		result := rt.Execute(APIRequest{Method: "GET", Path: path})
		if result.Status != 404 {
			t.Errorf("path %q: expected 404 (rejected), got %d", path, result.Status)
		}
		if string(result.Body) != `{"error":"API Route Not Found"}` {
			t.Errorf("path %q: expected not-found body, got %q", path, result.Body)
		}
	}
}

// TestAPIRouteIsolationFreshState verifies isolation mode runs each request in a
// fresh VM, so module state does not leak between requests.
func TestAPIRouteIsolationFreshState(t *testing.T) {
	tmpDir := t.TempDir()
	apiDir := filepath.Join(tmpDir, "api")
	_ = os.MkdirAll(apiDir, 0755)

	code := `var n = 0;
function GET(request) { n++; return Response.json({ n: n }); }`
	_ = os.WriteFile(filepath.Join(apiDir, "iso.js"), []byte(code), 0644)

	rt := NewAPIRouteRuntime(apiDir)
	rt.SetPool(4, true)

	for i := 0; i < 3; i++ {
		r := rt.Execute(APIRequest{Method: "GET", Path: "/api/iso"})
		if r.Status != 200 || string(r.Body) != `{"n":1}` {
			t.Fatalf("request %d: status=%d body=%s (expected fresh state)", i, r.Status, r.Body)
		}
	}
}
