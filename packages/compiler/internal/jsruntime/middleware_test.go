package jsruntime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMiddlewareExecution(t *testing.T) {
	rt, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer rt.Close()

	// Simulate esbuild-compiled middleware output
	middlewareCode := `
var middleware = function(request) {
	if (request.url.includes('/admin') && !request.headers['authorization']) {
		return {
			status: 302,
			headers: { 'Location': '/login' },
			text: function() { return ''; }
		};
	}
	return undefined;
};
`
	_, err = rt.Execute(middlewareCode)
	if err != nil {
		t.Fatalf("Execute middleware code failed: %v", err)
	}

	// Test: unauthenticated admin request → redirect
	result, err := rt.Execute(`
		(function() {
			var request = {
				url: 'http://localhost:3000/admin',
				method: 'GET',
				path: '/admin',
				headers: {},
			};
			var result = middleware(request);
			if (result && typeof result.status === 'number') {
				var headers = {};
				if (result.headers) {
					for (var k in result.headers) {
						headers[k] = result.headers[k];
					}
				}
				return JSON.stringify({ status: result.status, headers: headers });
			}
			return JSON.stringify({ action: 'continue' });
		})()
	`)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var res struct {
		Action  string            `json:"action"`
		Status  int               `json:"status"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal([]byte(result.(string)), &res); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if res.Status != 302 {
		t.Errorf("Expected 302, got %d", res.Status)
	}
	if res.Headers["Location"] != "/login" {
		t.Errorf("Expected /login redirect, got %s", res.Headers["Location"])
	}
}

func TestMiddlewareContinue(t *testing.T) {
	rt, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer rt.Close()

	middlewareCode := `
var middleware = function(request) {
	return undefined;
};
`
	_, err = rt.Execute(middlewareCode)
	if err != nil {
		t.Fatalf("Execute middleware code failed: %v", err)
	}

	result, err := rt.Execute(`
		(function() {
			var request = {
				url: 'http://localhost:3000/about',
				method: 'GET',
				path: '/about',
				headers: {},
			};
			var result = middleware(request);
			if (result && typeof result.status === 'number') {
				return JSON.stringify({ status: result.status });
			}
			return JSON.stringify({ action: 'continue' });
		})()
	`)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var res struct {
		Action string `json:"action"`
		Status int    `json:"status"`
	}
	if err := json.Unmarshal([]byte(result.(string)), &res); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if res.Action != "continue" {
		t.Errorf("Expected 'continue', got %s", res.Action)
	}
}

func TestMiddlewareObjectExport(t *testing.T) {
	rt, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer rt.Close()

	// Test: middleware exported as object property (Next.js-style default export)
	middlewareCode := `
var middleware = {
	middleware: function(request) {
		return { status: 403, text: function() { return 'Forbidden'; } };
	}
};
`
	_, err = rt.Execute(middlewareCode)
	if err != nil {
		t.Fatalf("Execute middleware code failed: %v", err)
	}

	result, err := rt.Execute(`
		(function() {
			var request = { url: 'http://localhost:3000/secret', method: 'GET', path: '/secret', headers: {} };
			var fn;
			if (typeof middleware === 'function') {
				fn = middleware;
			} else if (middleware && typeof middleware.middleware === 'function') {
				fn = middleware.middleware;
			}
			var result = fn(request);
			if (result && typeof result.status === 'number') {
				return JSON.stringify({ status: result.status, body: typeof result.text === 'function' ? result.text() : '' });
			}
			return JSON.stringify({ action: 'continue' });
		})()
	`)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var res struct {
		Action string `json:"action"`
		Status int    `json:"status"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal([]byte(result.(string)), &res); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if res.Status != 403 {
		t.Errorf("Expected 403, got %d", res.Status)
	}
	if res.Body != "Forbidden" {
		t.Errorf("Expected 'Forbidden', got %s", res.Body)
	}
}

func TestNewMiddlewareRuntime(t *testing.T) {
	// Create a temp middleware file
	tmpDir := t.TempDir()
	middlewarePath := filepath.Join(tmpDir, "middleware.js")

	middlewareCode := `
var middleware = function(request) {
	if (request.path === '/blocked') {
		return { status: 403, text: function() { return 'Blocked'; } };
	}
	return undefined;
};
`
	if err := os.WriteFile(middlewarePath, []byte(middlewareCode), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	mrt, err := NewMiddlewareRuntime(middlewarePath)
	if err != nil {
		t.Fatalf("NewMiddlewareRuntime failed: %v", err)
	}
	defer mrt.Close()

	// Test blocked path
	result := mrt.Execute(MiddlewareRequest{
		URL:    "http://localhost:3000/blocked",
		Method: "GET",
		Path:   "/blocked",
		Headers: map[string]string{},
	})
	if result.Status != 403 {
		t.Errorf("Expected 403, got %d", result.Status)
	}
	if result.Body != "Blocked" {
		t.Errorf("Expected 'Blocked', got %s", result.Body)
	}

	// Test allowed path
	result = mrt.Execute(MiddlewareRequest{
		URL:    "http://localhost:3000/about",
		Method: "GET",
		Path:   "/about",
		Headers: map[string]string{},
	})
	if result.Status != 0 || result.Action != "continue" {
		t.Errorf("Expected continue, got status=%d action=%s", result.Status, result.Action)
	}
}

func TestMiddlewareError(t *testing.T) {
	tmpDir := t.TempDir()
	middlewarePath := filepath.Join(tmpDir, "middleware.js")

	middlewareCode := `
var middleware = function(request) {
	throw new Error('middleware crashed');
};
`
	if err := os.WriteFile(middlewarePath, []byte(middlewareCode), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	mrt, err := NewMiddlewareRuntime(middlewarePath)
	if err != nil {
		t.Fatalf("NewMiddlewareRuntime failed: %v", err)
	}
	defer mrt.Close()

	result := mrt.Execute(MiddlewareRequest{
		URL:  "http://localhost:3000/test",
		Path: "/test",
	})
	if result.Error == "" {
		t.Error("Expected error message, got empty")
	}
}

func TestMiddlewareRealEsbuildOutput(t *testing.T) {
	tmpDir := t.TempDir()
	middlewarePath := filepath.Join(tmpDir, "middleware.js")

	// This is exactly what esbuild produces from the test-project middleware.ts
	realEsbuildOutput := `// ../../examples/middleware.ts
function middleware(request) {
  const url = new URL(request.url);
  const headers = {
    "X-Frame-Options": "DENY",
    "X-Content-Type-Options": "nosniff"
  };
  if (url.pathname === "/old-blog") {
    return new Response(null, {
      status: 301,
      headers: { ...headers, Location: "/blog" }
    });
  }
  if (url.pathname.startsWith("/admin")) {
    return new Response("Forbidden", {
      status: 403,
      headers
    });
  }
  return new Response(null, {
    status: 200,
    headers
  });
}
export {
  middleware
};
`
	if err := os.WriteFile(middlewarePath, []byte(realEsbuildOutput), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	mrt, err := NewMiddlewareRuntime(middlewarePath)
	if err != nil {
		t.Fatalf("NewMiddlewareRuntime failed: %v", err)
	}
	defer mrt.Close()

	// Test /old-blog → 301 redirect
	result := mrt.Execute(MiddlewareRequest{
		URL:  "http://localhost:3000/old-blog",
		Path: "/old-blog",
		Headers: map[string]string{},
	})
	if result.Status != 301 {
		t.Errorf("Expected 301, got %d", result.Status)
	}
	if result.Headers["location"] != "/blog" {
		t.Errorf("Expected Location: /blog, got %s", result.Headers["location"])
	}
	if result.Headers["x-frame-options"] != "DENY" {
		t.Errorf("Expected X-Frame-Options: DENY, got %s", result.Headers["x-frame-options"])
	}

	// Test /admin → 403
	result = mrt.Execute(MiddlewareRequest{
		URL:  "http://localhost:3000/admin/settings",
		Path: "/admin/settings",
		Headers: map[string]string{},
	})
	if result.Status != 403 {
		t.Errorf("Expected 403, got %d", result.Status)
	}
	if result.Body != "Forbidden" {
		t.Errorf("Expected 'Forbidden', got %s", result.Body)
	}

	// Test normal page → 200
	result = mrt.Execute(MiddlewareRequest{
		URL:  "http://localhost:3000/about",
		Path: "/about",
		Headers: map[string]string{},
	})
	if result.Status != 200 {
		t.Errorf("Expected 200, got %d", result.Status)
	}
}
