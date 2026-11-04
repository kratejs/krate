package build

import (
	"compress/gzip"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func secProject(t *testing.T) (root string, cfg *config.Config) {
	t.Helper()
	root = t.TempDir()
	long := strings.Repeat("<p>some page content to make the document big enough to precompress</p>", 40)
	writeTree(t, root, map[string]string{
		"src/pages/index.tsx": `export default function P() { return <main class="p-6 flex">home ` + long + `</main>; }`,
		"src/pages/admin.tsx": `export default function P() { return <main>admin secret ` + long + `</main>; }`,
		"src/pages/dash.tsx":  "export const config = { ssr: true };\nexport default function P() { return <main>dash " + long + "</main>; }",
		"src/api/hello.ts":    `export function GET() { return Response.json({ ok: true, secret: "api-source-marker" }); }`,
		"middleware.ts": `export function middleware(request) {
  const url = new URL(request.url);
  if (url.pathname.startsWith('/admin')) {
    return new Response('Forbidden', { status: 403, headers: { 'X-Mw': 'blocked' } });
  }
  return new Response(null, { status: 200, headers: { 'X-Mw': 'pass', 'x-middleware-next': '1' } });
}
`,
		"public/.env":                     "SECRET=1",
		"public/.git/config":              "[core]",
		"public/.well-known/security.txt": "Contact: x",
		"public/fonts/a.woff2":            "font",
		"public/images/team-leader.png":   "png",
		"public/app.bundle.js":            "console.log(1)",
		"public/robots.txt":               "User-agent: *",
	})
	cfg = config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.PublicDir = filepath.Join(root, "public")
	cfg.Sourcemap = true
	cfg.Tailwind.Enabled = true
	cfg.Compress.Gzip = true
	cfg.Redirects = []config.Redirect{
		{Source: "/legacy/*", Destination: "/:splat"},
		{Source: "/u/:name", Destination: "/:name"},
		{Source: "/docs/:path*", Destination: "/documentation/:path*", Permanent: true},
	}
	return root, cfg
}

func TestServerOnlyOutputStaysOutOfPublicRoot(t *testing.T) {
	root, cfg := secProject(t)
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	for _, rel := range []string{"api-node", "api", "server-manifest.json", "manifest.json", "workers.json", ".env", ".git"} {
		if _, err := os.Stat(filepath.Join(cfg.OutDir, rel)); err == nil {
			t.Errorf("dist/%s exists, want server-only output under dist/.krate", rel)
		}
	}
	for _, rel := range []string{".krate/api-node/hello.js", ".krate/api/hello.js", ".krate/manifest.json", ".krate/server-manifest.json"} {
		if _, err := os.Stat(filepath.Join(cfg.OutDir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("dist/%s missing: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(cfg.OutDir, ".well-known", "security.txt")); err != nil {
		t.Errorf(".well-known was not copied: %v", err)
	}
	// No file outside .krate may carry API source or a sourcemap of it.
	_ = filepath.Walk(cfg.OutDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || isUnderKrateDir(cfg.OutDir, p) {
			return nil
		}
		if b, err := os.ReadFile(p); err == nil && strings.Contains(string(b), "api-source-marker") {
			t.Errorf("%s leaks API source", p)
		}
		return nil
	})
	man, err := os.ReadFile(ManifestPath(cfg.OutDir))
	if err != nil {
		t.Fatal(err)
	}
	ms := string(man)
	if !strings.Contains(ms, `"hashedAssets"`) || !strings.Contains(ms, "/styles.") {
		t.Errorf("manifest lacks hashedAssets with the stylesheet:\n%.600s", ms)
	}
	if strings.Contains(ms, "team-leader") || strings.Contains(ms, "app.bundle") {
		t.Errorf("manifest hashedAssets lists user files:\n%.600s", ms)
	}
}

func getRaw(t *testing.T, base, path string, hdr map[string]string) (*http.Response, []byte) {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequest("GET", base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Accept-Encoding must be explicit so the client does not decode for us.
	req.Header.Set("Accept-Encoding", "identity")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func gunzipBody(t *testing.T, b []byte) string {
	t.Helper()
	zr, err := gzip.NewReader(strings.NewReader(string(b)))
	if err != nil {
		t.Fatalf("gunzip: %v (body %.80q)", err, b)
	}
	out, _ := io.ReadAll(zr)
	return string(out)
}

func TestServeStaticHardeningEndToEnd(t *testing.T) {
	root, cfg := secProject(t)
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.OutDir, "admin", "index.html.gz")); err != nil {
		t.Fatalf("expected precompressed sibling for /admin: %v", err)
	}
	provideRuntime(t, root)
	base, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("middleware wins over precompressed", func(t *testing.T) {
		for _, ae := range []string{"identity", "gzip", "br, gzip"} {
			resp, body := getRaw(t, base, "/admin/", map[string]string{"Accept-Encoding": ae})
			if resp.StatusCode != 403 {
				t.Errorf("Accept-Encoding %q: status = %d, want 403 (body %.80q)", ae, resp.StatusCode, body)
			}
			if resp.Header.Get("X-Mw") != "blocked" {
				t.Errorf("Accept-Encoding %q: middleware header missing", ae)
			}
		}
	})

	t.Run("static page uses sibling with cache headers", func(t *testing.T) {
		resp, body := getRaw(t, base, "/", map[string]string{"Accept-Encoding": "gzip"})
		if resp.StatusCode != 200 || resp.Header.Get("Content-Encoding") != "gzip" {
			t.Fatalf("status %d encoding %q", resp.StatusCode, resp.Header.Get("Content-Encoding"))
		}
		if !strings.Contains(gunzipBody(t, body), "home") {
			t.Errorf("gunzipped body is not the home page")
		}
		if resp.Header.Get("Cache-Control") != "no-cache" {
			t.Errorf("Cache-Control = %q, want no-cache", resp.Header.Get("Cache-Control"))
		}
		if !strings.Contains(resp.Header.Get("Vary"), "Accept-Encoding") {
			t.Errorf("Vary = %q, want Accept-Encoding", resp.Header.Get("Vary"))
		}
		if resp.Header.Get("X-Mw") != "pass" {
			t.Errorf("middleware header not applied to the precompressed response")
		}
	})

	t.Run("ssr page is never served from the baked sibling", func(t *testing.T) {
		plain, plainBody := getRaw(t, base, "/dash/", nil)
		gz, gzBody := getRaw(t, base, "/dash/", map[string]string{"Accept-Encoding": "gzip"})
		if gz.StatusCode != plain.StatusCode {
			t.Fatalf("status gzip %d vs plain %d", gz.StatusCode, plain.StatusCode)
		}
		if plain.Header.Get("Cache-Control") != "no-store" || gz.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("Cache-Control plain %q gzip %q, want no-store for both", plain.Header.Get("Cache-Control"), gz.Header.Get("Cache-Control"))
		}
		if gz.Header.Get("X-Mw") != "pass" {
			t.Errorf("middleware header missing on the gzip SSR response")
		}
		got := string(gzBody)
		if gz.Header.Get("Content-Encoding") == "gzip" {
			got = gunzipBody(t, gzBody)
		}
		if got != string(plainBody) {
			t.Errorf("gzip response differs from identity response for an SSR page")
		}
	})

	t.Run("open redirects stay on-site", func(t *testing.T) {
		for _, p := range []string{"/legacy/%2Fevil.com", "/legacy/%5Cevil.com", "/u/%5C%5Cevil.com", "/legacy//evil.com"} {
			resp, _ := getRaw(t, base, p, nil)
			loc := resp.Header.Get("Location")
			if resp.StatusCode/100 != 3 {
				t.Errorf("%s: status %d, want a redirect", p, resp.StatusCode)
			}
			if strings.HasPrefix(loc, "//") || strings.HasPrefix(loc, `/\`) || !strings.HasPrefix(loc, "/") {
				t.Errorf("%s: Location %q is not on-site", p, loc)
			}
		}
		resp, _ := getRaw(t, base, "/docs/a/b", nil)
		if loc := resp.Header.Get("Location"); resp.StatusCode != 301 || loc != "/documentation/a/b" {
			t.Errorf("/docs/a/b: %d Location %q, want 301 /documentation/a/b", resp.StatusCode, loc)
		}
	})

	t.Run("dotfiles directories and server files are not served", func(t *testing.T) {
		for _, p := range []string{
			"/.env", "/.git/config", "/fonts/", "/fonts", "/api-node/hello.js", "/api-node/", "/server-manifest.json",
			"/.krate/server-manifest.json", "/.krate/api-node/hello.js", "/.krate/api-node/hello.js.map",
		} {
			resp, body := getRaw(t, base, p, nil)
			if resp.StatusCode != 404 {
				t.Errorf("GET %s = %d, want 404 (body %.60q)", p, resp.StatusCode, body)
			}
		}
		for p, want := range map[string]int{"/.well-known/security.txt": 200, "/robots.txt": 200, "/fonts/a.woff2": 200} {
			if resp, _ := getRaw(t, base, p, nil); resp.StatusCode != want {
				t.Errorf("GET %s = %d, want %d", p, resp.StatusCode, want)
			}
		}
	})

	t.Run("user files are revalidated, built files are immutable", func(t *testing.T) {
		for _, p := range []string{"/images/team-leader.png", "/app.bundle.js"} {
			resp, _ := getRaw(t, base, p, nil)
			if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
				t.Errorf("%s: Cache-Control = %q, want no-cache", p, cc)
			}
		}
		resp, body := getRaw(t, base, "/", map[string]string{"Accept-Encoding": "identity"})
		_ = resp
		css := regexp.MustCompile(`/styles\.[a-z0-9]+\.css`).FindString(string(body))
		if css == "" {
			t.Fatalf("page has no hashed stylesheet link:\n%.400s", body)
		}
		resp, _ = getRaw(t, base, css, nil)
		if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
			t.Errorf("%s: Cache-Control = %q, want immutable", css, cc)
		}
	})

	t.Run("page routes are read only", func(t *testing.T) {
		for _, p := range []string{"/", "/dash/", "/admin/x"} {
			for _, m := range []string{"POST", "DELETE"} {
				req, _ := http.NewRequest(m, base+p, strings.NewReader("x=1"))
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				_ = resp.Body.Close()
				if p == "/admin/x" {
					continue // middleware answers first
				}
				if resp.StatusCode != 405 || resp.Header.Get("Allow") != "GET, HEAD" {
					t.Errorf("%s %s: %d Allow %q, want 405 GET, HEAD", m, p, resp.StatusCode, resp.Header.Get("Allow"))
				}
			}
		}
	})

	t.Run("trailing slash is canonical for ssg and ssr pages", func(t *testing.T) {
		for _, p := range []string{"/admin", "/dash"} {
			if p == "/admin" {
				continue // blocked by middleware
			}
			resp, _ := getRaw(t, base, p+"?a=1", nil)
			if resp.StatusCode != 301 || resp.Header.Get("Location") != "dash/?a=1" {
				t.Errorf("%s: %d Location %q, want 301 dash/?a=1", p, resp.StatusCode, resp.Header.Get("Location"))
			}
		}
	})

	t.Run("range request is not gzipped", func(t *testing.T) {
		resp, body := getRaw(t, base, "/robots.txt", map[string]string{"Accept-Encoding": "gzip", "Range": "bytes=0-3"})
		if resp.StatusCode != 206 || resp.Header.Get("Content-Encoding") != "" || string(body) != "User" {
			t.Errorf("range: %d encoding %q body %q", resp.StatusCode, resp.Header.Get("Content-Encoding"), body)
		}
	})
}

func TestDevServerBindsLoopbackAndGuardsEndpoints(t *testing.T) {
	root, cfg := secProject(t)
	cfg.Compress.Gzip = false
	cfg.Redirects = nil
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	provideRuntime(t, root)
	port := freePort(t)
	cfg.Server.Port = port
	hub := NewDevHub()
	errCh := make(chan error, 1)
	go func() { errCh <- ServeDev(root, cfg, hub, time.Now()) }()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(30 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("dev server did not start")
		}
		select {
		case err := <-errCh:
			t.Fatalf("dev server exited: %v", err)
		default:
		}
		if resp, err := http.Get(base + "/"); err == nil {
			_ = resp.Body.Close()
			registerServerStop(t, errCh)
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Not reachable on a non-loopback address of this machine.
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.IsLoopback() || ipn.IP.To4() == nil {
			continue
		}
		c, err := net.DialTimeout("tcp", net.JoinHostPort(ipn.IP.String(), fmt.Sprint(port)), time.Second)
		if err == nil {
			_ = c.Close()
			t.Errorf("dev server accepts connections on %s, want loopback only", ipn.IP)
		}
		break
	}

	check := func(name, path string, hdr map[string]string, host string, want int) {
		t.Helper()
		req, _ := http.NewRequest("GET", base+path, nil)
		if host != "" {
			req.Host = host
		}
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s: GET %s = %d, want %d", name, path, resp.StatusCode, want)
		}
	}
	// A valid same-origin call reaches the handler (400: path outside project).
	check("same origin", "/__krate/open?file=../../etc/passwd", map[string]string{"Origin": base}, "", 400)
	check("no browser headers", "/__krate/status", nil, "", 200)
	check("cross origin", "/__krate/open?file=src/pages/index.tsx", map[string]string{"Origin": "https://evil.example"}, "", 403)
	check("rebinding", "/__krate/status", nil, "evil.example:80", 403)
	check("cross site fetch", "/__krate/status", map[string]string{"Sec-Fetch-Site": "cross-site"}, "", 403)
}
