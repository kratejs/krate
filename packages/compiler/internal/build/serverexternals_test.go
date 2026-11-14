package build

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// externalsProject writes files under a temp project, links the runtime like
// serverlessProject does, loads the config and builds.
func externalsProject(t *testing.T, files map[string]string, mutate func(*config.Config)) (string, *config.Config) {
	t.Helper()
	return externalsProjectAt(t, files, "", mutate)
}

// externalsProjectAt is externalsProject with the project in the sub directory
// sub of the written tree (a workspace app inside a monorepo).
func externalsProjectAt(t *testing.T, files map[string]string, sub string, mutate func(*config.Config)) (string, *config.Config) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
		t.Skip("node is not installed")
	}
	ensureHandlerDist(t)
	top := t.TempDir()
	links := map[string]string{}
	for rel, content := range files {
		p := filepath.Join(top, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if target, ok := strings.CutPrefix(content, "symlink:"); ok {
			links[p] = filepath.Join(top, filepath.FromSlash(target))
			continue
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Links go last: on Windows a link made before its target directory
	// exists is a file link, which cannot be read as a directory.
	for p, target := range links {
		if err := os.Symlink(target, p); err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Join(top, filepath.FromSlash(sub))
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	repoRoot, _ := repoRootPath()
	if err := os.MkdirAll(filepath.Join(root, "packages"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repoRoot, "packages", "runtime"), filepath.Join(root, "packages", "runtime")); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if mutate != nil {
		mutate(cfg)
	}
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	return root, cfg
}

// nativeFixture is a package that loads a platform package holding a .node
// file inside a try block (like @napi-rs/canvas), with one dependency and one
// optional platform package that is not installed.
func nativeFixture() map[string]string {
	return map[string]string{
		"src/pages/index.tsx": `export default function Home() { return <h1>home</h1>; }`,
		"src/api/native.ts": `import canvas from "@fake/canvas";
export async function GET() {
  return Response.json({ name: canvas.name, native: canvas.native, util: canvas.util });
}`,
		"assets/worker.js":  `// worker entry`,
		"assets/data/a.txt": `a`,
		"node_modules/@fake/canvas/package.json": `{"name":"@fake/canvas","version":"1.0.0","main":"index.js",
  "dependencies":{"@fake/util":"1.0.0"},
  "optionalDependencies":{"@fake/canvas-linux-x64":"1.0.0","@fake/canvas-darwin-arm64":"1.0.0"}}`,
		"node_modules/@fake/canvas/index.js": `let native = null;
try { native = require("@fake/canvas-linux-x64"); } catch (e) { native = null; }
module.exports = { name: "canvas", native: !!native, util: require("@fake/util").tag };`,
		"node_modules/@fake/util/package.json":             `{"name":"@fake/util","version":"1.0.0","main":"index.js"}`,
		"node_modules/@fake/util/index.js":                 `exports.tag = "u1";`,
		"node_modules/@fake/canvas-linux-x64/package.json": `{"name":"@fake/canvas-linux-x64","version":"1.0.0","main":"canvas.node"}`,
		"node_modules/@fake/canvas-linux-x64/canvas.node":  `not a real addon`,
	}
}

func TestNativeAddonRouteBuildsAndRunsOnNode(t *testing.T) {
	root, _ := externalsProject(t, nativeFixture(), nil)
	data, err := os.ReadFile(filepath.Join(root, ".krate", "api-routes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var metas []apiRouteMeta
	if err := json.Unmarshal(data, &metas); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range metas {
		if m.File == "native.js" {
			found = true
			if !m.NodeOnly {
				t.Errorf("a route that imports a native addon must run on the Node sidecar: %+v", m)
			}
		}
	}
	if !found {
		t.Fatalf("native route missing from %s", data)
	}
	nodeBundle, err := os.ReadFile(filepath.Join(root, "dist", ".krate", "api-node", "native.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(nodeBundle), `"@fake/canvas"`) {
		t.Errorf("the Node bundle must import the package from node_modules:\n%s", nodeBundle)
	}
}

func TestNodeTargetTracesExternalPackagesAndIncludes(t *testing.T) {
	root, cfg := externalsProject(t, nativeFixture(), func(c *config.Config) {
		c.ServerExternalPackages = []string{"@fake/canvas"}
		c.OutputFileTracingIncludes = map[string][]string{
			"/api/native": {"assets/**"},
			"*":           {"assets/worker.js", "missing/**"},
		}
	})
	plan, err := BuildServerBundle(root, cfg, "node")
	if err != nil {
		t.Fatalf("BuildServerBundle: %v", err)
	}
	serverDir := filepath.Dir(plan.Bundle)
	for _, rel := range []string{
		"node_modules/@fake/canvas/index.js",
		"node_modules/@fake/util/index.js",
		"node_modules/@fake/canvas-linux-x64/canvas.node",
		"assets/worker.js",
		"assets/data/a.txt",
	} {
		if _, err := os.Stat(filepath.Join(serverDir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("server output is missing %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(serverDir, "node_modules", "@fake", "canvas-darwin-arm64")); err == nil {
		t.Errorf("an optional package that is not installed must not appear")
	}
	if !strings.Contains(strings.Join(plan.Warnings, "\n"), `"missing/**" matched no files`) {
		t.Errorf("an include that matches nothing must warn: %v", plan.Warnings)
	}

	// The server must run from its own node_modules, not the project's.
	if err := os.RemoveAll(filepath.Join(root, "node_modules", "@fake")); err != nil {
		t.Fatal(err)
	}
	base := startNodeBundle(t, root, cfg)
	res := doMatrix(t, base, "GET", "/api/native", nil, "")
	if res.status != 200 || !strings.Contains(res.body, `"name":"canvas"`) || !strings.Contains(res.body, `"util":"u1"`) {
		t.Errorf("api/native: %d %s", res.status, res.body)
	}
}

func TestServerlessTargetNamesNativeAddon(t *testing.T) {
	_, cfg := externalsProject(t, nativeFixture(), nil)
	root := filepath.Dir(cfg.OutDir)
	_, err := BuildServerBundle(root, cfg, "vercel")
	if err == nil || !strings.Contains(err.Error(), "@fake/canvas-linux-x64") || !strings.Contains(err.Error(), "--target node") {
		t.Fatalf("want an error naming the package and the node target, got %v", err)
	}
}

func TestOutputFileTracingIncludesStayInRoot(t *testing.T) {
	for _, g := range []string{"../secret/**", "/etc/passwd", "a/../../b"} {
		cfg := config.Default()
		cfg.OutputFileTracingIncludes = map[string][]string{"*": {g}}
		if _, err := cfg.Validate(); err == nil {
			t.Errorf("glob %q must be rejected", g)
		}
	}
	cfg := config.Default()
	cfg.OutputFileTracingIncludes = map[string][]string{"*": {"fonts/**", "node_modules/@x/*"}}
	if _, err := cfg.Validate(); err != nil {
		t.Errorf("project-relative globs are valid: %v", err)
	}
}

// A pnpm-style workspace: the app links a package that ships TypeScript
// source. Pages and API routes bundle it, and the node target runs without
// the workspace package being installed anywhere.
func TestWorkspacePackageSourceBundlesAndDeploys(t *testing.T) {
	files := map[string]string{
		"packages/ui/package.json": `{"name":"@acme/ui","version":"0.0.0","main":"src/index.ts"}`,
		"packages/ui/src/index.ts": `export interface Greeting { text: string }
export function greet(name: string): Greeting { return { text: "hello " + name }; }`,
		"apps/web/node_modules/@acme/ui": "symlink:packages/ui",
		"apps/web/package.json":          `{"name":"web","dependencies":{"@acme/ui":"workspace:*"}}`,
		"apps/web/src/pages/index.tsx": `import { greet } from "@acme/ui";
export default function Home() { return <h1>{greet("page").text}</h1>; }`,
		"apps/web/src/api/ws.ts": `import { greet } from "@acme/ui";
export async function GET() { return Response.json(greet("route")); }`,
	}
	root, cfg := externalsProjectAt(t, files, "apps/web", nil)
	html, err := os.ReadFile(filepath.Join(root, "dist", "index.html"))
	if err != nil || !strings.Contains(string(html), "hello page") {
		t.Fatalf("the page must render the workspace package: %v\n%s", err, html)
	}
	if _, err := BuildServerBundle(root, cfg, "node"); err != nil {
		t.Fatalf("BuildServerBundle: %v", err)
	}
	// The Node sidecar artifact (krate start, Docker) bundles the workspace
	// source too: plain Node cannot load its .ts entry from node_modules.
	nodeBundle, err := os.ReadFile(filepath.Join(root, "dist", ".krate", "api-node", "ws.js"))
	if err != nil || strings.Contains(string(nodeBundle), "@acme/ui") || !strings.Contains(string(nodeBundle), "hello ") {
		t.Errorf("the sidecar bundle must contain the workspace package source: %v\n%s", err, nodeBundle)
	}
	// The workspace link is gone at run time: the source is inside the bundle.
	if err := os.Remove(filepath.Join(root, "node_modules", "@acme", "ui")); err != nil {
		t.Fatal(err)
	}
	base := startNodeBundle(t, root, cfg)
	res := doMatrix(t, base, "GET", "/api/ws", nil, "")
	if res.status != 200 || !strings.Contains(res.body, "hello route") {
		t.Errorf("api/ws: %d %s", res.status, res.body)
	}
}
