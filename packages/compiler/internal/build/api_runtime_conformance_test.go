package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
)

type confReq struct {
	method  string
	query   string
	headers map[string]string
	body    []byte
}

type confCase struct {
	name  string
	route string
	reqs  []confReq
}

type confResp struct {
	status  int
	headers map[string]string
	cookies []string
	body    []byte
}

func doConf(t *testing.T, base, name string, rq confReq) confResp {
	t.Helper()
	method := rq.method
	if method == "" {
		method = "GET"
	}
	var rd io.Reader
	if rq.body != nil {
		rd = bytes.NewReader(rq.body)
	}
	req, err := http.NewRequest(method, base+"/api/"+name+rq.query, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "blog.example.com"
	for k, v := range rq.headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport:     &http.Transport{DisableCompression: true},
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, name, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	out := confResp{status: resp.StatusCode, headers: map[string]string{}, cookies: resp.Header.Values("Set-Cookie"), body: body}
	for _, h := range []string{"Content-Type", "Location", "Allow", "X-Custom"} {
		if v := resp.Header.Get(h); v != "" {
			if h == "Content-Type" {
				// Runtimes differ in charset spelling/casing; compare the type.
				v = strings.ToLower(strings.ReplaceAll(strings.TrimSuffix(strings.ToLower(v), ";charset=utf-8"), " ", ""))
			}
			out.headers[h] = v
		}
	}
	return out
}

func startNodeConformance(t *testing.T, apiDir, middlewarePath string) string {
	t.Helper()
	return startSidecarConformance(t, "node", apiDir, middlewarePath)
}

// startSidecarConformance starts the real API sidecar script on node or bun.
func startSidecarConformance(t *testing.T, runtime, apiDir, middlewarePath string) string {
	t.Helper()
	return startSidecarConformanceEnv(t, runtime, apiDir, middlewarePath)
}

// startSidecarConformanceEnv is startSidecarConformance with extra environment
// (for example KRATE_API_HOT=1).
func startSidecarConformanceEnv(t *testing.T, runtime, apiDir, middlewarePath string, env ...string) string {
	t.Helper()
	if _, err := exec.LookPath(runtime); err != nil {
		if runtime == "node" {
			requireE2E(t, "node is not installed")
		}
		t.Skipf("%s is not installed", runtime)
	}
	dir := t.TempDir()
	port := freePort(t)
	scriptPath := filepath.Join(dir, "sidecar.mjs")
	script := apiServerScriptContent
	args := []string{scriptPath, apiDir, fmt.Sprint(port), middlewarePath, filepath.Join(dir, "actions")}
	if runtime == "bun" {
		script = apiServerBunScriptContent
		args = append([]string{"run"}, args...)
	}
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(runtime, args...)
	cmd.Env = append(append(os.Environ(), "KRATE_API_WORKERS=1"), env...)
	var logs syncBuffer
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		if t.Failed() {
			t.Logf("%s sidecar output:\n%s", runtime, logs.String())
		}
	})
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(15 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s sidecar did not start", runtime)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return "http://" + addr
}

func multipartBody(t *testing.T) ([]byte, string) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("title", "café")
	fw, err := mw.CreateFormFile("file", "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write([]byte("file-bytes"))
	_ = mw.Close()
	return buf.Bytes(), mw.FormDataContentType()
}

// TestAPIRuntimeConformance runs the same route code under the embedded
// QuickJS runtime and the Node sidecar and requires identical responses.
func TestAPIRuntimeConformance(t *testing.T) {
	mpBody, mpType := multipartBody(t)
	cases := []confCase{
		{name: "headers", route: `export async function GET(request) {
  return Response.json({ auth: request.headers.get("authorization"), host: request.headers.get("host"), has: request.headers.has("AUTHORIZATION"), cookie: request.cookies.get("sid")?.value ?? null });
}`, reqs: []confReq{{headers: map[string]string{"Authorization": "Bearer t", "Cookie": "sid=9; x=1"}}}},
		{name: "jsonbody", route: `export async function POST(request) {
  const body = await request.json().catch(() => null);
  if (!body) return new Response(JSON.stringify({ error: "bad json" }), { status: 400, headers: { "content-type": "application/json" } });
  return Response.json({ got: body });
}`, reqs: []confReq{
			{method: "POST", body: []byte(`{"a":[1,2]}`), headers: map[string]string{"Content-Type": "application/json"}},
			{method: "POST", body: []byte(`not json`)},
		}},
		{name: "text", route: `export async function POST(request) { return new Response((await request.text()).toUpperCase(), { headers: { "x-custom": "yes" } }); }`,
			reqs: []confReq{{method: "POST", body: []byte("héllo \U0001F600")}}},
		{name: "formurl", route: `export async function POST(request) {
  const f = await request.formData();
  return Response.json({ a: f.getAll("a"), b: f.get("b") });
}`, reqs: []confReq{{method: "POST", body: []byte("a=1&b=x+y&a=%E2%82%AC"), headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"}}}},
		{name: "formmulti", route: `export async function POST(request) {
  const f = await request.formData();
  const file = f.get("file");
  return Response.json({ title: f.get("title"), name: file.name, text: await file.text(), size: file.size });
}`, reqs: []confReq{{method: "POST", body: mpBody, headers: map[string]string{"Content-Type": mpType}}}},
		{name: "bytes", route: `export async function POST(request) {
  const buf = new Uint8Array(await request.arrayBuffer());
  return Response.json({ len: buf.length, hex: Array.from(buf).map((b) => b.toString(16).padStart(2, "0")).join("") });
}`, reqs: []confReq{{method: "POST", body: []byte{0x00, 0xff, 0xfe, 0x80, 0x41}, headers: map[string]string{"Content-Type": "application/octet-stream"}}}},
		{name: "binaryresp", route: `export async function GET() { return new Response(new Uint8Array([0, 255, 254, 128, 65]), { headers: { "content-type": "application/octet-stream" } }); }`,
			reqs: []confReq{{}}},
		{name: "url", route: `export async function GET(request) {
  const u = new URL(request.url);
  let bad = "no";
  try { new URL("not a url"); } catch (e) { bad = e.name; }
  return Response.json({ origin: u.origin, host: u.host, path: u.pathname, e: u.searchParams.get("e"), p: u.searchParams.get("p"), all: u.searchParams.getAll("k"), bad });
}`, reqs: []confReq{{query: "?e=x=y&p=a+b&k=1&k=2"}}},
		{name: "timer", route: `export async function GET() {
  const t0 = Date.now();
  await new Promise((r) => setTimeout(r, 25));
  return Response.json({ waited: Date.now() - t0 >= 20 });
}`, reqs: []confReq{{}}},
		{name: "stream", route: `export async function GET() {
  const enc = new TextEncoder();
  const stream = new ReadableStream({
    async start(c) { for (let i = 0; i < 3; i++) { c.enqueue(enc.encode("chunk" + i + ";")); await new Promise((r) => setTimeout(r, 5)); } c.close(); },
  });
  return new Response(stream, { headers: { "content-type": "text/plain" } });
}`, reqs: []confReq{{}}},
		{name: "methods", route: `export async function GET() { return Response.json({ ok: true }); }
export async function POST() { return new Response(null, { status: 201 }); }`,
			reqs: []confReq{{method: "PUT"}, {method: "DELETE"}, {method: "HEAD"}, {method: "OPTIONS"}, {method: "POST"}}},
		{name: "cookies", route: `export async function GET() {
  const h = new Headers();
  h.append("Set-Cookie", "a=1; Path=/; HttpOnly");
  h.append("Set-Cookie", "b=2; Path=/");
  h.set("X-Custom", "v");
  return new Response("ok", { headers: h });
}`, reqs: []confReq{{}}},
		{name: "throws", route: `export async function GET() { throw new Error("secret internal detail"); }`, reqs: []confReq{{}}},
		{name: "nothing", route: `export async function GET() {}`, reqs: []confReq{{}}},
		{name: "plainobj", route: `export async function GET() { return { a: 1, b: [true, null] }; }`, reqs: []confReq{{}}},
		{name: "redirect", route: `export async function GET(request) { return Response.redirect(new URL("/login", request.url), 307); }`, reqs: []confReq{{}}},
		{name: "webapis", route: `export async function GET() {
  const d = new Uint8Array(await crypto.subtle.digest("SHA-256", new TextEncoder().encode("abc")));
  const key = await crypto.subtle.importKey("raw", new TextEncoder().encode("k"), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  const sig = new Uint8Array(await crypto.subtle.sign("HMAC", key, new TextEncoder().encode("m")));
  let order = "";
  queueMicrotask(() => { order += "m"; });
  order += "s";
  await Promise.resolve();
  const ac = new AbortController(); ac.abort();
  return Response.json({
    b64: btoa("hello") + atob("aGk="),
    uuid: /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(crypto.randomUUID()),
    rnd: crypto.getRandomValues(new Uint8Array(8)).some((x) => x !== 0),
    digest: Array.from(d).map((b) => b.toString(16).padStart(2, "0")).join(""),
    hmac: Array.from(sig).map((b) => b.toString(16).padStart(2, "0")).join(""),
    clone: structuredClone({ d: new Date(5), m: new Map([[1, 2]]) }).m.get(1),
    order, aborted: ac.signal.aborted, perf: typeof performance.now(),
    dec: new TextDecoder().decode(new Uint8Array([104, 195, 169])),
    hdrs: [...new Headers({ B: "2", a: "1" })],
    sp: new URLSearchParams({ q: "a b&c" }).toString(),
    blob: await new Blob(["ab", "cd"]).text(),
  });
}`, reqs: []confReq{{}}},
		{name: "params", route: `export async function GET(request, ctx) { return Response.json({ id: ctx.params.id }); }`, reqs: []confReq{{}}},
	}

	dir := t.TempDir()
	apiDir := filepath.Join(dir, "api")
	if err := os.MkdirAll(apiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(apiDir, "package.json"), []byte(`{"type":"module"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if err := os.WriteFile(filepath.Join(apiDir, c.name+".js"), []byte(c.route), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The embedded runtime serves the IIFE bundles the real build emits; the
	// sidecars import the ESM source directly.
	qjsDir := filepath.Join(dir, "qjs-api")
	if err := os.MkdirAll(qjsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		res := api.Build(api.BuildOptions{
			Stdin:      &api.StdinOptions{Contents: c.route, Loader: api.LoaderJS, ResolveDir: dir, Sourcefile: c.name + ".js"},
			Bundle:     true,
			Platform:   api.PlatformNode,
			Format:     api.FormatIIFE,
			GlobalName: "__krate_route",
			Outfile:    filepath.Join(qjsDir, c.name+".js"),
			Write:      true,
			LogLevel:   api.LogLevelSilent,
		})
		if len(res.Errors) > 0 {
			t.Fatalf("bundling %s: %s", c.name, res.Errors[0].Text)
		}
	}
	apiRT := jsruntime.NewAPIRouteRuntime(qjsDir)
	t.Cleanup(apiRT.Close)
	qjs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveQuickJSAPI(w, r, apiRT, false)
	}))
	t.Cleanup(qjs.Close)
	for _, runtime := range []string{"node", "bun"} {
		runtime := runtime
		t.Run(runtime, func(t *testing.T) {
			nodeBase := startSidecarConformance(t, runtime, apiDir, "")

			for _, c := range cases {
				for i, rq := range c.reqs {
					q := doConf(t, qjs.URL, c.name, rq)
					n := doConf(t, nodeBase, c.name, rq)
					label := fmt.Sprintf("%s[%d] %s", c.name, i, rq.method)
					if q.status != n.status {
						t.Errorf("%s: status quickjs=%d node=%d (qjs body %q, node body %q)", label, q.status, n.status, q.body, n.body)
						continue
					}
					if !bytes.Equal(q.body, n.body) {
						t.Errorf("%s: body differs\n quickjs: %q\n node:    %q", label, q.body, n.body)
					}
					for k, v := range n.headers {
						if q.headers[k] != v {
							t.Errorf("%s: header %s quickjs=%q node=%q", label, k, q.headers[k], v)
						}
					}
					if c.name == "cookies" && (len(n.cookies) != 2 || len(q.cookies) != 2) {
						t.Errorf("%s: want two Set-Cookie headers, quickjs=%v node=%v", label, q.cookies, n.cookies)
					}
					if strings.Join(q.cookies, "|") != strings.Join(n.cookies, "|") {
						t.Errorf("%s: set-cookie quickjs=%v node=%v", label, q.cookies, n.cookies)
					}
				}
			}
		})
	}
}

// TestNodeSidecarMiddlewareEnvelope runs the same middleware module through the
// embedded runtime and the Node sidecar endpoint and requires the same
// decisions: matcher scoping, pass-through headers with multiple cookies,
// rewrites and short-circuit responses (including an explicit 200).
func TestNodeSidecarMiddlewareEnvelope(t *testing.T) {
	code := `export function middleware(request) {
  const p = new URL(request.url).pathname;
  if (p === "/teapot") return new Response("teapot", { status: 200, headers: { "x-from": "mw" } });
  if (p === "/rewrite") return new Response(null, { headers: { "x-middleware-rewrite": "/other" } });
  const h = new Headers({ "x-middleware-next": "1" });
  h.append("set-cookie", "a=1"); h.append("set-cookie", "b=2");
  return new Response(null, { headers: h });
}
export const config = { matcher: ["/teapot", "/rewrite", "/pass/:path*"] };
`
	dir := t.TempDir()
	mwPath := filepath.Join(dir, "middleware.js")
	if err := os.WriteFile(mwPath, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	apiDir := filepath.Join(dir, "api")
	_ = os.MkdirAll(apiDir, 0o755)
	base := startNodeConformance(t, apiDir, mwPath)
	rt, err := jsruntime.NewMiddlewareRuntime(mwPath)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	type env struct {
		Action  string      `json:"action"`
		Status  int         `json:"status"`
		Headers [][2]string `json:"headers"`
		Body    string      `json:"body"`
		Rewrite string      `json:"rewrite"`
		Matcher bool        `json:"matcher"`
	}
	for _, path := range []string{"/teapot", "/rewrite", "/pass/a/b", "/unmatched", "/styles.css"} {
		payload := fmt.Sprintf(`{"url":"http://x%[1]s","method":"GET","path":%[1]q,"headers":{}}`, path)
		resp, err := http.Post(base+"/__krate/middleware", "application/json", strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var n env
		if err := json.Unmarshal(raw, &n); err != nil {
			t.Fatalf("%s: %v (%s)", path, err, raw)
		}
		q := rt.Execute(jsruntime.MiddlewareRequest{URL: "http://x" + path, Method: "GET", Path: path})
		if !n.Matcher {
			t.Errorf("%s: sidecar did not report the matcher", path)
		}
		qAction := q.Action
		if qAction == jsruntime.MiddlewareSkip {
			qAction = "continue"
		}
		if n.Action != qAction {
			t.Errorf("%s: action node=%q quickjs=%q", path, n.Action, qAction)
			continue
		}
		if n.Action == "response" && (n.Status != q.Status || n.Body != string(q.Body)) {
			t.Errorf("%s: response node=%d/%q quickjs=%d/%q", path, n.Status, n.Body, q.Status, q.Body)
		}
		cookies := 0
		for _, h := range n.Headers {
			if h[0] == "set-cookie" {
				cookies++
			}
		}
		qCookies := 0
		for _, h := range q.Headers {
			if h[0] == "set-cookie" {
				qCookies++
			}
		}
		if cookies != qCookies {
			t.Errorf("%s: set-cookie count node=%d quickjs=%d", path, cookies, qCookies)
		}
		if n.Rewrite != q.Rewrite {
			t.Errorf("%s: rewrite node=%q quickjs=%q", path, n.Rewrite, q.Rewrite)
		}
	}
}

// In dev (KRATE_API_HOT=1) the Node and Bun sidecars reload the middleware
// module when its file changes; in production it is loaded once.
func TestSidecarMiddlewareHotReload(t *testing.T) {
	for _, runtime := range []string{"node", "bun"} {
		for _, hot := range []bool{true, false} {
			name := runtime + "/prod"
			if hot {
				name = runtime + "/dev"
			}
			t.Run(name, func(t *testing.T) {
				mk := func(v string) string {
					return `export function middleware(request) {
  return new Response(null, { headers: { "x-middleware-next": "1", "x-version": "` + v + `" } });
}
`
				}
				dir := t.TempDir()
				mwPath := filepath.Join(dir, "middleware.js")
				if err := os.WriteFile(mwPath, []byte(mk("one")), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0o644); err != nil {
					t.Fatal(err)
				}
				apiDir := filepath.Join(dir, "api")
				_ = os.MkdirAll(apiDir, 0o755)
				var env []string
				if hot {
					env = append(env, "KRATE_API_HOT=1")
				}
				base := startSidecarConformanceEnv(t, runtime, apiDir, mwPath, env...)
				version := func() string {
					resp, err := http.Post(base+"/__krate/middleware", "application/json", strings.NewReader(`{"url":"http://x/a","method":"GET","path":"/a","headers":{}}`))
					if err != nil {
						t.Fatal(err)
					}
					defer resp.Body.Close()
					var out struct {
						Headers [][2]string `json:"headers"`
					}
					raw, _ := io.ReadAll(resp.Body)
					if err := json.Unmarshal(raw, &out); err != nil {
						t.Fatalf("%v (%s)", err, raw)
					}
					for _, h := range out.Headers {
						if h[0] == "x-version" {
							return h[1]
						}
					}
					return ""
				}
				if got := version(); got != "one" {
					t.Fatalf("initial x-version = %q", got)
				}
				if err := os.WriteFile(mwPath, []byte(mk("two-changed")), 0o644); err != nil {
					t.Fatal(err)
				}
				want := "one"
				if hot {
					want = "two-changed"
				}
				if got := version(); got != want {
					t.Errorf("after edit x-version = %q, want %q", got, want)
				}
			})
		}
	}
}
