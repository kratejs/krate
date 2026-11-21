package build

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOutIndexProdVersusDev(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"index.html":       "<h1>home</h1>",
		"about/index.html": "about",
		"app.abc123.js":    "js",
		"img/logo.png":     "png",
	})
	// Dev: no index registered, the file system answers.
	if staticRouteExists(dir, "/later") {
		t.Fatal("unexpected route")
	}
	writeTree(t, dir, map[string]string{"dev.html": "x"})
	if !staticRouteExists(dir, "/dev.html") {
		t.Fatal("dev must see files written after startup")
	}

	unregister := registerOutIndex(dir)
	defer unregister()
	for _, p := range []string{"/", "/index.html", "/about", "/about/", "/app.abc123.js", "/img/logo.png", "/dev.html"} {
		if !staticRouteExists(dir, p) {
			t.Errorf("staticRouteExists(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"/missing", "/img", "/../etc/passwd", "/about/x"} {
		if staticRouteExists(dir, p) {
			t.Errorf("staticRouteExists(%q) = true, want false", p)
		}
	}
	if !isBakedAsset(dir, "/app.abc123.js") || isBakedAsset(dir, "/about/") || isBakedAsset(dir, "/nope.js") {
		t.Error("isBakedAsset disagrees with the output")
	}
	if !staticDirExists(dir, "/img") || staticDirExists(dir, "/img/logo.png") {
		t.Error("staticDirExists disagrees with the output")
	}
	if !shouldServeStatic(dir, "/about/", nil) {
		t.Error("shouldServeStatic must serve a baked page")
	}

	// A file written after startup is invisible to the index: production does
	// not rescan, runtime writers only touch .krate.
	writeTree(t, dir, map[string]string{"late.html": "x"})
	if staticRouteExists(dir, "/late.html") {
		t.Error("index must be a startup snapshot")
	}
	unregister()
	if !staticRouteExists(dir, "/late.html") {
		t.Error("after unregistering, the file system answers again")
	}
}

func TestOutIndexISRWritesAndManifests(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"index.html":             "home",
		".krate/manifest.json":   `{"pages":[]}`,
		".krate/isr-cache.json":  "{}",
		"server-manifest.json":   "{}",
		".krate/data/secret.txt": "s",
	})
	defer registerOutIndex(dir)()
	fsys := newStaticFS(dir)

	// The ISR persistence file is rewritten while serving; it must stay
	// unreachable and must not disturb lookups.
	writeTree(t, dir, map[string]string{".krate/isr-cache.json": `{"a":1}`, ".krate/new-isr.json": "{}"})
	for _, name := range []string{"/.krate/isr-cache.json", "/.krate/manifest.json", "/.krate/data/secret.txt", "/.krate/new-isr.json"} {
		if _, err := fsys.Open(name); err == nil {
			t.Errorf("Open(%q) succeeded; the server-only dir must stay hidden", name)
		}
	}
	if got := ResolveManifestPath(dir, "manifest.json"); got != filepath.Join(dir, ".krate", "manifest.json") {
		t.Errorf("manifest = %s", got)
	}
	// Legacy location is found from the index; a name written later falls back
	// to the file system.
	if got := ResolveManifestPath(dir, "server-manifest.json"); got != filepath.Join(dir, "server-manifest.json") {
		t.Errorf("legacy manifest = %s", got)
	}
	writeTree(t, dir, map[string]string{".krate/workers.json": "[]"})
	if got := ResolveManifestPath(dir, "workers.json"); got != filepath.Join(dir, ".krate", "workers.json") {
		t.Errorf("late manifest = %s", got)
	}
}

// The indexed file system must refuse exactly what hardenedDir refuses.
func TestIndexedDirHardeningMatchesHardenedDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"index.html":            "home",
		"a.txt":                 "a",
		".env":                  "SECRET=1",
		".git/config":           "x",
		".well-known/ok.txt":    "ok",
		"sub/.hidden":           "h",
		"sub/index.html":        "sub",
		"empty/readme.txt":      "no index here",
		".krate/manifest.json":  "{}",
		"api-node/x.mjs":        "x",
		"server-manifest.json":  "{}",
		"blog/__404/index.html": "nf",
		"blog/__404.html":       "nf",
	})
	defer registerOutIndex(dir)()
	hd := hardenedDir{http.Dir(dir)}
	id := indexedDir{lookupOutIndex(dir)}
	names := []string{
		"/", "/index.html", "/a.txt", "/.env", "/.git/config", "/.well-known/ok.txt", "/sub", "/sub/", "/sub/.hidden",
		"/empty", "/empty/", "/empty/readme.txt", "/.krate/manifest.json", "/.KRATE/manifest.json", "/api-node/x.mjs",
		"/server-manifest.json", "/blog/__404/index.html", "/blog/__404", "/../a.txt", "/sub/../.env", "/sub/%2e%2e/.env",
		"/%2e/env", "/missing", "//a.txt", "/sub\\.hidden", "/.krate", "/a.txt/",
	}
	for _, n := range names {
		f1, e1 := hd.Open(n)
		f2, e2 := id.Open(n)
		if (e1 == nil) != (e2 == nil) {
			t.Errorf("Open(%q): hardenedDir err=%v, indexedDir err=%v", n, e1, e2)
		}
		// Windows cannot remove the temp dir while a file in it is open.
		for _, f := range []http.File{f1, f2} {
			if f != nil {
				f.Close()
			}
		}
	}
}

func TestIndexedStaticServing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body := strings.Repeat("0123456789", 100)
	writeTree(t, dir, map[string]string{"index.html": "<h1>home</h1>", "data.txt": body, "page/index.html": "page"})
	big := strings.Repeat("x", outIndexMaxFile+10)
	writeTree(t, dir, map[string]string{"big.bin": big})
	defer registerOutIndex(dir)()
	srv := httptest.NewServer(http.FileServer(newStaticFS(dir)))
	defer srv.Close()

	get := func(path string, hdr map[string]string, method string) *http.Response {
		req, _ := http.NewRequest(method, srv.URL+path, nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	resp := get("/data.txt", nil, "GET")
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(b) != body || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") {
		t.Fatalf("GET = %d %q %q", resp.StatusCode, resp.Header.Get("Content-Type"), b[:10])
	}
	lm := resp.Header.Get("Last-Modified")
	if lm == "" {
		t.Fatal("Last-Modified missing")
	}

	// Conditional request.
	resp = get("/data.txt", map[string]string{"If-Modified-Since": lm}, "GET")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotModified {
		t.Errorf("conditional GET = %d, want 304", resp.StatusCode)
	}
	// Range.
	resp = get("/data.txt", map[string]string{"Range": "bytes=10-19"}, "GET")
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || string(b) != "0123456789" || resp.Header.Get("Content-Range") != "bytes 10-19/1000" {
		t.Errorf("range = %d %q %q", resp.StatusCode, b, resp.Header.Get("Content-Range"))
	}
	// HEAD.
	resp = get("/data.txt", nil, "HEAD")
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || len(b) != 0 || resp.Header.Get("Content-Length") != "1000" {
		t.Errorf("HEAD = %d len=%d cl=%s", resp.StatusCode, len(b), resp.Header.Get("Content-Length"))
	}
	// Folder index, and the redirect for a folder without a slash.
	resp = get("/page/", nil, "GET")
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "page" || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Errorf("folder index = %q %q", resp.Header.Get("Content-Type"), b)
	}
	resp = get("/page", nil, "GET")
	resp.Body.Close()
	if resp.StatusCode != http.StatusMovedPermanently {
		t.Errorf("folder without slash = %d, want 301", resp.StatusCode)
	}
	// Large files come from disk with the same semantics.
	resp = get("/big.bin", map[string]string{"Range": "bytes=0-3"}, "GET")
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || string(b) != "xxxx" {
		t.Errorf("big range = %d %q", resp.StatusCode, b)
	}
	if lookupOutIndex(dir).entries["big.bin"].data.Load() != nil {
		t.Error("a file over the cap must not be held in memory")
	}
	if lookupOutIndex(dir).entries["data.txt"].data.Load() == nil {
		t.Error("a small file should be held in memory after its first read")
	}
	resp = get("/nope", nil, "GET")
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("missing = %d", resp.StatusCode)
	}
}

func TestIndexedPrecompressed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"app.js": "plain", "app.js.br": "BROTLI", "app.js.gz": "GZ", "only.css": "c", "only.css.gz": "GZCSS",
		"docs/index.html": "d", "docs/index.html.gz": "GZDOC",
	})
	defer registerOutIndex(dir)()
	var fellThrough bool
	h := precompressedMiddlewareIf(dir, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fellThrough = true
		_, _ = io.WriteString(w, "next")
	}))
	do := func(path, ae string, hdr ...string) (*httptest.ResponseRecorder, string) {
		fellThrough = false
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Accept-Encoding", ae)
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec, rec.Body.String()
	}
	rec, body := do("/app.js", "br, gzip")
	if rec.Header().Get("Content-Encoding") != "br" || body != "BROTLI" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
		t.Errorf("br: %v %q", rec.Header(), body)
	}
	rec, body = do("/app.js", "gzip")
	if rec.Header().Get("Content-Encoding") != "gzip" || body != "GZ" {
		t.Errorf("gz: %v %q", rec.Header(), body)
	}
	rec, body = do("/app.js", "gzip", "Range", "bytes=0-0")
	if rec.Code != http.StatusPartialContent || body != "G" {
		t.Errorf("range on sibling = %d %q", rec.Code, body)
	}
	if _, body = do("/app.js", "identity"); !fellThrough {
		t.Errorf("no accepted coding must fall through, got %q", body)
	}
	if _, body = do("/only.css", "br"); !fellThrough {
		t.Errorf("missing .br must fall through, got %q", body)
	}
	if rec, body = do("/docs/", "gzip"); body != "GZDOC" || rec.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("folder sibling: %q", body)
	}
	if _, body = do("/docs", "gzip"); !fellThrough {
		t.Errorf("folder without slash must fall through, got %q", body)
	}
	if _, body = do("/.krate/manifest.json", "gzip"); !fellThrough {
		t.Errorf("internal path must fall through, got %q", body)
	}
}

func TestMiddlewareGateFileStatOnce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mw := filepath.Join(dir, "middleware.js")
	g := &middlewareGate{file: mw}
	if g.fileExists() {
		t.Fatal("missing file reported present")
	}
	writeTree(t, dir, map[string]string{"middleware.js": "x"})
	if g.fileExists() {
		t.Error("outside dev the answer is read once")
	}
	gd := &middlewareGate{file: mw, dev: true}
	if !gd.fileExists() {
		t.Error("dev must stat every time")
	}
}

// A response with no length that trickles must reach the client piece by piece
// through the logging, gzip and deadline writers.
func TestProxyStreamsIncrementally(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "first\n")
		w.(http.Flusher).Flush()
		<-release
		_, _ = io.WriteString(w, "second\n")
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	proxy := httputil.NewSingleHostReverseProxy(u)
	front := httptest.NewServer(loggingMiddleware(gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(newDeadlineWriter(w), r)
	}))))
	defer front.Close()

	req, _ := http.NewRequest("GET", front.URL+"/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	zr, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	line := make(chan string, 1)
	go func() {
		s, _ := bufio.NewReader(zr).ReadString('\n')
		line <- s
	}()
	select {
	case s := <-line:
		if s != "first\n" {
			t.Fatalf("first chunk = %q", s)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first chunk was held back while the response was still open")
	}
	close(release)
}

func startNodeSidecar(t *testing.T, routes map[string]string) (addr string) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node not available: %v", err)
		t.SkipNow()
	}
	dir := t.TempDir()
	apiDir := filepath.Join(dir, "api")
	for name, src := range routes {
		writeTree(t, apiDir, map[string]string{name: src})
	}
	writeTree(t, dir, map[string]string{"package.json": `{"type":"module"}`, "sidecar.mjs": apiServerScriptContent})
	port := freePort(t)
	cmd := exec.Command("node", filepath.Join(dir, "sidecar.mjs"), apiDir, fmt.Sprint(port), "", filepath.Join(dir, "actions"))
	cmd.Env = append(os.Environ(), "KRATE_INTERNAL_SECRET=s3cret")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	addr = fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(15 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			c.Close()
			return addr
		}
		if time.Now().After(deadline) {
			t.Fatal("sidecar did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A complete response goes out with a Content-Length; a body still being
// produced (any content type) keeps streaming.
func TestNodeSidecarLengthAndStreaming(t *testing.T) {
	t.Parallel()
	addr := startNodeSidecar(t, map[string]string{
		"json.js": `export function GET() { return Response.json({ ok: true, n: 42 }); }`,
		"text.js": `export function GET() { return new Response("hello"); }`,
		"stream.js": `export function GET() {
  const enc = new TextEncoder();
  const s = new ReadableStream({
    async start(c) {
      c.enqueue(enc.encode("one\n"));
      await new Promise((r) => setTimeout(r, 2500));
      c.enqueue(enc.encode("two\n"));
      c.close();
    },
  });
  return new Response(s, { headers: { "content-type": "text/plain" } });
}`,
		"sse.js": `export function GET() {
  const enc = new TextEncoder();
  const s = new ReadableStream({
    async start(c) {
      c.enqueue(enc.encode("data: 1\n\n"));
      await new Promise((r) => setTimeout(r, 2500));
      c.enqueue(enc.encode("data: 2\n\n"));
      c.close();
    },
  });
  return new Response(s, { headers: { "content-type": "text/event-stream" } });
}`,
	})
	get := func(path string) *http.Response {
		req, _ := http.NewRequest("GET", "http://"+addr+path, nil)
		req.Header.Set(internalSecretHeader, "s3cret")
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	for path, want := range map[string]string{"/api/json": `{"ok":true,"n":42}`, "/api/text": "hello"} {
		resp := get(path)
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(b) != want || resp.ContentLength != int64(len(want)) || len(resp.TransferEncoding) != 0 {
			t.Errorf("%s: body %q, content-length %d, transfer-encoding %v", path, b, resp.ContentLength, resp.TransferEncoding)
		}
	}
	for path, first := range map[string]string{"/api/stream": "one\n", "/api/sse": "data: 1\n"} {
		start := time.Now()
		resp := get(path)
		line, err := bufio.NewReader(resp.Body).ReadString('\n')
		took := time.Since(start)
		resp.Body.Close()
		if err != nil || line != first {
			t.Errorf("%s first line = %q, %v", path, line, err)
		}
		if took > 1500*time.Millisecond {
			t.Errorf("%s first chunk took %s; it must not wait for the rest of the body", path, took)
		}
		if resp.ContentLength != -1 {
			t.Errorf("%s has a Content-Length of %d for a streamed body", path, resp.ContentLength)
		}
	}
}
