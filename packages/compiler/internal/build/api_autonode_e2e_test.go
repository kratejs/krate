package build

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A route that uses Buffer (itself and through an imported helper) is served by
// the Node sidecar without any configuration, and answers 200.
func TestServeAPIRouteWithGlobalBufferReturns200(t *testing.T) {
	e2eEnv(t)
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx": `export default function Home() { return <main><h1>home</h1></main>; }`,
		"src/server/embed.ts": `export function toHex(s: string) { return Buffer.from(s).toString("hex"); }`,
		"src/api/embed.ts": `import { toHex } from "../server/embed";
export function GET() {
  return Response.json({ hex: toHex("hi"), b64: Buffer.from("hi").toString("base64") });
}`,
		"src/api/plain.ts": `export function GET() { return Response.json({ ok: true }); }`,
	})
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	captureProgress(t)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	metas := readRouteMetas(t, root)
	if !metas["embed.js"].NodeOnly || metas["plain.js"].NodeOnly {
		t.Fatalf("route placement: %+v", metas)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	var code int
	var body string
	waitUntilTrue(t, 30*time.Second, "the Node API sidecar", func() bool {
		code, body = getText(t, base+"/api/embed")
		return code == 200
	})
	if !strings.Contains(body, `"hex":"6869"`) || !strings.Contains(body, `"b64":"aGk="`) {
		t.Errorf("body: %s", body)
	}
	if code, body := getText(t, base+"/api/plain"); code != 200 || !strings.Contains(body, "true") {
		t.Errorf("embedded route: %d %s", code, body)
	}
}

func getText(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// waitFor polls fn until it returns true or the timeout passes.
func waitUntilTrue(t *testing.T, d time.Duration, what string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
