package build

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestRewriteDestinationOpenRedirect(t *testing.T) {
	tests := []struct {
		name, path, source, dest, want string
	}{
		{"encoded slash in splat", "/legacy//evil.com", "/legacy/*", "/:splat", "/evil.com"},
		{"backslash in splat", `/legacy/\evil.com`, "/legacy/*", "/:splat", "/%5Cevil.com"},
		{"double backslash param", `/u/\\evil.com`, "/u/:name", "/:name", "/%5C%5Cevil.com"},
		{"scheme in splat stays a path", "/legacy/https://evil.com", "/legacy/*", "/:splat", "/https://evil.com"},
		{"control chars", "/legacy/a\nb", "/legacy/*", "/x/:splat", "/x/a%0Ab"},
		{"append form", `/old/\evil.com`, "/old/*", "/new", "/new/%5Cevil.com"},
		{"configured absolute stays", "/go/x", "/go/*", "https://example.com/:splat", "https://example.com/x"},
		{"configured scheme-relative stays", "/go/x", "/go/*", "//cdn.example.com/:splat", "//cdn.example.com/x"},
		{"spaces escaped", "/legacy/a b", "/legacy/*", "/new/:splat", "/new/a%20b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rewriteDestination(tt.path, tt.source, tt.dest)
			if got != tt.want {
				t.Fatalf("rewriteDestination(%q, %q, %q) = %q, want %q", tt.path, tt.source, tt.dest, got, tt.want)
			}
			if strings.HasPrefix(tt.dest, "/") && !strings.HasPrefix(tt.dest, "//") {
				if strings.HasPrefix(got, "//") || strings.HasPrefix(got, `/\`) {
					t.Fatalf("destination %q is not on-site", got)
				}
			}
		})
	}
}

func TestRewriteDestinationParamModifiers(t *testing.T) {
	tests := []struct {
		path, source, dest, want string
		match                    bool
	}{
		{"/docs/a/b", "/docs/:path*", "/documentation/:path*", "/documentation/a/b", true},
		{"/docs", "/docs/:path*", "/documentation/:path*", "/documentation/", true},
		{"/docs/a/b", "/docs/:path+", "/documentation/:path+", "/documentation/a/b", true},
		{"/docs", "/docs/:path+", "/documentation/:path+", "", false},
		{"/docs/a", "/docs/:slug?", "/documentation/:slug?", "/documentation/a", true},
		{"/docs", "/docs/:slug?", "/documentation/:slug?", "/documentation/", true},
		{"/docs/a/b", "/docs/:slug?", "/x/:slug?", "", false},
		{"/docs/a", "/docs/:path*", "/documentation/:path", "/documentation/a", true},
		// A '?' not declared on the source parameter is a query separator.
		{"/p/7", "/p/:id", "/item/:id?ref=1", "/item/7?ref=1", true},
		{"/u/bob", "/u/:name", "/:name", "/bob", true},
	}
	for _, tt := range tests {
		if got := matchRedirect(tt.path, tt.source); got != tt.match {
			t.Errorf("matchRedirect(%q, %q) = %v, want %v", tt.path, tt.source, got, tt.match)
			continue
		}
		if !tt.match {
			continue
		}
		if got := rewriteDestination(tt.path, tt.source, tt.dest); got != tt.want {
			t.Errorf("rewriteDestination(%q, %q, %q) = %q, want %q", tt.path, tt.source, tt.dest, got, tt.want)
		}
	}
}

func TestInternalOutputPathCoversLegacyServerFiles(t *testing.T) {
	for _, p := range []string{"/api-node/hello.js", "/api-node/", "/api-node", "/server-manifest.json", "/%61pi-node/x.js"} {
		if !isInternalOutputPath(p) {
			t.Errorf("isInternalOutputPath(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"/manifest.json", "/api/hello", "/api-nodes/x"} {
		if isInternalOutputPath(p) {
			t.Errorf("isInternalOutputPath(%q) = true, want false", p)
		}
	}
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHardenedDirNoListingNoDotfiles(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"index.html":                  "home",
		"about/index.html":            "about",
		"fonts/a.woff2":               "font",
		"docs/data/x.json":            "{}",
		".env":                        "SECRET=1",
		".git/config":                 "[core]",
		".well-known/security.txt":    "Contact: x",
		".krate/server-manifest.json": "{}",
		"api-node/hello.js":           "src",
		"server-manifest.json":        "{}",
	})
	h := http.FileServer(hardenedDir{http.Dir(root)})
	cases := []struct {
		path string
		want int
	}{
		{"/", 200},
		{"/about/", 200},
		{"/fonts/a.woff2", 200},
		{"/fonts/", 404},
		{"/fonts", 404},
		{"/docs/data/", 404},
		{"/docs/", 404},
		{"/.env", 404},
		{"/.git/config", 404},
		{"/.git/", 404},
		{"/%2eenv", 404},
		{"/.well-known/security.txt", 200},
		{"/.krate/server-manifest.json", 404},
		{"/api-node/hello.js", 404},
		{"/api-node/", 404},
		{"/server-manifest.json", 404},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", c.path, nil))
		if rec.Code != c.want {
			t.Errorf("GET %s = %d, want %d (body %.60q)", c.path, rec.Code, c.want, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "<pre>") {
			t.Errorf("GET %s returned a directory listing", c.path)
		}
	}
	// A directory with an index still gets the canonical trailing-slash redirect.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/about", nil))
	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("GET /about = %d, want 301", rec.Code)
	}
}

func TestIsHiddenPath(t *testing.T) {
	for p, want := range map[string]bool{
		"/.env": true, "/a/.git/config": true, "/%2Eenv": true, `/a\.env`: true,
		"/.well-known/x": false, "/a/b.c": false, "/": false, "/a/..b/c": true,
	} {
		if got := isHiddenPath(p); got != want {
			t.Errorf("isHiddenPath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestCopyDirToOutSkipsDotfiles(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeTree(t, src, map[string]string{
		"robots.txt": "ok", ".env": "SECRET", ".git/config": "x",
		".well-known/security.txt": "c", "img/.DS_Store": "x", "img/a.png": "p",
	})
	copied, err := copyDirToOut(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"robots.txt", ".well-known/security.txt", "img/a.png"} {
		if _, err := os.Stat(filepath.Join(dst, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s was not copied: %v", rel, err)
		}
	}
	for _, rel := range []string{".env", ".git", "img/.DS_Store"} {
		if _, err := os.Stat(filepath.Join(dst, filepath.FromSlash(rel))); err == nil {
			t.Errorf("%s was copied, want skipped", rel)
		}
	}
	if len(copied) != 3 {
		t.Errorf("copied = %v, want 3 files", copied)
	}
}

func TestGzipSkipsRangeAndEncoded(t *testing.T) {
	body := strings.Repeat("hello world ", 200)
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "x.html", time.Time{}, strings.NewReader(body))
	})
	h := gzipMiddleware(base)

	// Range request: raw partial bytes, no Content-Encoding.
	req := httptest.NewRequest("GET", "/x.html", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Range", "bytes=0-9")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", rec.Code)
	}
	if ce := rec.Header().Get("Content-Encoding"); ce != "" {
		t.Errorf("Content-Encoding = %q on a 206, want none", ce)
	}
	if rec.Body.String() != body[:10] {
		t.Errorf("206 body = %q, want first 10 identity bytes", rec.Body.String())
	}

	// Plain request still compresses.
	req = httptest.NewRequest("GET", "/x.html", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected gzip for a full response")
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(zr)
	if string(got) != body {
		t.Errorf("gzip body mismatch")
	}

	// A handler that already set Content-Encoding is not gzipped twice.
	pre := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Encoding", "br")
		_, _ = w.Write([]byte("already-br"))
	}))
	req = httptest.NewRequest("GET", "/x.html", nil)
	req.Header.Set("Accept-Encoding", "gzip, br")
	rec = httptest.NewRecorder()
	pre.ServeHTTP(rec, req)
	if rec.Header().Get("Content-Encoding") != "br" || rec.Body.String() != "already-br" {
		t.Errorf("pre-encoded response was re-encoded: %q %q", rec.Header().Get("Content-Encoding"), rec.Body.String())
	}
}

// The precompressed layer wraps only the file handler, so a handler in front of
// it (middleware, redirect, SSR) always wins.
func TestPrecompressedOnlyInsideStaticHandler(t *testing.T) {
	root := t.TempDir()
	html := strings.Repeat("<p>admin secret</p>", 100)
	writeTree(t, root, map[string]string{"admin/index.html": html})
	var zb bytes.Buffer
	zw := gzip.NewWriter(&zb)
	_, _ = zw.Write([]byte(html))
	_ = zw.Close()
	if err := os.WriteFile(filepath.Join(root, "admin", "index.html.gz"), zb.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	static := precompressedMiddlewareIf(root, nil, http.FileServer(hardenedDir{http.Dir(root)}))
	chain := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/admin") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		static.ServeHTTP(w, r)
	}))
	for _, ae := range []string{"", "gzip"} {
		req := httptest.NewRequest("GET", "/admin/", nil)
		if ae != "" {
			req.Header.Set("Accept-Encoding", ae)
		}
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("Accept-Encoding %q: status = %d, want 403", ae, rec.Code)
		}
	}
	// Directory URL without slash is the file server's redirect, not the sibling.
	req := httptest.NewRequest("GET", "/admin", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	static.ServeHTTP(rec, req)
	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("GET /admin = %d, want 301", rec.Code)
	}
	// The skip predicate keeps request-time pages off the sibling path.
	skipped := precompressedMiddlewareIf(root, func(*http.Request) bool { return true }, http.FileServer(hardenedDir{http.Dir(root)}))
	req = httptest.NewRequest("GET", "/admin/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec = httptest.NewRecorder()
	skipped.ServeHTTP(rec, req)
	if rec.Header().Get("Content-Encoding") != "" {
		t.Errorf("skip predicate ignored: Content-Encoding = %q", rec.Header().Get("Content-Encoding"))
	}
}

func TestPageMethodAllowed(t *testing.T) {
	for _, m := range []string{"GET", "HEAD"} {
		rec := httptest.NewRecorder()
		if !pageMethodAllowed(rec, httptest.NewRequest(m, "/", nil)) {
			t.Errorf("%s should be allowed", m)
		}
	}
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		rec := httptest.NewRecorder()
		if pageMethodAllowed(rec, httptest.NewRequest(m, "/", nil)) {
			t.Errorf("%s should be rejected", m)
		}
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" {
			t.Errorf("%s: status %d Allow %q", m, rec.Code, rec.Header().Get("Allow"))
		}
	}
}

func TestRedirectToCanonicalSlash(t *testing.T) {
	cases := []struct {
		method, target string
		redirect       bool
		loc            string
	}{
		{"GET", "/dash", true, "dash/"},
		{"GET", "/a/b?x=1", true, "b/?x=1"},
		{"HEAD", "/dash", true, "dash/"},
		{"GET", "/dash/", false, ""},
		{"GET", "/", false, ""},
		{"GET", "/feed.xml", false, ""},
		{"POST", "/dash", false, ""},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		got := redirectToCanonicalSlash(rec, httptest.NewRequest(c.method, c.target, nil))
		if got != c.redirect {
			t.Errorf("%s %s: redirect = %v, want %v", c.method, c.target, got, c.redirect)
			continue
		}
		if got && (rec.Code != 301 || rec.Header().Get("Location") != c.loc) {
			t.Errorf("%s %s: %d Location %q, want 301 %q", c.method, c.target, rec.Code, rec.Header().Get("Location"), c.loc)
		}
	}
}

func TestCanonicalPagePath(t *testing.T) {
	cfg := config.Default()
	for in, want := range map[string]string{
		"/": "/", "": "/", "/about": "/about/", "/about/": "/about/", "about": "/about/",
		"/blog/[slug]": "/blog/[slug]/", "/feed.xml": "/feed.xml",
	} {
		if got := CanonicalPagePath(in, cfg); got != want {
			t.Errorf("CanonicalPagePath(%q) = %q, want %q", in, got, want)
		}
	}
	if got := CanonicalPagePath("/about", nil); got != "/about/" {
		t.Errorf("nil cfg: %q", got)
	}
	cfg.BasePath = "/docs"
	if got := CanonicalPagePath("/about", cfg); got != "/docs/about/" {
		t.Errorf("with base path: %q", got)
	}
	if got := CanonicalPagePath("/", cfg); got != "/docs/" {
		t.Errorf("root with base path: %q", got)
	}
}

func TestDevBindHostDefaultsToLoopback(t *testing.T) {
	cfg := config.Default()
	if got := devBindHost(cfg); got != "127.0.0.1" {
		t.Errorf("default dev host = %q, want 127.0.0.1", got)
	}
	cfg.Server.Host = "10.0.0.5"
	if got := devBindHost(cfg); got != "10.0.0.5" {
		t.Errorf("server.host not honored: %q", got)
	}
	cfg.DevServer.Host = "0.0.0.0"
	if got := devBindHost(cfg); got != "0.0.0.0" {
		t.Errorf("devServer.host not honored: %q", got)
	}
}

func TestDevEndpointGuard(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	loop := devEndpointGuard("127.0.0.1", ok)
	any := devEndpointGuard("0.0.0.0", ok)
	cases := []struct {
		name    string
		h       http.Handler
		host    string
		headers map[string]string
		want    int
	}{
		{"loopback host", loop, "127.0.0.1:3000", nil, 204},
		{"localhost host", loop, "localhost:3000", nil, 204},
		{"ipv6 loopback", loop, "[::1]:3000", nil, 204},
		{"rebinding host", loop, "evil.example:3000", nil, 403},
		{"same origin", loop, "localhost:3000", map[string]string{"Origin": "http://localhost:3000", "Sec-Fetch-Site": "same-origin"}, 204},
		{"cross origin", loop, "localhost:3000", map[string]string{"Origin": "https://evil.example"}, 403},
		{"other port same host", loop, "localhost:3000", map[string]string{"Origin": "http://localhost:8080"}, 403},
		{"null origin", loop, "localhost:3000", map[string]string{"Origin": "null"}, 403},
		{"cross-site fetch", loop, "localhost:3000", map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"same-site fetch", loop, "localhost:3000", map[string]string{"Sec-Fetch-Site": "same-site"}, 403},
		{"wildcard bind lan host", any, "192.168.1.9:3000", nil, 204},
		{"wildcard bind still checks origin", any, "192.168.1.9:3000", map[string]string{"Origin": "https://evil.example"}, 403},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", "/__krate/open?file=x", nil)
		req.Host = c.host
		for k, v := range c.headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		c.h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s: status = %d, want %d", c.name, rec.Code, c.want)
		}
	}
}

func TestImageSrcOutsidePublicIsRejected(t *testing.T) {
	root := t.TempDir()
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><circle cx="5" cy="5" r="4"/></svg>`
	writeTree(t, root, map[string]string{
		"src/pages/index.tsx": `export default function Page() {
  return <div><Image src="/../secret.svg" alt="x" /><Image src="/hero.svg" alt="ok" /></div>;
}`,
		"secret.svg":      svg,
		"public/hero.svg": svg,
	})
	cfg := config.Default()
	cfg.Images.Unoptimized = true
	cfg.Resolve(root)
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	if !strings.Contains(html, "resolves outside public/") {
		t.Errorf("missing diagnostic for an escaping Image src:\n%.800s", html)
	}
	if !strings.Contains(html, "hero.svg") {
		t.Errorf("a valid Image src was rejected:\n%.800s", html)
	}
}
