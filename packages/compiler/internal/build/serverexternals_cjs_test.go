package build

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// nccEnvPackage is a stand-in for @next/env: an ncc-style CommonJS bundle
// whose exports cjs-module-lexer cannot see, so Node's ES module loader
// rejects `import { loadEnvConfig } from "ncc-env"`.
var nccEnvPackage = map[string]string{
	"node_modules/ncc-env/package.json": `{"name":"ncc-env","version":"1.0.0","main":"dist/index.js"}`,
	"node_modules/ncc-env/dist/index.js": `(()=>{"use strict";var n={};(()=>{var e=n;e.loadEnvConfig=function(dir){globalThis.__nccEnvCalls=(globalThis.__nccEnvCalls||0)+1;return{combinedEnv:{NCC_ENV_DIR:String(dir)},loadedEnvFiles:[]}}})();module.exports=n})();
`,
}

func withNccEnv(files map[string]string) map[string]string {
	for k, v := range nccEnvPackage {
		files[k] = v
	}
	return files
}

const instRegisterNccEnv = `import { loadEnvConfig } from "ncc-env";
import { mark } from "@/server/mark";
export async function register() {
  const { combinedEnv } = loadEnvConfig(process.cwd());
  mark(process.env.INST_MARKER!, "register env=" + (combinedEnv.NCC_ENV_DIR ? "ok" : "missing"));
  (globalThis as any).__krateInst = "ready";
}`

func TestNodeImportTarget(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		// Conditions are matched in key order: "import" before "require".
		"dual/package.json":     `{"exports":{".":{"types":"./x.d.ts","import":"./esm/i.js","require":"./cjs/i.cjs"},"./feature/*":{"node":"./lib/*.cjs","default":"./lib/*.mjs"},"./feature/special":"./special.js","./data.json":"./data.json"}}`,
		"dual/esm/package.json": `{"type":"module"}`,
		"dual/esm/i.js":         `export const a = 1;`,
		"dual/lib/x.cjs":        `module.exports = 1;`,
		"dual/special.js":       `module.exports = 2;`,
		"dual/data.json":        `{}`,
		// require-only exports: an import does not resolve, so it is left alone.
		"reqonly/package.json": `{"exports":{"require":"./r.js"}}`,
		"reqonly/r.js":         `module.exports = 1;`,
		// Sugar: a string "exports" is the "." entry.
		"sugar/package.json": `{"exports":"./main.cjs"}`,
		"sugar/main.cjs":     `module.exports = 1;`,
		// No "exports": main with the CommonJS extension search.
		"legacy/package.json": `{"main":"lib/index"}`,
		"legacy/lib/index.js": `module.exports = 1;`,
		"legacy/deep.js":      `exports.a = 1;`,
		// A .js file with ES module syntax outside a "type": "module" package.
		"detect/package.json": `{"main":"index.js"}`,
		"detect/index.js":     "// header\nimport x from 'y';\nexport default x;\n",
		// "type": "module" makes .js ES modules.
		"typed/package.json": `{"type":"module","main":"index.js"}`,
		"typed/index.js":     `export default 1;`,
	})
	det := &cjsDetector{}
	cases := []struct {
		pkg, sub string
		want     bool
	}{
		{"dual", "", false},
		{"dual", "/feature/x", true},
		{"dual", "/feature/special", true},
		{"dual", "/data.json", false},
		{"dual", "/missing", false},
		{"reqonly", "", false},
		{"sugar", "", true},
		{"legacy", "", true},
		{"legacy", "/deep", true},
		{"detect", "", false},
		{"typed", "", false},
	}
	for _, c := range cases {
		if got := det.packageImportIsCommonJS(filepath.Join(root, c.pkg), c.sub); got != c.want {
			t.Errorf("%s%s: commonjs = %v, want %v", c.pkg, c.sub, got, c.want)
		}
	}
	if got := nodeImportTarget(filepath.Join(root, "dual"), "/feature/x"); got != filepath.Join(root, "dual", "lib", "x.cjs") {
		t.Errorf("pattern target: %q", got)
	}
}

// A named import from an external CommonJS package whose exports Node cannot
// detect works in every Node-side server bundle: instrumentation (whose
// register() must run once and let the server serve) and Node API routes.
func TestServeNamedImportFromUndetectableCommonJS(t *testing.T) {
	e2eEnv(t)
	marker := filepath.Join(t.TempDir(), "marker.txt")
	t.Setenv("INST_MARKER", marker)
	files := withNccEnv(instFiles(instRegisterNccEnv))
	files["src/api/env.ts"] = `import { loadEnvConfig } from "ncc-env";
export const runtime = "nodejs";
export function GET() { return Response.json({ dir: loadEnvConfig("/x").combinedEnv.NCC_ENV_DIR }); }`
	b, root := buildInstProject(t, files)
	inst, err := os.ReadFile(instrumentationBundlePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`import\s*\{[^}]*loadEnvConfig`).Match(inst) || !regexp.MustCompile(`import\s*\w+\s*from\s*"ncc-env"`).Match(inst) {
		t.Errorf("the bundle must keep ncc-env external behind a default import:\n%s", inst)
	}
	code := exec.Command("node", "--input-type=module", "-e", "await import(process.argv[1])", instrumentationBundlePath(root))
	if out, err := code.CombinedOutput(); err != nil {
		t.Fatalf("Node cannot load the instrumentation bundle: %v\n%s", err, out)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	waitUntilTrue(t, 30*time.Second, "the API sidecar", func() bool {
		code, _ := getText(t, base+"/readyz")
		return code == 200
	})
	if code, body := getText(t, base+"/"); code != 200 || !strings.Contains(body, "home") {
		t.Errorf("GET /: %d", code)
	}
	if code, body := getText(t, base+"/api/env"); code != 200 || !strings.Contains(body, `"dir":"/x"`) {
		t.Errorf("GET /api/env: %d %s", code, body)
	}
	if lines := markerLines(t, marker); len(lines) != 1 || lines[0] != "register env=ok" {
		t.Errorf("register() must run once: %q", lines)
	}
}

// The node deploy bundle bundles packages, except serverExternalPackages and
// native addons: those stay external and get the same interop.
func TestNodeDeployNamedImportFromExternalCommonJS(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
		t.Skip("node is not installed")
	}
	ensureHandlerDist(t)
	marker := filepath.Join(t.TempDir(), "marker.txt")
	files := withNccEnv(instFiles(instRegisterNccEnv))
	files["src/api/env.ts"] = `import { loadEnvConfig } from "ncc-env";
export const runtime = "nodejs";
export function GET() { return Response.json({ dir: loadEnvConfig("/x").combinedEnv.NCC_ENV_DIR }); }`
	b, root := scProject(t, files)
	b.Cfg.ServerExternalPackages = []string{"ncc-env"}
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "packages"), 0o755); err != nil {
		t.Fatal(err)
	}
	repoRoot, _ := repoRootPath()
	if err := os.Symlink(filepath.Join(repoRoot, "packages", "runtime"), filepath.Join(root, "packages", "runtime")); err != nil {
		t.Skipf("cannot link the runtime: %v", err)
	}
	captureProgress(t)
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildServerBundle(root, b.Cfg, "node")
	if err != nil {
		t.Fatal(err)
	}
	bundle, _ := os.ReadFile(plan.Bundle)
	if !regexp.MustCompile(`import\s*\w+\s*from\s*"ncc-env"`).Match(bundle) {
		t.Errorf("ncc-env must stay external behind a default import")
	}
	base := startNodeBundle(t, root, b.Cfg, "INST_MARKER="+marker)
	if code, body := getText(t, base+"/api/env"); code != 200 || !strings.Contains(body, `"dir":"/x"`) {
		t.Errorf("GET /api/env: %d %s", code, body)
	}
	if code, _ := getText(t, base+"/"); code != 200 {
		t.Errorf("GET /: %d", code)
	}
	if lines := markerLines(t, marker); len(lines) != 1 || lines[0] != "register env=ok" {
		t.Errorf("register() must run once: %q", lines)
	}
}

// Middleware on the Node runtime bundles packages too; one listed in
// serverExternalPackages stays external and gets the interop.
func TestNodeMiddlewareNamedImportFromExternalCommonJS(t *testing.T) {
	e2eEnv(t)
	files := withNccEnv(instFiles(`export function register() {}`))
	delete(files, "src/instrumentation.ts")
	files["src/middleware.ts"] = `import { loadEnvConfig } from "ncc-env";
export function middleware() {
  const r = new Response(null, { headers: { "x-middleware-next": "1", "x-ncc-env": loadEnvConfig("/mw").combinedEnv.NCC_ENV_DIR } });
  return r;
}`
	b, root := scProject(t, files)
	b.Cfg.ServerExternalPackages = []string{"ncc-env"}
	b.Cfg.SSR.MiddlewareRuntime = "node"
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	captureProgress(t)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	mw, _ := os.ReadFile(filepath.Join(root, ".krate", "middleware.js"))
	if !regexp.MustCompile(`import\s*\w+\s*from\s*"ncc-env"`).Match(mw) {
		t.Errorf("ncc-env must stay external behind a default import:\n%s", mw)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	var got string
	waitUntilTrue(t, 30*time.Second, "the middleware header", func() bool {
		resp, err := http.Get(base + "/")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		got = resp.Header.Get("x-ncc-env")
		return resp.StatusCode == 200 && got != ""
	})
	if got != "/mw" {
		t.Errorf("x-ncc-env = %q", got)
	}
}
