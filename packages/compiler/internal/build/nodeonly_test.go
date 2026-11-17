package build

import (
	"bytes"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// nodeOnlyFiles has routes that are Node-only for every reason the QuickJS
// bundle can keep an external import, and routes whose external imports tree
// shaking drops, which stay on the embedded runtime.
func nodeOnlyFiles() map[string]string {
	files := map[string]string{
		"src/api/plain.ts":  `export async function GET() { return Response.json({ ok: true }); }`,
		"src/api/crypto.ts": "import { createHash } from \"node:crypto\";\nexport function GET() { return new Response(createHash(\"sha256\").update(\"x\").digest(\"hex\")); }",
		"src/api/ext.ts":    "import { tag } from \"@fake/ext\";\nexport async function GET() { return Response.json({ tag }); }",
		"src/api/db.ts":     "import { query } from \"../lib/db\";\nexport async function GET() { return Response.json({ db: query() }); }",
		"src/api/db2.ts":    "import { query } from \"../lib/db\";\nexport async function POST() { return Response.json({ db: query() }); }",
		"src/lib/db.ts":     "import pg from \"fakepg\";\nexport function query() { return pg.driver; }",
		// A package whose only builtin import sits in an unused function.
		"src/api/shaken.ts": "import cj from \"cjsunused\";\nexport function GET() { return Response.json(cj); }",
		// A side-effect free package with an unused builtin import.
		"src/api/sefree.ts": "import { z } from \"sefree\";\nexport function GET() { return Response.json({ z }); }",
		// A side-effect free package whose used export needs a builtin.
		"src/api/sefreeused.ts": "import { lookup } from \"sefreeused\";\nexport function GET() { return Response.json({ l: typeof lookup() }); }",
		// An unused builtin import in a JS route.
		"src/api/unusedfs.js":      "import fs from \"fs\";\nexport function GET() { return Response.json({ ok: 1 }); }",
		"src/api/items/[id].ts":    `export function GET() { return Response.json({ via: "dynamic" }); }`,
		"src/api/items/special.ts": "import { randomBytes } from \"node:crypto\";\nexport function GET() { return Response.json({ via: \"special\", n: randomBytes(4).length }); }",
		"src/pages/index.tsx":      `export default function Home() { return <h1>home</h1>; }`,
		// A package whose entry is the native addon itself.
		"src/api/nativemain.ts":                "import bind from \"@fake/bind\";\nexport function GET() { return Response.json({ b: typeof bind }); }",
		"node_modules/@fake/bind/package.json": `{"name":"@fake/bind","version":"1.0.0","main":"bind.node"}`,
		"node_modules/@fake/bind/bind.node":    `not a real addon`,
		"node_modules/@fake/ext/package.json":  `{"name":"@fake/ext","version":"1.0.0","main":"index.js"}`,
		"node_modules/@fake/ext/index.js":      `exports.tag = "ext";`,
		"node_modules/fakepg/package.json":     `{"name":"fakepg","version":"1.0.0","main":"lib/index.js"}`,
		"node_modules/fakepg/lib/index.js":     `const conn = require("./conn"); module.exports = { driver: conn.kind };`,
		"node_modules/fakepg/lib/conn.js":      `const net = require("net"); module.exports = { kind: typeof net.connect };`,
		"node_modules/cjsunused/package.json":  `{"name":"cjsunused","version":"1.0.0","main":"index.js"}`,
		"node_modules/cjsunused/index.js":      "function unused() { return require(\"net\"); }\nmodule.exports = { a: 1 };",
		"node_modules/sefree/package.json":     `{"name":"sefree","version":"1.0.0","main":"index.js","sideEffects":false}`,
		"node_modules/sefree/index.js":         "import \"tls\";\nexport const z = 1;",
		"node_modules/sefreeused/package.json": `{"name":"sefreeused","version":"1.0.0","main":"index.js","sideEffects":false}`,
		"node_modules/sefreeused/index.js":     "export function lookup() { return require(\"dns\").lookup; }\nexport function other() { return 1; }",
	}
	for k, v := range nativeFixture() {
		if strings.HasPrefix(k, "src/api/") || strings.HasPrefix(k, "node_modules/") {
			files[k] = v
		}
	}
	return files
}

func nodeOnlyRoutes(root string) []string {
	var out []string
	_ = filepath.WalkDir(filepath.Join(root, "src", "api"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	slices.Sort(out)
	return out
}

func nodeOnlyProject(t *testing.T, root string) *Builder {
	t.Helper()
	writeTree(t, root, nodeOnlyFiles())
	cfg := config.Default()
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.ServerExternalPackages = []string{"@fake/ext"}
	return New(root, cfg)
}

func setLegacyNodeOnly(t *testing.T, on bool) {
	t.Helper()
	prev := legacyNodeOnlyDetection
	legacyNodeOnlyDetection = on
	t.Cleanup(func() { legacyNodeOnlyDetection = prev })
}

// The detection gives every route the Node-only flag the full QuickJS bundle
// gives it, proves the expected routes without that bundle, and writes a stub
// instead of the full bundle for a Node-only route.
func TestNodeOnlyDetectionMatchesFullBundle(t *testing.T) {
	legacyRoot, newRoot := t.TempDir(), t.TempDir()

	setLegacyNodeOnly(t, true)
	lb := nodeOnlyProject(t, legacyRoot)
	if err := lb.CompileAPIRoutes(nodeOnlyRoutes(legacyRoot)); err != nil {
		t.Fatal(err)
	}
	legacy := readRouteMetas(t, legacyRoot)

	legacyNodeOnlyDetection = false
	nb := nodeOnlyProject(t, newRoot)
	if err := nb.CompileAPIRoutes(nodeOnlyRoutes(newRoot)); err != nil {
		t.Fatal(err)
	}
	got := readRouteMetas(t, newRoot)

	wantNodeOnly := map[string]bool{
		"plain.js": false, "crypto.js": true, "ext.js": true, "db.js": true, "db2.js": true,
		"native.js": true, "nativemain.js": true, "shaken.js": false, "sefree.js": false, "sefreeused.js": true,
		"unusedfs.js": false, "items/[id].js": false, "items/special.js": true,
	}
	if !slices.Equal(slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(wantNodeOnly))) {
		t.Fatalf("routes = %v", slices.Sorted(maps.Keys(got)))
	}
	for name, want := range wantNodeOnly {
		if legacy[name].NodeOnly != want {
			t.Errorf("%s: full bundle says nodeOnly=%v, the fixture expects %v", name, legacy[name].NodeOnly, want)
		}
		if got[name].NodeOnly != legacy[name].NodeOnly {
			t.Errorf("%s: nodeOnly=%v, full bundle says %v", name, got[name].NodeOnly, legacy[name].NodeOnly)
		}
		if (got[name].Features == nil) != (legacy[name].Features == nil) {
			t.Errorf("%s: features %v, full bundle %v", name, got[name].Features, legacy[name].Features)
		}
	}

	// Proven without the full bundle: the routes whose Node-only imports are
	// in project code or in a package entry with side effects.
	quickjsOpts, _ := nb.apiRouteOptions()
	fam := &apiRouteFamily{}
	proven := map[string]bool{"crypto.ts": true, "ext.ts": true, "db.ts": true, "db2.ts": true, "native.ts": true, "nativemain.ts": true, "special.ts": true}
	for _, route := range nodeOnlyRoutes(newRoot) {
		o := quickjsOpts
		o.EntryPoints = []string{route}
		o.Outfile = filepath.Join(apiBundleDir(nb.Cfg.OutDir), "probe.js")
		if p := provablyNodeOnly(o, nb.Cfg.ServerExternalPackages, fam); p != proven[filepath.Base(route)] {
			t.Errorf("%s: provablyNodeOnly = %v, want %v", filepath.Base(route), p, proven[filepath.Base(route)])
		}
	}

	qdir, ndir := apiBundleDir(nb.Cfg.OutDir), apiNodeBundleDir(nb.Cfg.OutDir)
	lqdir := apiBundleDir(lb.Cfg.OutDir)
	for name, m := range got {
		q, err := os.ReadFile(filepath.Join(qdir, filepath.FromSlash(name)))
		if err != nil {
			t.Errorf("%s: QuickJS artifact: %v", name, err)
			continue
		}
		_, nodeErr := os.Stat(filepath.Join(ndir, filepath.FromSlash(name)))
		if m.NodeOnly {
			if !bytes.Equal(q, nodeOnlyStub(name)) {
				t.Errorf("%s: a Node-only route must get the QuickJS stub, got %d bytes", name, len(q))
			}
			if nodeErr != nil {
				t.Errorf("%s: Node-only route without a Node bundle: %v", name, nodeErr)
			}
			if _, err := os.Stat(filepath.Join(qdir, filepath.FromSlash(name)+".map")); err == nil {
				t.Errorf("%s: the stub must not keep a source map", name)
			}
			continue
		}
		if nodeErr == nil {
			t.Errorf("%s: an embedded-runtime route must not get a Node bundle", name)
		}
		lq, err := os.ReadFile(filepath.Join(lqdir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(normalizeRoot(q, newRoot), normalizeRoot(lq, legacyRoot)) {
			t.Errorf("%s: the embedded-runtime bundle changed", name)
		}
	}

	// Dev rebuilds still know what a Node-only route depends on.
	db := filepath.Join(newRoot, "src", "api", "db.ts")
	if deps := nb.pageDeps[db]; !slices.ContainsFunc(deps, func(d string) bool {
		return strings.HasSuffix(filepath.ToSlash(d), "src/lib/db.ts")
	}) {
		t.Errorf("db.ts deps = %v, want src/lib/db.ts", deps)
	}
	if affected := nb.affectedPages(nb.pageDeps[db]); !slices.Contains(affected, db) {
		t.Errorf("a change to db.ts's deps must rebuild it: %v", affected)
	}
}

// normalizeRoot replaces the project root, which bundles name in comments
// relative to the working directory, with ROOT.
func normalizeRoot(b []byte, root string) []byte {
	if cwd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(cwd, root); err == nil {
			b = bytes.ReplaceAll(b, []byte(filepath.ToSlash(rel)), []byte("ROOT"))
		}
	}
	b = bytes.ReplaceAll(b, []byte(filepath.ToSlash(root)), []byte("ROOT"))
	// esbuild prints inputs with their symlinks resolved (on macOS the
	// temporary directory /var is /private/var), so a source map path relative
	// to an output under the unresolved root climbs into the resolved one and
	// names the test's own temporary directories.
	if real, err := filepath.EvalSymlinks(root); err == nil && real != root {
		tail := filepath.Join(filepath.Base(filepath.Dir(real)), filepath.Base(real))
		b = bytes.ReplaceAll(b, []byte(filepath.ToSlash(tail)), []byte("ROOT"))
	}
	return b
}

// A route that turns Node-only replaces its full QuickJS bundle with the stub,
// and a route that stops being Node-only gets its bundle back.
func TestNodeOnlyStubFollowsRoute(t *testing.T) {
	b, root := compatProject(t, map[string]string{
		"src/api/r.ts": `export function GET() { return Response.json({ ok: true }); }`,
	}, nil)
	route := filepath.Join(root, "src", "api", "r.ts")
	qpath := filepath.Join(apiBundleDir(b.Cfg.OutDir), "r.js")
	compile := func(src string) []byte {
		t.Helper()
		writeTree(t, root, map[string]string{"src/api/r.ts": src})
		if err := b.CompileAPIRoutes([]string{route}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(qpath)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if q := compile(`export function GET() { return Response.json({ ok: true }); }`); bytes.Equal(q, nodeOnlyStub("r.js")) {
		t.Fatal("an embedded-runtime route got the stub")
	}
	if q := compile("import { hostname } from \"node:os\";\nexport function GET() { return new Response(hostname()); }"); !bytes.Equal(q, nodeOnlyStub("r.js")) {
		t.Fatalf("a Node-only route must get the stub:\n%s", q)
	}
	if q := compile(`export function GET() { return Response.json({ back: true }); }`); !strings.Contains(string(q), "back") {
		t.Fatalf("the route's QuickJS bundle must come back:\n%s", q)
	}
	if _, err := os.Stat(filepath.Join(apiNodeBundleDir(b.Cfg.OutDir), "r.js")); err == nil {
		t.Error("the stale Node bundle must be removed")
	}
}

// An esbuild error in a route that is not provably Node-only is reported as
// before.
func TestNodeOnlyDetectionKeepsBundleErrors(t *testing.T) {
	b, root := compatProject(t, map[string]string{
		"src/api/bad.ts": "import { x } from \"./missing\";\nexport function GET() { return Response.json({ x }); }",
	}, nil)
	err := b.CompileAPIRoutes([]string{filepath.Join(root, "src", "api", "bad.ts")})
	if err == nil || !strings.Contains(err.Error(), "esbuild compilation failed") || !strings.Contains(err.Error(), "./missing") {
		t.Fatalf("err = %v", err)
	}
}

// krate serve sends a Node-only route to the Node sidecar, including one that
// a dynamic route next to it would otherwise match.
func TestServeRoutesNodeOnlyStubsToNode(t *testing.T) {
	root, cfg := externalsProject(t, nodeOnlyFiles(), func(c *config.Config) {
		c.ServerExternalPackages = []string{"@fake/ext"}
	})
	if got := readRouteMetas(t, root); !got["items/special.js"].NodeOnly || got["items/[id].js"].NodeOnly {
		t.Fatalf("metas = %+v", got)
	}
	stub, err := os.ReadFile(filepath.Join(apiBundleDir(cfg.OutDir), "items", "special.js"))
	if err != nil || !bytes.Equal(stub, nodeOnlyStub("items/special.js")) {
		t.Fatalf("items/special.js must be the stub: %v\n%s", err, stub)
	}
	base, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string) (int, string) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for {
			resp, err := http.Get(base + path)
			if err == nil {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				// 502/503 while the sidecar starts.
				if resp.StatusCode < 502 || time.Now().After(deadline) {
					return resp.StatusCode, string(body)
				}
			} else if time.Now().After(deadline) {
				t.Fatalf("GET %s: %v", path, err)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	for path, want := range map[string]string{
		"/api/items/special": `"via":"special"`,
		"/api/items/42":      `"via":"dynamic"`,
		"/api/db":            `"db":"function"`,
		"/api/crypto":        "2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881",
		"/api/plain":         `"ok":true`,
	} {
		if status, body := get(path); status != 200 || !strings.Contains(body, want) {
			t.Errorf("GET %s = %d %s, want %s", path, status, body, want)
		}
	}
}
