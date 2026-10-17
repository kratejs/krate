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

	"github.com/andybalholm/brotli"
)

func writeAsset(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func brotliEncode(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := brotli.NewWriterLevel(&buf, brotli.BestSpeed)
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func gzipEncode(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPrecompressOutputWritesSiblings(t *testing.T) {
	dir := t.TempDir()
	html := "<html>" + strings.Repeat("compress me ", 500) + "</html>"
	js := "console.log('" + strings.Repeat("x", 2000) + "');"
	writeAsset(t, filepath.Join(dir, "index.html"), html)
	writeAsset(t, filepath.Join(dir, "app.js"), js)
	writeAsset(t, filepath.Join(dir, "tiny.txt"), "small")                   // below threshold
	writeAsset(t, filepath.Join(dir, "logo.png"), strings.Repeat("p", 3000)) // not compressible
	writeAsset(t, filepath.Join(dir, ".krate", "server-bundles", "page.mjs"), strings.Repeat("bundle ", 500))

	n, err := PrecompressOutput(dir, true, true)
	if err != nil {
		t.Fatalf("PrecompressOutput: %v", err)
	}
	if n != 4 {
		t.Fatalf("wrote %d siblings, want 4 (html+js x br+gz)", n)
	}

	for _, name := range []string{"index.html.br", "index.html.gz", "app.js.br", "app.js.gz"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s missing: %v", name, err)
		}
	}
	for _, name := range []string{"tiny.txt.br", "logo.png.br", "logo.png.gz", filepath.Join(".krate", "server-bundles", "page.mjs.br")} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s should not have been written", name)
		}
	}

	// Siblings decode back to the original bytes.
	assertBrotliEquals(t, filepath.Join(dir, "index.html.br"), html)
	assertGzipEquals(t, filepath.Join(dir, "app.js.gz"), js)

	// Idempotent: nothing is rewritten while the source is unchanged.
	n2, err := PrecompressOutput(dir, true, true)
	if err != nil {
		t.Fatalf("second PrecompressOutput: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("second pass wrote %d siblings, want 0", n2)
	}
}

func assertBrotliEquals(t *testing.T, path, want string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := io.ReadAll(brotli.NewReader(f))
	if err != nil {
		t.Fatalf("brotli decode %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s decoded to %q, want original", path, got)
	}
}

func assertGzipEquals(t *testing.T, path, want string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gzip decode %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s decoded to %q, want original", path, got)
	}
}

func TestAcceptsEncoding(t *testing.T) {
	cases := []struct {
		header string
		coding string
		want   bool
	}{
		{"gzip, deflate, br", "br", true},
		{"gzip", "br", false},
		{"br;q=0, gzip", "br", false},
		{"gzip;q=0.8", "gzip", true},
		{"*", "br", true},
		{"", "gzip", false},
		{"identity", "br", false},
	}
	for _, c := range cases {
		if got := acceptsEncoding(c.header, c.coding); got != c.want {
			t.Errorf("acceptsEncoding(%q, %q) = %v, want %v", c.header, c.coding, got, c.want)
		}
	}
}

func TestPrecompressedMiddlewareServesBrotli(t *testing.T) {
	dir := t.TempDir()
	body := "console.log('" + strings.Repeat("z", 3000) + "');"
	writeAsset(t, filepath.Join(dir, "app.js"), body)
	if err := os.WriteFile(filepath.Join(dir, "app.js.br"), brotliEncode(t, body), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js.gz"), gzipEncode(t, body), 0644); err != nil {
		t.Fatal(err)
	}

	next := false
	h := precompressedMiddleware(dir, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.Header.Set("Accept-Encoding", "gzip, br")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if next {
		t.Fatal("expected the precompressed path to short-circuit the handler")
	}
	if got := rec.Header().Get("Content-Encoding"); got != "br" {
		t.Fatalf("Content-Encoding = %q, want br", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatal("missing Vary: Accept-Encoding")
	}
	got, err := io.ReadAll(brotli.NewReader(rec.Body))
	if err != nil || string(got) != body {
		t.Fatalf("decoded body = %q (err %v), want original", got, err)
	}
}

func TestPrecompressedMiddlewareGzipWhenBrotliUnsupported(t *testing.T) {
	dir := t.TempDir()
	body := strings.Repeat("g", 2000)
	writeAsset(t, filepath.Join(dir, "style.css"), body)
	if err := os.WriteFile(filepath.Join(dir, "style.css.gz"), gzipEncode(t, body), 0644); err != nil {
		t.Fatal(err)
	}
	h := precompressedMiddleware(dir, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/style.css", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/css; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestPrecompressedMiddlewareFallsThrough(t *testing.T) {
	dir := t.TempDir()
	writeAsset(t, filepath.Join(dir, "app.js"), "no sibling here")

	next := false
	h := precompressedMiddleware(dir, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next = true
		w.WriteHeader(http.StatusNoContent)
	}))

	// No sibling built.
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.Header.Set("Accept-Encoding", "br, gzip")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !next {
		t.Fatal("expected fall-through when no sibling exists")
	}

	// Client accepts nothing compressible.
	next = false
	req = httptest.NewRequest(http.MethodGet, "/app.js", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !next {
		t.Fatal("expected fall-through when the client accepts no coding")
	}

	// Path traversal must not escape the output root.
	next = false
	req = httptest.NewRequest(http.MethodGet, "/../secret.js", nil)
	req.Header.Set("Accept-Encoding", "br")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !next {
		t.Fatal("expected fall-through for a traversal path with no sibling")
	}
}

func TestPrecompressedMiddlewareDirectoryIndex(t *testing.T) {
	dir := t.TempDir()
	body := "<html>" + strings.Repeat("d", 2000) + "</html>"
	writeAsset(t, filepath.Join(dir, "guide", "index.html"), body)
	if err := os.WriteFile(filepath.Join(dir, "guide", "index.html.br"), brotliEncode(t, body), 0644); err != nil {
		t.Fatal(err)
	}
	h := precompressedMiddleware(dir, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/guide/", nil)
	req.Header.Set("Accept-Encoding", "br")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "br" {
		t.Fatalf("Content-Encoding = %q, want br", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
}

// A compressible response served without a coding still advertises Vary so a
// shared cache cannot hand an uncompressed body to a gzip-capable client.
func TestGzipMiddlewareVaryWithoutGzip(t *testing.T) {
	h := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("plain"))
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil) // no Accept-Encoding
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatal("compressible response missing Vary: Accept-Encoding")
	}
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("must not gzip without client support")
	}
	if rec.Body.String() != "plain" {
		t.Fatalf("body = %q, want plain", rec.Body.String())
	}
}

// The root document (/) must be precompressed-served, not just directory subpaths.
func TestPrecompressedMiddlewareRootIndex(t *testing.T) {
	dir := t.TempDir()
	body := "<!doctype html>" + strings.Repeat("home ", 2000)
	writeAsset(t, filepath.Join(dir, "index.html"), body)
	if err := os.WriteFile(filepath.Join(dir, "index.html.br"), brotliEncode(t, body), 0644); err != nil {
		t.Fatal(err)
	}
	h := precompressedMiddleware(dir, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "br")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "br" {
		t.Fatalf("Content-Encoding = %q, want br", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
}

// End-to-end over a real socket: the production middleware chain
// (precompressed -> gzip -> http.FileServer) negotiates the sibling per
// Accept-Encoding and serves the correct bytes.
func TestPrecompressedLiveServer(t *testing.T) {
	root := t.TempDir()
	home := "<!doctype html><html><body>" + strings.Repeat("home page content ", 200) + "</body></html>"
	js := "export const x = 1;\n" + strings.Repeat("console.log('y');\n", 200)
	writeAsset(t, filepath.Join(root, "index.html"), home)
	writeAsset(t, filepath.Join(root, "app.js"), js)

	// Exactly what `krate build --precompress` writes.
	n, err := PrecompressOutput(root, true, true)
	if err != nil {
		t.Fatalf("PrecompressOutput: %v", err)
	}
	if n != 4 {
		t.Fatalf("wrote %d siblings, want 4", n)
	}

	handler := precompressedMiddleware(root, gzipMiddleware(http.FileServer(http.Dir(root))))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// Disable the client's automatic gzip so we observe the wire bytes.
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	get := func(path, accept string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if accept != "" {
			req.Header.Set("Accept-Encoding", accept)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		return resp
	}

	// 1. Root document negotiated as brotli.
	resp := get("/", "br")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Encoding"); got != "br" {
		t.Fatalf("GET / Content-Encoding = %q, want br", got)
	}
	if !strings.Contains(resp.Header.Get("Vary"), "Accept-Encoding") {
		t.Fatalf("GET / missing Vary: Accept-Encoding")
	}
	body, err := io.ReadAll(brotli.NewReader(resp.Body))
	resp.Body.Close()
	if err != nil || string(body) != home {
		t.Fatalf("GET / br body mismatch (err %v)", err)
	}

	// 2. Same response negotiated as gzip for a gzip-only client.
	resp = get("/", "gzip")
	if got := resp.Header.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("GET / Content-Encoding = %q, want gzip", got)
	}
	zr, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	body, _ = io.ReadAll(zr)
	resp.Body.Close()
	if string(body) != home {
		t.Fatal("GET / gzip body mismatch")
	}

	// 3. Static asset at a non-root path, brotli.
	resp = get("/app.js", "br, gzip")
	if got := resp.Header.Get("Content-Encoding"); got != "br" {
		t.Fatalf("GET /app.js Content-Encoding = %q, want br", got)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Fatalf("GET /app.js Content-Type = %q", got)
	}
	body, _ = io.ReadAll(brotli.NewReader(resp.Body))
	resp.Body.Close()
	if string(body) != js {
		t.Fatal("GET /app.js br body mismatch")
	}

	// 4. No Accept-Encoding: identity, and the file server's raw bytes.
	resp = get("/app.js", "")
	if enc := resp.Header.Get("Content-Encoding"); enc != "" {
		t.Fatalf("GET /app.js without Accept-Encoding got encoding %q", enc)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != js {
		t.Fatal("GET /app.js identity body mismatch")
	}
}
