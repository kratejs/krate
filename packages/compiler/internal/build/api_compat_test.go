package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// compatProject writes files under a temp root and returns a Builder for it.
func compatProject(t *testing.T, files map[string]string, edit func(*config.Config)) (*Builder, string) {
	t.Helper()
	root := t.TempDir()
	for rel, src := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.OutDir = filepath.Join(root, "dist")
	if edit != nil {
		edit(cfg)
	}
	return New(root, cfg), root
}

func readRouteMetas(t *testing.T, root string) map[string]apiRouteMeta {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".krate", "api-routes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var list []apiRouteMeta
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	out := map[string]apiRouteMeta{}
	for _, m := range list {
		out[m.File] = m
	}
	return out
}

func TestAPIRouteUnsupportedAPIWarning(t *testing.T) {
	b, root := compatProject(t, map[string]string{
		"src/api/stream.ts": `import { WebSocket } from "./ws";

export async function GET(req: Request) {
  const upper = new TransformStream<string, string>();
  const ws: WebSocket = new WebSocket();
  return new Response(upper.readable, { headers: { id: crypto.randomUUID() } });
}
`,
		"src/api/ws.ts": "export class WebSocket { send() {} }\n",
	}, nil)
	route := filepath.Join(root, "src", "api", "stream.ts")
	if err := b.CompileAPIRoutes([]string{route}); err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(apiBundleDir(b.Cfg.OutDir), "stream.js")); err != nil {
		t.Fatalf("route bundle missing: %v", err)
	}
	ws := b.RuntimeCompatWarnings()
	if len(ws) != 1 {
		t.Fatalf("warnings = %v, want one for TransformStream", ws)
	}
	w := ws[0]
	if w.Code != diag.CodeAPIRuntimeUnsupported || !w.IsWarning() || w.File != "src/api/stream.ts" || w.Line != 4 || w.Col != 21 {
		t.Errorf("warning = %s:%d:%d %s %q", w.File, w.Line, w.Col, w.Code, w.Message)
	}
	if !strings.Contains(w.Message, "TransformStream") || !strings.Contains(w.Hint, "ReadableStream") || !strings.Contains(w.Source, "new TransformStream") {
		t.Errorf("warning text = %q / %q / %q", w.Message, w.Hint, w.Source)
	}
	meta := readRouteMetas(t, root)["stream.js"]
	if meta.NodeOnly || meta.Features == nil {
		t.Fatalf("meta = %+v, want an embedded route with features", meta)
	}
	if want := []string{"crypto", "fetch", "streams"}; !slices.Equal(*meta.Features, want) {
		t.Errorf("features = %v, want %v", *meta.Features, want)
	}
}

func TestAPIRouteNodeBuiltinIsNodeOnly(t *testing.T) {
	b, root := compatProject(t, map[string]string{
		"src/api/hash.ts": `import { createHash } from "node:crypto";
export function GET() {
  return new Response(Buffer.from(createHash("sha256").update("x").digest()).toString("hex"));
}
`,
	}, nil)
	if err := b.CompileAPIRoutes([]string{filepath.Join(root, "src", "api", "hash.ts")}); err != nil {
		t.Fatal(err)
	}
	meta := readRouteMetas(t, root)["hash.js"]
	if !meta.NodeOnly || meta.Features != nil {
		t.Errorf("meta = %+v, want nodeOnly without features", meta)
	}
	if ws := b.RuntimeCompatWarnings(); len(ws) != 0 {
		t.Errorf("a Node-only route must not be scanned: %v", ws)
	}
}

func TestAPIRouteNodeRuntimeSkipsCompatScan(t *testing.T) {
	b, root := compatProject(t, map[string]string{
		"src/api/t.ts": "export const GET = () => new Response(new TransformStream().readable);\n",
	}, func(c *config.Config) { c.SSR.APIRuntime = "node" })
	if err := b.CompileAPIRoutes([]string{filepath.Join(root, "src", "api", "t.ts")}); err != nil {
		t.Fatal(err)
	}
	if ws := b.RuntimeCompatWarnings(); len(ws) != 0 {
		t.Errorf("warnings with apiRuntime node: %v", ws)
	}
	if meta := readRouteMetas(t, root)["t.js"]; meta.Features != nil {
		t.Errorf("features recorded for a Node runtime route: %v", *meta.Features)
	}
}

func TestMiddlewareUnsupportedAPIWarning(t *testing.T) {
	b, root := compatProject(t, map[string]string{
		"middleware.ts": `export function middleware(req: Request) {
  if (typeof CompressionStream !== "undefined") new CompressionStream("gzip");
  return new Response(Buffer.from("x"));
}
`,
	}, nil)
	if err := b.BuildMiddleware(); err != nil {
		t.Fatal(err)
	}
	ws := b.RuntimeCompatWarnings()
	if len(ws) != 1 || ws[0].File != "middleware.ts" || ws[0].Line != 3 || ws[0].Col != 23 || !strings.Contains(ws[0].Message, "Buffer") || !strings.Contains(ws[0].Hint, "node:buffer") {
		t.Fatalf("warnings = %v, want one for Buffer at middleware.ts:3:23", ws)
	}
	data, err := os.ReadFile(middlewareMetaPath(root))
	if err != nil {
		t.Fatal(err)
	}
	var meta middlewareMeta
	if err := json.Unmarshal(data, &meta); err != nil || meta.Features == nil {
		t.Fatalf("middleware meta = %s (%v)", data, err)
	}
	if want := []string{"fetch", "node", "streams"}; !slices.Equal(*meta.Features, want) {
		t.Errorf("features = %v, want %v", *meta.Features, want)
	}
}

// TestLoadAPIRouteFeatures checks the union the server preloads into pooled
// API workers, and that any route without a complete scan makes it unknown.
func TestLoadAPIRouteFeatures(t *testing.T) {
	for _, tc := range []struct {
		name     string
		manifest string
		want     []string
		known    bool
	}{
		{"missing", "", nil, false},
		{"union", `[{"file":"a.js","nodeOnly":false,"features":["url","crypto"]},{"file":"b.js","nodeOnly":false,"features":["crypto","fetch"]}]`, []string{"crypto", "fetch", "url"}, true},
		{"node-only routes do not count", `[{"file":"a.js","nodeOnly":false,"features":[]},{"file":"n.js","nodeOnly":true}]`, nil, true},
		{"incomplete scan", `[{"file":"a.js","nodeOnly":false,"features":["url"]},{"file":"b.js","nodeOnly":false}]`, nil, false},
		{"invalid", `{`, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.manifest != "" {
				if err := os.MkdirAll(filepath.Join(root, ".krate"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, ".krate", "api-routes.json"), []byte(tc.manifest), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, known := loadAPIRouteFeatures(root)
			if known != tc.known || strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("got %v known=%v, want %v known=%v", got, known, tc.want, tc.known)
			}
		})
	}
}

// TestRuntimeCompatWarningsPerBuild checks that rebuilding a route replaces its
// warnings instead of adding to them, and that a fixed or deleted route drops
// its warnings.
func TestRuntimeCompatWarningsPerBuild(t *testing.T) {
	b, root := compatProject(t, map[string]string{
		"src/api/t.ts": "export const GET = () => new Response(new TransformStream().readable);\n",
	}, nil)
	route := filepath.Join(root, "src", "api", "t.ts")
	for i := 0; i < 3; i++ {
		if err := b.CompileAPIRoutes([]string{route}); err != nil {
			t.Fatal(err)
		}
	}
	if ws := b.RuntimeCompatWarnings(); len(ws) != 1 {
		t.Fatalf("after three builds: %d warnings, want 1: %v", len(ws), ws)
	}
	if err := os.WriteFile(route, []byte("export const GET = () => new Response(\"ok\");\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := b.CompileAPIRoutes([]string{route}); err != nil {
		t.Fatal(err)
	}
	if ws := b.RuntimeCompatWarnings(); len(ws) != 0 {
		t.Fatalf("after the fix: %v", ws)
	}
	if err := os.WriteFile(route, []byte("export const GET = () => new Response(new TransformStream().readable);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := b.CompileAPIRoutes([]string{route}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(route); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildAllAPI(); err != nil {
		t.Fatal(err)
	}
	if ws := b.RuntimeCompatWarnings(); len(ws) != 0 {
		t.Fatalf("after deleting the route: %v", ws)
	}
}

// TestEmbeddedRuntimeOption checks the runtime option values that select the
// embedded runtime; serve and the build both use embeddedRuntime.
func TestEmbeddedRuntimeOption(t *testing.T) {
	for opt, want := range map[string]bool{"": true, "quickjs": true, "QuickJS": true, "node": false, "Bun": false, "deno": false} {
		if got := embeddedRuntime(opt); got != want {
			t.Errorf("embeddedRuntime(%q) = %v, want %v", opt, got, want)
		}
	}
}
