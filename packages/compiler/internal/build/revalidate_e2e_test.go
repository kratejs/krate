package build

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

var pidRe = regexp.MustCompile(`pid-(\d+)`)

// freshConnClient opens a new connection per request so clustered workers
// (which own whole connections) are all reached.
var freshConnClient = &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 30 * time.Second}

// isrRenderNewConn is isrRender over a fresh connection.
func isrRenderNewConn(t *testing.T, base, route string) isrResult {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"route": route, "url": route, "method": "GET", "regions": []map[string]string{{"id": "page", "kind": "page"}}})
	resp, err := freshConnClient.Post(base+"/__krate/regions", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var out isrResult
	for _, line := range strings.Split(string(data), "\n") {
		var fr struct {
			Type, ID, HTML, CacheStatus string
		}
		if json.Unmarshal([]byte(line), &fr) == nil && fr.Type == "region" && fr.ID == "page" {
			out = isrResult{html: fr.HTML, cacheStatus: fr.CacheStatus, cached: fr.CacheStatus == "hit" || fr.CacheStatus == "stale"}
		}
	}
	return out
}

func pidOf(html string) string {
	if m := pidRe.FindStringSubmatch(html); m != nil {
		return m[1]
	}
	return ""
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

// TestRevalidateFansOutToWorkersE2E runs two clustered renderer workers, makes
// sure both hold a cached /posts entry, and checks a single revalidateTag
// purges the entry on both.
func TestRevalidateFansOutToWorkersE2E(t *testing.T) {
	f := startCacheRenderer(t, 2)

	// Requests are spread over the workers; go until both cached the page.
	cached := map[string]string{}
	deadline := time.Now().Add(15 * time.Second)
	for len(cached) < 2 && time.Now().Before(deadline) {
		r := isrRenderNewConn(t, f.base, "/posts")
		if r.cacheStatus == "hit" {
			cached[pidOf(r.html)] = r.html
		}
	}
	if len(cached) < 2 {
		t.Fatalf("could not reach two workers with a cached entry (got %v)", cached)
	}
	f.data.version.Store(2)
	if _, err := f.host.Apply(revalidateRequest{Tags: []string{"posts"}}); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	deadline = time.Now().Add(15 * time.Second)
	for len(seen) < 2 && time.Now().Before(deadline) {
		r := isrRenderNewConn(t, f.base, "/posts")
		if !strings.Contains(r.html, "[v2_posts]") {
			t.Fatalf("worker %s served pre-revalidation HTML: %q (%s)", pidOf(r.html), r.html, r.cacheStatus)
		}
		seen[pidOf(r.html)] = true
	}
	if len(seen) < 2 {
		t.Fatalf("post-revalidation requests reached only %v", seen)
	}
}

// startCacheAPISidecar runs the Node API/action sidecar with the host's env.
func startCacheAPISidecar(t *testing.T, host *revalidateHost, actionsSrc, routes map[string]string, workers int) (*httptest.Server, string) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	ensureRuntimeDist(t)
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0644); err != nil {
		t.Fatal(err)
	}
	cacheURL := "file://" + filepath.ToSlash(filepath.Join(repoRoot, "packages", "runtime", "dist", "cache.js"))
	if !strings.HasPrefix(cacheURL, "file:///") {
		cacheURL = "file:///" + strings.TrimPrefix(cacheURL, "file://")
	}
	actionDir := filepath.Join(dir, "actions")
	apiDir := filepath.Join(dir, "api")
	for _, d := range []string{actionDir, apiDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	var entries []actionEntry
	for name, body := range actionsSrc {
		src := "import { revalidateTag, revalidatePath, unstable_cache } from " + fmt.Sprintf("%q", cacheURL) + ";\n" + body
		if err := os.WriteFile(filepath.Join(actionDir, name+".mjs"), []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, actionEntry{ID: "a" + name, Export: name, File: name + ".mjs", Source: "src/actions/" + name + ".ts"})
	}
	manifest, _ := json.Marshal(actionManifest{Actions: entries})
	if err := os.WriteFile(filepath.Join(actionDir, "manifest.json"), manifest, 0644); err != nil {
		t.Fatal(err)
	}
	for name, body := range routes {
		src := "import { revalidateTag, revalidatePath, unstable_cache } from " + fmt.Sprintf("%q", cacheURL) + ";\n" + body
		if err := os.WriteFile(filepath.Join(apiDir, name+".js"), []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
	scriptPath := filepath.Join(dir, "sidecar.mjs")
	if err := os.WriteFile(scriptPath, []byte(apiServerScriptContent), 0644); err != nil {
		t.Fatal(err)
	}
	cacheRuntime := stageCacheRuntime(repoRoot, filepath.Join(dir, "dist"))
	if cacheRuntime == "" {
		t.Fatal("stageCacheRuntime returned empty path")
	}
	port := freePort(t)
	cmd := exec.Command("node", scriptPath, apiDir, fmt.Sprint(port), "", actionDir)
	cmd.Env = append(os.Environ(), host.EnvList()...)
	cmd.Env = append(cmd.Env, "KRATE_CACHE_RUNTIME="+cacheRuntime, fmt.Sprintf("KRATE_API_WORKERS=%d", workers))
	var logs syncBuffer
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		if t.Failed() {
			t.Logf("sidecar output:\n%s", logs.String())
		}
	})
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	target, _ := url.Parse(base)
	deadline := time.Now().Add(15 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", target.Host, 200*time.Millisecond)
		if err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sidecar did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}
	srv := httptest.NewServer(actionHandler(httputil.NewSingleHostReverseProxy(target), 1<<20))
	t.Cleanup(srv.Close)
	return srv, base
}

// gatedSidecar is a fake SSR renderer whose cache-invalidate handler blocks
// until released, so a test can observe ordering against the action response.
type gatedSidecar struct {
	received chan map[string]any
	release  chan struct{}
}

func newGatedSidecar(t *testing.T, secret string) (*gatedSidecar, *httptest.Server) {
	g := &gatedSidecar{received: make(chan map[string]any, 4), release: make(chan struct{})}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(internalSecretHeader) != secret {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		g.received <- body
		<-g.release
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)
	return g, srv
}

// TestActionRevalidationReadYourWritesE2E: a server action that calls
// revalidateTag/revalidatePath gets its invalidation applied by the host
// before the action response is sent, and the response says so.
func TestActionRevalidationReadYourWritesE2E(t *testing.T) {
	host := newTestRevalidateHost(t)
	gate, ssr := newGatedSidecar(t, host.secret)
	host.ssrBase = ssr.URL

	srv, _ := startCacheAPISidecar(t, host, map[string]string{
		"save":  `export async function save(tag) { revalidateTag(tag); revalidatePath('/blog/hello'); revalidatePath('/about'); return 'saved'; }`,
		"plain": `export async function plain() { return 1; }`,
		"fail":  `export async function fail() { revalidateTag('x'); throw new Error('nope'); }`,
	}, nil, 1)

	type result struct {
		res sidecarResult
	}
	done := make(chan result, 1)
	go func() {
		req := actionRequest(t, srv, "/__krate/action/asave", "application/json", `{"args":["posts"]}`, true)
		done <- result{doAction(t, req)}
	}()

	var got map[string]any
	select {
	case got = <-gate.received:
	case r := <-done:
		t.Fatalf("action answered before the invalidation reached the renderer: %d %s", r.res.status, r.res.raw)
	case <-time.After(20 * time.Second):
		t.Fatal("invalidation never reached the renderer")
	}
	select {
	case r := <-done:
		t.Fatalf("action answered while the invalidation was still running: %s", r.res.raw)
	default:
	}
	close(gate.release)
	var r result
	select {
	case r = <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("action never answered")
	}
	if r.res.status != 200 || r.res.body["result"] != "saved" || r.res.body["revalidated"] != true {
		t.Fatalf("action response: %d %s", r.res.status, r.res.raw)
	}
	rv := r.res.body["revalidation"].(map[string]any)
	if tags := rv["tags"].([]any); len(tags) != 1 || tags[0] != "posts" {
		t.Fatalf("revalidation tags: %v", rv)
	}
	if paths := rv["paths"].([]any); len(paths) != 2 || paths[0].(map[string]any)["path"] != "/blog/hello" {
		t.Fatalf("revalidation paths: %v", rv)
	}
	targets := got["targets"].([]any)
	if len(targets) != 1 || targets[0].(map[string]any)["route"] != "/blog/[slug]" {
		t.Fatalf("renderer targets: %v (the static /about must be skipped)", got)
	}

	// An action that revalidates nothing has no revalidation fields.
	p := doAction(t, actionRequest(t, srv, "/__krate/action/aplain", "application/json", `{"args":[]}`, true))
	if p.status != 200 || p.body["revalidated"] != nil {
		t.Fatalf("plain action: %d %s", p.status, p.raw)
	}

	// A failing action still applies what it queued, then reports the failure.
	go func() {
		<-gate.received
	}()
	fl := doAction(t, actionRequest(t, srv, "/__krate/action/afail", "application/json", `{"args":[]}`, true))
	if fl.status != 500 {
		t.Fatalf("failing action: %d %s", fl.status, fl.raw)
	}
}

// TestAPIRouteRevalidationAndDataCacheE2E: a Node API route's revalidateTag is
// applied before its response, and the host's tag fan-out clears
// unstable_cache entries on every clustered API worker.
func TestAPIRouteRevalidationAndDataCacheE2E(t *testing.T) {
	host := newTestRevalidateHost(t)
	side := &fakeSidecar{secret: host.secret}
	ssr := httptest.NewServer(side.handler(t))
	defer ssr.Close()
	host.ssrBase = ssr.URL

	_, base := startCacheAPISidecar(t, host, nil, map[string]string{
		"write": `export async function POST() { revalidateTag('counter'); return Response.json({ ok: true }); }`,
		"read": `let n = 0;
const get = unstable_cache(async () => ++n, ['counter'], { tags: ['counter'] });
export async function GET() { return Response.json({ pid: process.pid, n: await get() }); }`,
	}, 2)
	host.SetNodeAPI(base)

	// Unauthenticated internal requests are refused by the sidecar.
	resp, err := http.Post(base+"/__krate/internal/cache-invalidate", "application/json", strings.NewReader(`{"tags":["counter"]}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("unauthenticated cache-invalidate: %d", resp.StatusCode)
	}

	read := func() (string, float64) {
		resp, err := freshConnClient.Get(base + "/api/read")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out struct {
			Pid float64 `json:"pid"`
			N   float64 `json:"n"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return fmt.Sprint(out.Pid), out.N
	}
	before := map[string]float64{}
	deadline := time.Now().Add(15 * time.Second)
	for len(before) < 2 && time.Now().Before(deadline) {
		pid, n := read()
		before[pid] = n
	}
	if len(before) < 2 {
		t.Fatalf("reached only %v API workers", before)
	}
	// Cached per worker: repeated reads return the same value.
	for i := 0; i < 10; i++ {
		pid, n := read()
		if n != before[pid] {
			t.Fatalf("worker %s: unstable_cache value changed without revalidation (%v -> %v)", pid, before[pid], n)
		}
	}

	// The write route's revalidateTag reaches the host before it answers.
	resp, err = http.Post(base+"/api/write", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("write route: %d %s", resp.StatusCode, body)
	}
	side.mu.Lock()
	calls := len(side.bodies)
	side.mu.Unlock()
	if calls != 1 {
		t.Fatalf("renderer invalidations when the write route answered: %d, want 1", calls)
	}

	after := map[string]bool{}
	deadline = time.Now().Add(15 * time.Second)
	for len(after) < 2 && time.Now().Before(deadline) {
		pid, n := read()
		if prev, ok := before[pid]; ok && n == prev {
			t.Fatalf("worker %s still served the pre-revalidation unstable_cache value %v", pid, n)
		}
		after[pid] = true
	}
	if len(after) < 2 {
		t.Fatalf("post-revalidation reads reached only %v", after)
	}
}
