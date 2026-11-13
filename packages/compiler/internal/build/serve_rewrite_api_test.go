package build

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// A config rewrite whose destination is an /api route must reach the API
// handler, not the page handler. The Next.js migrator relies on this to keep
// the public URL of route handlers that live outside app/api.
func TestServeRewriteToAPIRoute(t *testing.T) {
	root, cfg := secProject(t)
	cfg.Redirects = nil
	cfg.Rewrites = []config.Rewrite{
		{Source: "/hi", Destination: "/api/hello"},
		{Source: "/r/:code", Destination: "/api/r/:code"},
	}
	writeTree(t, root, map[string]string{
		"src/api/r/[code].ts": "export function GET(req, ctx) { return Response.json({ code: ctx.params.code }); }",
	})
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	provideRuntime(t, root)
	base, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	resp, body := getRaw(t, base, "/hi", nil)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"ok":true`) {
		t.Errorf("/hi: status %d body %.120q, want the /api/hello JSON", resp.StatusCode, body)
	}
	resp, body = getRaw(t, base, "/r/abc", nil)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"abc"`) {
		t.Errorf("/r/abc: status %d body %.120q, want the dynamic API route", resp.StatusCode, body)
	}
}
