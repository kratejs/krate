package build

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
)

// cacheDataServer is the upstream data source the e2e pages fetch from. Its
// version is bumped by the test to simulate a write.
type cacheDataServer struct {
	*httptest.Server
	version atomic.Int64
	hits    atomic.Int64
}

func newCacheDataServer(t *testing.T) *cacheDataServer {
	t.Helper()
	d := &cacheDataServer{}
	d.version.Store(1)
	d.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.hits.Add(1)
		_, _ = fmt.Fprintf(w, "v%d%s", d.version.Load(), strings.ReplaceAll(r.URL.Path, "/", "_"))
	}))
	t.Cleanup(d.Close)
	return d
}

// Pages for the cache e2e. /posts reads a tagged fetch (not data-cached, the
// page's ISR entry carries the tag). /item/[id] reads a data-cached tagged
// fetch. Both print the worker pid so clustered tests can tell workers apart.
const cachePostsPage = `
export default async function Posts() {
  const res = await fetch(process.env.KRATE_TEST_DATA_URL + "/posts", { next: { tags: ["posts"] } });
  const body = await res.text();
  return <div class="posts">[{body}] pid-{String(process.pid)}</div>;
}
`

const cacheItemPage = `
export default async function Item(props) {
  const id = props.params ? props.params.id : "none";
  const res = await fetch(process.env.KRATE_TEST_DATA_URL + "/item/" + id, { next: { tags: ["item"], revalidate: 3600 } });
  const body = await res.text();
  return <div class="item">[{body}] pid-{String(process.pid)}</div>;
}
`

// bundleCachePage compiles a page into a server bundle the way
// TestISRVariantCacheE2E does (@krate/runtime/server aliased to source).
func bundleCachePage(t *testing.T, repoRoot, outDir, rel, src, sourcefile string) {
	t.Helper()
	runtimeSrc := filepath.Join(repoRoot, "packages", "runtime", "src")
	serverTS := filepath.Join(runtimeSrc, "server.ts")
	res := api.Build(api.BuildOptions{
		AbsWorkingDir:   repoRoot,
		Bundle:          true,
		Format:          api.FormatESModule,
		Platform:        api.PlatformNode,
		Outfile:         filepath.Join(outDir, filepath.FromSlash(rel)),
		Write:           true,
		JSX:             api.JSXAutomatic,
		JSXImportSource: "@krate/runtime/server",
		Plugins: []api.Plugin{{
			Name: "krate-cache-e2e-alias",
			Setup: func(build api.PluginBuild) {
				build.OnResolve(api.OnResolveOptions{Filter: `^@krate/runtime/server(/|$)`},
					func(args api.OnResolveArgs) (api.OnResolveResult, error) {
						spec := strings.TrimPrefix(args.Path, "@krate/runtime/server/")
						if spec == args.Path || spec == "" {
							return api.OnResolveResult{Path: serverTS}, nil
						}
						return api.OnResolveResult{Path: filepath.Join(runtimeSrc, "server-"+spec+".ts")}, nil
					})
			},
		}},
		Stdin: &api.StdinOptions{Loader: api.LoaderTSX, Contents: src, Sourcefile: sourcefile},
	})
	if len(res.Errors) > 0 {
		t.Fatalf("bundle %s: %s", rel, res.Errors[0].Text)
	}
}

// cacheRendererFixture is a running SSR renderer plus the Go revalidation host
// wired to it.
type cacheRendererFixture struct {
	base string
	host *revalidateHost
	data *cacheDataServer
}

func startCacheRenderer(t *testing.T, workers int) *cacheRendererFixture {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		requireE2E(t, "node not available: %v", err)
	}
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "packages", "runtime", "src", "cache.ts")); err != nil {
		requireE2E(t, "@krate/runtime source not found: %v", err)
	}
	root := t.TempDir()
	outDir := filepath.Join(root, "dist")
	bundleCachePage(t, repoRoot, outDir, ".krate/server-bundles/posts.server.mjs", cachePostsPage, "posts.tsx")
	bundleCachePage(t, repoRoot, outDir, ".krate/server-bundles/item.server.mjs", cacheItemPage, "item/[id].tsx")

	sm := ServerManifest{Pages: []ManifestPage{
		{Route: "/posts", Mode: "isr", Revalidate: 3600, BundlePath: ".krate/server-bundles/posts.server.mjs"},
		{Route: "/item/[id]", Mode: "isr", Revalidate: 3600, BundlePath: ".krate/server-bundles/item.server.mjs"},
	}}
	smData, _ := json.Marshal(sm)
	if err := os.WriteFile(filepath.Join(outDir, "server-manifest.json"), smData, 0644); err != nil {
		t.Fatal(err)
	}
	// The full manifest (all modes) is what the host resolves paths against.
	full := Manifest{Pages: []PageMeta{
		{Route: "/about", Mode: RenderSSG},
		{Route: "/posts", Mode: RenderISR, Revalidate: 3600},
		{Route: "/item/[id]", Mode: RenderISR, Revalidate: 3600},
	}}
	fullData, _ := json.Marshal(full)
	if err := os.WriteFile(filepath.Join(outDir, "manifest.json"), fullData, 0644); err != nil {
		t.Fatal(err)
	}

	staged := stageServerRenderer(repoRoot, outDir)
	if staged == "" {
		t.Fatal("stageServerRenderer returned empty path")
	}
	host, err := newRevalidateHost(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(host.Close)
	t.Setenv("KRATE_TEST_INTERNAL_SECRET", host.secret)
	data := newCacheDataServer(t)

	port := freePort(t)
	cmd := exec.Command(node, staged)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("KRATE_SSR_PORT=%d", port),
		fmt.Sprintf("KRATE_MANIFEST=%s", filepath.Join(outDir, "server-manifest.json")),
		fmt.Sprintf("KRATE_ROOT=%s", root),
		"KRATE_INTERNAL_SECRET="+host.secret,
		"KRATE_TEST_DATA_URL="+data.URL,
		fmt.Sprintf("KRATE_SSR_WORKERS=%d", workers),
	)
	var logs syncBuffer
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting renderer: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("renderer output:\n%s", logs.String())
		}
	})
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	if !waitHealth(t, base) {
		t.Fatal("renderer did not become ready")
	}
	host.ssrBase = base
	return &cacheRendererFixture{base: base, host: host, data: data}
}

// TestRevalidateTagAndPathE2E renders ISR pages that read tagged fetches
// against a local data server and checks that on-demand revalidation through
// the Go host purges exactly the right entries in the Node renderer.
func TestRevalidateTagAndPathE2E(t *testing.T) {
	f := startCacheRenderer(t, 1)

	// 1. miss then hit; a data change alone does not show through the cache.
	r1 := isrRender(t, f.base, "/posts", "/posts", nil)
	if r1.cacheStatus != "miss" || !strings.Contains(r1.html, "[v1_posts]") {
		t.Fatalf("first render: %+v", r1)
	}
	if r2 := isrRender(t, f.base, "/posts", "/posts", nil); r2.cacheStatus != "hit" || r2.html != r1.html {
		t.Fatalf("second render: %+v", r2)
	}
	f.data.version.Store(2)
	if r3 := isrRender(t, f.base, "/posts", "/posts", nil); r3.cacheStatus != "hit" || !strings.Contains(r3.html, "[v1_posts]") {
		t.Fatalf("cached render after data change: %+v", r3)
	}

	// 2. The mutating sidecar endpoints refuse requests without the secret.
	if res := postJSON(t, f.base, "/__krate/ssr/cache-invalidate", `{"tags":["posts"]}`); res.status != http.StatusForbidden {
		t.Fatalf("unauthenticated cache-invalidate: status %d", res.status)
	}
	if res := postJSON(t, f.base, "/__krate/ssr/revalidate", `{"route":"/posts"}`); res.status != http.StatusForbidden {
		t.Fatalf("unauthenticated revalidate: status %d", res.status)
	}

	// 3. An unrelated tag leaves the entry alone; revalidateTag("posts") purges it.
	if _, err := f.host.Apply(revalidateRequest{Tags: []string{"other"}}); err != nil {
		t.Fatal(err)
	}
	if r := isrRender(t, f.base, "/posts", "/posts", nil); r.cacheStatus != "hit" {
		t.Fatalf("unrelated tag purged /posts: %+v", r)
	}
	res, err := f.host.Apply(revalidateRequest{Tags: []string{"posts"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Revalidated) != 1 || res.Revalidated[0].Kind != "tag" {
		t.Fatalf("tag result: %+v", res)
	}
	r4 := isrRender(t, f.base, "/posts", "/posts", nil)
	if r4.cacheStatus != "miss" || !strings.Contains(r4.html, "[v2_posts]") {
		t.Fatalf("render after revalidateTag: %+v", r4)
	}

	// 4. revalidatePath on a concrete URL purges that variant (and the data its
	// render cached) only.
	a1 := isrRender(t, f.base, "/item/[id]", "/item/a", map[string]string{"id": "a"})
	b1 := isrRender(t, f.base, "/item/[id]", "/item/b", map[string]string{"id": "b"})
	if !strings.Contains(a1.html, "[v2_item_a]") || !strings.Contains(b1.html, "[v2_item_b]") {
		t.Fatalf("item renders: %q %q", a1.html, b1.html)
	}
	f.data.version.Store(3)
	if _, err := f.host.Apply(revalidateRequest{Paths: []revalidatePathReq{{Path: "/item/a", Type: "page"}}}); err != nil {
		t.Fatal(err)
	}
	a2 := isrRender(t, f.base, "/item/[id]", "/item/a", map[string]string{"id": "a"})
	if a2.cacheStatus != "miss" || !strings.Contains(a2.html, "[v3_item_a]") {
		t.Fatalf("item a after revalidatePath: %+v", a2)
	}
	if b2 := isrRender(t, f.base, "/item/[id]", "/item/b", map[string]string{"id": "b"}); b2.cacheStatus != "hit" || b2.html != b1.html {
		t.Fatalf("item b must stay cached: %+v", b2)
	}

	// 5. revalidateTag("item") drops the data-cache entries and every variant.
	f.data.version.Store(4)
	if _, err := f.host.Apply(revalidateRequest{Tags: []string{"item"}}); err != nil {
		t.Fatal(err)
	}
	b3 := isrRender(t, f.base, "/item/[id]", "/item/b", map[string]string{"id": "b"})
	if b3.cacheStatus != "miss" || !strings.Contains(b3.html, "[v4_item_b]") {
		t.Fatalf("item b after revalidateTag(item): %+v", b3)
	}

	// 6. The dynamic pattern targets every variant.
	f.data.version.Store(5)
	isrRender(t, f.base, "/item/[id]", "/item/a", map[string]string{"id": "a"})
	if _, err := f.host.Apply(revalidateRequest{Paths: []revalidatePathReq{{Path: "/item/[id]"}}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		r := isrRender(t, f.base, "/item/[id]", "/item/"+id, map[string]string{"id": id})
		if r.cacheStatus != "miss" || !strings.Contains(r.html, "[v5_item_"+id+"]") {
			t.Fatalf("item %s after pattern revalidatePath: %+v", id, r)
		}
	}

	// 7. A static page is reported, not silently accepted.
	res, err = f.host.Apply(revalidateRequest{Paths: []revalidatePathReq{{Path: "/about"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0].Reason, "static (SSG)") || len(res.Revalidated) != 0 {
		t.Fatalf("static page result: %+v", res)
	}
}
