package build

import (
	"encoding/json"
	"os"
	"path/filepath"
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
