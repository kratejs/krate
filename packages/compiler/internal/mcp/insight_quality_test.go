package mcp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditPageFiltersByCategorySeverityAndRoute(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function P() { return <main><h1>Home</h1><img src=\"/a.png\" /></main>; }\n",
		"src/pages/about.tsx": "export default function P() { return <main><h1>About</h1></main>; }\n",
	})
	all := mustOK(t, svc, "audit_page", nil)
	if all["total"].(float64) == 0 {
		t.Fatalf("the default rules must find something on pages with no title or description: %v", all)
	}
	cats := asMap(all["byCategory"])
	if cats["seo"] == nil {
		t.Fatalf("byCategory = %v", cats)
	}
	seo := mustOK(t, svc, "audit_page", map[string]any{"category": "seo"})
	for _, f := range list(t, seo, "findings") {
		if asMap(f)["category"] != "seo" {
			t.Errorf("category filter leaked: %v", f)
		}
	}
	route := mustOK(t, svc, "audit_page", map[string]any{"route": "/about"})
	for _, f := range list(t, route, "findings") {
		if asMap(f)["route"] != "/about" {
			t.Errorf("route filter leaked: %v", f)
		}
	}
	warn := mustOK(t, svc, "audit_page", map[string]any{"severity": "warning", "limit": 1})
	if len(list(t, warn, "findings")) != 1 || warn["nextCursor"] == "" {
		t.Errorf("limit/cursor = %v", warn)
	}
	for _, bad := range []map[string]any{{"category": "x"}, {"severity": "x"}} {
		if msg := mustFail(t, svc, "audit_page", bad); !strings.Contains(msg, "must be one of") {
			t.Errorf("%v: %s", bad, msg)
		}
	}
	if len(list(t, all, "knownRules")) == 0 {
		t.Error("knownRules missing")
	}
}

func TestExplainDiagnostic(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }"})
	m := mustOK(t, svc, "explain_diagnostic", map[string]any{"code": "kr5002"})
	match := asMap(m["match"])
	if match["code"] != "KR5002" || match["title"] == "" || match["explanation"] == "" {
		t.Errorf("match = %v", match)
	}
	if !strings.Contains(match["safeFix"].(string), "apply_safe_fixes") {
		t.Errorf("KR5002 has a safe codemod: %v", match)
	}
	nofix := mustOK(t, svc, "explain_diagnostic", map[string]any{"code": "KR7115"})
	if asMap(nofix["match"])["safeFix"] != nil {
		t.Errorf("KR7115 has no codemod: %v", nofix)
	}
	if msg := mustFail(t, svc, "explain_diagnostic", map[string]any{"code": "KR7999"}); !strings.Contains(msg, "no registered diagnostic") || !strings.Contains(msg, "KR7") {
		t.Errorf("unknown code: %s", msg)
	}
	s := mustOK(t, svc, "explain_diagnostic", map[string]any{"query": "dependency array", "limit": 2})
	if s["total"].(float64) == 0 || len(list(t, s, "matches")) > 2 {
		t.Errorf("search = %v", s)
	}
	if msg := mustFail(t, svc, "explain_diagnostic", nil); !strings.Contains(msg, "provide code") {
		t.Errorf("no args: %s", msg)
	}
}

func TestParseTSC(t *testing.T) {
	out := "src/a.ts(3,7): error TS2322: Type 'string' is not assignable to type 'number'.\n  Type detail line\nsrc/b.tsx(10,1): warning TS6133: 'x' is declared but never used.\nFound 2 errors.\n"
	diags, other := parseTSC(out)
	if len(diags) != 2 || diags[0].File != "src/a.ts" || diags[0].Line != 3 || diags[0].Col != 7 || diags[0].Code != "TS2322" {
		t.Fatalf("diags = %+v", diags)
	}
	if !strings.Contains(diags[0].Message, "Type detail line") || diags[1].Severity != "warning" {
		t.Errorf("diags = %+v", diags)
	}
	if len(other) != 1 || other[0] != "Found 2 errors." {
		t.Errorf("other = %v", other)
	}
}

func fakeTSC(t *testing.T, svc *Service, output string, exit int) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	script := "process.stdout.write(" + jsString(output) + "); process.exit(" + string(rune('0'+exit)) + ");\n"
	writeFile(t, svc.root, "node_modules/typescript/bin/tsc", script)
}

func jsString(s string) string {
	r := strings.NewReplacer("\\", "\\\\", "'", "\\'", "\n", "\\n")
	return "'" + r.Replace(s) + "'"
}

func TestTypecheckGatingAndStructuredOutput(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }"})
	if msg := mustFail(t, svc, "typecheck", nil); !strings.Contains(msg, `"typecheck" to allowTasks`) {
		t.Fatalf("default policy: %s", msg)
	}
	writePolicy(t, svc, `{"allowTasks":["typecheck"],"readOnlyMode":true}`)
	if msg := mustFail(t, svc, "typecheck", nil); !strings.Contains(msg, "readOnlyMode") {
		t.Fatalf("readOnlyMode: %s", msg)
	}
	writePolicy(t, svc, `{"allowTasks":["typecheck"]}`)
	if msg := mustFail(t, svc, "typecheck", nil); !strings.Contains(msg, "typescript is not installed") {
		t.Fatalf("no tsc: %s", msg)
	}
	fakeTSC(t, svc, "src/pages/index.tsx(2,5): error TS2304: Cannot find name 'foo'.\nsrc/lib/x.ts(1,1): error TS2322: bad\n", 2)
	m := mustOK(t, svc, "typecheck", nil)
	if m["ok"] != false || m["errors"] != float64(2) {
		t.Fatalf("typecheck = %v", m)
	}
	d := asMap(list(t, m, "diagnostics")[0])
	if d["file"] != "src/pages/index.tsx" || d["line"] != float64(2) || d["code"] != "TS2304" {
		t.Errorf("diagnostic = %v", d)
	}
	one := mustOK(t, svc, "typecheck", map[string]any{"file": "src/lib/x.ts"})
	if len(list(t, one, "diagnostics")) != 1 {
		t.Errorf("file filter = %v", one)
	}
	pg := mustOK(t, svc, "typecheck", map[string]any{"limit": 1})
	if pg["nextCursor"] == "" {
		t.Errorf("paging = %v", pg)
	}
	fakeTSC(t, svc, "", 0)
	if ok := mustOK(t, svc, "typecheck", nil); ok["ok"] != true || ok["errors"] != float64(0) {
		t.Errorf("clean = %v", ok)
	}
}

func TestUpgradeReportsStepsAndAppliesSafeFixesAsATransaction(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx":                      "export default function P() { return <h1>x</h1>; }\n",
		"src/components/Btn.tsx":                   "export default function Btn() { return <button onclick={() => 1}>b</button>; }\n",
		"tsconfig.json":                            `{"compilerOptions":{"baseUrl":"./src","paths":{"@/*":["./src/*"]}}}`,
		"package.json":                             `{"name":"app","scripts":{"start":"krate start","check":"node scripts/x.js"}}`,
		"Dockerfile":                               "FROM node\nCMD [\"krate\", \"dev\"]\nRUN cat dist/manifest.json\nRUN node_modules/@krate/core/bin/krate build\n",
		"scripts/x.js":                             "const m = require('./dist/manifest.json');\n",
		"node_modules/@krate/core/package.json":    `{"name":"@krate/core","version":"0.4.9"}`,
		"node_modules/@krate/runtime/package.json": `{"name":"@krate/runtime","version":"0.5.0"}`,
		"tailwind.config.js":                       "module.exports = require('./x')\n",
	})
	m := mustOK(t, svc, "upgrade", map[string]any{"build": false})
	steps := map[string]map[string]any{}
	for _, s := range list(t, m, "steps") {
		sm := asMap(s)
		steps[sm["id"].(string)] = sm
	}
	for id, status := range map[string]string{
		"versions": "action", "safe-fixes": "action", "start-no-build": "manual", "dev-host": "manual",
		"manifest-path": "action", "bin-krate": "action", "tailwind-sandbox": "manual",
	} {
		if steps[id] == nil || steps[id]["status"] != status {
			t.Errorf("step %s = %v, want status %s", id, steps[id], status)
		}
	}
	if m["from"] != "0.4.9" || m["dryRun"] != true || m["pending"].(float64) < 6 {
		t.Errorf("summary = from %v pending %v", m["from"], m["pending"])
	}
	if ev := strs(list(t, steps["manifest-path"], "evidence")); len(ev) < 2 || !contains(ev, "Dockerfile:3") {
		t.Errorf("evidence should point at file:line: %v", ev)
	}
	// A dry run changes nothing.
	tsBefore, _ := os.ReadFile(filepath.Join(svc.root, "tsconfig.json"))
	btnBefore, _ := os.ReadFile(filepath.Join(svc.root, "src/components/Btn.tsx"))
	if !strings.Contains(string(tsBefore), `"./src"`) || !strings.Contains(string(btnBefore), "onclick") {
		t.Fatal("fixture changed")
	}

	applied := mustOK(t, svc, "upgrade", map[string]any{"build": false, "apply": true})
	if applied["applied"] == nil || applied["dryRun"] == true {
		t.Fatalf("apply = %v", applied)
	}
	tsAfter, _ := os.ReadFile(filepath.Join(svc.root, "tsconfig.json"))
	btnAfter, _ := os.ReadFile(filepath.Join(svc.root, "src/components/Btn.tsx"))
	if strings.Contains(string(tsAfter), `"./src"`) || !strings.Contains(string(btnAfter), "onClick") {
		t.Errorf("safe fixes were not applied:\n%s\n%s", tsAfter, btnAfter)
	}
	// The apply is one undoable transaction.
	if _, res := invoke(t, svc, "undo", map[string]any{"apply": true}); res.IsError {
		t.Fatalf("undo: %v", res.Content)
	}
	restored, _ := os.ReadFile(filepath.Join(svc.root, "src/components/Btn.tsx"))
	if string(restored) != string(btnBefore) {
		t.Error("undo did not restore the codemod")
	}
}

func TestUpgradeHonoursTheWritePolicy(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/components/Btn.tsx": "export default function Btn() { return <button onclick={() => 1}>b</button>; }\n",
		"src/pages/index.tsx":    "export default function P() { return <h1>x</h1>; }\n",
	})
	writePolicy(t, svc, `{"readOnly":["src/components/**"]}`)
	m := mustOK(t, svc, "upgrade", map[string]any{"build": false, "apply": true})
	data, _ := os.ReadFile(filepath.Join(svc.root, "src/components/Btn.tsx"))
	if strings.Contains(string(data), "onClick") {
		t.Fatalf("upgrade wrote a read-only path: %v\n%s", m, data)
	}
}

func TestUpgradeBuildStepSurfacesNewErrors(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "import X from './missing';\nexport default function P() { return <h1>x</h1>; }\n",
	})
	m := mustOK(t, svc, "upgrade", nil)
	var step map[string]any
	for _, s := range list(t, m, "steps") {
		if asMap(s)["id"] == "build-errors" {
			step = asMap(s)
		}
	}
	if step == nil || step["status"] != "action" {
		t.Fatalf("build-errors step = %v", step)
	}
	if ev := strs(list(t, step, "evidence")); len(ev) == 0 || !strings.Contains(ev[0], "src/pages/index.tsx:1") {
		t.Errorf("evidence = %v", ev)
	}
}

func TestMigrateNextjsPlansWithoutWriting(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"package.json":             `{"dependencies":{"next":"15.0.0","react":"19"}}`,
		"next.config.js":           "module.exports = {}\n",
		"app/layout.tsx":           "export default function Layout({ children }) { return <html><body>{children}</body></html>; }\n",
		"app/page.tsx":             "export default function Page() { return <h1>Home</h1>; }\n",
		"app/blog/[slug]/page.tsx": "export default function Post() { return <h1>Post</h1>; }\n",
	})
	before := snapshotTree(t, svc.root)
	m := mustOK(t, svc, "migrate_nextjs", nil)
	if m["framework"] != "nextjs" || m["fileCount"].(float64) < 3 {
		t.Fatalf("plan = %v", m)
	}
	var sawPage bool
	for _, f := range list(t, m, "files") {
		fm := asMap(f)
		if fm["source"] == "app/page.tsx" && strings.HasSuffix(fm["target"].(string), "index.tsx") {
			sawPage = true
		}
	}
	if !sawPage {
		t.Errorf("app/page.tsx must map to the index page: %v", m["files"])
	}
	if len(list(t, m, "routes")) < 2 {
		t.Errorf("routes = %v", m["routes"])
	}
	if !strings.Contains(m["apply"].(string), "krate migrate nextjs --apply") {
		t.Errorf("apply hint = %v", m["apply"])
	}
	if after := snapshotTree(t, svc.root); after != before {
		t.Errorf("the plan wrote files:\nbefore %s\nafter  %s", before, after)
	}
	pg := mustOK(t, svc, "migrate_nextjs", map[string]any{"limit": 1})
	if pg["nextCursor"] == "" {
		t.Errorf("paging = %v", pg)
	}

	plain := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }"})
	msg := mustFail(t, plain, "migrate_nextjs", nil)
	if !strings.Contains(msg, "not a Next.js project") || strings.Contains(msg, plain.root) {
		t.Errorf("non-Next project error (must not leak the root path): %s", msg)
	}
}

func snapshotTree(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !strings.Contains(p, ".krate") {
			info, _ := d.Info()
			b.WriteString(p + ":" + info.ModTime().String() + ";")
		}
		return nil
	})
	return b.String()
}

func TestBrowserToolsAreGatedAndExplainMissingPlaywright(t *testing.T) {
	svc := newBuiltService(t, builtFixture)
	if msg := mustFail(t, svc, "page_check", nil); !strings.Contains(msg, `"browser" in allowTasks`) && !strings.Contains(msg, `"browser" to allowTasks`) {
		t.Fatalf("default policy: %s", msg)
	}
	writePolicy(t, svc, `{"allowTasks":["browser"],"readOnlyMode":true}`)
	if msg := mustFail(t, svc, "browser_eval", map[string]any{"expression": "1"}); !strings.Contains(msg, "readOnlyMode") {
		t.Fatalf("readOnlyMode: %s", msg)
	}
	writePolicy(t, svc, `{"allowTasks":["browser"]}`)
	if msg := mustRPCError(t, svc, "browser_eval", nil); !strings.Contains(msg, "expression") {
		t.Errorf("expression is required: %s", msg)
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	t.Setenv("NODE_PATH", "")
	msg := mustFail(t, svc, "page_check", map[string]any{"route": "/"})
	if !strings.Contains(msg, "Playwright is not installed") || !strings.Contains(msg, "npx playwright install chromium") {
		t.Errorf("missing Playwright must come with install instructions: %s", msg)
	}
	if msg := mustFail(t, svc, "page_check", map[string]any{"route": "about"}); !strings.Contains(msg, "starting with /") {
		t.Errorf("relative route: %s", msg)
	}
}

func TestBrowserToolsDriveARealBrowserWhenPlaywrightIsPresent(t *testing.T) {
	if testing.Short() {
		t.Skip("launches Chromium")
	}
	pwRoot := findPlaywrightNodeModules()
	if pwRoot == "" {
		t.Skip("no Playwright install found (set KRATE_PLAYWRIGHT_NODE_MODULES)")
	}
	svc := newBuiltService(t, map[string]string{
		"src/pages/index.tsx": `export default function Home() { return <main><h1>Hi</h1><button id="b" onClick={() => { document.title = "clicked"; }}>go</button></main>; }
`,
	})
	writePolicy(t, svc, `{"allowTasks":["browser"]}`)
	t.Setenv("NODE_PATH", pwRoot)
	m := mustOK(t, svc, "browser_eval", map[string]any{"route": "/", "expression": "document.querySelector('h1').textContent", "screenshot": true, "aria": true})
	if m["evalResult"] != `"Hi"` || m["status"] != float64(200) {
		t.Fatalf("browser_eval = %v", m)
	}
	if !strings.Contains(m["ariaSnapshot"].(string), "heading") {
		t.Errorf("aria = %v", m["ariaSnapshot"])
	}
	if !strings.Contains(m["source"].(string), "static server") {
		t.Errorf("source = %v", m["source"])
	}
	srv := NewServer(os.Stderr, "krate", "test")
	svc.Register(srv)
	res, _ := srv.tools["page_check"].Handler(t.Context(), map[string]any{"screenshot": true})
	var image bool
	for _, c := range res.Content {
		if c.Type == "image" && c.MIMEType == "image/png" && len(c.Data) > 100 {
			image = true
		}
	}
	if !image {
		t.Error("page_check with screenshot must attach a PNG image block")
	}
	// Requests to other origins are blocked.
	ext := mustOK(t, svc, "browser_eval", map[string]any{"expression": "fetch('https://example.com/').then(() => 'reached', () => 'blocked')"})
	if ext["evalResult"] != `"blocked"` {
		t.Errorf("external fetch from the page must be blocked: %v", ext["evalResult"])
	}
	if ext["blockedExternalRequests"].(float64) < 1 {
		t.Errorf("blocked count = %v", ext["blockedExternalRequests"])
	}
}

func findPlaywrightNodeModules() string {
	if v := os.Getenv("KRATE_PLAYWRIGHT_NODE_MODULES"); v != "" {
		return v
	}
	dir, _ := os.Getwd()
	for ; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		p := filepath.Join(dir, "node_modules")
		if _, err := os.Stat(filepath.Join(p, "playwright")); err == nil {
			return p
		}
	}
	return ""
}
