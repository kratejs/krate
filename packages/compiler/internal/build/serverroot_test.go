package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func listTree(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out
}

// monorepoFiles is a pnpm-like repository: the app apps/web, a sibling
// package whose built worker the app reads by relative path, and a pnpm store
// entry with a platform binary the tracer cannot see.
func monorepoFiles() map[string]string {
	return map[string]string{
		"apps/sibling/dist/x.mjs":         "export const x = 1;",
		"apps/web/public/hello.txt":       "hello static",
		"apps/web/src/pages/index.tsx":    `export default function Home() { return <h1>home page</h1>; }`,
		"apps/web/src/instrumentation.ts": `export function register() { (globalThis as any).__inst = "ran"; }`,
		"apps/web/src/middleware.ts":      `export function middleware(req: Request) { if (new URL(req.url).pathname === "/mw") return new Response("from middleware"); }`,
		"apps/web/src/api/probe.ts": `import fs from "node:fs";
import path from "node:path";
export async function GET() {
  return Response.json({
    sibling: fs.existsSync(path.resolve(process.cwd(), "../sibling/dist/x.mjs")),
    inst: (globalThis as any).__inst ?? null,
    store: fs.existsSync(path.resolve(process.cwd(), "../../node_modules/.pnpm/@fake+img@1.0.0/node_modules/@fake/img-linux/img.node")),
  });
}`,
		"node_modules/.pnpm/@fake+img@1.0.0/node_modules/@fake/img-linux/package.json": `{"name":"@fake/img-linux","version":"1.0.0"}`,
		"node_modules/.pnpm/@fake+img@1.0.0/node_modules/@fake/img-linux/img.node":     "binary",
		"node_modules/.pnpm/@fake+bin@1.0.0/node_modules/@fake/bin-linux/b.node":       "bin",
		"node_modules/.pnpm/@fake+img@1.0.0/node_modules/@fake/linked":                 "symlink:node_modules/.pnpm/@fake+bin@1.0.0/node_modules/@fake/bin-linux",
		"node_modules/.pnpm/@fake+other@2.0.0/node_modules/@fake/other/index.js":       "// not included",
	}
}

func monorepoIncludes() map[string][]string {
	return map[string][]string{
		"/api/probe": {"../sibling/dist/x.mjs"},
		"**": {
			"../../node_modules/.pnpm/@fake+img@*/node_modules/@fake/img-*/*",
			"../../node_modules/.pnpm/@fake+img@*/node_modules/@fake/linked/*",
		},
	}
}

func TestNodeTargetMirrorsOutputFileTracingRoot(t *testing.T) {
	var top string
	root, cfg := externalsProjectAt(t, monorepoFiles(), "apps/web", func(c *config.Config) {
		top = filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(c.PagesDir))))
		c.OutputFileTracingRoot = top
		c.PublicDir = filepath.Join(filepath.Dir(filepath.Dir(c.PagesDir)), "public")
		c.OutputFileTracingIncludes = monorepoIncludes()
	})
	plan, err := BuildServerBundle(root, cfg, "node")
	if err != nil {
		t.Fatalf("BuildServerBundle: %v", err)
	}
	base := filepath.Join(cfg.OutDir, ".krate", "server")
	want := filepath.Join(base, "apps", "web", "server.mjs")
	if plan.Bundle != want {
		t.Fatalf("bundle at %s, want %s", plan.Bundle, want)
	}
	for _, rel := range []string{
		"apps/sibling/dist/x.mjs",
		"node_modules/.pnpm/@fake+img@1.0.0/node_modules/@fake/img-linux/img.node",
		"node_modules/.pnpm/@fake+img@1.0.0/node_modules/@fake/linked/b.node",
	} {
		if _, err := os.Stat(filepath.Join(base, filepath.FromSlash(rel))); err != nil {
			t.Errorf("server output is missing %s: %v", rel, err)
		}
	}
	for _, rel := range listTree(t, base) {
		if strings.Contains(rel, "@fake+other") {
			t.Errorf("a file outside the globs was copied: %s", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(base, "server.mjs")); err == nil {
		t.Errorf("a nested layout must not also write a flat server.mjs")
	}

	// Only the server output may supply the files at run time.
	if err := os.RemoveAll(filepath.Join(top, "apps", "sibling")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(top, "node_modules", ".pnpm")); err != nil {
		t.Fatal(err)
	}
	url := startNodeBundle(t, root, cfg)
	res := doMatrix(t, url, "GET", "/api/probe", nil, "")
	if res.status != 200 || !strings.Contains(res.body, `"sibling":true`) || !strings.Contains(res.body, `"store":true`) || !strings.Contains(res.body, `"inst":"ran"`) {
		t.Errorf("api/probe: %d %s", res.status, res.body)
	}
	if res := doMatrix(t, url, "GET", "/hello.txt", nil, ""); res.status != 200 || !strings.Contains(res.body, "hello static") {
		t.Errorf("static file: %d %s", res.status, res.body)
	}
	if res := doMatrix(t, url, "GET", "/", nil, ""); res.status != 200 || !strings.Contains(res.body, "home page") {
		t.Errorf("page: %d %s", res.status, res.body)
	}
	if res := doMatrix(t, url, "GET", "/mw", nil, ""); res.status != 200 || !strings.Contains(res.body, "from middleware") {
		t.Errorf("middleware: %d %s", res.status, res.body)
	}
}

func TestNodeTargetFlatLayoutWithoutTracingRoot(t *testing.T) {
	root, cfg := externalsProjectAt(t, monorepoFiles(), "apps/web", func(c *config.Config) {
		c.OutputFileTracingIncludes = map[string][]string{"*": {"public/**"}}
	})
	plan, err := BuildServerBundle(root, cfg, "node")
	if err != nil {
		t.Fatalf("BuildServerBundle: %v", err)
	}
	base := filepath.Join(cfg.OutDir, ".krate", "server")
	if plan.Bundle != filepath.Join(base, "server.mjs") {
		t.Fatalf("flat bundle at %s", plan.Bundle)
	}
	got := strings.Join(listTree(t, base), ",")
	if got != "public/hello.txt,server.mjs" {
		t.Errorf("flat layout changed: %s", got)
	}
}

func TestOutputFileTracingRootMustContainProjectAndGlobs(t *testing.T) {
	root, cfg := externalsProjectAt(t, monorepoFiles(), "apps/web", func(c *config.Config) {
		c.OutputFileTracingRoot = filepath.Join(filepath.Dir(c.PagesDir), "pages")
	})
	if _, err := BuildServerBundle(root, cfg, "node"); err == nil || !strings.Contains(err.Error(), "does not contain the project") {
		t.Errorf("a root inside the project must fail, got %v", err)
	}
	cfg.OutputFileTracingRoot = filepath.Dir(filepath.Dir(root))
	cfg.OutputFileTracingIncludes = map[string][]string{"*": {"../../../escape/**"}}
	if _, err := BuildServerBundle(root, cfg, "node"); err == nil || !strings.Contains(err.Error(), "leaves outputFileTracingRoot") {
		t.Errorf("a glob leaving the root must fail, got %v", err)
	}
}

func TestExpandProjectGlobPrunesUnreachableDirs(t *testing.T) {
	if !canMatchPrefix([]string{"node_modules", ".pnpm", "a*", "x"}, []string{"node_modules", ".pnpm", "abc"}) {
		t.Error("a matching prefix must be walked")
	}
	if canMatchPrefix([]string{"node_modules", ".pnpm", "a*", "x"}, []string{"node_modules", ".pnpm", "zzz"}) {
		t.Error("a non-matching directory must be pruned")
	}
	if !canMatchPrefix([]string{"a", "**", "x"}, []string{"a", "b", "c"}) {
		t.Error("** keeps every subtree")
	}
}
