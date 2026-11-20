package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// safeBuffer is an io.Writer the progress output and the test can share.
type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// nodeStripsTypes reports whether node runs TypeScript with type stripping
// (22.6 or later), which the stand-in server uses to run the generated entry.
func nodeStripsTypes() error {
	out, err := exec.Command("node", "--version").Output()
	if err != nil {
		return err
	}
	var major, minor int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "v%d.%d", &major, &minor); err != nil {
		return err
	}
	if major < 22 || (major == 22 && minor < 6) {
		return fmt.Errorf("node %s cannot strip types", strings.TrimSpace(string(out)))
	}
	return nil
}

const scriptcProbeRoute = `export function GET() {
  const via = typeof process !== 'undefined' && process.env.KRATE_API_PORT !== undefined ? 'scriptc' : 'other';
  return Response.json({ via });
}
export async function POST(request: Request) {
  const body = await request.text();
  return new Response(body.toUpperCase(), { status: 201, headers: { 'content-type': 'text/plain' } });
}`

// scriptcServeProject is a project whose routes split three ways with
// apiRuntime "scriptc": ok, users/[id] and sse compile, blocked (which holds
// blockerLine) falls back to the embedded runtime, and ext needs Node.
func scriptcServeProject(t *testing.T, blockerLine string) (*Builder, string) {
	t.Helper()
	files := map[string]string{
		"package.json":                        `{"type":"module"}`,
		"src/pages/index.tsx":                 `export default function Home() { return <main><h1>home</h1></main>; }`,
		"src/api/ok.ts":                       scriptcProbeRoute,
		"src/api/users/[id].ts":               "export function GET(request: Request, { params }: { params: { id: string } }) { return Response.json({ id: params.id }); }",
		"src/api/blog/[...slug].ts":           "export function GET(request: Request, { params }: { params: { slug: string } }) { return Response.json({ slug: params.slug }); }",
		"src/api/sse.ts":                      "export function GET() {\n  const enc = new TextEncoder();\n  const stream = new ReadableStream<Uint8Array>({\n    async start(c) {\n      for (let i = 0; i < 3; i++) { c.enqueue(enc.encode('data: ' + i + '\\n\\n')); await new Promise<void>((r) => setTimeout(r, 300)); }\n      c.close();\n    },\n  });\n  return new Response(stream, { headers: { 'content-type': 'text/event-stream' } });\n}",
		"src/api/boom.ts":                     "export function GET(): Response { throw new Error('secret detail'); }",
		"src/api/blocked.ts":                  "export function GET() {\n  " + blockerLine + "\n  return Response.json({ via: typeof process !== 'undefined' && process.env.KRATE_API_PORT !== undefined ? 'scriptc' : 'other' });\n}",
		"src/api/ext.ts":                      "import { tag } from \"@fake/ext\";\nexport function GET() { return Response.json({ tag }); }",
		"node_modules/@fake/ext/package.json": `{"name":"@fake/ext","version":"1.0.0","main":"index.js"}`,
		"node_modules/@fake/ext/index.js":     `exports.tag = "ext";`,
	}
	b, root := scProject(t, files)
	b.Cfg.SSR.APIRuntime = "scriptc"
	b.Cfg.ServerExternalPackages = []string{"@fake/ext"}
	if err := linkRuntimePackage(t, root+"/node_modules/@krate"); err != nil {
		t.Fatal(err)
	}
	return b, root
}

// checkScriptcServe builds the project and serves it, then checks that each
// route runs where the manifest says and answers as the Node sidecar does.
func checkScriptcServe(t *testing.T, b *Builder, root string) {
	t.Helper()
	var out safeBuffer
	SetOutput(&out)
	t.Cleanup(func() { SetOutput(os.Stdout) })
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	m := readScriptcManifest(t, root)
	for file, rt := range map[string]string{
		"ok.js": scriptcRouteScriptc, "users/[id].js": scriptcRouteScriptc, "blog/[...slug].js": scriptcRouteScriptc, "sse.js": scriptcRouteScriptc,
		"boom.js": scriptcRouteScriptc, "blocked.js": scriptcRouteQuickJS, "ext.js": scriptcRouteNode,
	} {
		if m[file].Runtime != rt {
			t.Fatalf("%s: manifest says %q, want %q", file, m[file].Runtime, rt)
		}
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "scriptc API server (5 routes)") {
		t.Fatalf("serve did not start the scriptc server:\n%s", out.String())
	}
	client := &http.Client{Timeout: 30 * time.Second}
	get := func(method, path string, body string) (*http.Response, []byte) {
		t.Helper()
		req, _ := http.NewRequest(method, base+path, strings.NewReader(body))
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp, data
	}
	via := func(path string) string {
		t.Helper()
		resp, data := get("GET", path, "")
		var v struct{ Via string }
		if resp.StatusCode != 200 || json.Unmarshal(data, &v) != nil {
			t.Fatalf("GET %s: %d %s", path, resp.StatusCode, data)
		}
		return v.Via
	}

	if got := via("/api/ok"); got != "scriptc" {
		t.Errorf("/api/ok ran on %q, want the scriptc server", got)
	}
	if got := via("/api/blocked"); got != "other" {
		t.Errorf("/api/blocked ran on %q, want the embedded runtime", got)
	}
	resp, data := get("GET", "/api/ext", "")
	if resp.StatusCode != 200 || !strings.Contains(string(data), `"tag":"ext"`) {
		t.Errorf("the Node-only route: %d %s", resp.StatusCode, data)
	}

	// A complete body has a Content-Length.
	resp, _ = get("GET", "/api/ok", "")
	if resp.ContentLength < 0 {
		t.Errorf("a complete body must carry a Content-Length, got %d", resp.ContentLength)
	}
	// Params, request bodies and statuses.
	if _, data := get("GET", "/api/users/42", ""); string(data) != `{"id":"42"}` {
		t.Errorf("/api/users/42 = %s", data)
	}
	if _, data := get("GET", "/api/blog/a/b%20c", ""); string(data) != `{"slug":"a/b c"}` {
		t.Errorf("/api/blog/a/b%%20c = %s", data)
	}
	resp, data = get("POST", "/api/ok", "hello")
	if resp.StatusCode != 201 || string(data) != "HELLO" || resp.Header.Get("Content-Type") != "text/plain" {
		t.Errorf("POST /api/ok = %d %q %q", resp.StatusCode, data, resp.Header.Get("Content-Type"))
	}
	resp, _ = get("DELETE", "/api/ok", "")
	if resp.StatusCode != 405 || resp.Header.Get("Allow") != "GET, HEAD, POST, OPTIONS" {
		t.Errorf("DELETE /api/ok = %d Allow %q", resp.StatusCode, resp.Header.Get("Allow"))
	}
	resp, _ = get("OPTIONS", "/api/ok", "")
	if resp.StatusCode != 204 || resp.Header.Get("Allow") == "" {
		t.Errorf("OPTIONS /api/ok = %d Allow %q", resp.StatusCode, resp.Header.Get("Allow"))
	}
	resp, data = get("HEAD", "/api/ok", "")
	if resp.StatusCode != 200 || len(data) != 0 {
		t.Errorf("HEAD /api/ok = %d with %d body bytes", resp.StatusCode, len(data))
	}
	// Errors answer 500 without the detail; unknown routes 404.
	resp, data = get("GET", "/api/boom", "")
	if resp.StatusCode != 500 || strings.Contains(string(data), "secret detail") {
		t.Errorf("/api/boom = %d %s", resp.StatusCode, data)
	}
	if resp, _ = get("GET", "/api/nothing", ""); resp.StatusCode != 404 {
		t.Errorf("/api/nothing = %d, want 404", resp.StatusCode)
	}
	// A streamed body arrives in pieces.
	start := time.Now()
	req, _ := http.NewRequest("GET", base+"/api/sse", nil)
	sresp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer sresp.Body.Close()
	buf := make([]byte, 64)
	n, _ := sresp.Body.Read(buf)
	if first := time.Since(start); first > 800*time.Millisecond || !strings.HasPrefix(string(buf[:n]), "data: 0") {
		t.Errorf("first chunk %q after %s: the stream was buffered", buf[:n], first)
	}
	rest, _ := io.ReadAll(sresp.Body)
	if !strings.Contains(string(rest), "data: 2") {
		t.Errorf("rest of the stream = %q", rest)
	}

	// The server answers only the host: a direct connection has no secret.
	mm := regexp.MustCompile(`scriptc API server \(\d+ routes\) -> .*http://localhost:(\d+)`).FindStringSubmatch(out.String())
	if mm == nil {
		t.Fatalf("no scriptc port in the output:\n%s", out.String())
	}
	port, _ := strconv.Atoi(mm[1])
	direct, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/ok", port))
	if err != nil {
		t.Fatal(err)
	}
	direct.Body.Close()
	if direct.StatusCode != 403 {
		t.Errorf("direct request to the scriptc server = %d, want 403", direct.StatusCode)
	}
}

// With a stand-in scriptc (the test binary), build and serve run end to end
// without scriptc installed: the generated entry runs under node.
func TestScriptcBuildAndServeWithStandIn(t *testing.T) {
	e2eEnv(t)
	if err := nodeStripsTypes(); err != nil {
		requireE2E(t, "%v", err)
		return
	}
	fakeScriptcOnPath(t)
	b, root := scriptcServeProject(t, "const x = 1; // scriptc-blocker")
	checkScriptcServe(t, b, root)
}

// The same run with the real scriptc. It is opt-in (KRATE_SCRIPTC=1) because
// scriptc is experimental and not installed in CI, so it is a plain skip and
// not an environment failure under KRATE_REQUIRE_E2E.
func TestScriptcBuildAndServeReal(t *testing.T) {
	if os.Getenv("KRATE_SCRIPTC") != "1" {
		t.Skip("set KRATE_SCRIPTC=1 to build with the real scriptc (needs scriptc, Node 24+ and clang or zig)")
	}
	e2eEnv(t)
	b, root := scriptcServeProject(t, "const f = new Function('return 1'); f();")
	checkScriptcServe(t, b, root)
}
