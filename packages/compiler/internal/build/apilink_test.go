package build

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestLinkedNodeRoutesShareModules: in a production build the Node bundles of
// the sidecar's routes import the app modules they share from one chunk, so
// the process loads each module once and every route sees the same instance.
func TestLinkedNodeRoutesShareModules(t *testing.T) {
	t.Parallel()
	b, root := compatProject(t, map[string]string{
		"package.json":     `{"type":"module"}`,
		"src/lib/store.ts": "export const hits = { n: 0 };\nexport const big = " + `"` + strings.Repeat("x", 4096) + `"` + ";\n",
		"src/api/add.ts":   `import { hits, big } from "../lib/store"; export function GET() { hits.n++; return Response.json({ n: hits.n, len: big.length }); }`,
		"src/api/read.ts":  `import { hits } from "../lib/store"; export function GET() { return Response.json({ n: hits.n }); }`,
	}, func(c *config.Config) { c.SSR.APIRuntime = "node" })
	routes := []string{filepath.Join(root, "src", "api", "add.ts"), filepath.Join(root, "src", "api", "read.ts")}
	if err := b.CompileAPIRoutes(routes); err != nil {
		t.Fatal(err)
	}
	ndir := apiNodeBundleDir(b.Cfg.OutDir)
	chunks, _ := filepath.Glob(filepath.Join(ndir, nodeRouteChunks, "*.js"))
	if len(chunks) == 0 {
		t.Fatalf("no shared chunk under %s", ndir)
	}
	for _, name := range []string{"add.js", "read.js"} {
		data, err := os.ReadFile(filepath.Join(ndir, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), strings.Repeat("x", 4096)) || !strings.Contains(string(data), nodeRouteChunks+"/") {
			t.Errorf("%s does not import the shared module from a chunk:\n%.400s", name, data)
		}
	}
	base := startSidecarConformance(t, "node", ndir, "")
	get := func(path string) string {
		res, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return string(body)
	}
	if got := get("/api/add"); got != `{"n":1,"len":4096}` {
		t.Fatalf("/api/add = %s", got)
	}
	if got := get("/api/read"); got != `{"n":1}` {
		t.Errorf("/api/read = %s, want the instance /api/add changed", got)
	}
	chunk := strings.TrimSuffix(filepath.Base(chunks[0]), ".js")
	res, err := http.Get(base + "/api/" + nodeRouteChunks + "/" + chunk)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("a shared chunk is served as a route: %d", res.StatusCode)
	}
}

// TestDevNodeRoutesStayUnlinked: dev rebuilds routes one at a time, so their
// bundles stay self-contained.
func TestDevNodeRoutesStayUnlinked(t *testing.T) {
	t.Parallel()
	b, root := compatProject(t, map[string]string{
		"package.json":     `{"type":"module"}`,
		"src/lib/store.ts": `export const hits = { n: 0 };`,
		"src/api/a.ts":     `import { hits } from "../lib/store"; export function GET() { return Response.json(hits); }`,
		"src/api/b.ts":     `import { hits } from "../lib/store"; export function GET() { return Response.json(hits); }`,
	}, func(c *config.Config) { c.SSR.APIRuntime = "node" })
	b.DevMode = true
	if err := b.CompileAPIRoutes([]string{filepath.Join(root, "src", "api", "a.ts"), filepath.Join(root, "src", "api", "b.ts")}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(apiNodeBundleDir(b.Cfg.OutDir), nodeRouteChunks)); !os.IsNotExist(err) {
		t.Errorf("dev build split chunks: %v", err)
	}
}
