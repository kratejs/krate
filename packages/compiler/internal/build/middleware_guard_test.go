package build

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// guardProject lays out root/.krate (optionally with middleware.js) and
// root/dist/.krate/manifest.json with the given middleware record ("" omits it).
func guardProject(t *testing.T, withModule bool, record string) (root, out string) {
	t.Helper()
	root = t.TempDir()
	out = filepath.Join(root, "dist")
	if err := os.MkdirAll(filepath.Join(out, ".krate"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"pages":[]}`
	if record != "" {
		manifest = `{"pages":[],"middleware":` + record + `}`
	}
	if err := os.WriteFile(filepath.Join(out, ".krate", "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if withModule {
		if err := os.MkdirAll(filepath.Join(root, ".krate"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".krate", "middleware.js"), []byte("export function middleware() {}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, out
}

// Serve must refuse to start when the build recorded middleware that was not
// deployed with dist.
func TestServeRefusesWhenRecordedMiddlewareIsMissing(t *testing.T) {
	t.Parallel()
	root, out := guardProject(t, false, "true")
	cfg := config.Default()
	cfg.OutDir = out
	done := make(chan error, 1)
	go func() { done <- Serve(root, cfg, time.Now()) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "middleware.js") || !strings.Contains(err.Error(), ".krate/") {
			t.Fatalf("Serve error = %v, want an actionable missing-middleware error", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve started instead of refusing")
	}
}

// A middleware module that exists but fails to import must not read as "no
// middleware" in the Node sidecar.
func TestNodeSidecarMiddlewareLoadErrorFailsClosed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mwPath := filepath.Join(dir, "middleware.js")
	if err := os.WriteFile(mwPath, []byte("export function middleware( {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	apiDir := filepath.Join(dir, "api")
	_ = os.MkdirAll(apiDir, 0o755)
	base := startNodeConformance(t, apiDir, mwPath)
	resp, err := http.Post(base+"/__krate/middleware", "application/json", strings.NewReader(`{"url":"http://x/admin","method":"GET","path":"/admin","headers":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Action string `json:"action"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%v (%s)", err, raw)
	}
	if out.Action != "error" || out.Error == "" {
		t.Errorf("load failure answered %s, want an error envelope", raw)
	}
}

// A middleware the embedded engine cannot load must stop startup, never be
// served around (that would skip the auth and maintenance gates).
func TestServeRefusesWhenMiddlewareCannotLoad(t *testing.T) {
	t.Parallel()
	root, out := guardProject(t, true, "true")
	src := "import { randomUUID } from \"node:crypto\";\nexport function middleware() { return randomUUID(); }\n"
	if err := os.WriteFile(filepath.Join(root, ".krate", "middleware.js"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.OutDir = out
	done := make(chan error, 1)
	go func() { done <- Serve(root, cfg, time.Now()) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "middleware failed to load") || !strings.Contains(err.Error(), "ssr.middlewareRuntime") {
			t.Fatalf("Serve error = %v, want a middleware load failure with a hint", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve started without its middleware")
	}
}

// A middleware that imports Node built-ins is built for the Node sidecar and
// recorded as such, instead of shipping a bundle QuickJS rejects.
func TestBuildMiddlewareWithNodeImportsTargetsTheSidecar(t *testing.T) {
	t.Parallel()
	b, root := compatProject(t, map[string]string{
		"middleware.ts": `import { randomUUID } from "node:crypto";
export function middleware() {
  return new Response(randomUUID());
}
`,
	}, nil)
	if err := b.BuildMiddleware(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".krate", "middleware.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "__krateCreateRequire") {
		t.Errorf("sidecar bundle lacks the createRequire banner:\n%.300s", data)
	}
	if !readMiddlewareNodeOnly(root) {
		t.Error("middleware.json does not record nodeOnly")
	}
}

// On the Node middleware runtime, serverExternalPackages and packages with a
// native addon stay external, so a startup warm-up in the middleware module
// can import them (they used to fail with "No loader is configured for .node").
func TestNodeMiddlewareKeepsServerExternalPackagesExternal(t *testing.T) {
	t.Parallel()
	b, root := compatProject(t, map[string]string{
		"middleware.ts": `import { tag } from "@fake/ext";
import { addon } from "@fake/native";
export function middleware() {
  return new Response(tag + addon);
}
`,
		"node_modules/@fake/ext/package.json":        `{"name":"@fake/ext","version":"1.0.0","main":"index.js"}`,
		"node_modules/@fake/ext/index.js":            `exports.tag = "ext";`,
		"node_modules/@fake/native/package.json":     `{"name":"@fake/native","version":"1.0.0","main":"index.js"}`,
		"node_modules/@fake/native/index.js":         `exports.addon = require("./build/addon.node").name;`,
		"node_modules/@fake/native/build/addon.node": "binary",
	}, func(c *config.Config) {
		c.SSR.MiddlewareRuntime = "node"
		c.ServerExternalPackages = []string{"@fake/ext"}
	})
	if err := b.BuildMiddleware(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".krate", "middleware.js"))
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []string{`"@fake/ext"`, `"@fake/native"`} {
		if !strings.Contains(string(data), pkg) {
			t.Errorf("middleware bundle does not import %s at run time:\n%.600s", pkg, data)
		}
	}
	if strings.Contains(string(data), `exports.tag = "ext"`) {
		t.Errorf("a serverExternalPackages package was bundled")
	}
}
