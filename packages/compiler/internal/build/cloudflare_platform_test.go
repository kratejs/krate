package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// cfStub stands in for the workerd-only `cloudflare:workers` module when the
// generated Worker runs under Node.
const cfStub = `export const env = { DB: 'module-db' };
export class DurableObject { constructor(state, env) { this.ctx = state; this.env = env; } }
`

// cloudflareScript loads the generated Worker with `cloudflare:workers`
// pointed at cfStub and probes it with a fake ASSETS binding that serves files
// under dist the way assets.html_handling says: "auto" redirects /x to /x/,
// "none" serves exact files only.
func cloudflareScript(t *testing.T, root string, cfg *config.Config, probes string) string {
	t.Helper()
	dir := filepath.Join(root, ".krate", "deploy", "cloudflare")
	src, err := os.ReadFile(filepath.Join(dir, "worker.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `"cloudflare:workers"`) {
		t.Fatalf("worker.mjs must keep cloudflare:workers as an external import")
	}
	patched := strings.ReplaceAll(string(src), `"cloudflare:workers"`, `"./cf-stub.mjs"`)
	if err := os.WriteFile(filepath.Join(dir, "worker.test.mjs"), []byte(patched), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cf-stub.mjs"), []byte(cfStub), 0644); err != nil {
		t.Fatal(err)
	}
	return runNodeScript(t, dir, `import * as mod from './worker.test.mjs';
import fs from 'node:fs';
const worker = mod.default;
const base = `+"`"+filepath.ToSlash(cfg.OutDir)+"`"+`;
const html = process.env.HTML_HANDLING || 'auto';
const waited = [];
const env = {
  DB: 'ctx-db', KV: 'kv-binding', GREETING: 'hi',
  ASSETS: { fetch: async (r) => {
    const u = new URL(r.url); const p = u.pathname;
    const file = (f) => { try { return fs.statSync(f).isFile() ? f : null; } catch { return null; } };
    let f = file(base + p);
    if (!f && p.endsWith('/')) f = file(base + p + 'index.html');
    if (!f && html === 'auto' && !p.endsWith('/') && file(base + p + '/index.html')) return new Response(null, { status: 307, headers: { Location: p + '/' } });
    if (!f && html === 'drop') {
      if (p.endsWith('/') && p.length > 1 && file(base + p + 'index.html')) return new Response(null, { status: 307, headers: { Location: p.slice(0, -1) } });
      f = file(base + p + '/index.html');
    }
    if (!f) return new Response('nf', { status: 404 });
    return new Response(fs.readFileSync(f), { headers: { 'content-type': f.endsWith('.html') ? 'text/html' : 'application/octet-stream' } });
  } },
};
const ctx = { waitUntil: (p) => waited.push(p) };
const results = {};
async function req(path, init) {
  const res = await worker.fetch(new Request('https://example.test' + path, { redirect: 'manual', ...init }), env, ctx);
  return { status: res.status, loc: res.headers.get('location'), mw: res.headers.get('x-mw-db'), body: await res.text() };
}
`+probes)
}

func TestCloudflarePlatformBindings(t *testing.T) {
	root, cfg := serverlessProject(t, "serverless-cloudflare")
	plan, _ := emitServerTarget(t, root, cfg, "cloudflare")
	if !strings.HasSuffix(filepath.ToSlash(plan.WorkerModule), "src/cloudflare.ts") {
		t.Errorf("src/cloudflare.ts must be picked up automatically, got %q", plan.WorkerModule)
	}
	ids := actionIDs(t, root)
	out := cloudflareScript(t, root, cfg, `
globalThis.__afterRan = 0;
results.page = await req('/platform/', { headers: { accept: 'text/html' } });
await Promise.all(waited);
results.afterRan = globalThis.__afterRan;
results.api = await req('/api/binding');
results.action = await req('/__krate/action/`+ids["ping"]+`', { method: 'POST', headers: { 'Sec-Fetch-Site': 'same-origin', 'X-Krate-Action': 'fetch', 'Content-Type': 'application/json' }, body: '{"args":[]}' });
results.exports = { counter: typeof mod.Counter, instance: new mod.Counter({}, env).value(), handlers: Object.keys(worker).sort() };
await worker.scheduled({ cron: '*/5 * * * *' }, env, ctx);
await worker.queue({ messages: [1, 2, 3] }, env, ctx);
results.cron = globalThis.__cron;
results.queued = globalThis.__queued;
results.procDB = typeof process.env.DB;
console.log(JSON.stringify(results));
`)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var r struct {
		Page struct {
			Status int
			Mw     string
			Body   string
		}
		AfterRan int
		API      struct {
			Status int
			Body   string
		}
		Action struct {
			Status int
			Body   string
		}
		Exports struct {
			Counter  string
			Instance int
			Handlers []string
		}
		Cron   string
		Queued int
		ProcDB string
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &r); err != nil {
		t.Fatalf("parsing %q: %v", out, err)
	}
	for _, want := range []string{`id="module-env">module-db<`, `id="ctx-env">ctx-db<`, `id="target">cloudflare<`} {
		if !strings.Contains(r.Page.Body, want) {
			t.Errorf("page missing %s:\n%s", want, r.Page.Body)
		}
	}
	if r.Page.Status != 200 || r.Page.Mw != "ctx-db" {
		t.Errorf("page %d, middleware saw DB %q (want ctx-db)", r.Page.Status, r.Page.Mw)
	}
	if r.AfterRan != 1 {
		t.Errorf("after() in a page must run through ctx.waitUntil, ran %d times", r.AfterRan)
	}
	if r.API.Status != 200 || r.API.Body != `{"db":"module-db","kv":"kv-binding"}` {
		t.Errorf("api route: %+v", r.API)
	}
	if r.Action.Status != 200 || !strings.Contains(r.Action.Body, "ctx-db") {
		t.Errorf("server action must reach env through getCloudflareContext: %+v", r.Action)
	}
	if r.Exports.Counter != "function" || r.Exports.Instance != 7 {
		t.Errorf("Durable Object class must be exported from the Worker: %+v", r.Exports)
	}
	if strings.Join(r.Exports.Handlers, ",") != "fetch,queue,scheduled" {
		t.Errorf("worker handlers: %v", r.Exports.Handlers)
	}
	if r.Cron != "*/5 * * * *:ctx-db" || r.Queued != 3 {
		t.Errorf("scheduled/queue handlers: cron %q queued %d", r.Cron, r.Queued)
	}
	if r.ProcDB != "string" {
		t.Errorf("string bindings keep being copied into process.env: %q", r.ProcDB)
	}
}

func TestCloudflareModuleImportOnOtherTargets(t *testing.T) {
	root, cfg := serverlessProject(t, "serverless-cloudflare")
	for _, target := range []string{"node", "vercel", "netlify"} {
		_, err := BuildServerBundle(root, cfg, target)
		if err == nil {
			t.Fatalf("%s: importing cloudflare:workers must fail the bundle", target)
		}
		msg := err.Error()
		if !strings.Contains(msg, `"cloudflare:workers"`) || !strings.Contains(msg, "only on the cloudflare target") || !strings.Contains(msg, "getPlatform()") {
			t.Errorf("%s: unhelpful error: %v", target, err)
		}
	}
}

func TestCloudflareWorkerModuleConfig(t *testing.T) {
	root, cfg := serverlessProject(t, "serverless-cloudflare")
	cfg.Deploy.Worker = "src/missing.ts"
	if _, err := BuildServerBundle(root, cfg, "cloudflare"); err == nil || !strings.Contains(err.Error(), "deploy.worker") {
		t.Errorf("a missing deploy.worker must be reported, got %v", err)
	}
	cfg.Deploy.Worker = "../outside.ts"
	if _, err := BuildServerBundle(root, cfg, "cloudflare"); err == nil || !strings.Contains(err.Error(), "outside the project root") {
		t.Errorf("deploy.worker outside the root must be refused, got %v", err)
	}
	cfg.Deploy.Worker = "none"
	plan, err := BuildServerBundle(root, cfg, "cloudflare")
	if err == nil && plan.WorkerModule != "" {
		t.Errorf(`deploy.worker "none" must skip src/cloudflare.ts, got %q`, plan.WorkerModule)
	}
}

func TestCloudflareTrailingSlashAndNotFoundPage(t *testing.T) {
	probes := `
for (const p of ['/history', '/history/', '/about', '/about/', '/404', '/404/', '/404.html']) results[p] = await req(p, { headers: { accept: 'text/html' } });
for (const p of ['/nope', '/a/b/c', '/nope/', '/a/b/c/']) {
  results[p + '#html'] = await req(p, { headers: { accept: 'text/html' } });
  results[p + '#any'] = await req(p, { headers: { accept: '*/*' } });
}
console.log(JSON.stringify(results));
`
	type res struct {
		Status int
		Loc    string
		Body   string
	}
	cases := []struct {
		mode         string
		html         string
		wantHandling string
		want         map[string][2]interface{}
	}{
		{"always", "auto", "", map[string][2]interface{}{
			"/history": {301, "./history/"}, "/history/": {200, ""}, "/about": {307, "/about/"}, "/about/": {200, ""},
		}},
		{"never", "drop-trailing-slash", "drop-trailing-slash", map[string][2]interface{}{
			"/history": {200, ""}, "/history/": {301, "../history"}, "/about": {200, ""}, "/about/": {301, "../about"},
		}},
		{"ignore", "none", "none", map[string][2]interface{}{
			"/history": {200, ""}, "/history/": {200, ""}, "/about": {200, ""}, "/about/": {200, ""},
		}},
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			root, cfg := serverlessProject(t, "serverless-cloudflare")
			cfg.TrailingSlash = c.mode
			cfg.Deploy.Worker = "none"
			emitServerTarget(t, root, cfg, "cloudflare")
			conf, err := os.ReadFile(filepath.Join(root, "wrangler.jsonc"))
			if err != nil {
				t.Fatal(err)
			}
			var wc struct {
				Assets struct {
					HTMLHandling   string      `json:"html_handling"`
					RunWorkerFirst interface{} `json:"run_worker_first"`
				} `json:"assets"`
			}
			if err := json.Unmarshal([]byte(regexp.MustCompile(`(?m)^//.*$`).ReplaceAllString(string(conf), "")), &wc); err != nil {
				t.Fatal(err)
			}
			if wc.Assets.HTMLHandling != c.wantHandling {
				t.Errorf("assets.html_handling = %q, want %q", wc.Assets.HTMLHandling, c.wantHandling)
			}
			if c.mode == "ignore" && wc.Assets.RunWorkerFirst != true {
				t.Errorf("ignore serves pages from the Worker, run_worker_first = %v", wc.Assets.RunWorkerFirst)
			}
			t.Setenv("HTML_HANDLING", map[string]string{"drop-trailing-slash": "drop", "none": "none", "auto": "auto"}[c.html])
			out := cloudflareScript(t, root, cfg, probes)
			lines := strings.Split(strings.TrimSpace(out), "\n")
			var got map[string]res
			if err := json.Unmarshal([]byte(lines[len(lines)-1]), &got); err != nil {
				t.Fatalf("parsing %q: %v", out, err)
			}
			for p, w := range c.want {
				if got[p].Status != w[0].(int) || got[p].Loc != w[1].(string) {
					t.Errorf("%s %s: got %d %q, want %d %q", c.mode, p, got[p].Status, got[p].Loc, w[0], w[1])
				}
			}
			// Unknown URLs follow the page slash rule (always: 301 to the slash
			// form, never: slashed form redirects), then answer with the 404 page
			// whatever the Accept header says.
			for _, p := range []string{"/nope", "/a/b/c", "/nope/", "/a/b/c/"} {
				slashed := strings.HasSuffix(p, "/")
				for _, kind := range []string{"#html", "#any"} {
					r := got[p+kind]
					redirect := (c.mode == "always" && !slashed) || (c.mode == "never" && slashed)
					if redirect {
						if r.Status != 301 || r.Loc == "" {
							t.Errorf("%s %s%s: got %d %q, want the slash redirect", c.mode, p, kind, r.Status, r.Loc)
						}
						continue
					}
					if r.Status != 404 || !strings.Contains(r.Body, "custom-404") {
						t.Errorf("%s %s%s: got %d %q, want the 404 page with status 404", c.mode, p, kind, r.Status, r.Body)
					}
				}
			}
			// The not-found page's own URLs are a 404 with that page, never a
			// redirect or the file with 200, in every mode.
			for _, p := range []string{"/404", "/404/", "/404.html"} {
				if got[p].Status != 404 || !strings.Contains(got[p].Body, "custom-404") {
					t.Errorf("%s %s: got %d %q, want the 404 page with status 404", c.mode, p, got[p].Status, got[p].Body)
				}
			}
		})
	}
}
