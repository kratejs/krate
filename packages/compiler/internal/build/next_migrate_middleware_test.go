package build

import (
	"os"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/migrate"
)

// A migrated Next middleware keeps working through NextResponse: next() with
// request header overrides reaches the layout through headers(), next() with
// response headers sets them, rewrite() serves the target, new NextResponse()
// answers with its status and body, and redirect() redirects. It runs in the
// embedded runtime and in the Node sidecar.
func TestMigratedNextMiddlewareNextResponse(t *testing.T) {
	root := t.TempDir()
	for rel, content := range map[string]string{
		"package.json":            `{"name":"mw-app","private":true,"dependencies":{"next":"16.0.0","react":"19.0.0"}}`,
		"src/app/layout.tsx":      "import { headers } from 'next/headers';\n\nexport default async function L({ children }: { children: React.ReactNode }) {\n  const h = await headers();\n  return <html><body data-url={h.get('x-dd-url') || 'none'}>{children}</body></html>;\n}\n",
		"src/app/page.tsx":        `export default function Home() { return <main>home</main>; }`,
		"src/app/target/page.tsx": `export default function Target() { return <main>target page</main>; }`,
		"src/middleware.ts": `import { NextRequest, NextResponse } from 'next/server';

export function middleware(request: NextRequest) {
  const { pathname, search } = request.nextUrl;
  if (pathname === '/old') return NextResponse.rewrite(new URL('/target', request.url));
  if (pathname === '/gone') return new NextResponse('<h1>gone</h1>', { status: 404, headers: { 'content-type': 'text/html' } });
  if (pathname === '/go') return NextResponse.redirect(new URL('/target', request.url));
  if (pathname === '/hdr') return NextResponse.next({ headers: { 'x-resp': 'yes' } });
  const headers = new Headers(request.headers);
  headers.set('x-dd-url', pathname + search);
  return NextResponse.next({ request: { headers } });
}
`,
	} {
		writeFileRel(t, root, rel, content)
	}
	rep, err := migrate.ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Apply(rep, migrate.ApplyOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	if mw, _ := os.ReadFile(root + "/src/middleware.ts"); !strings.Contains(string(mw), "class NextResponse extends Response") {
		t.Fatalf("middleware was not migrated:\n%s", mw)
	}
	prepareMigratedRuntime(t, root)
	ver, err := VerifyMigration(root, rep.Routes, MigrationVerifyOptions{SkipCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	if !ver.BuildOK {
		t.Fatalf("the migrated app does not build: %s", ver.BuildError)
	}

	for _, runtime := range []string{"", "node"} {
		t.Run("runtime="+runtime, func(t *testing.T) {
			cfg, err := config.Load(root)
			if err != nil {
				t.Fatal(err)
			}
			cfg.SSR.MiddlewareRuntime = runtime
			base, err := startKrateServer(t, root, cfg)
			if err != nil {
				t.Fatal(err)
			}
			status, h, body := getBody(t, base+"/?next=%2Fa")
			if status != 200 || !strings.Contains(body, `data-url="/?next=%2Fa"`) {
				t.Errorf("layout did not see the overridden request header: %d\n%s", status, body)
			}
			if h.Get("x-middleware-next") != "" || h.Get("x-middleware-override-headers") != "" || h.Get("x-middleware-request-x-dd-url") != "" {
				t.Errorf("protocol headers leaked: %v", h)
			}
			if _, h, _ := getBody(t, base+"/hdr"); h.Get("x-resp") != "yes" {
				t.Errorf("response header from next({ headers }) missing: %v", h)
			}
			if status, _, body := getBody(t, base+"/old"); status != 200 || !strings.Contains(body, "target page") {
				t.Errorf("rewrite: %d\n%s", status, body)
			}
			status, h, body = getBody(t, base+"/gone")
			if status != 404 || !strings.Contains(body, "<h1>gone</h1>") || !strings.HasPrefix(h.Get("content-type"), "text/html") {
				t.Errorf("new NextResponse(html, { status: 404 }): %d %q\n%s", status, h.Get("content-type"), body)
			}
			if status, h, _ := getBody(t, base+"/go"); status != 307 || !strings.HasSuffix(h.Get("location"), "/target") {
				t.Errorf("redirect: %d %q", status, h.Get("location"))
			}
		})
	}
}
