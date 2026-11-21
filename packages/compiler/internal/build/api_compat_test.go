package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
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

func TestAPIRouteNodeBuiltinIsNodeOnly(t *testing.T) {
	t.Parallel()
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

func compileRoutes(t *testing.T, b *Builder, root string, rels ...string) error {
	t.Helper()
	var routes []string
	for _, r := range rels {
		routes = append(routes, filepath.Join(root, filepath.FromSlash(r)))
	}
	return b.CompileAPIRoutes(routes)
}

func captureProgress(t *testing.T) *strings.Builder {
	t.Helper()
	var sb strings.Builder
	SetOutput(&sb)
	t.Cleanup(func() { SetOutput(os.Stdout) })
	return &sb
}

func TestAPIRouteGlobalBufferMovesToNode(t *testing.T) {
	b, root := compatProject(t, map[string]string{
		"src/api/enc.ts": `export function GET() {
  return Response.json({ b64: Buffer.from("hi").toString("base64") });
}
`,
		"src/api/plain.ts": `export function GET() { return Response.json({ ok: true }); }
`,
	}, nil)
	out := captureProgress(t)
	if err := compileRoutes(t, b, root, "src/api/enc.ts", "src/api/plain.ts"); err != nil {
		t.Fatal(err)
	}
	metas := readRouteMetas(t, root)
	if !metas["enc.js"].NodeOnly {
		t.Errorf("enc.js must be served by Node: %+v", metas["enc.js"])
	}
	if metas["plain.js"].NodeOnly {
		t.Errorf("plain.js must stay embedded: %+v", metas["plain.js"])
	}
	if ws := b.RuntimeCompatWarnings(); len(ws) != 0 {
		t.Errorf("no KR7160 expected for a route moved to Node: %v", ws)
	}
	if !strings.Contains(out.String(), "enc.ts -> Node (Buffer at src/api/enc.ts:2)") {
		t.Errorf("missing info line:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(root, "dist", ".krate", "api-node", "enc.js")); err != nil {
		t.Errorf("Node bundle missing: %v", err)
	}
}

// The user's case: Buffer is used by the route file and by a helper it imports.
func TestAPIRouteHelperBufferMovesToNode(t *testing.T) {
	b, root := compatProject(t, map[string]string{
		"src/server/embed.ts": `export function encode(s: string) {
  return Buffer.from(s).toString("hex");
}
`,
		"src/api/one.ts": `import { encode } from "../server/embed";
export function GET() { return Response.json({ hex: encode("a") + Buffer.from("b").toString("hex") }); }
`,
		"src/api/two.ts": `import { encode } from "../server/embed";
export function GET() { return Response.json({ hex: encode("a") }); }
`,
	}, nil)
	out := captureProgress(t)
	if err := compileRoutes(t, b, root, "src/api/one.ts", "src/api/two.ts"); err != nil {
		t.Fatal(err)
	}
	metas := readRouteMetas(t, root)
	for _, f := range []string{"one.js", "two.js"} {
		if !metas[f].NodeOnly {
			t.Errorf("%s must be served by Node: %+v", f, metas[f])
		}
	}
	if !strings.Contains(out.String(), "two.ts -> Node (Buffer at src/server/embed.ts:2)") {
		t.Errorf("the info line must name the helper:\n%s", out.String())
	}
	if ws := b.RuntimeCompatWarnings(); len(ws) != 0 {
		t.Errorf("unexpected warnings: %v", ws)
	}
}

func TestAPIRouteSingleCompileMatchesBatch(t *testing.T) {
	b, root := compatProject(t, map[string]string{
		"src/api/one.ts": `export function GET() { return Response.json({ cwd: process.cwd() }); }
`,
	}, nil)
	captureProgress(t)
	if err := compileRoutes(t, b, root, "src/api/one.ts"); err != nil {
		t.Fatal(err)
	}
	if !readRouteMetas(t, root)["one.js"].NodeOnly {
		t.Error("a single-route compile must make the same decision as the batch")
	}
}

func TestAPIRouteGuardedBufferStaysEmbedded(t *testing.T) {
	b, root := compatProject(t, map[string]string{
		"src/api/g.ts": `export function GET() {
  const b = typeof Buffer !== "undefined" ? Buffer.from("x").toString("hex") : "none";
  return Response.json({ b });
}
`,
	}, nil)
	captureProgress(t)
	if err := compileRoutes(t, b, root, "src/api/g.ts"); err != nil {
		t.Fatal(err)
	}
	if readRouteMetas(t, root)["g.js"].NodeOnly {
		t.Error("a guarded reference must not move the route")
	}
}

func TestAPIRouteExplicitQuickJSMakesKR7160AnError(t *testing.T) {
	b, root := compatProject(t, map[string]string{
		"src/server/embed.ts": `export const enc = (s: string) => Buffer.from(s).toString("hex");
`,
		"src/api/one.ts": `import { enc } from "../server/embed";
export function GET() { return Response.json({ a: enc("a"), b: Buffer.from("b").length }); }
`,
	}, func(c *config.Config) { c.SSR.APIRuntime = "quickjs" })
	captureProgress(t)
	err := compileRoutes(t, b, root, "src/api/one.ts")
	if err == nil {
		t.Fatal("ssr.apiRuntime \"quickjs\" with an unsupported global must fail the build")
	}
	msg := err.Error()
	for _, want := range []string{"KR7160", "src/api/one.ts", "src/server/embed.ts"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error lacks %q:\n%s", want, msg)
		}
	}
}

func TestAPIRouteRuntimeExport(t *testing.T) {
	t.Run("edge with Buffer is an error", func(t *testing.T) {
		b, root := compatProject(t, map[string]string{
			"src/api/e.ts": `export const runtime = "edge";
export function GET() { return Response.json({ b: Buffer.from("x").length }); }
`,
		}, nil)
		captureProgress(t)
		err := compileRoutes(t, b, root, "src/api/e.ts")
		if err == nil || !strings.Contains(err.Error(), "KR7160") {
			t.Fatalf("want a KR7160 error, got %v", err)
		}
	})
	t.Run("edge without unsupported refs stays embedded", func(t *testing.T) {
		b, root := compatProject(t, map[string]string{
			"src/api/e.ts": `export const runtime = 'edge';
export function GET() { return Response.json({ ok: 1 }); }
`,
		}, nil)
		captureProgress(t)
		if err := compileRoutes(t, b, root, "src/api/e.ts"); err != nil {
			t.Fatal(err)
		}
		if readRouteMetas(t, root)["e.js"].NodeOnly {
			t.Error("an edge route stays embedded")
		}
	})
	t.Run("nodejs goes to Node with no refs", func(t *testing.T) {
		b, root := compatProject(t, map[string]string{
			"src/api/n.ts": `export const runtime = "nodejs";
export function GET() { return Response.json({ ok: 1 }); }
`,
			"src/api/m.ts": `export const runtime = "nodejs";
export function GET() { return Response.json({ ok: 2 }); }
`,
		}, nil)
		captureProgress(t)
		if err := compileRoutes(t, b, root, "src/api/n.ts", "src/api/m.ts"); err != nil {
			t.Fatal(err)
		}
		metas := readRouteMetas(t, root)
		if !metas["n.js"].NodeOnly || !metas["m.js"].NodeOnly {
			t.Errorf("nodejs routes must be Node-only: %+v", metas)
		}
	})
}
