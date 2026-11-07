package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/kratedocs"
)

func TestCompileMiddlewareMatcherMatchesRuntimeSemantics(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"/admin", "/admin", true},
		{"/admin", "/admin/", true},
		{"/admin", "/admin/x", false},
		{"/admin/:path*", "/admin", true},
		{"/admin/:path*", "/admin/a/b", true},
		{"/admin/:path*", "/other", false},
		{"/blog/:slug", "/blog/hello", true},
		{"/blog/:slug", "/blog/hello/more", false},
		{"/docs/:rest+", "/docs", false},
		{"/docs/:rest+", "/docs/a/b", true},
		{"/maybe/:id?", "/maybe", true},
		{"/maybe/:id?", "/maybe/3", true},
		{"/api/:v(v1|v2)/x", "/api/v2/x", true},
		{"/api/:v(v1|v2)/x", "/api/v3/x", false},
		{"/files/*", "/files/a/b.txt", true},
		{"/((?!api|_next).*)", "/about", true},
		{"/((?!api|_next).*)", "/api/x", false},
		{"/((?!api|_next).*)", "/_next/static/x", false},
		{"/ADMIN", "/admin", true},
	}
	for _, c := range cases {
		got, err := matcherMatches([]string{c.pattern}, c.path)
		if err != nil {
			t.Errorf("%s: %v", c.pattern, err)
			continue
		}
		if got != c.want {
			t.Errorf("matcher %q on %q = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
	if _, err := compileMiddlewareMatcher("admin"); err == nil {
		t.Error("a matcher must start with /")
	}
	if ok, _ := matcherMatches(nil, "/anything"); !ok {
		t.Error("no matcher means every path")
	}
	if ok, _ := matcherMatches([]string{"/a", "/b/:x*"}, "/b/c"); !ok {
		t.Error("any matcher may match")
	}
}

func TestListMiddlewareReadsMatcherAndTestsPaths(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }",
		"middleware.ts": `export function middleware(request: Request) { return undefined; }
export const config = { matcher: ['/admin/:path*', { source: '/account' }] };
`,
	})
	svc.cfg.Redirects = nil
	m := mustOK(t, svc, "list_middleware", map[string]any{"path": "/admin/users"})
	mw := asMap(m["middleware"])
	if mw["file"] != "middleware.ts" || mw["handler"] != "middleware" || mw["hasMatcher"] != true {
		t.Fatalf("middleware = %v", mw)
	}
	if got := strs(list(t, mw, "matchers")); len(got) != 2 || got[0] != "/admin/:path*" || got[1] != "/account" {
		t.Errorf("matchers = %v", got)
	}
	if asMap(m["pathCheck"])["runs"] != true {
		t.Errorf("pathCheck = %v", m["pathCheck"])
	}
	if no := mustOK(t, svc, "list_middleware", map[string]any{"path": "/blog"}); asMap(no["pathCheck"])["runs"] != false {
		t.Errorf("/blog must not run the middleware: %v", no["pathCheck"])
	}

	none := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }"})
	n := mustOK(t, none, "list_middleware", nil)
	if n["middleware"] != nil || !strings.Contains(n["note"].(string), "no middleware") {
		t.Errorf("no middleware = %v", n)
	}
	defaultExport := newTestService(t, map[string]string{"src/middleware.ts": "export default function mw() {}\n"})
	d := mustOK(t, defaultExport, "list_middleware", nil)
	if asMap(d["middleware"])["handler"] != "default" || !strings.Contains(d["scope"].(string), "every request") {
		t.Errorf("default export middleware = %v", d)
	}
}

func TestListAPIRoutes(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx":    "export default function P() { return <h1>x</h1>; }",
		"src/api/hello.ts":       "export default function handler() { return new Response('hi'); }\n",
		"src/api/users/index.ts": "export async function GET() { return Response.json([]); }\nexport const POST = async () => new Response('ok');\n",
		"src/api/users/[id].ts":  "export function GET() {}\nexport function DELETE() {}\n",
		"src/api/_private.ts":    "export function GET() {}\n",
		"src/api/types.d.ts":     "export {}\n",
		"src/api/go/thing.go":    "package main\nfunc GET(w http.ResponseWriter, r *http.Request) {}\nfunc POST(w http.ResponseWriter, r *http.Request) {}\n",
		"src/api/go/any.go":      "package main\nfunc Handler(w http.ResponseWriter, r *http.Request) {}\n",
		"src/api/dup.ts":         "export function GET() {}\n",
		"src/api/dup.go":         "package main\nfunc GET(w http.ResponseWriter, r *http.Request) {}\n",
	})
	m := mustOK(t, svc, "list_api_routes", nil)
	byRoute := map[string]map[string]any{}
	for _, r := range list(t, m, "routes") {
		rm := asMap(r)
		key := rm["route"].(string) + "|" + rm["language"].(string)
		byRoute[key] = rm
	}
	check := func(key string, methods ...string) map[string]any {
		t.Helper()
		r := byRoute[key]
		if r == nil {
			t.Fatalf("route %s missing in %v", key, sortedKeys(byRoute))
		}
		if got := strs(list(t, r, "methods")); strings.Join(got, ",") != strings.Join(methods, ",") {
			t.Errorf("%s methods = %v, want %v", key, got, methods)
		}
		return r
	}
	check("/api/hello|typescript", "ALL")
	check("/api/users|typescript", "GET", "POST")
	r := check("/api/users/[id]|typescript", "DELETE", "GET")
	if got := strs(list(t, r, "params")); len(got) != 1 || got[0] != "id" {
		t.Errorf("params = %v", got)
	}
	check("/api/go/thing|go", "GET", "POST")
	check("/api/go/any|go", "ALL")
	if byRoute["/api/dup|typescript"]["shadowed"] != true {
		t.Errorf("a Go route shadows a TypeScript one at the same path: %v", byRoute["/api/dup|typescript"])
	}
	for k := range byRoute {
		if strings.Contains(k, "_private") || strings.Contains(k, "/api/types") {
			t.Errorf("private and declaration files are not routes: %s", k)
		}
	}
	f := mustOK(t, svc, "list_api_routes", map[string]any{"query": "users", "limit": 1})
	if f["total"] != float64(2) || f["nextCursor"] == "" {
		t.Errorf("filter+page = %v", f)
	}
}

func TestListCollectionsActionsAndLookup(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx":   "export default function P() { return <h1>x</h1>; }",
		"src/content/blog/a.md": "---\ntitle: A\n---\nbody\n",
		"src/content/blog/b.md": "---\ntitle: B\n---\nbody\n",
		"src/actions/todo.ts":   "'use server';\nexport async function addTodo(fd) { return 1; }\nexport const removeTodo = async (id) => id;\n",
		"src/actions/plain.ts":  "export function notAnAction() {}\n",
	})
	svc.cfg.Content = map[string]any{"blog": map[string]any{"dir": "src/content/blog", "schema": map[string]any{
		"title": map[string]any{"type": "string", "required": true},
	}}}
	c := mustOK(t, svc, "list_collections", nil)
	var blog map[string]any
	for _, col := range list(t, c, "collections") {
		if asMap(col)["name"] == "blog" {
			blog = asMap(col)
		}
	}
	if blog == nil || blog["entries"] != float64(2) || blog["source"] != "krate.config" {
		t.Fatalf("blog = %v in %v", blog, c)
	}
	schema := list(t, blog, "schema")
	if asMap(schema[0])["name"] != "title" || asMap(schema[0])["required"] != true {
		t.Errorf("schema = %v", schema)
	}

	a := mustOK(t, svc, "list_actions", nil)
	if a["total"] != float64(2) {
		t.Fatalf("actions = %v", a)
	}
	first := asMap(list(t, a, "actions")[0])
	if first["export"] != "addTodo" || first["source"] != "src/actions/todo.ts" || !strings.HasPrefix(first["id"].(string), "a") {
		t.Errorf("first action = %v", first)
	}
	id := first["id"].(string)
	got := mustOK(t, svc, "get_server_action_by_id", map[string]any{"id": "/__krate/action/" + id})
	if got["export"] != "addTodo" || got["line"] != float64(2) || got["endpoint"] != "/__krate/action/"+id {
		t.Errorf("lookup = %v", got)
	}
	if msg := mustFail(t, svc, "get_server_action_by_id", map[string]any{"id": "adeadbeef"}); !strings.Contains(msg, "no server action") {
		t.Errorf("unknown id: %s", msg)
	}
	if msg := mustRPCError(t, svc, "get_server_action_by_id", nil); !strings.Contains(msg, "id") {
		t.Errorf("missing id: %s", msg)
	}
	q := mustOK(t, svc, "list_actions", map[string]any{"query": "remove"})
	if q["total"] != float64(1) {
		t.Errorf("query = %v", q)
	}
}

func TestEnvCheckNeverReturnsValues(t *testing.T) {
	const secret = "s3cr3t-value-DO-NOT-LEAK"
	const plain = "plain-visible-looking-value"
	svc := newTestService(t, map[string]string{
		".env":       "DATABASE_URL=" + secret + "\nSTRIPE_SECRET=" + secret + "\nUNUSED_FLAG=" + plain + "\nKRATE_PUBLIC_API_KEY=" + secret + "\nKRATE_PUBLIC_SITE=" + plain + "\n# COMMENTED=1\n",
		".env.local": "OVERRIDE=" + plain + "\n",
		"src/pages/index.tsx": `const site = process.env.KRATE_PUBLIC_SITE;
export default function P() { return <h1>{import.meta.env.KRATE_PUBLIC_API_KEY}{process.env.NOT_PUBLIC}</h1>; }
`,
		"src/api/db.ts": "const url = process.env.DATABASE_URL; const k = process.env['STRIPE_SECRET']; const m = process.env.MISSING_ONE;\nexport function GET() { return new Response(url + k + m); }\n",
	})
	_, res := invoke(t, svc, "env_check", nil)
	raw := res.Content[0].Text
	if strings.Contains(raw, secret) || strings.Contains(raw, plain) {
		t.Fatalf("env_check leaked a value:\n%s", raw)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(raw), &m)
	if !contains(strs(list(t, m, "missing")), "MISSING_ONE") {
		t.Errorf("missing = %v", m["missing"])
	}
	if !contains(strs(list(t, m, "missing")), "NOT_PUBLIC") {
		t.Errorf("NOT_PUBLIC is neither declared nor set: %v", m["missing"])
	}
	if got := strs(list(t, m, "secretExposure")); len(got) != 1 || got[0] != "KRATE_PUBLIC_API_KEY" {
		t.Errorf("secretExposure = %v", got)
	}
	unused := strs(list(t, m, "unused"))
	if !contains(unused, "UNUSED_FLAG") || !contains(unused, "OVERRIDE") || contains(unused, "DATABASE_URL") || contains(unused, "COMMENTED") {
		t.Errorf("unused = %v", unused)
	}
	var db map[string]any
	for _, u := range list(t, m, "used") {
		if asMap(u)["name"] == "DATABASE_URL" {
			db = asMap(u)
		}
	}
	if db == nil || db["declared"] != true {
		t.Fatalf("DATABASE_URL = %v", db)
	}
	loc := asMap(list(t, db, "locations")[0])
	if loc["file"] != "src/api/db.ts" || loc["line"] != float64(1) {
		t.Errorf("location = %v", loc)
	}
	// Variables set in the shell count as provided, but only by name.
	t.Setenv("MISSING_ONE", "shell-value-hidden")
	_, res2 := invoke(t, svc, "env_check", nil)
	if strings.Contains(res2.Content[0].Text, "shell-value-hidden") {
		t.Fatal("a shell value leaked")
	}
	var m2 map[string]any
	_ = json.Unmarshal([]byte(res2.Content[0].Text), &m2)
	if contains(strs(list(t, m2, "missing")), "MISSING_ONE") {
		t.Error("a variable set in the shell is not missing")
	}
}

func TestEnvCheckRefusesWithMalformedPolicy(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }"})
	writePolicy(t, svc, `{not json`)
	if msg := mustFail(t, svc, "env_check", nil); !strings.Contains(msg, "malformed") {
		t.Errorf("env_check must fail closed: %s", msg)
	}
}

func TestGetConfigReportsSourcesAndRedactsSecrets(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }",
		"krate.config.ts":     "export default {}\n",
	})
	svc.cfg.SEO.SiteName = "Shop"
	svc.cfg.Plugins = nil
	m := mustOK(t, svc, "get_config", nil)
	entries := map[string]map[string]any{}
	for _, e := range list(t, m, "entries") {
		em := asMap(e)
		entries[em["key"].(string)] = em
	}
	if e := entries["seo.siteName"]; e == nil || e["source"] != "krate.config.ts" || e["value"] != "Shop" {
		t.Errorf("seo.siteName = %v", e)
	}
	if e := entries["pagesDir"]; e == nil || e["source"] != "default" || e["value"] != "src/pages" {
		t.Errorf("pagesDir must be relative and default: %v", e)
	}
	if s, _ := json.Marshal(m); strings.Contains(string(s), svc.root) {
		t.Errorf("absolute project path leaked: %s", s)
	}
	only := mustOK(t, svc, "get_config", map[string]any{"onlyChanged": true})
	for _, e := range list(t, only, "entries") {
		if asMap(e)["source"] == "default" {
			t.Errorf("onlyChanged returned a default: %v", e)
		}
	}
	pre := mustOK(t, svc, "get_config", map[string]any{"prefix": "seo."})
	for _, e := range list(t, pre, "entries") {
		if !strings.HasPrefix(asMap(e)["key"].(string), "seo.") {
			t.Errorf("prefix filter: %v", e)
		}
	}
	paged := mustOK(t, svc, "get_config", map[string]any{"limit": 5})
	if len(list(t, paged, "entries")) != 5 || paged["nextCursor"] == "" {
		t.Errorf("paging = %v", paged["nextCursor"])
	}
	if sensitiveArgKey("deploy.token") != true {
		t.Error("sensitive key detection")
	}
}

func TestGetProjectMetadata(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }",
		"src/pages/about.tsx": "export default function P() { return <h1>x</h1>; }",
		"src/api/a.ts":        "export default function h() {}\n",
		"middleware.ts":       "export function middleware() {}\n",
		"package.json":        `{"name":"shop","version":"1.2.3","scripts":{"build":"krate build","dev":"krate dev"},"dependencies":{"@krate/core":"^0.5.0","react":"^18"}}`,
		"pnpm-lock.yaml":      "",
	})
	m := mustOK(t, svc, "get_project_metadata", nil)
	pkg := asMap(m["package"])
	if pkg["name"] != "shop" || pkg["version"] != "1.2.3" || strings.Join(strs(list(t, pkg, "scripts")), ",") != "build,dev" {
		t.Errorf("package = %v", pkg)
	}
	if m["packageManager"] != "pnpm" || m["root"] != "." || m["middleware"] != "middleware.ts" || m["apiRoutes"] != float64(1) {
		t.Errorf("metadata = %v", m)
	}
	if asMap(m["routes"])["count"] != float64(2) {
		t.Errorf("routes = %v", m["routes"])
	}
	fw := asMap(m["frameworkPackages"])
	if asMap(fw["@krate/core"])["range"] != "^0.5.0" {
		t.Errorf("framework packages = %v", fw)
	}
	if asMap(m["devServer"])["running"] != false || asMap(m["build"])["built"] != false {
		t.Errorf("state = %v / %v", m["devServer"], m["build"])
	}
	if s, _ := json.Marshal(m); strings.Contains(string(s), svc.root) {
		t.Errorf("absolute path leaked: %s", s)
	}
	pol := asMap(m["mcpPolicy"])
	if pol["readOnlyMode"] != false {
		t.Errorf("policy = %v", pol)
	}
}

func TestBundleReportSizesAssetsAndFlagsBudgets(t *testing.T) {
	svc := newBuiltService(t, builtFixture)
	m := mustOK(t, svc, "bundle_report", map[string]any{"limit": 1, "topAssets": 2})
	if m["routeCount"].(float64) < 2 || len(list(t, m, "routes")) != 1 || m["nextCursor"] == "" {
		t.Fatalf("bundle_report = %v", m)
	}
	r := asMap(list(t, m, "routes")[0])
	if r["route"] != "/" || r["jsBytes"].(float64) <= 0 {
		t.Errorf("heaviest route should be the interactive home page: %v", r)
	}
	if len(list(t, m, "largestAssets")) == 0 {
		t.Error("no largest assets")
	}
	for _, a := range list(t, m, "largestAssets") {
		am := asMap(a)
		if am["gzipBytes"].(float64) <= 0 || am["routes"].(float64) < 1 {
			t.Errorf("asset = %v", am)
		}
	}
	// A tiny budget flags the interactive route.
	svc.cfg.Checks = map[string]any{"budget": map[string]any{"js": 0.25}}
	over := mustOK(t, svc, "bundle_report", nil)
	if !contains(strs(list(t, over, "routesOverBudget")), "/") {
		t.Errorf("routesOverBudget = %v", over["routesOverBudget"])
	}
	if asMap(over["budgets"])["jsBytes"] != float64(256) {
		t.Errorf("budgets = %v", over["budgets"])
	}
	empty := newTestService(t, builtFixture)
	if msg := mustFail(t, empty, "bundle_report", nil); !strings.Contains(msg, "no build output") {
		t.Errorf("unbuilt: %s", msg)
	}
}

func TestCompatCheckUsesPackageMetadataAndDocs(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx":                    "export default function P() { return <h1>x</h1>; }",
		"node_modules/uses-react/package.json":   `{"name":"uses-react","version":"2.0.0","peerDependencies":{"react":">=18"},"main":"i.js"}`,
		"node_modules/native-thing/package.json": `{"name":"native-thing","version":"1.0.0","gypfile":true}`,
		"node_modules/plain/package.json":        `{"name":"plain","version":"3.1.0","type":"module","exports":"./i.js"}`,
	})
	r := mustOK(t, svc, "compat_check", map[string]any{"package": "uses-react"})
	if r["verdict"] != "island" || r["installed"] != "2.0.0" {
		t.Errorf("react package = %v", r)
	}
	n := mustOK(t, svc, "compat_check", map[string]any{"package": "native-thing"})
	if n["verdict"] != "unsupported-in-browser" {
		t.Errorf("native = %v", n)
	}
	p := mustOK(t, svc, "compat_check", map[string]any{"package": "plain"})
	if p["verdict"] != "plain-package" {
		t.Errorf("plain = %v", p)
	}
	z := mustOK(t, svc, "compat_check", map[string]any{"package": "zustand"})
	// The docs are generated, so a test run without `pnpm sync:docs` has none.
	if len(kratedocs.Docs()) > 0 && (len(list(t, z, "docs")) == 0 || z["verdict"] != "documented") {
		t.Errorf("zustand is documented in the compatibility docs: %v", z)
	}
	if msg := mustFail(t, svc, "compat_check", map[string]any{"package": "../../etc"}); !strings.Contains(msg, "npm package name") {
		t.Errorf("path-like package: %s", msg)
	}
}
