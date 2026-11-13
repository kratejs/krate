package build

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const w5SlowPage = `import { Suspense } from "react";
async function Slow() {
  await new Promise((r) => setTimeout(r, 1500));
  return <p id="slow">slow-done</p>;
}
export default function Page() {
  return <main><h1>Streaming</h1><Suspense fallback={<p>loading-fallback</p>}><Slow /></Suspense></main>;
}`

// w5Project builds a project that exercises the API runtime, middleware and
// streaming together, and returns its root and builder.
func w5Project(t *testing.T) (*Builder, string) {
	t.Helper()
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx":  `export default function Home() { return <main><h1>home</h1></main>; }`,
		"src/pages/stream.tsx": w5SlowPage,
		"src/api/ok.ts": `export async function GET() { return Response.json({ ok: true }); }
export async function POST(req: Request) { return Response.json({ len: (await req.text()).length }); }
export async function PUT(req: Request) { return Response.json({ len: (await req.arrayBuffer()).byteLength }); }`,
		"src/api/cookies.ts": `export async function GET() {
  const h = new Headers(); h.append("Set-Cookie", "a=1; Path=/"); h.append("Set-Cookie", "b=2; Path=/");
  return new Response("c", { headers: h });
}`,
		"src/api/throws.ts": `export async function GET() { throw new Error("secret detail"); }`,
		"src/api/slow.ts": `export async function GET() {
  await new Promise((r) => setTimeout(r, 2500));
  return new Response("finished");
}`,
		"src/api/sse.ts": `export async function GET() {
  const enc = new TextEncoder();
  const stream = new ReadableStream({
    async start(c) {
      for (let i = 0; i < 3; i++) { c.enqueue(enc.encode("data: " + i + "\n\n")); await new Promise((r) => setTimeout(r, 400)); }
      c.close();
    },
  });
  return new Response(stream, { headers: { "content-type": "text/event-stream" } });
}`,
		"src/api/echohost.ts": `export async function GET(req: Request) { return Response.json({ url: req.url, host: req.headers.get("host") }); }`,
		"middleware.ts": `export function middleware(request: Request) {
  const url = new URL(request.url);
  if (url.pathname === "/admin") return Response.redirect(new URL("/login", request.url));
  if (url.pathname === "/teapot") return new Response("teapot", { status: 200 });
  return new Response(null, { headers: { "x-middleware-next": "1", "x-mw": "ran" } });
}
export const config = { matcher: ["/admin", "/teapot", "/api/:path*", "/stream", "/"] };`,
	})
	return b, root
}

func e2eEnv(t *testing.T) {
	t.Helper()
	if !canStopInProcessServer {
		t.Skip("in-process server needs a unix host")
	}
	ensureRuntimeDist(t)
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
		t.Skip("node is not installed")
	}
}

func buildW5(t *testing.T) (*Builder, string) {
	t.Helper()
	b, root := w5Project(t)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	return b, root
}

func TestServeW5Behaviors(t *testing.T) {
	e2eEnv(t)
	b, root := buildW5(t)
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	do := func(method, path string, body []byte, host string) (*http.Response, []byte) {
		t.Helper()
		req, _ := http.NewRequest(method, base+path, bytes.NewReader(body))
		if host != "" {
			req.Host = host
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp, data
	}

	t.Run("streaming shell and fallback arrive before the slow region", func(t *testing.T) {
		start := time.Now()
		resp, err := client.Get(base + "/stream/")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		buf := make([]byte, 32*1024)
		n, _ := resp.Body.Read(buf)
		ttfb := time.Since(start)
		first := string(buf[:n])
		if ttfb > 1200*time.Millisecond {
			t.Errorf("first bytes took %s; the shell must not wait for the 1.5s region", ttfb)
		}
		if !strings.Contains(first, "loading-fallback") {
			t.Errorf("first chunk lacks the Suspense fallback:\n%s", first)
		}
		rest, _ := io.ReadAll(resp.Body)
		all := first + string(rest)
		if !strings.Contains(all, "slow-done") {
			t.Errorf("the resolved region never arrived:\n%s", all)
		}
		if time.Since(start) < 1400*time.Millisecond {
			t.Errorf("finished in %s, before the region could have resolved", time.Since(start))
		}
	})

	t.Run("multiple Set-Cookie headers survive", func(t *testing.T) {
		resp, _ := do("GET", "/api/cookies", nil, "")
		if got := resp.Header.Values("Set-Cookie"); len(got) != 2 {
			t.Fatalf("Set-Cookie = %v, want two headers", got)
		}
	})

	t.Run("method handling", func(t *testing.T) {
		resp, _ := do("DELETE", "/api/ok", nil, "")
		if resp.StatusCode != 405 || resp.Header.Get("Allow") == "" {
			t.Errorf("DELETE: %d allow=%q", resp.StatusCode, resp.Header.Get("Allow"))
		}
		resp, body := do("HEAD", "/api/ok", nil, "")
		if resp.StatusCode != 200 || len(body) != 0 {
			t.Errorf("HEAD: %d body=%q", resp.StatusCode, body)
		}
		resp, _ = do("OPTIONS", "/api/ok", nil, "")
		if resp.StatusCode != 204 || !strings.Contains(resp.Header.Get("Allow"), "POST") {
			t.Errorf("OPTIONS: %d allow=%q", resp.StatusCode, resp.Header.Get("Allow"))
		}
	})

	t.Run("production error body is generic", func(t *testing.T) {
		resp, body := do("GET", "/api/throws", nil, "")
		if resp.StatusCode != 500 || strings.TrimSpace(string(body)) != `{"error":"Internal Server Error"}` {
			t.Errorf("throws: %d %q", resp.StatusCode, body)
		}
	})

	t.Run("20 MB body", func(t *testing.T) {
		payload := bytes.Repeat([]byte("abcd"), 5<<20)
		resp, body := do("POST", "/api/ok", payload, "")
		if resp.StatusCode != 200 || !strings.Contains(string(body), fmt.Sprintf(`"len":%d`, len(payload))) {
			t.Errorf("big text body: %d %.200q", resp.StatusCode, body)
		}
		small := bytes.Repeat([]byte{0x00, 0xff, 0xfe, 0x80}, 50000)
		resp, body = do("PUT", "/api/ok", small, "")
		if resp.StatusCode != 200 || !strings.Contains(string(body), fmt.Sprintf(`"len":%d`, len(small))) {
			t.Errorf("binary body: %d %.200q", resp.StatusCode, body)
		}
	})

	t.Run("SSE chunks arrive as produced", func(t *testing.T) {
		start := time.Now()
		resp, err := client.Get(base + "/api/sse")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		buf := make([]byte, 256)
		n, _ := resp.Body.Read(buf)
		if time.Since(start) > 700*time.Millisecond || !strings.HasPrefix(string(buf[:n]), "data: 0") {
			t.Errorf("first SSE event after %s: %q", time.Since(start), buf[:n])
		}
		rest, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(buf[:n])+string(rest), "data: 2") {
			t.Errorf("stream truncated: %q", rest)
		}
	})

	t.Run("middleware sees the real host", func(t *testing.T) {
		resp, _ := do("GET", "/admin", nil, "blog.example.com")
		if resp.StatusCode != 302 || resp.Header.Get("Location") != "http://blog.example.com/login" {
			t.Errorf("redirect: %d Location=%q", resp.StatusCode, resp.Header.Get("Location"))
		}
		_, body := do("GET", "/api/echohost", nil, "blog.example.com")
		if !strings.Contains(string(body), `"url":"http://blog.example.com/api/echohost"`) || !strings.Contains(string(body), `"host":"blog.example.com"`) {
			t.Errorf("api url/host: %s", body)
		}
	})

	t.Run("middleware 200 response is honored", func(t *testing.T) {
		resp, body := do("GET", "/teapot", nil, "")
		if resp.StatusCode != 200 || string(body) != "teapot" {
			t.Errorf("200 response: %d %q", resp.StatusCode, body)
		}
	})

	t.Run("middleware runs for API routes and passthrough adds headers", func(t *testing.T) {
		resp, _ := do("GET", "/api/ok", nil, "")
		if resp.Header.Get("x-mw") != "ran" || resp.Header.Get("x-middleware-next") != "" {
			t.Errorf("api headers: x-mw=%q x-middleware-next=%q", resp.Header.Get("x-mw"), resp.Header.Get("x-middleware-next"))
		}
		resp, _ = do("GET", "/", nil, "")
		if resp.Header.Get("x-mw") != "ran" {
			t.Errorf("page passthrough header missing")
		}
	})
}
