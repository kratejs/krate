package build

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// Config redirects run before middleware and cover /api, rewrites run after
// middleware, and a user headers rule beats the static file server's
// Cache-Control.
func TestServeRouteRulesOrderAndHeaders(t *testing.T) {
	root, cfg := secProject(t)
	writeTree(t, root, map[string]string{
		// A maintenance gate that refuses every page and API request, except / and /ok-rw.
		"middleware.ts": `export function middleware(request) {
  const url = new URL(request.url);
  if (url.pathname === '/ok-rw' || url.pathname === '/') {
    return new Response(null, { status: 200, headers: { 'x-middleware-next': '1' } });
  }
  return new Response('maintenance', { status: 503 });
}
`,
		"src/pages/templates.tsx": `export default function P() { return <main>templates page</main>; }`,
	})
	cfg.Redirects = append(ddRedirects(),
		config.Redirect{Source: "/old-api/:rest*", Destination: "/api/:rest*", StatusCode: 307},
	)
	cfg.Rewrites = []config.Rewrite{
		{Source: "/gone", Destination: "/ok-rw"},
		{Source: "/ok-rw", Destination: "/api/hello", Has: []config.RouteCondition{{Type: "query", Key: "go"}}},
	}
	cfg.Headers = []config.HeaderRule{
		{Source: "/:path*", Headers: map[string]string{"X-Rule": "all"}},
		{Source: "/robots.txt", Headers: map[string]string{"Cache-Control": "public, max-age=99"}},
	}
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	provideRuntime(t, root)
	base, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		path   string
		status int
		loc    string
	}{
		{"/store?vehicle=x", 308, "/templates?vehicle=x"},
		{"/store/some-slug", 308, "/templates/some-slug"},
		{"/store/compare", 503, ""}, // not redirected, so the gate answers
		{"/store/plans?x=1", 308, "/store?x=1"},
		{"/old-api/hello?a=1", 307, "/api/hello?a=1"},
		{"/gone", 503, ""},       // the gate sees /gone before the rewrite maps it
		{"/ok-rw?go=1", 200, ""}, // the gate passes it, then the conditional rewrite serves the API
		{"/api/hello", 503, ""},
	} {
		resp, _ := getRaw(t, base, tt.path, nil)
		if resp.StatusCode != tt.status || resp.Header.Get("Location") != tt.loc {
			t.Errorf("%s: %d %q, want %d %q", tt.path, resp.StatusCode, resp.Header.Get("Location"), tt.status, tt.loc)
		}
		if resp.Header.Get("X-Rule") != "all" {
			t.Errorf("%s: the headers rule did not apply (X-Rule=%q)", tt.path, resp.Header.Get("X-Rule"))
		}
	}

	resp, _ := getRaw(t, base, "/robots.txt", nil)
	if resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "public, max-age=99" {
		t.Errorf("/robots.txt: %d Cache-Control=%q, want the rule's value over the file server's", resp.StatusCode, resp.Header.Get("Cache-Control"))
	}
	resp, _ = getRaw(t, base, "/robots.txt", map[string]string{"Cookie": "a=1"})
	if strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("a public file must not be immutable: %q", resp.Header.Get("Cache-Control"))
	}
}

// A hashed asset the build emitted keeps its immutable Cache-Control even when
// a catch-all headers rule sets one.
func TestServeHashedAssetKeepsImmutableUnderHeadersRule(t *testing.T) {
	root, cfg := secProject(t)
	cfg.Redirects = nil
	cfg.Headers = []config.HeaderRule{{Source: "/:path*", Headers: map[string]string{"Cache-Control": "public, max-age=60"}}}
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	provideRuntime(t, root)
	base, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	hashed := loadHashedAssets(cfg.OutDir)
	var css string
	for p := range hashed {
		if strings.HasSuffix(p, ".css") {
			css = p
			break
		}
	}
	if css == "" {
		t.Skip("the build emitted no hashed stylesheet")
	}
	resp, _ := getRaw(t, base, css, nil)
	if got := resp.Header.Get("Cache-Control"); got != immutableCacheControl {
		t.Errorf("%s: Cache-Control = %q, want the immutable value", css, got)
	}
	resp, _ = getRaw(t, base, "/", nil)
	if got := resp.Header.Get("Cache-Control"); got != "public, max-age=60" {
		t.Errorf("/: Cache-Control = %q, want the rule's value", got)
	}
}
