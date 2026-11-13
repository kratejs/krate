package scheme

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testAssets() fstest.MapFS {
	return fstest.MapFS{
		"index.html":             {Data: []byte("<h1>home</h1>")},
		"about/index.html":       {Data: []byte("about")},
		"docs.html":              {Data: []byte("docs")},
		"assets/app.js":          {Data: []byte("console.log(1)")},
		"assets/app.css":         {Data: []byte("a{}")},
		"assets/font.woff2":      {Data: []byte("font")},
		"assets/data.json":       {Data: []byte("{}")},
		"assets/icon.svg":        {Data: []byte("<svg/>")},
		"assets/mod.wasm":        {Data: []byte("\x00asm")},
		"assets/blob.unknownext": {Data: []byte("zz")},
		"404.html":               {Data: []byte("custom 404")},
		"tasks/[id]/index.html":  {Data: []byte("task shell")},
		"emptydir/keep":          {Data: []byte("k")},
	}
}

func fallback(p string) (string, bool) {
	if strings.HasPrefix(p, "/tasks/") {
		return "tasks/[id]/index.html", true
	}
	return "", false
}

func TestStaticResolution(t *testing.T) {
	h := New(Options{Assets: testAssets(), Fallback: fallback})
	tests := []struct {
		path   string
		status int
		body   string
		ctype  string
	}{
		{"/", 200, "<h1>home</h1>", "text/html; charset=utf-8"},
		{"/index.html", 200, "<h1>home</h1>", "text/html; charset=utf-8"},
		{"/about", 200, "about", "text/html; charset=utf-8"},
		{"/about/", 200, "about", "text/html; charset=utf-8"},
		{"/about/index.html", 200, "about", "text/html; charset=utf-8"},
		{"/docs", 200, "docs", "text/html; charset=utf-8"},
		{"/docs.html", 200, "docs", "text/html; charset=utf-8"},
		{"/assets/app.js", 200, "console.log(1)", "text/javascript; charset=utf-8"},
		{"/assets/app.css", 200, "a{}", "text/css; charset=utf-8"},
		{"/assets/font.woff2", 200, "font", "font/woff2"},
		{"/assets/data.json", 200, "{}", "application/json"},
		{"/assets/icon.svg", 200, "<svg/>", "image/svg+xml"},
		{"/assets/mod.wasm", 200, "\x00asm", "application/wasm"},
		{"/assets/blob.unknownext", 200, "zz", "application/octet-stream"},
		{"/tasks/123", 200, "task shell", "text/html; charset=utf-8"},
		{"/tasks/123/", 200, "task shell", "text/html; charset=utf-8"},
		{"/missing", 404, "custom 404", "text/html; charset=utf-8"},
		{"/emptydir", 404, "custom 404", "text/html; charset=utf-8"},
		{"/assets", 404, "custom 404", "text/html; charset=utf-8"},
		{"/about//", 200, "about", "text/html; charset=utf-8"},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("GET", tc.path, nil))
			if rec.Code != tc.status || rec.Body.String() != tc.body {
				t.Fatalf("%d %q, want %d %q", rec.Code, rec.Body.String(), tc.status, tc.body)
			}
			hd := rec.Header()
			if hd.Get("Content-Type") != tc.ctype {
				t.Errorf("content-type %q, want %q", hd.Get("Content-Type"), tc.ctype)
			}
			if hd.Get("Cache-Control") != "no-cache" || hd.Get("ETag") != "" || hd.Get("Last-Modified") != "" {
				t.Errorf("cache headers: %v", hd)
			}
			if hd.Get("X-Content-Type-Options") != "nosniff" {
				t.Error("nosniff missing")
			}
			if csp := hd.Get("Content-Security-Policy"); csp != DefaultCSP {
				t.Errorf("csp %q", csp)
			}
		})
	}
}

func TestStaticHeadAndMethods(t *testing.T) {
	h := New(Options{Assets: testAssets()})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("HEAD", "/assets/app.js", nil))
	if rec.Code != 200 || rec.Body.Len() != 0 || rec.Header().Get("Content-Length") != "14" {
		t.Fatalf("HEAD: %d len=%d cl=%s", rec.Code, rec.Body.Len(), rec.Header().Get("Content-Length"))
	}
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(m, "/", nil))
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") == "" {
			t.Errorf("%s: %d", m, rec.Code)
		}
	}
}

func TestStaticRejectsTraversal(t *testing.T) {
	assets := testAssets()
	assets["secret.txt"] = &fstest.MapFile{Data: []byte("secret")}
	h := New(Options{Assets: assets, Fallback: func(p string) (string, bool) { return "../etc/passwd", true }})
	for _, p := range []string{
		"/../secret.txt", "/assets/../secret.txt", "/assets/%2e%2e/secret.txt", "/%2e%2e/%2e%2e/etc/passwd",
		"/assets/..%2fsecret.txt", "/..", "/assets/..", `/assets\..\secret.txt`, "/a%00b", "/%5c..%5csecret.txt",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d body %q", p, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "secret") {
			t.Errorf("%s: leaked", p)
		}
	}
	// A Fallback that returns an invalid name must not escape the FS either.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/nothing", nil))
	if rec.Code != 404 || strings.Contains(rec.Body.String(), "root") {
		t.Errorf("bad fallback name: %d %q", rec.Code, rec.Body.String())
	}
}

func TestStaticWithoutAssetsOr404Page(t *testing.T) {
	rec := httptest.NewRecorder()
	New(Options{}).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 404 {
		t.Fatal(rec.Code)
	}
	rec = httptest.NewRecorder()
	New(Options{Assets: fstest.MapFS{"index.html": {Data: []byte("x")}}}).ServeHTTP(rec, httptest.NewRequest("GET", "/nope", nil))
	if rec.Code != 404 {
		t.Fatal(rec.Code)
	}
}

func TestCustomCSP(t *testing.T) {
	h := New(Options{Assets: testAssets(), CSP: "default-src 'none'"})
	for _, p := range []string{"/", "/__krate/nope", "/missing"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if got := rec.Header().Get("Content-Security-Policy"); got != "default-src 'none'" {
			t.Errorf("%s: %q", p, got)
		}
	}
}

func TestDefaultCSPShape(t *testing.T) {
	for _, want := range []string{"default-src 'self'", "connect-src 'self'", "img-src 'self' data: blob:", "object-src 'none'"} {
		if !strings.Contains(DefaultCSP, want) {
			t.Errorf("DefaultCSP lacks %q", want)
		}
	}
}
