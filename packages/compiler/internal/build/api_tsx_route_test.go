package build

import (
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/native/jsruntime"
)

// TestAPIRouteTSX: a route handler written in .tsx (app/og/route.tsx with an
// ImageResponse tree, migrated to src/api/og.tsx) is built and served, and its
// JSX is the server element tree. It used to be skipped by the build.
func TestAPIRouteTSX(t *testing.T) {
	t.Parallel()
	ensureRuntimeDist(t)
	files := map[string]string{
		"package.json": `{"name":"tsx-route","type":"module"}`,
		"src/api/og.tsx": `import { renderToString } from "@krate/runtime/server";
const Title = (p: { text: string }) => <b>{p.text}</b>;
export function GET() {
  return new Response(renderToString(<p class="x"><Title text="hi" /> {1 + 1}</p>), { headers: { "content-type": "text/html" } });
}
`,
		"src/api/types.d.ts": `export type Unused = string;`,
	}
	const want = `<p class="x"><b>hi</b> 2</p>`
	for _, runtime := range []string{"quickjs", "node"} {
		t.Run(runtime, func(t *testing.T) {
			b, root := compatProject(t, files, func(c *config.Config) { c.SSR.APIRuntime = runtime })
			if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
				t.Fatal(err)
			}
			if err := b.buildJSAPI(); err != nil {
				t.Fatal(err)
			}
			metas := readRouteMetas(t, root)
			if _, ok := metas["og.js"]; !ok {
				t.Fatalf("og.tsx was not built: %v", metas)
			}
			if _, ok := metas["types.js"]; ok {
				t.Errorf("a .d.ts file was built as a route")
			}
			var got string
			if runtime == "quickjs" {
				res := jsruntime.NewAPIRouteRuntime(apiBundleDir(b.Cfg.OutDir)).Execute(jsruntime.APIRequest{Method: "GET", Path: "/api/og"})
				if res.Status != 200 {
					t.Fatalf("GET /api/og: %d %s %s", res.Status, res.Body, res.Error)
				}
				got = string(res.Body)
			} else {
				base := startSidecarConformance(t, "node", apiNodeBundleDir(b.Cfg.OutDir), "")
				res, err := http.Get(base + "/api/og")
				if err != nil {
					t.Fatal(err)
				}
				body, _ := io.ReadAll(res.Body)
				res.Body.Close()
				if res.StatusCode != 200 {
					t.Fatalf("GET /api/og: %d %s", res.StatusCode, body)
				}
				got = string(body)
			}
			if got != want {
				t.Errorf("body = %q, want %q", got, want)
			}
		})
	}
}
