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

// TestAPIRouteCompletionValueIsDiscarded guards against a bundle whose last
// evaluated statement yields a value the host cannot marshal (a BigInt, which
// bundled crypto libraries produce at the top level).
func TestAPIRouteCompletionValueIsDiscarded(t *testing.T) {
	apiDir := filepath.Join(t.TempDir(), "api")
	_ = os.MkdirAll(apiDir, 0755)
	routeCode := `
function GET(request) {
	return { status: 200, text: function() { return 'ok'; } };
}
123456789012345678901234567890n;
`
	_ = os.WriteFile(filepath.Join(apiDir, "big.js"), []byte(routeCode), 0644)

	rt := NewAPIRouteRuntime(apiDir)
	defer rt.Close()
	result := rt.Execute(APIRequest{Method: "GET", Path: "/api/big"})
	if result.Status != 200 || string(result.Body) != "ok" {
		t.Fatalf("status %d body %q error %q", result.Status, result.Body, result.Error)
	}
}

// A route runs in the same request scope as the Node sidecar: cookies set
// through the action store reach the response and a thrown redirect signal
// answers 307 (what @krate/auth signIn() relies on).
func TestAPIRouteRequestScope(t *testing.T) {
	apiDir := filepath.Join(t.TempDir(), "api")
	_ = os.MkdirAll(apiDir, 0755)
	routeCode := `
export async function GET(request) {
	const scope = globalThis[Symbol.for('krate.action.store')].getStore();
	scope.cookies.set('flow', scope.cookies.get('sid').value + '!', { maxAge: 900 });
	await Promise.resolve();
	throw Object.assign(new Error('r'), { __krateRedirect: 'https://provider.example/authorize', __krateRedirectExternal: true });
}
`
	_ = os.WriteFile(filepath.Join(apiDir, "scope.js"), []byte(routeCode), 0644)

	rt := NewAPIRouteRuntime(apiDir)
	result := rt.Execute(APIRequest{Method: "GET", Path: "/api/scope", URL: "http://localhost:3000/api/scope", Headers: map[string][]string{"Cookie": {"sid=abc"}}})
	if result.Status != 307 {
		t.Fatalf("status = %d (%s), want 307", result.Status, result.Error)
	}
	if got := result.Headers["location"]; len(got) == 0 || got[0] != "https://provider.example/authorize" {
		t.Errorf("Location = %v", result.Headers)
	}
	if got := result.Headers["set-cookie"]; len(got) == 0 || got[0] != "flow=abc!; Path=/; Max-Age=900; HttpOnly; SameSite=Lax" {
		t.Errorf("Set-Cookie = %v", result.Headers)
	}
}

// TestAPIRouteStaticLeafUnderDynamicDir: a static route file inside a dynamic
// directory (orders/[id]/download.js) resolves, next to orders/[id].js.
func TestAPIRouteStaticLeafUnderDynamicDir(t *testing.T) {
	apiDir := filepath.Join(t.TempDir(), "api")
	if err := os.MkdirAll(filepath.Join(apiDir, "orders", "[id]"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(apiDir, "orders", "[id].js"), []byte("function GET() { return 'order'; }"), 0o644)
	_ = os.WriteFile(filepath.Join(apiDir, "orders", "[id]", "download.js"), []byte("function GET() { return 'download'; }"), 0o644)
	_ = os.WriteFile(filepath.Join(apiDir, "orders", "[id]", "[part].js"), []byte("function GET() { return 'part'; }"), 0o644)

	rt := NewAPIRouteRuntime(apiDir)
	for path, want := range map[string]string{
		"/api/orders/7":          `"order"`,
		"/api/orders/7/download": `"download"`,
		"/api/orders/7/other":    `"part"`,
	} {
		res := rt.Execute(APIRequest{Method: "GET", Path: path})
		if res.Status != 200 || string(res.Body) != want {
			t.Errorf("%s: got %d %q, want 200 %q", path, res.Status, res.Body, want)
		}
	}
}

func TestEmbeddedPoolDefaultsToOne(t *testing.T) {
	if DefaultEmbeddedPoolSize != 1 {
		t.Fatalf("DefaultEmbeddedPoolSize = %d, want 1", DefaultEmbeddedPoolSize)
	}
	a := NewAPIRouteRuntime(t.TempDir())
	if a.workers != 1 || cap(a.idle) != 1 {
		t.Errorf("API pool = %d workers, idle cap %d; want 1", a.workers, cap(a.idle))
	}
	a.SetPool(3, false)
	if a.workers != 3 || cap(a.idle) != 3 {
		t.Errorf("explicit SetPool(3) gave %d workers, idle cap %d", a.workers, cap(a.idle))
	}
	m := NewLazyMiddlewareRuntime(filepath.Join(t.TempDir(), "middleware.js"))
	if m.workers != 1 || cap(m.tokens) != 1 {
		t.Errorf("middleware pool = %d workers, tokens cap %d; want 1", m.workers, cap(m.tokens))
	}
	if DefaultAPIWorkers() < 1 {
		t.Errorf("sidecar default must stay at least 1")
	}
}
