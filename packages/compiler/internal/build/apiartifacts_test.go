package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// apiArtifactFiles has a route that bundles for the embedded runtime and one
// that imports a server external package, which only Node can load.
func apiArtifactFiles() map[string]string {
	return map[string]string{
		"src/api/plain.ts":                    `export async function GET() { return Response.json({ ok: true }); }`,
		"src/api/ext.ts":                      "import { tag } from \"@fake/ext\";\nexport async function GET() { return Response.json({ tag }); }",
		"node_modules/@fake/ext/package.json": `{"name":"@fake/ext","version":"1.0.0","main":"index.js"}`,
		"node_modules/@fake/ext/index.js":     `exports.tag = "ext";`,
	}
}

func compileArtifactRoutes(t *testing.T, root string, runtime string) *Builder {
	t.Helper()
	cfg := config.Default()
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.SSR.APIRuntime = runtime
	cfg.ServerExternalPackages = []string{"@fake/ext"}
	b := New(root, cfg)
	files := []string{filepath.Join(root, "src", "api", "plain.ts"), filepath.Join(root, "src", "api", "ext.ts")}
	if err := b.CompileAPIRoutes(files); err != nil {
		t.Fatal(err)
	}
	return b
}

func artifactExists(t *testing.T, dir, name string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

// A route gets the artifacts of the runtime that serves it: the embedded
// runtime's for every route plus Node's for a Node-only route by default, and
// Node's alone with apiRuntime node. Switching drops the other runtime's.
func TestAPIRouteArtifactsFollowRuntime(t *testing.T) {
	_, root := compatProject(t, apiArtifactFiles(), nil)
	type want struct{ quickjs, node bool }
	cases := []struct {
		runtime string
		plain   want
		ext     want
	}{
		{"", want{true, false}, want{true, true}},
		{"node", want{false, true}, want{false, true}},
		{"quickjs", want{true, false}, want{true, true}},
	}
	for _, c := range cases {
		b := compileArtifactRoutes(t, root, c.runtime)
		qdir, ndir := apiBundleDir(b.Cfg.OutDir), apiNodeBundleDir(b.Cfg.OutDir)
		for name, w := range map[string]want{"plain.js": c.plain, "ext.js": c.ext} {
			if got := artifactExists(t, qdir, name); got != w.quickjs {
				t.Errorf("apiRuntime %q: QuickJS %s exists=%v, want %v", c.runtime, name, got, w.quickjs)
			}
			if got := artifactExists(t, ndir, name); got != w.node {
				t.Errorf("apiRuntime %q: Node %s exists=%v, want %v", c.runtime, name, got, w.node)
			}
		}
		metas := readRouteMetas(t, root)
		if c.runtime != "node" && !metas["ext.js"].NodeOnly {
			t.Errorf("apiRuntime %q: ext.js must be Node-only: %+v", c.runtime, metas["ext.js"])
		}
		if msg := apiArtifactMismatch(c.runtime, qdir, ndir); msg != "" {
			t.Errorf("apiRuntime %q: a fresh build reports a mismatch: %s", c.runtime, msg)
		}
	}
	// Serving a build for one runtime with the other asks for a rebuild. The
	// Node-only route is removed so the embedded build has no Node artifact.
	b := compileArtifactRoutes(t, root, "")
	qdir, ndir := apiBundleDir(b.Cfg.OutDir), apiNodeBundleDir(b.Cfg.OutDir)
	if err := os.Remove(filepath.Join(ndir, "ext.js")); err != nil {
		t.Fatal(err)
	}
	if msg := apiArtifactMismatch("node", qdir, ndir); !strings.Contains(msg, "krate build") {
		t.Errorf("serving an embedded-runtime build with apiRuntime node must ask for a rebuild, got %q", msg)
	}
	b = compileArtifactRoutes(t, root, "node")
	if msg := apiArtifactMismatch("", apiBundleDir(b.Cfg.OutDir), apiNodeBundleDir(b.Cfg.OutDir)); !strings.Contains(msg, "krate build") {
		t.Errorf("serving a node build with the embedded runtime must ask for a rebuild, got %q", msg)
	}
}
