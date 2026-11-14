package jsruntime

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestMiddlewareRenamedExportsDoNotClobberHelpers(t *testing.T) {
	// esbuild output when a helper module owns a top-level config() and the
	// middleware's own config is renamed config2.
	m := newTestMiddleware(t, `
function config() { return "ok=1"; }
var config2 = { matcher: ["/locked"] };
function middleware(request) {
  if (!(request.headers.get("cookie") || "").includes(config())) return Response.redirect(new URL("/", request.url));
}
export {
  config2 as config,
  middleware
};
`)
	r := m.Execute(MiddlewareRequest{URL: "http://h/locked", Path: "/locked"})
	if r.Action != MiddlewareResponse || r.Status != 302 {
		t.Fatalf("no cookie: %+v", r)
	}
	r = m.Execute(MiddlewareRequest{URL: "http://h/locked", Path: "/locked", Headers: map[string][]string{"Cookie": {"ok=1"}}})
	if r.Action != MiddlewareContinue {
		t.Fatalf("cookie: %+v", r)
	}
	if !m.HasMatcher() {
		t.Fatal("renamed config export must supply the matcher")
	}
}
