package build

import (
	"encoding/json"
	"html"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// metaFixture is a workspace package linked into an app's node_modules (as
// pnpm does) whose modules read files and name themselves through their source
// location, imported by an API route, an SSR page and instrumentation. app is
// the app's directory below the repository ("" when the app is the repository).
func metaFixture(app string) map[string]string {
	at := func(rel string) string { return strings.TrimPrefix(app+"/"+rel, "/") }
	return map[string]string{
		"packages/lib/package.json":   `{"name":"@fixture/lib","version":"1.0.0","type":"module","exports":{"./meta":"./src/meta.ts"}}`,
		"packages/lib/data/x.txt":     "hello-lib\n",
		"packages/lib/data/dir/y.txt": "dir-file\n",
		"packages/lib/src/meta.ts": `import fs from "node:fs";
import path from "node:path";
// @ts-ignore
import one from "../cjs/one/where.cjs";
// @ts-ignore
import two from "../cjs/two/where.cjs";
export function readMeta() {
  const a = fs.readFileSync(new URL("../data/x.txt", import.meta.url), "utf8").trim();
  const b = fs.readFileSync(path.join(import.meta.dirname, "..", "data", "x.txt"), "utf8").trim();
  const c = fs.readdirSync(path.join(import.meta.dirname, "../data/dir")).join(",");
  return { a, b, c, url: import.meta.url, dirname: import.meta.dirname, filename: import.meta.filename, one, two };
}`,
		"packages/lib/cjs/one/where.cjs": `module.exports = { dir: __dirname, file: __filename };`,
		"packages/lib/cjs/two/where.cjs": `module.exports = { dir: __dirname, file: __filename };`,
		at("node_modules/@fixture/lib"):  "symlink:packages/lib",
		at("src/pages/index.tsx"):        `export default function Home() { return <main><h1>home</h1></main>; }`,
		at("src/pages/ssr.tsx"): `import { readMeta } from "@fixture/lib/meta";
export const dynamic = 'force-dynamic';
export default function S() { return <main><h1>ssr-meta</h1><pre id="meta">{JSON.stringify(readMeta())}</pre></main>; }`,
		at("src/api/meta.ts"): `import { readMeta } from "@fixture/lib/meta";
export const runtime = "nodejs";
export async function GET() { return Response.json(readMeta()); }`,
		at("src/api/inst.ts"): `export const runtime = "nodejs";
export async function GET() { return Response.json((globalThis as any).__krateMeta ?? null); }`,
		at("src/instrumentation.ts"): `import { readMeta } from "@fixture/lib/meta";
import { writeFileSync } from "node:fs";
export async function register() {
  const m = readMeta();
  (globalThis as any).__krateMeta = m;
  if (process.env.META_OUT) writeFileSync(process.env.META_OUT, JSON.stringify(m));
}`,
	}
}

type metaResult struct {
	A, B, C  string
	URL      string `json:"url"`
	Dirname  string `json:"dirname"`
	Filename string `json:"filename"`
	One, Two struct{ Dir, File string }
}

func fileURLOf(p string) string {
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// checkMeta verifies what the fixture's modules reported for a package
// directory lib.
func checkMeta(t *testing.T, what, body, lib string) {
	t.Helper()
	var m metaResult
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Errorf("%s: %v in %s", what, err, body)
		return
	}
	if m.A != "hello-lib" || m.B != "hello-lib" || m.C != "y.txt" {
		t.Errorf("%s: files not read: %s", what, body)
	}
	if want := fileURLOf(filepath.Join(lib, "src", "meta.ts")); m.URL != want {
		t.Errorf("%s: import.meta.url = %s, want %s", what, m.URL, want)
	}
	if want := filepath.Join(lib, "src"); filepath.Clean(m.Dirname) != want {
		t.Errorf("%s: import.meta.dirname = %s, want %s", what, m.Dirname, want)
	}
	if want := filepath.Join(lib, "src", "meta.ts"); filepath.Clean(m.Filename) != want {
		t.Errorf("%s: import.meta.filename = %s, want %s", what, m.Filename, want)
	}
	for name, got := range map[string]struct{ Dir, File string }{"one": m.One, "two": m.Two} {
		wantDir := filepath.Join(lib, "cjs", name)
		if filepath.Clean(got.Dir) != wantDir || filepath.Clean(got.File) != filepath.Join(wantDir, "where.cjs") {
			t.Errorf("%s: CommonJS %s reports %s, %s; want %s", what, name, got.Dir, got.File, wantDir)
		}
	}
}

func ssrMeta(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`<pre id="meta">(.*?)</pre>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no meta in page: %s", body)
	}
	return html.UnescapeString(m[1])
}

// noMetaPlaceholder fails when an output file still has the banner's
// placeholder.
func noMetaPlaceholder(t *testing.T, dir string) {
	t.Helper()
	n := 0
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.Contains(filepath.ToSlash(p), "/node_modules/") {
			return nil
		}
		if ext := filepath.Ext(p); ext != ".js" && ext != ".mjs" && ext != ".cjs" {
			return nil
		}
		data, _ := os.ReadFile(p)
		if strings.Contains(string(data), metaPlaceholder) {
			t.Errorf("%s still has the source meta placeholder", p)
		}
		n++
		return nil
	})
	if n == 0 {
		t.Errorf("no output files under %s", dir)
	}
}

func metaProject(t *testing.T, app string, tracing bool) (top, root string, cfg *config.Config) {
	t.Helper()
	root, cfg = externalsProjectAt(t, metaFixture(app), app, func(c *config.Config) {
		if tracing {
			c.OutputFileTracingRoot = strings.TrimSuffix(filepath.Dir(filepath.Dir(c.PagesDir)), filepath.FromSlash("/"+app))
		}
	})
	top, err := filepath.EvalSymlinks(strings.TrimSuffix(root, filepath.FromSlash("/"+app)))
	if app == "" {
		top, err = filepath.EvalSymlinks(root)
	}
	if err != nil {
		t.Fatal(err)
	}
	return top, root, cfg
}

func TestSourceMetaServeInPlace(t *testing.T) {
	e2eEnv(t)
	metaOut := filepath.Join(t.TempDir(), "inst.json")
	t.Setenv("META_OUT", metaOut)
	top, root, cfg := metaProject(t, "apps/web", true)
	noMetaPlaceholder(t, filepath.Join(root, "dist", ".krate"))
	noMetaPlaceholder(t, filepath.Join(root, ".krate"))
	base, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	lib := filepath.Join(top, "packages", "lib")
	var apiBody, instBody, ssrBody string
	waitUntilTrue(t, 30*time.Second, "the API sidecar", func() bool {
		code, body := getText(t, base+"/api/meta")
		apiBody = body
		return code == 200
	})
	checkMeta(t, "api route", apiBody, lib)
	// register() runs in the sidecar's primary process, not in the cluster
	// workers that answer routes, so it reports through a file.
	waitUntilTrue(t, 30*time.Second, "register() to run", func() bool {
		data, err := os.ReadFile(metaOut)
		instBody = string(data)
		return err == nil && len(data) > 0
	})
	checkMeta(t, "instrumentation", instBody, lib)
	if code, body := getText(t, base+"/ssr/"); code != 200 {
		t.Fatalf("/ssr/: %d %s", code, body)
	} else {
		ssrBody = ssrMeta(t, body)
	}
	checkMeta(t, "ssr page", ssrBody, lib)
}

// The built tree can move as a whole (a Docker image copies the repository to
// /app): the locations follow the bundle.
func TestSourceMetaBuildTreeMoves(t *testing.T) {
	e2eEnv(t)
	top, root, _ := metaProject(t, "apps/web", true)
	moved := filepath.Join(t.TempDir(), "elsewhere")
	if err := copyTreeSkippingLinks(top, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(top); err != nil {
		t.Fatal(err)
	}
	rel, _ := filepath.Rel(top, root)
	bundle := filepath.Join(moved, rel, "dist", ".krate", "api-node", "meta.js")
	script := `const m = await import(process.argv[1]); const r = await m.GET(new Request("http://x/")); console.log(await r.text());`
	out, err := exec.Command("node", "--input-type=module", "-e", script, fileURLOf(bundle)).CombinedOutput()
	if err != nil {
		t.Fatalf("running the moved route: %v\n%s", err, out)
	}
	moved, _ = filepath.EvalSymlinks(moved)
	checkMeta(t, "moved build tree", strings.TrimSpace(string(out)), filepath.Join(moved, "packages", "lib"))
}

// The node deploy output carries the files the bundle reads through its
// source location, and the locations follow it when it moves.
func TestSourceMetaNodeDeployMoves(t *testing.T) {
	for _, tc := range []struct {
		name, app string
		tracing   bool
	}{
		{"tracing root", "apps/web", true},
		{"flat", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			top, root, cfg := metaProject(t, tc.app, tc.tracing)
			plan, err := BuildServerBundle(root, cfg, "node")
			if err != nil {
				t.Fatalf("BuildServerBundle: %v", err)
			}
			for _, w := range plan.Warnings {
				t.Logf("warning: %s", w)
			}
			base := filepath.Join(cfg.OutDir, ".krate", "server")
			noMetaPlaceholder(t, base)
			moved := filepath.Join(t.TempDir(), "image")
			if err := copyTree(t, base, moved); err != nil {
				t.Fatal(err)
			}
			if err := os.RemoveAll(top); err != nil {
				t.Fatal(err)
			}
			appDir := moved
			if tc.tracing {
				appDir = filepath.Join(moved, filepath.FromSlash(tc.app))
			}
			moved, _ = filepath.EvalSymlinks(moved)
			srv := startNodeBundleAt(t, filepath.Join(appDir, "server.mjs"), appDir, "INST_UNUSED=1")
			lib := filepath.Join(moved, "packages", "lib")
			for what, path := range map[string]string{"api route": "/api/meta", "instrumentation": "/api/inst"} {
				var body string
				waitUntilTrue(t, 20*time.Second, what, func() bool {
					code, b := getText(t, srv+path)
					body = b
					return code == 200 && b != "null"
				})
				checkMeta(t, what, body, lib)
			}
			if code, body := getText(t, srv+"/ssr"); code != 200 {
				t.Errorf("ssr page: %d %s", code, body)
			} else {
				checkMeta(t, "ssr page", ssrMeta(t, body), lib)
			}
		})
	}
}

// copyTreeSkippingLinks copies a directory tree without its symlinks.
func copyTreeSkippingLinks(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		switch {
		case d.Type()&os.ModeSymlink != 0:
			return nil
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// The targets that bundle the output again get the page and action bundles
// without the node banner, and helpers that need no file layout.
func TestSourceMetaStubTargets(t *testing.T) {
	for _, target := range []string{"vercel", "netlify"} {
		t.Run(target, func(t *testing.T) {
			_, root, cfg := metaProject(t, "apps/web", true)
			plan, err := BuildServerBundle(root, cfg, target)
			if err != nil {
				t.Fatalf("BuildServerBundle: %v", err)
			}
			data, err := os.ReadFile(plan.Bundle)
			if err != nil {
				t.Fatal(err)
			}
			code := string(data)
			if strings.Contains(code, "__krateMetaRoot") || strings.Contains(code, metaBannerOpen) || strings.Contains(code, metaPlaceholder) {
				t.Errorf("the node banner reached the %s bundle", target)
			}
			if !strings.Contains(code, "__krateMetaCwd") || !strings.Contains(code, "packages/lib/src/meta.ts") {
				t.Errorf("the page bundle's source locations are not served by the stub helpers")
			}
		})
	}
}
