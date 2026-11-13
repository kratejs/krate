package build

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/deploy"
)

func emitServerTarget(t *testing.T, root string, cfg *config.Config, target string) (*deploy.ServerPlan, []string) {
	t.Helper()
	plan, err := BuildServerBundle(root, cfg, target)
	if err != nil {
		t.Fatalf("BuildServerBundle(%s): %v", target, err)
	}
	files, err := deploy.Emit(target, cfg, root, cfg.OutDir, deploy.Options{Server: plan})
	if err != nil {
		t.Fatalf("Emit(%s): %v", target, err)
	}
	return plan, files
}

// runNodeScript runs an ES module script in dir and returns its stdout.
func runNodeScript(t *testing.T, dir, script string) string {
	t.Helper()
	f := filepath.Join(dir, "check.mjs")
	if err := os.WriteFile(f, []byte(script), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", f)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node script failed: %v\n%s", err, out)
	}
	return string(out)
}

const handlerChecks = `
const results = {};
async function req(path, init) {
  const res = await call(new Request('https://example.test' + path, init));
  return { status: res.status, ct: res.headers.get('content-type'), cc: res.headers.get('cache-control'), loc: res.headers.get('location'), body: await res.text() };
}
results.ssr = await req('/ssr/?name=ada');
results.isr = await req('/isr/');
results.api = await req('/api/items/9');
results.notFound = await req('/post/missing/');
results.redirect = await req('/old-about');
results.stream = await req('/stream/');
results.template = await req('/blog/zzz/');
results.segNotFound = await req('/blog/a/b/c/', { headers: { accept: 'text/html' } });
results.segMissNoSlash = await req('/blog/a/b/c', { headers: { accept: 'text/html' } });
results.segGone = await req('/blog/ssr-gone/');
results.segCrash = await req('/blog/ssr-crash/');
results.rootNotFound = await req('/elsewhere/deep/', { headers: { accept: 'text/html' } });
console.log(JSON.stringify(results));
`

func checkHandlerResults(t *testing.T, out string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var r map[string]struct {
		Status int
		Ct, Cc string
		Loc    string
		Body   string
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &r); err != nil {
		t.Fatalf("parsing %q: %v", out, err)
	}
	if r["ssr"].Status != 200 || !strings.Contains(r["ssr"].Body, ">ada<") || r["ssr"].Cc != "no-store" {
		t.Errorf("ssr: %+v", r["ssr"])
	}
	if r["isr"].Status != 200 || !strings.HasPrefix(r["isr"].Cc, "public, s-maxage=30, stale-while-revalidate=60") {
		t.Errorf("isr cache policy: %+v", r["isr"])
	}
	if r["api"].Status != 200 || r["api"].Body != `{"id":"9"}` {
		t.Errorf("api: %+v", r["api"])
	}
	if r["notFound"].Status != 404 {
		t.Errorf("notFound: %+v", r["notFound"])
	}
	if r["redirect"].Status != 301 || r["redirect"].Loc != "/about" {
		t.Errorf("redirect: %+v", r["redirect"])
	}
	if r["stream"].Status != 200 || !strings.Contains(r["stream"].Body, "slow-done") || !strings.Contains(r["stream"].Body, "loading-fallback") {
		t.Errorf("stream: %+v", r["stream"])
	}
	if r["template"].Status != 200 || !strings.Contains(r["template"].Body, "zzz") {
		t.Errorf("template: %+v", r["template"])
	}
	for _, k := range []string{"segNotFound", "segMissNoSlash", "segGone"} {
		if r[k].Status != 404 || !strings.Contains(r[k].Body, "blog-404") || !strings.Contains(r[k].Body, "id=blog-layout") || r[k].Cc != "no-store" {
			t.Errorf("%s must render blog's own 404 inside its layout: %+v", k, r[k])
		}
	}
	if c := r["segCrash"]; c.Status != 500 || !strings.Contains(c.Body, "blog-error") || strings.Contains(c.Body, "secret crash detail") || c.Cc != "no-store" || !regexp.MustCompile(`"digest":"[0-9a-f]{10}"`).MatchString(c.Body) {
		t.Errorf("segCrash must render blog's sanitized error page with a digest: %+v", c)
	}
	if r["rootNotFound"].Status != 404 || !strings.Contains(r["rootNotFound"].Body, "custom-404") || strings.Contains(r["rootNotFound"].Body, "blog-404") {
		t.Errorf("rootNotFound: %+v", r["rootNotFound"])
	}
}

func TestVercelServerOutput(t *testing.T) {
	root, cfg := serverlessProject(t, "serverless")
	plan, files := emitServerTarget(t, root, cfg, "vercel")
	out := filepath.Join(root, ".vercel", "output")
	for _, rel := range []string{"config.json", "functions/__krate.func/index.mjs", "functions/__krate.func/.vc-config.json", "static/about/index.html", "static/index.html"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel))); err != nil {
			t.Errorf("missing %s: %v (files %v)", rel, err, files)
		}
	}
	// Request-time shells and templates are inside the function, never public.
	for _, rel := range append([]string{"ssr/index.html", "post/[id]/index.html", "blog/[slug]/index.html"}, ".krate") {
		if _, err := os.Stat(filepath.Join(out, "static", filepath.FromSlash(rel))); err == nil {
			t.Errorf("static output must not contain %s", rel)
		}
	}
	var conf struct {
		Version int                      `json:"version"`
		Routes  []map[string]interface{} `json:"routes"`
	}
	data, _ := os.ReadFile(filepath.Join(out, "config.json"))
	if err := json.Unmarshal(data, &conf); err != nil || conf.Version != 3 {
		t.Fatalf("config.json: %v %s", err, data)
	}
	sawFS, sawFallback := false, false
	for i, r := range conf.Routes {
		if r["handle"] == "filesystem" {
			sawFS = true
			continue
		}
		src, _ := r["src"].(string)
		if src == "" {
			t.Errorf("route %d has neither src nor handle: %v", i, r)
			continue
		}
		if _, err := regexp.Compile(strings.ReplaceAll(src, "(?<", "(?P<")); err != nil {
			t.Errorf("route %d src %q is not a valid regex: %v", i, src, err)
		}
		if r["dest"] == "/__krate" && sawFS && src == "^/(.*)$" {
			sawFallback = true
		}
	}
	if !sawFS || !sawFallback {
		t.Errorf("routes need a filesystem handle followed by a function fallback: %s", data)
	}
	var vc map[string]interface{}
	data, _ = os.ReadFile(filepath.Join(out, "functions", "__krate.func", ".vc-config.json"))
	if err := json.Unmarshal(data, &vc); err != nil || vc["runtime"] == nil || vc["handler"] != "index.mjs" || vc["launcherType"] != "Nodejs" {
		t.Errorf(".vc-config.json: %v %s", err, data)
	}
	for _, w := range plan.Warnings {
		if strings.Contains(w, "bypass") {
			t.Errorf("middleware no longer bypasses prerendered pages, the warning must be gone: %s", w)
		}
	}
	// Middleware: page requests reach the function before the filesystem.
	htmlIdx, fsIdx := -1, -1
	for i, r := range conf.Routes {
		if r["handle"] == "filesystem" && fsIdx < 0 {
			fsIdx = i
		}
		if r["src"] == `^(?:/|/[^.]*|/.*\.html)$` && r["dest"] == "/__krate" {
			htmlIdx = i
		}
	}
	if htmlIdx < 0 || fsIdx < 0 || htmlIdx > fsIdx {
		t.Errorf("an HTML route to the function must precede handle:filesystem when there is middleware: %s", data)
	}
	htmlRe := regexp.MustCompile(`^(?:/|/[^.]*|/.*\.html)$`)
	for path, want := range map[string]bool{"/": true, "/about/": true, "/admin": true, "/a/b.html": true, "/chunks/x.js": false, "/logo.png": false, "/favicon.ico": false} {
		if htmlRe.MatchString(path) != want {
			t.Errorf("HTML route regex on %s = %v, want %v", path, !want, want)
		}
	}
	fn := filepath.Join(out, "functions", "__krate.func")
	guard := runNodeScript(t, fn, `import { handleRequest as call } from './index.mjs';
const r = await call(new Request('https://example.test/admin'));
const h = await call(new Request('https://example.test/'));
const a = await call(new Request('https://example.test/about/'));
console.log(JSON.stringify({ admin: [r.status, r.headers.get('location')], home: [h.status, h.headers.get('x-mw'), (await h.text()).includes('static-home')], about: [a.status, (await a.text()).includes('about')] }));`)
	if !strings.Contains(guard, `"admin":[307,"https://example.test/about"]`) || !strings.Contains(guard, `"home":[200,"ran",true]`) || !strings.Contains(guard, `"about":[200,true]`) {
		t.Errorf("the function must run middleware for prerendered pages and then serve them: %s", guard)
	}
	res := runNodeScript(t, fn, `import { handleRequest as call } from './index.mjs';`+handlerChecks)
	checkHandlerResults(t, res)
}

func TestCloudflareServerOutput(t *testing.T) {
	root, cfg := serverlessProject(t, "serverless")
	_, _ = emitServerTarget(t, root, cfg, "cloudflare")
	worker := filepath.Join(root, ".krate", "deploy", "cloudflare", "worker.mjs")
	src, err := os.ReadFile(worker)
	if err != nil {
		t.Fatal(err)
	}
	// No filesystem or vm in the import graph, and nothing outside nodejs_compat.
	importRe := regexp.MustCompile(`(?m)(?:from|import)\s*\(?\s*["'](node:[a-z_/]+|[a-z_]+)["']`)
	allowed := map[string]bool{"node:async_hooks": true, "node:buffer": true, "node:crypto": true, "node:events": true, "node:util": true, "node:stream": true, "node:path": true, "node:url": true}
	for _, m := range importRe.FindAllStringSubmatch(string(src), -1) {
		if strings.HasPrefix(m[1], "node:") && !allowed[m[1]] {
			t.Errorf("worker imports %s, which Workers do not provide", m[1])
		}
	}
	for _, bad := range []string{"node:fs", "node:vm", "node:http", "node:child_process", "node:worker_threads", "node:cluster", "node:net", "node:tls", "node:fs/promises", "RespConnection"} {
		if strings.Contains(string(src), bad) {
			t.Errorf("worker bundle references %s", bad)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "wrangler.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	var conf struct {
		Main   string   `json:"main"`
		Flags  []string `json:"compatibility_flags"`
		Assets struct {
			Directory      string      `json:"directory"`
			Binding        string      `json:"binding"`
			RunWorkerFirst interface{} `json:"run_worker_first"`
		} `json:"assets"`
	}
	body := regexp.MustCompile(`(?m)^//.*$`).ReplaceAllString(string(data), "")
	if err := json.Unmarshal([]byte(body), &conf); err != nil {
		t.Fatalf("wrangler.jsonc: %v\n%s", err, data)
	}
	if conf.Main != ".krate/deploy/cloudflare/worker.mjs" || conf.Assets.Binding != "ASSETS" || conf.Assets.Directory != "dist" || conf.Assets.RunWorkerFirst != true {
		t.Errorf("wrangler config: %+v", conf)
	}
	hasCompat := false
	for _, f := range conf.Flags {
		hasCompat = hasCompat || f == "nodejs_compat"
	}
	if !hasCompat {
		t.Errorf("nodejs_compat missing: %+v", conf)
	}
	// Run the worker in Node with a fake ASSETS binding (no workerd needed).
	res := runNodeScript(t, filepath.Dir(worker), `import worker from './worker.mjs';
const env = { ASSETS: { fetch: async () => new Response('nf', { status: 404 }) }, GREETING: 'hi' };
const call = (request) => worker.fetch(request, env, { waitUntil() {} });`+handlerChecks)
	checkHandlerResults(t, res)
}

func TestServerBundleHasNoSecrets(t *testing.T) {
	root := t.TempDir()
	_ = root
	proj, cfg := serverlessProject(t, "serverless")
	if err := os.WriteFile(filepath.Join(proj, ".env"), []byte("KRATE_SECRET_TOKEN=s3cr3t-value-123\nKRATE_PUBLIC_NAME=visible\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "src", "api", "env.ts"), []byte("export async function GET() { return Response.json({ v: process.env.KRATE_SECRET_TOKEN ?? null }); }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	b := New(proj, cfg)
	b.Env = map[string]string{"KRATE_SECRET_TOKEN": "s3cr3t-value-123", "KRATE_PUBLIC_NAME": "visible"}
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"node", "cloudflare"} {
		plan, err := BuildServerBundle(proj, cfg, target)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(plan.Bundle)
		if strings.Contains(string(data), "s3cr3t-value-123") {
			t.Errorf("%s bundle contains the secret", target)
		}
	}
	base := startNodeBundle(t, proj, cfg, "KRATE_SECRET_TOKEN=from-platform")
	r := doMatrix(t, base, "GET", "/api/env", nil, "")
	if !strings.Contains(r.body, "from-platform") {
		t.Errorf("runtime env must come from the platform: %s", r.body)
	}
}

// Platform deploys rewrite dist/: segment pages and their hydration dirs must
// survive, and unmatched URLs must reach the function (not a CDN 404).
func TestPlatformDeploysKeepSegmentPages(t *testing.T) {
	for _, target := range []string{"vercel", "netlify", "cloudflare"} {
		t.Run(target, func(t *testing.T) {
			root, cfg := serverlessProject(t, "serverless")
			emitServerTarget(t, root, cfg, target)
			base := cfg.OutDir
			if target == "vercel" {
				base = filepath.Join(root, ".vercel", "output", "static")
			}
			for _, rel := range []string{"404.html", "500.html", "blog/404.html", "blog/500.html", "shop/[cat]/404.html"} {
				if _, err := os.Stat(filepath.Join(base, filepath.FromSlash(rel))); err != nil {
					t.Errorf("%s: segment page %s was removed from the public output", target, rel)
				}
			}
			for _, rel := range []string{"blog/__404"} {
				if fi, err := os.Stat(filepath.Join(base, filepath.FromSlash(rel))); err != nil || !fi.IsDir() {
					t.Errorf("%s: hydration dir %s was removed", target, rel)
				}
			}
			switch target {
			case "vercel":
				data, _ := os.ReadFile(filepath.Join(root, ".vercel", "output", "config.json"))
				// Anything the filesystem does not have falls through to the function.
				if !strings.Contains(string(data), `"handle": "filesystem"`) || !strings.Contains(string(data), `"src": "^/(.*)$"`) {
					t.Errorf("vercel misses must reach the function: %s", data)
				}
			case "netlify":
				data, _ := os.ReadFile(filepath.Join(root, "netlify", "functions", "krate.mjs"))
				if !strings.Contains(string(data), `path: "/*"`) {
					t.Errorf("netlify function must own every path so misses reach it: %s", data)
				}
			case "cloudflare":
				// Workers assets fall back to the Worker for paths without an asset.
				data, _ := os.ReadFile(filepath.Join(root, "wrangler.jsonc"))
				if strings.Contains(string(data), "not_found_handling") {
					t.Errorf("not_found_handling would answer misses with a global 404 page before the Worker: %s", data)
				}
			}
		})
	}
}
