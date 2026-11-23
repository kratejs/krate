package build

import (
	"encoding/json"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestCookieValuesWithEqualsAndAmpersandRoundTrip: a cookie value such as
// "mode=light&nav=open" survives being set (cookies().set in a server action,
// a raw Set-Cookie header) and read back in a server component, an API route
// and middleware, whichever way the browser sends it: raw, percent-encoded,
// quoted, among other cookies, or on two Cookie header lines. The readers used
// to disagree: the Node sidecar's request.cookies kept the percent-encoded form
// that the page and action readers decode.
func TestCookieValuesWithEqualsAndAmpersandRoundTrip(t *testing.T) {
	t.Parallel()
	ensureRuntimeDist(t)
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx": `import { cookies } from "@krate/runtime/server";
export default async function Home() {
  const c = (await cookies()).get("ui");
  return <main id="ui">[{c ? c.value : "none"}]</main>;
}`,
		"src/api/read.ts": `export function GET(req: any) {
  return Response.json({ ui: req.cookies ? (req.cookies.get("ui") || {}).value ?? null : "no-cookies" });
}`,
		"src/api/set.ts": `export function GET() { return new Response("ok", { headers: { "set-cookie": "ui=mode=light&nav=open; Path=/" } }); }`,
		"src/actions/ui.ts": `"use server";
import { cookies } from "@krate/runtime/server";
export async function setUi() { (await cookies()).set("ui", "mode=light&nav=open"); return "done"; }`,
		"src/middleware.ts": `export function middleware(req: any) {
  if (new URL(req.url).pathname !== "/mw") return;
  return Response.json({ ui: req.cookies ? (req.cookies.get("ui") || {}).value ?? null : "no-cookies" });
}`,
	})
	b.Cfg.SSR.APIRuntime = "node"
	b.Cfg.SSR.MiddlewareRuntime = "node"
	withPreact(t, root)
	linkRuntimeDist(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	do := func(req *http.Request) (*http.Response, string) {
		t.Helper()
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp, string(body)
	}

	// What the server sends.
	req, _ := http.NewRequest("GET", base+"/api/set", nil)
	resp, _ := do(req)
	if got := resp.Header.Values("Set-Cookie"); len(got) != 1 || got[0] != "ui=mode=light&nav=open; Path=/" {
		t.Errorf("a raw Set-Cookie header changed on the way out: %q", got)
	}
	data, err := os.ReadFile(filepath.Join(root, ".krate", "actions", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct{ Actions []struct{ ID, Export string } }
	if err := json.Unmarshal(data, &m); err != nil || len(m.Actions) != 1 {
		t.Fatalf("action manifest: %v %s", err, data)
	}
	req, _ = http.NewRequest("POST", base+"/__krate/action/"+m.Actions[0].ID, strings.NewReader(`{"args":[]}`))
	req.Header.Set("X-Krate-Action", "fetch")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, body := do(req)
	sc := resp.Header.Values("Set-Cookie")
	if resp.StatusCode != 200 || len(sc) != 1 || !strings.HasPrefix(sc[0], "ui=mode%3Dlight%26nav%3Dopen;") {
		t.Fatalf("cookies().set in an action: %d %q %s", resp.StatusCode, sc, body)
	}

	// What the readers make of every way a browser can send it back.
	sent := map[string][]string{
		"raw":         {"ui=mode=light&nav=open"},
		"encoded":     {"ui=mode%3Dlight%26nav%3Dopen"},
		"quoted":      {`ui="mode=light&nav=open"`},
		"among":       {"a=1; ui=mode=light&nav=open; b=2"},
		"two lines":   {"a=1", "ui=mode=light&nav=open"},
		"from action": {strings.SplitN(sc[0], ";", 2)[0]},
	}
	const want = "mode=light&nav=open"
	page := regexp.MustCompile(`id="ui">\[(.*?)\]</main>`)
	for name, lines := range sent {
		get := func(path string) string {
			req, _ := http.NewRequest("GET", base+path, nil)
			req.Header["Cookie"] = lines
			resp, body := do(req)
			if resp.StatusCode != 200 {
				t.Errorf("%s %s: status %d\n%s", name, path, resp.StatusCode, body)
			}
			return body
		}
		if m := page.FindStringSubmatch(get("/")); m == nil || html.UnescapeString(m[1]) != want {
			t.Errorf("%s: server component read %q, want %q", name, m, want)
		}
		for _, path := range []string{"/api/read", "/mw"} {
			var out struct{ UI string }
			if err := json.Unmarshal([]byte(get(path)), &out); err != nil || out.UI != want {
				t.Errorf("%s: %s read %q (%v), want %q", name, path, out.UI, err, want)
			}
		}
	}
}
