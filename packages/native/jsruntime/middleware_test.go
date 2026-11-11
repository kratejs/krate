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

	// Test: unauthenticated admin request -> redirect
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
		URL:     "http://localhost:3000/blocked",
		Method:  "GET",
		Path:    "/blocked",
		Headers: map[string][]string{},
	})
	if result.Status != 403 {
		t.Errorf("Expected 403, got %d", result.Status)
	}
	if string(result.Body) != "Blocked" {
		t.Errorf("Expected 'Blocked', got %s", result.Body)
	}

	// Test allowed path
	result = mrt.Execute(MiddlewareRequest{
		URL:     "http://localhost:3000/about",
		Method:  "GET",
		Path:    "/about",
		Headers: map[string][]string{},
	})
	if result.Action != "continue" {
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

	// Test /old-blog -> 301 redirect
	result := mrt.Execute(MiddlewareRequest{
		URL:     "http://localhost:3000/old-blog",
		Path:    "/old-blog",
		Headers: map[string][]string{},
	})
	if result.Status != 301 {
		t.Errorf("Expected 301, got %d", result.Status)
	}
	if mwHeader(result, "location") != "/blog" {
		t.Errorf("Expected Location: /blog, got %s", mwHeader(result, "location"))
	}
	if mwHeader(result, "x-frame-options") != "DENY" {
		t.Errorf("Expected X-Frame-Options: DENY, got %s", mwHeader(result, "x-frame-options"))
	}

	// Test /admin -> 403
	result = mrt.Execute(MiddlewareRequest{
		URL:     "http://localhost:3000/admin/settings",
		Path:    "/admin/settings",
		Headers: map[string][]string{},
	})
	if result.Status != 403 {
		t.Errorf("Expected 403, got %d", result.Status)
	}
	if string(result.Body) != "Forbidden" {
		t.Errorf("Expected 'Forbidden', got %s", result.Body)
	}

	// Test normal page -> 200
	result = mrt.Execute(MiddlewareRequest{
		URL:     "http://localhost:3000/about",
		Path:    "/about",
		Headers: map[string][]string{},
	})
	if result.Status != 200 {
		t.Errorf("Expected 200, got %d", result.Status)
	}
}

func mwHeader(r MiddlewareResult, name string) string {
	for _, h := range r.Headers {
		if h[0] == name {
			return h[1]
		}
	}
	return ""
}

func newTestMiddleware(t *testing.T, code string) *MiddlewareRuntime {
	t.Helper()
	p := filepath.Join(t.TempDir(), "middleware.js")
	if err := os.WriteFile(p, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := NewMiddlewareRuntime(p)
	if err != nil {
		t.Fatalf("NewMiddlewareRuntime: %v", err)
	}
	t.Cleanup(m.Close)
	return m
}

func TestMiddlewareResponseRedirectAsyncAndHost(t *testing.T) {
	m := newTestMiddleware(t, `
export async function middleware(request) {
  await new Promise((r) => setTimeout(r, 5));
  const url = new URL(request.url);
  if (url.pathname === "/admin") return Response.redirect(new URL("/login", request.url));
  if (url.pathname === "/ok200") return new Response("hello", { status: 200 });
  if (url.pathname === "/pass") return new Response(null, { headers: { "x-middleware-next": "1", "x-added": "yes" } });
  if (url.pathname === "/cookies") { const h = new Headers({"x-middleware-next": "1"}); h.append("set-cookie", "a=1; Path=/"); h.append("set-cookie", "b=2"); return new Response(null, { headers: h }); }
  if (url.pathname === "/rewrite") return new Response(null, { headers: { "x-middleware-rewrite": "/other" } });
  if (request.cookies.get("sid")?.value === "42" && request.headers.get("Authorization") === "t") return new Response("authed", { status: 202 });
}
`)
	r := m.Execute(MiddlewareRequest{URL: "http://blog.example.com/admin", Method: "GET", Path: "/admin"})
	if r.Action != MiddlewareResponse || r.Status != 302 || mwHeader(r, "location") != "http://blog.example.com/login" {
		t.Fatalf("redirect: %+v", r)
	}
	r = m.Execute(MiddlewareRequest{URL: "http://h/ok200", Path: "/ok200"})
	if r.Action != MiddlewareResponse || r.Status != 200 || string(r.Body) != "hello" {
		t.Fatalf("an explicit 200 Response must be honored: %+v", r)
	}
	r = m.Execute(MiddlewareRequest{URL: "http://h/pass", Path: "/pass"})
	if r.Action != MiddlewareContinue || mwHeader(r, "x-added") != "yes" || mwHeader(r, "x-middleware-next") != "" {
		t.Fatalf("passthrough: %+v", r)
	}
	r = m.Execute(MiddlewareRequest{URL: "http://h/cookies", Path: "/cookies"})
	n := 0
	for _, h := range r.Headers {
		if h[0] == "set-cookie" {
			n++
		}
	}
	if r.Action != MiddlewareContinue || n != 2 {
		t.Fatalf("multi set-cookie must survive: %+v", r)
	}
	r = m.Execute(MiddlewareRequest{URL: "http://h/rewrite", Path: "/rewrite"})
	if r.Action != MiddlewareContinue || r.Rewrite != "/other" {
		t.Fatalf("rewrite: %+v", r)
	}
	r = m.Execute(MiddlewareRequest{URL: "http://h/x", Path: "/x", Headers: map[string][]string{"Authorization": {"t"}, "Cookie": {"sid=42; other=1"}}})
	if r.Action != MiddlewareResponse || r.Status != 202 {
		t.Fatalf("headers.get + cookies: %+v", r)
	}
	r = m.Execute(MiddlewareRequest{URL: "http://h/none", Path: "/none"})
	if r.Action != MiddlewareContinue {
		t.Fatalf("undefined must continue: %+v", r)
	}
}

func TestMiddlewareMatcher(t *testing.T) {
	m := newTestMiddleware(t, `
export function middleware() { return new Response("mw", { status: 200 }); }
export const config = { matcher: ["/dashboard/:path*", "/account", "/((?!api|_next/static|favicon.ico).*)/private"] };
`)
	if !m.HasMatcher() {
		t.Fatal("matcher export not detected")
	}
	cases := map[string]string{
		"/dashboard": MiddlewareResponse, "/dashboard/a/b": MiddlewareResponse, "/account": MiddlewareResponse,
		"/dashboards": MiddlewareSkip, "/other": MiddlewareSkip, "/x/private": MiddlewareResponse, "/api/private": MiddlewareSkip,
	}
	for path, want := range cases {
		if got := m.Execute(MiddlewareRequest{URL: "http://h" + path, Path: path}).Action; got != want {
			t.Errorf("%s: action = %s, want %s", path, got, want)
		}
	}
}

func TestMiddlewareReloadsWhenFileChanges(t *testing.T) {
	p := filepath.Join(t.TempDir(), "middleware.js")
	write := func(body string) {
		if err := os.WriteFile(p, []byte("export function middleware() { return new Response('"+body+"'); }"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("one")
	m := NewLazyMiddlewareRuntime(p)
	defer m.Close()
	if r := m.Execute(MiddlewareRequest{Path: "/"}); string(r.Body) != "one" {
		t.Fatalf("first load: %+v", r)
	}
	write("two-changed")
	if r := m.Execute(MiddlewareRequest{Path: "/"}); string(r.Body) != "two-changed" {
		t.Fatalf("expected reload, got %+v", r)
	}
	_ = os.Remove(p)
	if r := m.Execute(MiddlewareRequest{Path: "/"}); r.Action != MiddlewareSkip {
		t.Fatalf("missing file should skip, got %+v", r)
	}
}

func TestMiddlewareConcurrent(t *testing.T) {
	m := newTestMiddleware(t, `export async function middleware(request) { await new Promise((r) => setTimeout(r, 20)); return new Response(new URL(request.url).pathname); }`)
	done := make(chan string, 8)
	for i := 0; i < 8; i++ {
		go func(i int) {
			p := "/p" + string(rune('a'+i))
			done <- p + "=" + string(m.Execute(MiddlewareRequest{URL: "http://h" + p, Path: p}).Body)
		}(i)
	}
	for i := 0; i < 8; i++ {
		s := <-done
		if len(s) < 7 || s[:3] != s[4:7] {
			t.Errorf("mismatched concurrent result %q", s)
		}
	}
}
