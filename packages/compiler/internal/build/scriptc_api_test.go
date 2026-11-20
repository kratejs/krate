package build

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

var defaultImportRe = regexp.MustCompile(`(?m)^\s*import\s+[A-Za-z_$][\w$]*\s*(,|from)`)

func TestScriptcEntryUsesNamedImportsOnly(t *testing.T) {
	dir := filepath.Join("proj", ".krate", "api-scriptc")
	routes := []scriptcRoute{
		{file: "users/[id].js", source: filepath.Join("proj", "src", "api", "users", "[id].ts"), methods: []string{"GET", "POST"}},
		{file: "index.js", source: filepath.Join("proj", "src", "api", "index.ts"), methods: []string{"GET"}},
	}
	src := scriptcEntrySource(dir, routes)
	for _, want := range []string{
		`import { GET as r0_GET, POST as r0_POST } from "../../src/api/users/[id].ts";`,
		`import { GET as r1_GET } from "../../src/api/index.ts";`,
		`addRoute("users/[id].js", [{ name: "id", index: 1, kind: 0 }], "GET, HEAD, POST, OPTIONS")`,
		`route0.handlers.set("POST", r0_POST as unknown as Handler);`,
		`addRoute("index.js", [], "GET, HEAD, OPTIONS")`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("entry lacks %q:\n%s", want, src)
		}
	}
	if m := defaultImportRe.FindString(src); m != "" {
		t.Errorf("default import in the entry: %q", m)
	}
	for _, banned := range []string{"export default", "eval(", "new Function", "import(", "require(", "node:vm", "node:cluster", "AsyncLocalStorage"} {
		if strings.Contains(src, banned) {
			t.Errorf("the entry contains %q, which scriptc does not compile", banned)
		}
	}
	if !strings.Contains(scriptcPackageJSON, `"type": "module"`) {
		t.Errorf("the entry's package.json must set type module: %s", scriptcPackageJSON)
	}
	var pkg map[string]any
	if err := json.Unmarshal([]byte(scriptcPackageJSON), &pkg); err != nil || pkg["type"] != "module" {
		t.Errorf("package.json: %v %v", pkg, err)
	}
}

func TestScriptcCheckEntryCallsEachHandler(t *testing.T) {
	src := scriptcCheckSource(filepath.Join("p", "check"), scriptcRoute{file: "a.js", source: filepath.Join("p", "src", "api", "a.ts"), methods: []string{"GET"}})
	if !strings.Contains(src, "handler(probe") || !strings.Contains(src, `from "../src/api/a.ts"`) {
		t.Errorf("check entry:\n%s", src)
	}
	if defaultImportRe.MatchString(src) {
		t.Errorf("default import in the check entry:\n%s", src)
	}
}

func TestScriptcParamSpec(t *testing.T) {
	cases := map[string]string{
		"json.js":              "[]",
		"index.js":             "[]",
		"users/index.js":       "[]",
		"users/[id].js":        `[{ name: "id", index: 1, kind: 0 }]`,
		"a/[x]/b/[y].js":       `[{ name: "x", index: 1, kind: 0 }, { name: "y", index: 3, kind: 0 }]`,
		"blog/[...slug].js":    `[{ name: "slug", index: 1, kind: 1 }]`,
		"[[...all]].js":        `[{ name: "all", index: 0, kind: 2 }]`,
		"docs/[[...path]].js":  `[{ name: "path", index: 1, kind: 2 }]`,
		"[org]/[...rest]/x.js": `[{ name: "org", index: 0, kind: 0 }, { name: "rest", index: 1, kind: 1 }]`,
	}
	for file, want := range cases {
		if got := scriptcParamSpec(file); got != want {
			t.Errorf("scriptcParamSpec(%q) = %s, want %s", file, got, want)
		}
	}
}

func TestScriptcAllow(t *testing.T) {
	cases := []struct {
		methods []string
		want    string
	}{
		{[]string{"GET"}, "GET, HEAD, OPTIONS"},
		{[]string{"POST", "GET"}, "GET, HEAD, POST, OPTIONS"},
		{[]string{"DELETE"}, "DELETE, OPTIONS"},
		{[]string{"GET", "HEAD", "OPTIONS"}, "GET, HEAD, OPTIONS"},
	}
	for _, c := range cases {
		if got := scriptcAllow(c.methods); got != c.want {
			t.Errorf("scriptcAllow(%v) = %q, want %q", c.methods, got, c.want)
		}
	}
}

func TestScriptcMethods(t *testing.T) {
	cases := []struct {
		name, src string
		methods   []string
		reason    string
	}{
		{"functions", "export async function GET() {}\nexport function POST() {}", []string{"GET", "POST"}, ""},
		{"consts", "export const PUT = () => new Response('x');\nexport const maxDuration = 5;", []string{"PUT"}, ""},
		{"specifiers", "const a = () => 1;\nexport { a as DELETE, a as helper };", []string{"DELETE"}, ""},
		{"reexport", "export { GET } from './other';", []string{"GET"}, ""},
		{"default", "export default function h() {}\nexport function GET() {}", nil, "default export"},
		{"star", "export * from './other';\nexport function GET() {}", nil, "export *"},
		{"none", "export const config = {};", nil, "no GET"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "r.ts")
			if err := os.WriteFile(file, []byte(c.src), 0o644); err != nil {
				t.Fatal(err)
			}
			methods, reason := scriptcMethods(file)
			if strings.Join(methods, ",") != strings.Join(c.methods, ",") {
				t.Errorf("methods = %v, want %v", methods, c.methods)
			}
			if (c.reason == "") != (reason == "") || !strings.Contains(reason, c.reason) {
				t.Errorf("reason = %q, want it to contain %q", reason, c.reason)
			}
		})
	}
}

func scriptcFiles(extra map[string]string) map[string]string {
	files := map[string]string{
		"package.json":                        `{"type":"module"}`,
		"src/api/ok.ts":                       "export async function GET() { return Response.json({ via: 'ok' }); }\nexport async function POST(req: Request) { return new Response((await req.text()).toUpperCase(), { status: 201 }); }",
		"src/api/users/[id].ts":               "export async function GET(request: Request, { params }: { params: { id: string } }) { return Response.json({ id: params.id }); }",
		"src/api/blog/[...slug].ts":           "export function GET(request: Request, { params }: { params: { slug: string } }) { return Response.json({ slug: params.slug }); }",
		"src/api/blocked.ts":                  "export async function GET() {\n  const x = 1; // scriptc-blocker\n  return Response.json({ x });\n}",
		"src/api/dflt.ts":                     "export default function handler() { return Response.json({}); }",
		"src/api/ext.ts":                      "import { tag } from \"@fake/ext\";\nexport async function GET() { return Response.json({ tag }); }",
		"node_modules/@fake/ext/package.json": `{"name":"@fake/ext","version":"1.0.0","main":"index.js"}`,
		"node_modules/@fake/ext/index.js":     `exports.tag = "ext";`,
	}
	for k, v := range extra {
		if v == "" {
			delete(files, k)
		} else {
			files[k] = v
		}
	}
	return files
}

func scriptcBuilder(t *testing.T, files map[string]string, edit func(*config.Config)) (*Builder, string) {
	t.Helper()
	b, root := compatProject(t, files, func(c *config.Config) {
		c.SSR.APIRuntime = "scriptc"
		c.ServerExternalPackages = []string{"@fake/ext"}
		if edit != nil {
			edit(c)
		}
	})
	return b, root
}

func routeSources(t *testing.T, root string) []string {
	t.Helper()
	files, err := apiRouteSourceFiles(filepath.Join(root, "src", "api"))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func readScriptcManifest(t *testing.T, root string) map[string]scriptcManifestItem {
	t.Helper()
	m, ok := loadScriptcManifest(root)
	if !ok {
		t.Fatal("no .krate/api-scriptc.json")
	}
	out := map[string]scriptcManifestItem{}
	for _, r := range m.Routes {
		out[r.File] = r
	}
	return out
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// captureStderr runs f and returns what it wrote to stderr. Child processes
// the server starts inherit the pipe and keep it open, so it does not wait for
// the end of the stream.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	var buf safeBuffer
	go func() { _, _ = io.Copy(&buf, r) }()
	func() {
		defer func() { os.Stderr = old }()
		f()
	}()
	time.Sleep(150 * time.Millisecond)
	_ = w.Close()
	return buf.String()
}

// Routes scriptc accepts are compiled together; each other route stays on
// the runtime that served it before and is reported with a KR7238 warning.
func TestScriptcPartitionAndFallback(t *testing.T) {
	logPath := fakeScriptcOnPath(t)
	b, root := scriptcBuilder(t, scriptcFiles(nil), nil)
	if err := b.CompileAPIRoutes(routeSources(t, root)); err != nil {
		t.Fatal(err)
	}
	var err error
	stderr := captureStderr(t, func() { err = b.buildScriptcAPI() })
	if err != nil {
		t.Fatalf("buildScriptcAPI: %v", err)
	}
	m := readScriptcManifest(t, root)
	want := map[string]string{
		"ok.js": scriptcRouteScriptc, "users/[id].js": scriptcRouteScriptc, "blog/[...slug].js": scriptcRouteScriptc,
		"blocked.js": scriptcRouteQuickJS, "dflt.js": scriptcRouteQuickJS, "ext.js": scriptcRouteNode,
	}
	for file, rt := range want {
		if m[file].Runtime != rt {
			t.Errorf("%s runs on %q, want %q (%+v)", file, m[file].Runtime, rt, m[file])
		}
	}
	if len(m) != len(want) {
		t.Errorf("manifest has %d routes, want %d: %+v", len(m), len(want), m)
	}
	if m["ok.js"].Source != "src/api/ok.ts" {
		t.Errorf("source = %q", m["ok.js"].Source)
	}
	mf, _ := loadScriptcManifest(root)
	if mf.Executable == "" {
		t.Fatal("the manifest names no executable")
	}
	if _, err := os.Stat(scriptcServerPath(root)); err != nil {
		t.Errorf("the server was not built: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".krate", "main.ll")); err == nil {
		t.Error("scriptc's main.ll was left in .krate")
	}

	// The blocker is reported at its file:line with the scriptc code and hint.
	for _, frag := range []string{"KR7238", "src/api/blocked.ts:2:", "SC9999", "rewrite it as the hint suggests", "src/api/dflt.ts:1:1", "default export"} {
		if !strings.Contains(stderr, frag) {
			t.Errorf("stderr lacks %q:\n%s", frag, stderr)
		}
	}
	if strings.Contains(stderr, "ext.js") {
		t.Errorf("a route that needs Node is not a scriptc fallback:\n%s", stderr)
	}

	// The final entry holds only the accepted routes, with named imports.
	entry, err := os.ReadFile(filepath.Join(root, ".krate", "api-scriptc", "main.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"ok.ts", "users/[id].ts", "blog/[...slug].ts"} {
		if !strings.Contains(string(entry), "../../src/api/"+rel) {
			t.Errorf("entry lacks %s", rel)
		}
	}
	for _, rel := range []string{"blocked.ts", "dflt.ts", "ext.ts"} {
		if strings.Contains(string(entry), rel) {
			t.Errorf("entry has the route %s that must fall back", rel)
		}
	}
	if defaultImportRe.Match(entry) {
		t.Errorf("default import in the entry")
	}
	pkg, err := os.ReadFile(filepath.Join(root, ".krate", "api-scriptc", "package.json"))
	if err != nil || !bytes.Contains(pkg, []byte(`"type": "module"`)) {
		t.Errorf("package.json = %s, %v", pkg, err)
	}

	// Coverage ran on the whole entry, then per route, then on the rest; the
	// server was built once.
	var coverage, builds int
	for _, l := range readLines(t, logPath) {
		switch {
		case strings.HasPrefix(l, "coverage"):
			coverage++
		case strings.HasPrefix(l, "build"):
			builds++
		}
	}
	if coverage != 1+4+1 || builds != 1 {
		t.Errorf("coverage runs = %d, builds = %d, want 6 and 1:\n%s", coverage, builds, strings.Join(readLines(t, logPath), "\n"))
	}
}

func TestScriptcCleanCoverageRunsOnce(t *testing.T) {
	logPath := fakeScriptcOnPath(t)
	b, root := scriptcBuilder(t, scriptcFiles(map[string]string{"src/api/blocked.ts": "", "src/api/dflt.ts": "", "src/api/ext.ts": ""}), nil)
	if err := b.CompileAPIRoutes(routeSources(t, root)); err != nil {
		t.Fatal(err)
	}
	if err := b.buildScriptcAPI(); err != nil {
		t.Fatal(err)
	}
	var coverage int
	for _, l := range readLines(t, logPath) {
		if strings.HasPrefix(l, "coverage") {
			coverage++
		}
	}
	if coverage != 1 {
		t.Errorf("a clean entry needs one coverage run, got %d", coverage)
	}
	for file, it := range readScriptcManifest(t, root) {
		if it.Runtime != scriptcRouteScriptc {
			t.Errorf("%s = %+v", file, it)
		}
	}
}

func TestScriptcStrictMakesFallbackAnError(t *testing.T) {
	fakeScriptcOnPath(t)
	b, root := scriptcBuilder(t, scriptcFiles(nil), func(c *config.Config) { c.SSR.ScriptcStrict = true })
	if err := b.CompileAPIRoutes(routeSources(t, root)); err != nil {
		t.Fatal(err)
	}
	err := b.buildScriptcAPI()
	if err == nil {
		t.Fatal("strict mode must fail the build when a route falls back")
	}
	text := diag.FormatDiagnostics(unwrapJoined(err))
	for _, frag := range []string{"KR7238", "src/api/blocked.ts:2", "src/api/dflt.ts", "ssr.scriptcStrict"} {
		if !strings.Contains(text, frag) {
			t.Errorf("error lacks %q:\n%s", frag, text)
		}
	}
	if _, statErr := os.Stat(scriptcServerPath(root)); statErr == nil {
		t.Error("a strict failure must not leave a server built")
	}
}

func unwrapJoined(err error) []error {
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		return j.Unwrap()
	}
	return []error{err}
}

func TestScriptcToolchainMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("SCRIPTC_LINKER", "")
	b, root := scriptcBuilder(t, scriptcFiles(nil), nil)
	if err := b.CompileAPIRoutes(routeSources(t, root)); err != nil {
		t.Fatal(err)
	}
	err := b.buildScriptcAPI()
	var d diag.Diagnostic
	if !errors.As(err, &d) || d.Code != diag.CodeScriptcToolchain {
		t.Fatalf("want KR7236, got %v", err)
	}
	if !strings.Contains(d.Message, "ssr.apiRuntime") || strings.Contains(d.Message, "native.host") || !strings.Contains(d.Hint, "npm install -g scriptc") {
		t.Errorf("message %q, hint %q", d.Message, d.Hint)
	}
}

// With no route to compile there is nothing to build, so missing tools are
// not an error; the manifest still says where each route runs.
func TestScriptcNoEligibleRouteNeedsNoToolchain(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	b, root := scriptcBuilder(t, map[string]string{
		"package.json":  `{"type":"module"}`,
		"src/api/d.ts":  "export default function h() { return Response.json({}); }",
		"src/api/ok.ts": "export const config = {};",
	}, nil)
	if err := b.CompileAPIRoutes(routeSources(t, root)); err != nil {
		t.Fatal(err)
	}
	var err error
	captureStderr(t, func() { err = b.buildScriptcAPI() })
	if err != nil {
		t.Fatal(err)
	}
	m, ok := loadScriptcManifest(root)
	if !ok || m.Executable != "" {
		t.Fatalf("manifest = %+v, %v", m, ok)
	}
	for _, it := range m.Routes {
		if it.Runtime != scriptcRouteQuickJS || it.Reason == "" {
			t.Errorf("%+v", it)
		}
	}
	if _, err := os.Stat(scriptcServerPath(root)); err == nil {
		t.Error("no server should exist")
	}
}

func TestScriptcOnlyForProductionBuildsWithTheOption(t *testing.T) {
	fakeScriptcOnPath(t)
	for name, edit := range map[string]func(*Builder){
		"dev":        func(b *Builder) { b.DevMode = true },
		"quickjs":    func(b *Builder) { b.Cfg.SSR.APIRuntime = "quickjs" },
		"default":    func(b *Builder) { b.Cfg.SSR.APIRuntime = "" },
		"nodeopt":    func(b *Builder) { b.Cfg.SSR.APIRuntime = "node" },
		"no-scriptc": func(b *Builder) { b.Cfg.SSR.APIRuntime = "bun" },
	} {
		b, root := scriptcBuilder(t, scriptcFiles(nil), nil)
		edit(b)
		if err := b.buildScriptcAPI(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := os.Stat(scriptcManifestPath(root)); err == nil {
			t.Errorf("%s: wrote a scriptc manifest", name)
		}
	}
}

// scriptc leaves the routes' QuickJS bundles in place: a route that falls
// back has one, and a build with the option is otherwise a default build.
func TestScriptcKeepsEmbeddedBundles(t *testing.T) {
	fakeScriptcOnPath(t)
	b, root := scriptcBuilder(t, scriptcFiles(nil), nil)
	if err := b.CompileAPIRoutes(routeSources(t, root)); err != nil {
		t.Fatal(err)
	}
	qdir := apiBundleDir(b.Cfg.OutDir)
	for _, name := range []string{"ok.js", "blocked.js", "dflt.js", "users/[id].js"} {
		if !artifactExists(t, qdir, name) {
			t.Errorf("no embedded bundle for %s", name)
		}
	}
	if !embeddedRuntime("scriptc") || isNodeFamilyRuntime("scriptc") {
		t.Error("scriptc must be an embedded runtime for bundling and not a Node sidecar")
	}
	_ = root
}

func TestLoadScriptcServePlan(t *testing.T) {
	root := t.TempDir()
	if p := loadScriptcServePlan(root); p.warning == "" || !strings.Contains(p.warning, "krate build") {
		t.Errorf("no manifest: %+v", p)
	}
	write := func(m scriptcManifest) {
		data, _ := json.Marshal(m)
		if err := os.MkdirAll(filepath.Join(root, ".krate"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(scriptcManifestPath(root), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(scriptcManifest{Version: 1, Routes: []scriptcManifestItem{{File: "a.js", Runtime: scriptcRouteQuickJS}}})
	if p := loadScriptcServePlan(root); p.warning != "" || p.exe != "" || len(p.routes) != 0 {
		t.Errorf("no compiled routes: %+v", p)
	}
	write(scriptcManifest{Version: 1, Executable: "api-scriptc-server", Routes: []scriptcManifestItem{{File: "a.js", Runtime: scriptcRouteScriptc}, {File: "b.js", Runtime: scriptcRouteNode}}})
	if p := loadScriptcServePlan(root); !strings.Contains(p.warning, "missing") {
		t.Errorf("server missing: %+v", p)
	}
	if err := os.WriteFile(scriptcServerPath(root), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := loadScriptcServePlan(root)
	if p.warning != "" || p.exe != scriptcServerPath(root) || !p.routes["a.js"] || p.routes["b.js"] {
		t.Errorf("plan = %+v", p)
	}
}

// The proxy to the compiled server sends the secret and the route the host
// resolved (replacing a client's header), enforces the route budget with a
// 504 and answers 503 while the server is not up.
func TestScriptcSidecarProxy(t *testing.T) {
	var seen http.Header
	slow := make(chan struct{})
	t.Cleanup(func() { close(slow) })
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/slow" {
			select {
			case <-slow:
			case <-r.Context().Done():
			}
			return
		}
		seen = r.Header.Clone()
		w.Header().Set("X-Up", "1")
		_, _ = io.WriteString(w, "from-server")
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	port, _ := strconv.Atoi(u.Port())

	s := newScriptcSidecar("unused", port, "sekret", nil, map[string]time.Duration{"slow.js": 100 * time.Millisecond})
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file := strings.TrimPrefix(r.URL.Path, "/api/")
		s.ServeRoute(w, r, file+".js")
	}))
	defer front.Close()

	s.ready.timeout = 50 * time.Millisecond
	resp, err := http.Get(front.URL + "/api/ok")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("before the server is up: %d, want 503", resp.StatusCode)
	}

	s.ready.markUp(s.ready.begin())
	req, _ := http.NewRequest("GET", front.URL+"/api/ok", nil)
	req.Header.Set(scriptcRouteHeader, "evil.js")
	req.Header.Set(internalSecretHeader, "client-secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "from-server" || resp.Header.Get("X-Up") != "1" {
		t.Fatalf("proxied response: %d %q", resp.StatusCode, body)
	}
	if seen.Get(internalSecretHeader) != "sekret" {
		t.Errorf("the server saw secret %q", seen.Get(internalSecretHeader))
	}
	if seen.Get(scriptcRouteHeader) != "ok.js" {
		t.Errorf("the server saw route %q, want the host's ok.js", seen.Get(scriptcRouteHeader))
	}

	start := time.Now()
	resp, err = http.Get(front.URL + "/api/slow")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusGatewayTimeout || !strings.Contains(string(body), "Gateway Timeout") {
		t.Errorf("a route over its budget: %d %q, want 504", resp.StatusCode, body)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("the budget took %s to apply", took)
	}
}

func TestScriptcSidecarSupervision(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("needs a POSIX shell")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "starts")
	script := filepath.Join(dir, "server.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho start >> '"+marker+"'\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := newScriptcSidecar(script, 1, "s", []string{"KRATE_TEST_ENV=1"}, nil)
	done := make(chan struct{})
	go func() { s.Run(); close(done) }()
	deadline := time.Now().Add(10 * time.Second)
	for len(readLines(t, marker)) < 2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if n := len(readLines(t, marker)); n < 2 {
		t.Fatalf("the server was started %d time(s), want a restart after it exited", n)
	}
	s.Stop()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after Stop")
	}
}

func TestScriptcDevNoticeOnce(t *testing.T) {
	scriptcDevNote = sync.Once{}
	var out strings.Builder
	SetOutput(&out)
	t.Cleanup(func() { SetOutput(os.Stdout) })
	scriptcDevNotice()
	scriptcDevNotice()
	if strings.Count(out.String(), "embedded QuickJS runtime") != 1 {
		t.Errorf("the dev notice must print once:\n%s", out.String())
	}
}
