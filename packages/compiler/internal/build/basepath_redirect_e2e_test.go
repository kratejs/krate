package build

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// Under a base path the app sees base-less paths, so every root-relative
// redirect it answers with must leave the server with the base on it.
func TestServeRedirectsUnderBasePath(t *testing.T) {
	e2eEnv(t)
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx": `export default function Home() { return <main><h1>home</h1></main>; }`,
		"src/pages/gate/[slug].tsx": `import { redirect } from '@krate/runtime/server';
export default async function Gate({ params }: any) {
  const { slug } = await params;
  if (slug === "out") redirect("/login");
  return <main>{slug}</main>;
}`,
		"src/api/go.ts": `export function GET() { return new Response(null, { status: 302, headers: { Location: "/login?from=api" } }); }`,
		"middleware.ts": `export function middleware(request: Request) {
  const url = new URL(request.url);
  if (url.pathname === "/admin") return new Response(null, { status: 307, headers: { Location: "/login?from=mw" } });
  return new Response(null, { headers: { "x-middleware-next": "1" } });
}
export const config = { matcher: ["/admin"] };`,
	})
	off := false
	b.Cfg.Site.URL = "https://example.com/sub"
	b.Cfg.Redirects = []config.Redirect{
		{Source: "/old", Destination: "/new", Permanent: true},
		{Source: "/legacy/*", Destination: "/:splat"},
		{Source: "/outside", Destination: "/elsewhere", BasePath: &off},
	}
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	for _, tc := range []struct {
		name, method, path, want string
	}{
		{"config redirect", "GET", "/sub/old", "/sub/new"},
		{"config redirect with a splat", "GET", "/sub/legacy/a/b", "/sub/a/b"},
		{"config redirect on POST", "POST", "/sub/old", "/sub/new"},
		{"basePath: false matches outside the base", "GET", "/outside", "/elsewhere"},
		{"API route Location", "GET", "/sub/api/go", "/sub/login?from=api"},
		{"middleware Location", "GET", "/sub/admin", "/sub/login?from=mw"},
		{"redirect() from an SSR page", "GET", "/sub/gate/out/", "/sub/login"},
		{"outside the base", "GET", "/old", "/sub/old"},
	} {
		req, _ := http.NewRequest(tc.method, base+tc.path, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		_ = resp.Body.Close()
		if got := resp.Header.Get("Location"); got != tc.want || resp.StatusCode < 300 || resp.StatusCode >= 400 {
			t.Errorf("%s: %s %s = %d Location %q, want a redirect to %q", tc.name, tc.method, tc.path, resp.StatusCode, got, tc.want)
		}
	}

	// A page still renders where the redirects point.
	resp, err := client.Get(base + "/sub/gate/ok/")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /sub/gate/ok/ = %d, want 200", resp.StatusCode)
	}
}
