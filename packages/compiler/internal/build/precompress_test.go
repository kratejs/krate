package build

import (
	"bytes"
	"compress/gzip"
	"fmt"
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

// Scripts and stylesheets are compressed at brotli's maximum level unless the
// default level does better, other assets at the default level.
func TestPrecompressScriptsAtMaxBrotliLevel(t *testing.T) {
	dir := t.TempDir()
	var src strings.Builder
	for i := 0; i < 4000; i++ {
		fmt.Fprintf(&src, "function f%d(a,b){return a*%d+b.map(x=>x+%d).join(',')}\n", i, i%97, i%13)
	}
	data := []byte(src.String())
	for _, name := range []string{"app.js", "page.html"} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := PrecompressOutput(dir, true, false); err != nil {
		t.Fatal(err)
	}
	best, _ := brotliCompressLevel(data, brotliScriptLevel)
	def, _ := brotliCompressLevel(data, brotliLevel)
	smaller := best
	if len(def) < len(best) {
		smaller = def
	}
	for name, want := range map[string][]byte{"app.js": smaller, "page.html": def} {
		got, err := os.ReadFile(filepath.Join(dir, name+".br"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s.br has %d bytes, want %d", name, len(got), len(want))
		}
	}
	// On real script code the maximum level wins.
	code, err := os.ReadFile("precompress.go")
	if err != nil {
		t.Fatal(err)
	}
	if hi, _ := brotliCompressFor("x.js", code); len(hi) >= len(mustBrotli(t, code, brotliLevel)) {
		t.Errorf("a script compressed to %d bytes, not smaller than the default level", len(hi))
	}
}

func mustBrotli(t *testing.T, data []byte, level int) []byte {
	t.Helper()
	out, err := brotliCompressLevel(data, level)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// A precompressed asset served through the gzip writer names Accept-Encoding
// in Vary once.
func TestAddVaryOnce(t *testing.T) {
	h := http.Header{}
	addVary(h, "Accept-Encoding")
	addVary(h, "accept-encoding")
	h.Add("Vary", "Origin")
	addVary(h, "Origin")
	if got := h.Values("Vary"); len(got) != 2 || got[0] != "Accept-Encoding" || got[1] != "Origin" {
		t.Errorf("Vary = %q", got)
	}
}
