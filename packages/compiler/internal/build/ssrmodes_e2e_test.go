package build

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestBuildServerModesManifestE2E builds a synthetic project whose pages opt
// into every rendering mode and verifies the decisions survive all the way to
// disk: manifest.json (Go) and server-manifest.json (Node renderer). It also
// guards the ssr-backend unification by asserting the orphaned QuickJS SSR
// bundle staging (.krate/ssr-bundles) is gone and only sidecar bundles exist.
func TestBuildServerModesManifestE2E(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}

	pages := map[string]string{
		// Plain SSG - must NOT appear in the server manifest.
		"index.tsx": `export default function P() { return <div>home</div>; }`,
		// Explicit ISR with revalidate.
		"isr.tsx": "export const config = { isr: true, revalidate: 30 };\nexport default function P() { return <div>isr</div>; }",
		// ISR without revalidate -> defaults to 60s.
		"isr-default.tsx": "export const config = { isr: true };\nexport default function P() { return <div>isr-default</div>; }",
		// Explicit SSR.
		"ssr.tsx": "export const config = { ssr: true };\nexport default function P() { return <div>ssr</div>; }",
		// Explicit streaming config.
		"streaming.tsx": "export const config = { streaming: true };\nexport default function P() { return <div>streaming</div>; }",
		// <Suspense> usage without config -> auto streaming.
		"suspense.tsx": "export default function P() { return <Suspense fallback={<span>load</span>}><div>resolved</div></Suspense>; }",
		// isr beats streaming in precedence.
		"precedence.tsx": "export const config = { isr: true, streaming: true, revalidate: 5 };\nexport default function P() { return <div>precedence</div>; }",
		// A runtime component inside a <Suspense> boundary -> a streaming page
		// that owns a suspense-primary region. The page is marked // @server so
		// importing the *.runtime.tsx component is a legal composition.
		"live.tsx": "// @server\nimport { Live } from '../Live.runtime'; export default function P() { return <Suspense fallback={<span>loading</span>}><Live name=\"world\" /></Suspense>; }",
	}
	for name, src := range pages {
		if err := os.WriteFile(filepath.Join(pagesDir, name), []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// A *.runtime.tsx component under src/ - the file convention tiers it
	// runtime, so it is a valid suspense-primary region target. Arrow-fn form:
	// the annotator must collect `export const` components like FnDecls.
	runtimeSrc := "export const Live = ({ name }: any) => <p>live {name}</p>;"
	if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "Live.runtime.tsx"), []byte(runtimeSrc), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = filepath.Join(root, "dist")
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	outDir := cfg.OutDir

	// Full manifest: every page (including ssg) appears with its mode.
	manData, err := os.ReadFile(ManifestPath(outDir))
	if err != nil {
		t.Fatalf("reading manifest.json: %v", err)
	}
	var fullMan Manifest
	if err := json.Unmarshal(manData, &fullMan); err != nil {
		t.Fatalf("unmarshal manifest.json: %v\n%s", err, manData)
	}
	full := make(map[string]RenderMode, len(fullMan.Pages))
	for _, p := range fullMan.Pages {
		full[p.Route] = p.Mode
	}
	wantFull := map[string]RenderMode{
		"/":            RenderSSG,
		"/isr":         RenderISR,
		"/isr-default": RenderISR,
		"/ssr":         RenderSSR,
		"/streaming":   RenderStreaming,
		"/suspense":    RenderStreaming,
		"/live":        RenderStreaming,
		"/precedence":  RenderISR,
	}
	for route, want := range wantFull {
		if got, ok := full[route]; !ok || got != want {
			t.Errorf("manifest.json route %s: got mode %v (ok=%v), want %v", route, got, ok, want)
		}
	}

	// Server manifest: only non-SSG pages, with string modes + revalidate.
	srvData, err := os.ReadFile(ServerManifestPath(outDir))
	if err != nil {
		t.Fatalf("reading server-manifest.json: %v", err)
	}
	var srvMan struct {
		Pages []struct {
			Route      string `json:"route"`
			Mode       string `json:"mode"`
			Revalidate int    `json:"revalidate,omitempty"`
			BundlePath string `json:"bundlePath"`
		} `json:"pages"`
	}
	if err := json.Unmarshal(srvData, &srvMan); err != nil {
		t.Fatalf("unmarshal server-manifest.json: %v\n%s", err, srvData)
	}
	srv := make(map[string]struct {
		mode       string
		revalidate int
	}, len(srvMan.Pages))
	for _, p := range srvMan.Pages {
		srv[p.Route] = struct {
			mode       string
			revalidate int
		}{p.Mode, p.Revalidate}
	}

	if _, ok := srv["/index"]; ok {
		t.Error("SSG page /index leaked into server-manifest.json")
	}
	wantSrv := map[string]struct {
		mode       string
		revalidate int
	}{
		"/isr":         {"isr", 30},
		"/isr-default": {"isr", defaultISRRevalidate},
		"/ssr":         {"ssr", 0},
		"/streaming":   {"streaming", 0},
		"/suspense":    {"streaming", 0},
		"/live":        {"streaming", 0},
		"/precedence":  {"isr", 5},
	}
	for route, want := range wantSrv {
		got, ok := srv[route]
		if !ok {
			t.Errorf("server-manifest missing route %s (want mode %s)", route, want.mode)
			continue
		}
		if got.mode != want.mode || got.revalidate != want.revalidate {
			t.Errorf("server-manifest route %s: got (mode=%s revalidate=%d), want (mode=%s revalidate=%d)",
				route, got.mode, got.revalidate, want.mode, want.revalidate)
		}
	}

	// Sidecar unification regression: no QuickJS SSR bundles, but sidecar
	// server bundles exist for every non-SSG page and are referenced.
	bundlesDir := filepath.Join(outDir, ".krate", "server-bundles")
	if info, err := os.Stat(filepath.Join(outDir, ".krate", "ssr-bundles")); err == nil && info.IsDir() {
		t.Error(".krate/ssr-bundles (orphaned QuickJS SSR) still exists")
	}
	// Only pages that need the Node renderer (SSR/ISR, or any non-Go dynamic
	// region) get a compiled server bundle; a streaming page with no dynamic
	// regions is served statically and has none.
	var bundleManifest Manifest
	if data, err := os.ReadFile(ManifestPath(outDir)); err == nil {
		_ = json.Unmarshal(data, &bundleManifest)
	}
	needsNode := func(route, mode string) bool {
		if mode == "ssr" || mode == "isr" {
			return true
		}
		for _, reg := range bundleManifest.Regions[route] {
			if reg.Runtime != "go" {
				return true
			}
		}
		return false
	}
	for _, p := range srvMan.Pages {
		if !needsNode(p.Route, p.Mode) {
			if p.BundlePath != "" {
				t.Errorf("route %s has a server bundle but does not need the Node renderer", p.Route)
			}
			continue
		}
		if p.BundlePath == "" {
			t.Errorf("route %s has empty bundlePath", p.Route)
			continue
		}
		bundleAbs := filepath.Join(outDir, filepath.FromSlash(p.BundlePath))
		if _, err := os.Stat(bundleAbs); os.IsNotExist(err) {
			t.Errorf("route %s bundle not emitted at %s", p.Route, bundleAbs)
		}
		if dir := filepath.Dir(filepath.FromSlash(p.BundlePath)); dir != ".krate\\server-bundles" && !strings.Contains(dir, "server-bundles") {
			t.Errorf("route %s bundle %s not under .krate/server-bundles", p.Route, p.BundlePath)
		}
	}
	_ = bundlesDir

	// Region registry: the /live page owns a suspense-primary region backed by
	// the Live runtime component.
	var fullMan2 Manifest
	manData2, err := os.ReadFile(ManifestPath(outDir))
	if err != nil {
		t.Fatalf("re-reading manifest.json: %v", err)
	}
	if err := json.Unmarshal(manData2, &fullMan2); err != nil {
		t.Fatalf("unmarshal manifest.json (regions): %v", err)
	}
	liveRegs := fullMan2.Regions["/live"]
	if len(liveRegs) == 0 {
		t.Fatalf("expected region registry entries for /live, got none: %+v", fullMan2.Regions)
	}
	found := false
	for _, r := range liveRegs {
		if r.Component == "Live" && r.Suspense && strings.HasSuffix(r.SourcePath, "Live.runtime.tsx") {
			found = true
			// The region is backed by the already-compiled runtime component
			// bundle (no per-region esbuild pass) - the sidecar loads this
			// bundle and calls __krate_render(props) to render the region.
			if !strings.HasSuffix(r.BundlePath, "Live.runtime.js") {
				t.Errorf("expected region BundlePath to point at the compiled runtime bundle, got %q", r.BundlePath)
			}
			// Build-time resolved props are baked into the registry so the
			// sidecar renders the region without re-deriving anything.
			if got := r.Props["name"]; got != "world" {
				t.Errorf("expected baked region props name=world, got %v", r.Props)
			}
		}
	}
	if !found {
		t.Errorf("expected a suspense region for component Live from Live.runtime.tsx, got %+v", liveRegs)
	}

	// The server manifest carries the same region registry.
	var srvMan2 struct {
		Regions map[string][]RegionMeta `json:"regions"`
	}
	srvData2, err := os.ReadFile(ServerManifestPath(outDir))
	if err != nil {
		t.Fatalf("re-reading server-manifest.json: %v", err)
	}
	if err := json.Unmarshal(srvData2, &srvMan2); err != nil {
		t.Fatalf("unmarshal server-manifest.json (regions): %v", err)
	}
	if len(srvMan2.Regions["/live"]) == 0 {
		t.Errorf("server-manifest missing region registry for /live")
	}
}

// TestISRVariantCacheE2E boots the staged SSR renderer against an ISR dynamic
// route and verifies variant-aware cache keys (params are part of the key),
// stale-while-revalidate with single-flight background refresh, the
// /__krate/ssr/revalidate invalidation endpoint, and cache persistence.
func TestISRVariantCacheE2E(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		requireE2E(t, "node not available: %v", err)
	}

	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	runtimeSrc := filepath.Join(repoRoot, "packages", "runtime", "src")
	serverTS := filepath.Join(runtimeSrc, "server.ts")
	if _, err := os.Stat(serverTS); err != nil {
		requireE2E(t, "@krate/runtime server source not found: %v", err)
	}

	fakeRoot := t.TempDir()
	outDir := filepath.Join(fakeRoot, "dist")

	// Page bundle: an ISR page whose output changes every render so cache
	// hits/staleness are observable via identical/different HTML timestamps.
	pageSrc := `
export default function VideoPage(props) {
  const now = new Date().toISOString();
  return <div class="page">v-{props.params ? props.params.id : "missing"}-{now}</div>;
}
`
	bundleDir := filepath.Join(outDir, ".krate", "server-bundles")
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		t.Fatal(err)
	}
	bundleRel := ".krate/server-bundles/video.isr.server.mjs"
	res := api.Build(api.BuildOptions{
		AbsWorkingDir:   repoRoot,
		Bundle:          true,
		Format:          api.FormatESModule,
		Platform:        api.PlatformNode,
		Outfile:         filepath.Join(outDir, filepath.FromSlash(bundleRel)),
		Write:           true,
		JSX:             api.JSXAutomatic,
		JSXSideEffects:  false,
		JSXImportSource: "@krate/runtime/server",
		Plugins: []api.Plugin{{
			Name: "krate-isr-e2e-alias",
			Setup: func(build api.PluginBuild) {
				build.OnResolve(api.OnResolveOptions{Filter: `^@krate/runtime/server(/|$)`},
					func(args api.OnResolveArgs) (api.OnResolveResult, error) {
						spec := strings.TrimPrefix(args.Path, "@krate/runtime/server/")
						if spec == args.Path || spec == "" {
							return api.OnResolveResult{Path: serverTS}, nil
						}
						candidates := []string{
							filepath.Join(runtimeSrc, "server-"+spec+".ts"),
							filepath.Join(runtimeSrc, spec+".ts"),
						}
						for _, c := range candidates {
							if _, err := os.Stat(c); err == nil {
								return api.OnResolveResult{Path: c}, nil
							}
						}
						return api.OnResolveResult{}, fmt.Errorf("alias: %s not found", args.Path)
					})
			},
		}},
		Stdin: &api.StdinOptions{
			Loader:     api.LoaderTSX,
			Contents:   pageSrc,
			Sourcefile: "video/[id].tsx",
		},
	})
	if len(res.Errors) > 0 {
		t.Fatalf("page bundle failed: %s", res.Errors[0].Text)
	}

	// ISR page with a short revalidate window so staleness is reachable.
	man := ServerManifest{
		Pages: []ManifestPage{{
			Route:      "/video/[id]",
			Source:     filepath.Join(fakeRoot, "video", "[id].tsx"),
			Mode:       "isr",
			Revalidate: 2,
			BundlePath: bundleRel,
		}},
	}
	mData, _ := json.Marshal(man)
	if err := os.WriteFile(filepath.Join(outDir, "server-manifest.json"), mData, 0644); err != nil {
		t.Fatal(err)
	}

	staged := stageServerRenderer(repoRoot, outDir)
	if staged == "" {
		t.Fatal("stageServerRenderer returned empty path")
	}

	port := freePort(t)
	cmd := exec.Command(node, staged)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("KRATE_SSR_PORT=%d", port),
		fmt.Sprintf("KRATE_MANIFEST=%s", filepath.Join(outDir, "server-manifest.json")),
		fmt.Sprintf("KRATE_ROOT=%s", fakeRoot),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting renderer: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	base := fmt.Sprintf("http://localhost:%d", port)
	if !waitHealth(t, base) {
		t.Fatal("renderer did not become ready")
	}

	// 1. First render of a variant is a miss.
	alpha1 := isrRender(t, base, "/video/[id]", "video/alpha", map[string]string{"id": "alpha"})
	if alpha1.cacheStatus != "miss" || alpha1.cached {
		t.Fatalf("first alpha render: cacheStatus=%q cached=%v, want miss", alpha1.cacheStatus, alpha1.cached)
	}

	// 2. Same variant -> hit with byte-identical HTML.
	alpha2 := isrRender(t, base, "/video/[id]", "video/alpha", map[string]string{"id": "alpha"})
	if alpha2.cacheStatus != "hit" || !alpha2.cached {
		t.Fatalf("alpha re-render: cacheStatus=%q cached=%v, want hit", alpha2.cacheStatus, alpha2.cached)
	}
	if alpha2.html != alpha1.html {
		t.Fatalf("hit returned different HTML:\n%q\nvs\n%q", alpha2.html, alpha1.html)
	}

	// 3. A different variant is keyed separately -> miss, different HTML.
	beta := isrRender(t, base, "/video/[id]", "video/beta", map[string]string{"id": "beta"})
	if beta.cacheStatus != "miss" || beta.cached {
		t.Fatalf("beta render: cacheStatus=%q cached=%v, want miss", beta.cacheStatus, beta.cached)
	}
	if beta.html == alpha1.html {
		t.Fatalf("beta and alpha rendered identical HTML - variant key collision?")
	}

	// 4. Alpha unaffected by beta render -> still a hit.
	alpha3 := isrRender(t, base, "/video/[id]", "video/alpha", map[string]string{"id": "alpha"})
	if alpha3.cacheStatus != "hit" || alpha3.html != alpha1.html {
		t.Fatalf("alpha after beta: cacheStatus=%q, want hit with original HTML", alpha3.cacheStatus)
	}

	// 5. After the revalidate window the entry is stale (old HTML) and a
	// background refresh kicks off.
	time.Sleep(2600 * time.Millisecond)
	stale := isrRender(t, base, "/video/[id]", "video/alpha", map[string]string{"id": "alpha"})
	if stale.cacheStatus != "stale" || !stale.cached {
		t.Fatalf("alpha past revalidate: cacheStatus=%q cached=%v, want stale", stale.cacheStatus, stale.cached)
	}
	if stale.html != alpha1.html {
		t.Fatalf("stale entry served different HTML than the cached page, want old HTML")
	}

	// 6. Background revalidation lands: alpha becomes hit with new HTML.
	fresh := pollUntilHitWithNewHTML(t, base, alpha1.html)
	if fresh == "" {
		t.Fatalf("background revalidation never produced fresh HTML")
	}

	// 6b. A time-based background refresh must regenerate cached variants in
	// place, not evict them. Regression: the periodic ISR timer previously
	// called the destructive /__krate/ssr/revalidate endpoint (deleteRoute +
	// render the paramless base), so every dynamic variant was wiped and the
	// next request that should have been a HIT came back MISS.
	// The refresh only re-renders variants older than the window, so let the
	// freshly revalidated entries age past it first.
	time.Sleep(2600 * time.Millisecond)
	postRefresh := postJSON(t, base, "/__krate/ssr/refresh", `{"route":"/video/[id]"}`)
	if postRefresh.status != 200 {
		t.Fatalf("refresh endpoint status %d: %s", postRefresh.status, postRefresh.body)
	}
	refreshed := isrRender(t, base, "/video/[id]", "video/alpha", map[string]string{"id": "alpha"})
	if refreshed.cacheStatus != "hit" {
		t.Fatalf("alpha after refresh: cacheStatus=%q, want hit (variant was evicted)", refreshed.cacheStatus)
	}
	if refreshed.html == fresh {
		t.Fatalf("refresh did not regenerate alpha HTML")
	}
	betaRefreshed := isrRender(t, base, "/video/[id]", "video/beta", map[string]string{"id": "beta"})
	if betaRefreshed.cacheStatus != "hit" {
		t.Fatalf("beta after refresh: cacheStatus=%q, want hit (variant was evicted)", betaRefreshed.cacheStatus)
	}

	// 7. Cache persistence: the debounced writer must have flushed a file.
	waitFile(t, filepath.Join(outDir, ".krate", "isr-cache.json"), 3*time.Second)
	data, err := os.ReadFile(filepath.Join(outDir, ".krate", "isr-cache.json"))
	if err != nil {
		t.Fatalf("reading persisted isr-cache.json: %v", err)
	}
	if !strings.Contains(string(data), "id=alpha") {
		t.Errorf("persisted cache missing alpha variant key:\n%s", data)
	}

	// 8. /__krate/ssr/revalidate clears the whole route -> next render is a miss.
	post := postJSON(t, base, "/__krate/ssr/revalidate", `{"route":"/video/[id]"}`)
	if post.status != 200 {
		t.Fatalf("revalidate endpoint status %d: %s", post.status, post.body)
	}
	after := isrRender(t, base, "/video/[id]", "video/alpha", map[string]string{"id": "alpha"})
	if after.cacheStatus != "miss" {
		t.Fatalf("alpha after revalidate endpoint: cacheStatus=%q, want miss", after.cacheStatus)
	}
	if after.html == fresh {
		t.Fatalf("revalidated render returned the pre-invalidation HTML")
	}
}

type isrResult struct {
	html        string
	cached      bool
	cacheStatus string
}

// regionFrames holds the parsed NDJSON frames from a /__krate/regions stream.
type regionFrames struct {
	regions     map[string]string // region ID -> HTML
	errors      map[string]string // region ID -> error text
	cacheStatus map[string]string // region ID -> hit/stale/miss (page kind)
	titles      map[string]string // region ID -> fresh <title> (page kind)
	count       int
}

func postRegionsList(t *testing.T, base, route, url string, params map[string]string, regions []map[string]string) regionFrames {
	t.Helper()
	body := map[string]interface{}{
		"route": route, "url": url, "method": "GET", "params": params,
	}
	if regions != nil {
		body["regions"] = regions
	}
	data, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, base+"/__krate/regions", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("regions request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if secret := os.Getenv("KRATE_TEST_INTERNAL_SECRET"); secret != "" {
		req.Header.Set("X-Krate-Internal", secret)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("regions request: %v", err)
	}
	defer resp.Body.Close()

	out := regionFrames{regions: map[string]string{}, errors: map[string]string{}, cacheStatus: map[string]string{}, titles: map[string]string{}}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var frame struct {
			Type        string `json:"type"`
			ID          string `json:"id,omitempty"`
			Kind        string `json:"kind,omitempty"`
			HTML        string `json:"html,omitempty"`
			Error       string `json:"error,omitempty"`
			Count       int    `json:"count,omitempty"`
			CacheStatus string `json:"cacheStatus,omitempty"`
			Title       string `json:"title,omitempty"`
		}
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("unmarshal region frame: %v\nline: %s", err, line)
		}
		switch frame.Type {
		case "region":
			out.regions[frame.ID] = frame.HTML
			if frame.CacheStatus != "" {
				out.cacheStatus[frame.ID] = frame.CacheStatus
			}
			if frame.Title != "" {
				out.titles[frame.ID] = frame.Title
			}
		case "error":
			if frame.ID != "" {
				out.errors[frame.ID] = frame.Error
			}
		case "end":
			out.count = frame.Count
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scanning regions response: %v", err)
	}
	return out
}

func isrRender(t *testing.T, base, route, url string, params map[string]string) isrResult {
	t.Helper()
	// ISR pages render as a single coarse "page" region via /__krate/regions.
	frames := postRegionsList(t, base, route, url, params, []map[string]string{{"id": "page", "kind": "page"}})
	html := frames.regions["page"]
	cs := frames.cacheStatus["page"]
	return isrResult{html: html, cached: cs == "hit" || cs == "stale", cacheStatus: cs}
}

// linkRuntimePackage links the monorepo @krate/runtime package into a temp
// project's node_modules so compiled server bundles can resolve it, mirroring
// a real installed dependency. destDir is the "@krate" directory to create.
// ensureRuntimeDist builds packages/runtime's dist/ when it is absent so the
// sidecar fixtures can resolve @krate/runtime/dist/* like a real installed
// package. packages/runtime/dist is gitignored (built, not committed), so a
// fresh clone fails the sidecar E2E with ERR_MODULE_NOT_FOUND until the runtime
// has been built. Building it here keeps upstream end-to-end checks green on a
// clean checkout and exercises the real supported package path.
func ensureRuntimeDist(t *testing.T) {
	t.Helper()
	runtimeEnsureOnce.Do(func() {
		repoRoot, err := repoRootPath()
		if err != nil {
			t.Fatalf("repoRootPath: %v", err)
		}
		runtimeDir := filepath.Join(repoRoot, "packages", "runtime")
		if _, err := os.Stat(filepath.Join(runtimeDir, "dist", "server-jsx-runtime.js")); err == nil {
			return
		}
		npmCmd := "npm"
		if _, err := exec.LookPath(npmCmd); err != nil {
			// npm may be npm.cmd on Windows.
			npmCmd = "npm.cmd"
		}
		if _, err := exec.LookPath(npmCmd); err != nil {
			requireE2E(t, "packages/runtime/dist missing and npm not available: %v", err)
		}
		out, err := exec.Command(npmCmd, "run", "build").CombinedOutput()
		if err != nil {
			t.Fatalf("building packages/runtime dist: %v\n%s", err, out)
		}
	})
}

var runtimeEnsureOnce = sync.Once{}

func linkRuntimePackage(t *testing.T, destDir string) error {
	t.Helper()
	repoRoot, err := repoRootPath()
	if err != nil {
		return err
	}
	src := filepath.Join(repoRoot, "packages", "runtime")
	if _, err := os.Stat(filepath.Join(src, "package.json")); err != nil {
		return fmt.Errorf("runtime package not found at %s", src)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}
	link := filepath.Join(destDir, "runtime")
	// Directory junction (works without admin on Windows) or symlink elsewhere.
	if err := os.Symlink(src, link); err != nil {
		cmd := exec.Command("cmd", "/c", "mklink", "/J", link, src)
		if out, cerr := cmd.CombinedOutput(); cerr != nil {
			return fmt.Errorf("linking runtime package: %v (%s)", cerr, out)
		}
	}
	return nil
}

type postResult struct {
	status int
	body   string
}

func postJSON(t *testing.T, base, path, payload string) postResult {
	t.Helper()
	resp, err := http.Post(base+path, "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return postResult{status: resp.StatusCode, body: string(out)}
}

func pollUntilHitWithNewHTML(t *testing.T, base, oldHTML string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		r := isrRender(t, base, "/video/[id]", "video/alpha", map[string]string{"id": "alpha"})
		if r.cacheStatus == "hit" && r.html != oldHTML {
			return r.html
		}
		time.Sleep(300 * time.Millisecond)
	}
	return ""
}

func waitHealth(t *testing.T, base string) bool {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/__krate/ssr/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func waitFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for file %s", path)
}
