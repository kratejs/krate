package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/deploy"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/gohost"
)

// serverDeployDir holds the generated server entries under <out>/.krate.
const serverDeployDir = "server"

// ServerTargets are the deploy targets that run the portable server handler.
var ServerTargets = []string{"node", "vercel", "netlify", "cloudflare"}

// IsServerTarget reports whether target runs server features through the handler.
func IsServerTarget(target string) bool {
	for _, t := range ServerTargets {
		if t == target {
			return true
		}
	}
	return false
}

// nodeBuiltinsOnWorkers are the Node modules Cloudflare provides under
// nodejs_compat that the handler and typical server code rely on.
var nodeBuiltinsOnWorkers = map[string]bool{
	"async_hooks": true, "buffer": true, "crypto": true, "events": true, "util": true, "string_decoder": true,
	"stream": true, "path": true, "url": true, "assert": true, "querystring": true, "punycode": true,
	"timers": true, "diagnostics_channel": true, "stream/web": true, "path/posix": true, "util/types": true,
}

var nodeBuiltinNames = map[string]bool{
	"assert": true, "async_hooks": true, "buffer": true, "child_process": true, "cluster": true, "console": true,
	"constants": true, "crypto": true, "dgram": true, "diagnostics_channel": true, "dns": true, "domain": true,
	"events": true, "fs": true, "http": true, "http2": true, "https": true, "inspector": true, "module": true,
	"net": true, "os": true, "path": true, "perf_hooks": true, "process": true, "punycode": true, "querystring": true,
	"readline": true, "repl": true, "stream": true, "string_decoder": true, "sys": true, "timers": true, "tls": true,
	"trace_events": true, "tty": true, "url": true, "util": true, "v8": true, "vm": true, "wasi": true,
	"worker_threads": true, "zlib": true, "fs/promises": true, "stream/web": true, "path/posix": true, "util/types": true,
	"timers/promises": true, "stream/promises": true, "dns/promises": true,
}

// ServerDeployProblems lists what the project uses that the portable handler
// cannot run. target is only used in the wording; the checks are the same for
// every server target.
func ServerDeployProblems(root string, cfg *config.Config, target string) []string {
	var out []string
	outDir := cfg.OutDir
	if regularFileExists(gohost.RoutesManifestPath(root)) {
		out = append(out, "Go API routes (src/api/*.go) need the Go runtime and cannot run on "+target)
	}
	if data, err := os.ReadFile(ResolveManifestPath(outDir, "manifest.json")); err == nil {
		var m Manifest
		if json.Unmarshal(data, &m) == nil {
			var routes []string
			for route, regs := range m.Regions {
				for _, r := range regs {
					if r.Runtime == "go" {
						routes = append(routes, route)
						break
					}
				}
			}
			sort.Strings(routes)
			if len(routes) > 0 {
				out = append(out, "Go components rendered at request time (pages "+strings.Join(routes, ", ")+") need the Go host and cannot run on "+target)
			}
		}
	}
	if cfg.API.Sidecar != nil && (cfg.API.Sidecar.Command != "" || cfg.API.Sidecar.Target != "" || cfg.API.Sidecar.Port != 0) {
		out = append(out, "the custom API sidecar (api.sidecar) is a separate process and cannot run on "+target)
	}
	if cfg.Compat.UserAgent {
		out = append(out, "compat.userAgent document selection is not implemented by the portable server")
	}
	for _, p := range cfg.Plugins {
		if p.Module != "" {
			out = append(out, "community plugin "+p.Name+" may use ServeRequest/ServeResponse hooks, which run only in the Go host; remove it from the deployed config or deploy with --target docker")
			break
		}
	}
	return out
}

var revalidateCallRe = regexp.MustCompile(`\brevalidate(Path|Tag)\s*\(`)

// usesRevalidate reports whether project sources call revalidatePath/Tag.
func usesRevalidate(root string, cfg *config.Config) bool {
	found := false
	scan := func(dir string) {
		_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || found {
				return nil
			}
			if d.IsDir() {
				if d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			switch filepath.Ext(p) {
			case ".ts", ".tsx", ".js", ".jsx", ".mjs":
				if data, err := os.ReadFile(p); err == nil && revalidateCallRe.Match(data) {
					found = true
				}
			}
			return nil
		})
	}
	scan(filepath.Join(root, "src"))
	return found
}

type serverManifestFile struct {
	Pages   []ManifestPage          `json:"pages"`
	Regions map[string][]RegionMeta `json:"regions"`
}

// handlerConfig mirrors HandlerConfig in @krate/runtime/server-handler.
type handlerConfig struct {
	BasePath string `json:"basePath"`
	// TrustProxy is server.trustProxy: null when the project does not set it,
	// which makes each serverless target use what its platform guarantees.
	TrustProxy      *bool               `json:"trustProxy"`
	MaxBodyBytes    int64               `json:"maxBodyBytes"`
	MaxAPIBodyBytes int64               `json:"maxAPIBodyBytes"`
	Redirects       []config.Redirect   `json:"redirects"`
	Rewrites        []config.Rewrite    `json:"rewrites"`
	Headers         []config.HeaderRule `json:"headers"`
	Security        handlerSecurity     `json:"security"`
	CORS            handlerCORS         `json:"cors"`
	SSR             handlerSSR          `json:"ssr"`
	// Cache is the shared cache setting without its URL: the bundle never
	// contains a secret, the URL comes from the environment at run time.
	Cache *handlerCache `json:"cache,omitempty"`
}

type handlerCache struct {
	Adapter    string         `json:"adapter"`
	URLEnv     string         `json:"urlEnv,omitempty"`
	Prefix     string         `json:"prefix,omitempty"`
	TimeoutMs  int            `json:"timeoutMs,omitempty"`
	TTLSeconds int            `json:"ttlSeconds,omitempty"`
	Options    map[string]any `json:"options,omitempty"`
}

type handlerSecurity struct {
	Nosniff           bool   `json:"nosniff"`
	FrameOptions      string `json:"frameOptions"`
	ReferrerPolicy    string `json:"referrerPolicy"`
	PermissionsPolicy string `json:"permissionsPolicy"`
	HSTS              string `json:"hsts"`
	CSP               string `json:"csp"`
}

type handlerCORS struct {
	Enabled     bool     `json:"enabled"`
	Origins     []string `json:"origins"`
	Methods     []string `json:"methods"`
	Headers     []string `json:"headers"`
	Credentials bool     `json:"credentials"`
	MaxAge      int      `json:"maxAge"`
}

type handlerSSR struct {
	Timeout       int  `json:"timeout"`
	MaxCacheSize  int  `json:"maxCacheSize"`
	PPR           bool `json:"ppr"`
	PPRRevalidate int  `json:"pprRevalidate"`
}

func resolvedHeader(v, def string) string {
	v = headerOrDefault(v, def)
	if config.HeaderDisabled(v) {
		return ""
	}
	return v
}

func buildHandlerConfig(cfg *config.Config) handlerConfig {
	sec := cfg.Security
	hsts := sec.HSTS
	if hsts == "" && strings.HasPrefix(strings.ToLower(cfg.SEO.BaseURL), "https://") {
		hsts = "max-age=31536000; includeSubDomains"
	}
	if config.HeaderDisabled(hsts) {
		hsts = ""
	}
	c := cfg.CORS
	hc := handlerConfig{
		BasePath:        cfg.BaseURLPath(),
		TrustProxy:      trustProxyOf(cfg),
		MaxBodyBytes:    cfg.MaxBodyBytes(),
		MaxAPIBodyBytes: cfg.MaxAPIBodyBytes(),
		Redirects:       nonNilRedirects(cfg.Redirects),
		Rewrites:        nonNilRewrites(cfg.Rewrites),
		Headers:         nonNilHeaders(cfg.Headers),
		Security: handlerSecurity{
			Nosniff:           sec.ContentTypeOptions == nil || *sec.ContentTypeOptions,
			FrameOptions:      resolvedHeader(sec.FrameOptions, "DENY"),
			ReferrerPolicy:    resolvedHeader(sec.ReferrerPolicy, "strict-origin-when-cross-origin"),
			PermissionsPolicy: resolvedHeader(sec.PermissionsPolicy, "camera=(), microphone=(), geolocation=()"),
			HSTS:              hsts,
			CSP:               buildCSPHeader(cfg),
		},
		CORS: handlerCORS{
			Enabled: c.Enabled, Origins: nonNilStrings(c.Origins), Methods: nonNilStrings(c.Methods),
			Headers: nonNilStrings(c.Headers), Credentials: c.Credentials, MaxAge: c.MaxAge,
		},
		SSR: handlerSSR{Timeout: cfg.SSR.Timeout, MaxCacheSize: cfg.SSR.MaxCacheSize, PPR: cfg.PPR, PPRRevalidate: cfg.PPRRevalidate},
	}
	if cc := cfg.Cache; cc.Shared() {
		hc.Cache = &handlerCache{Adapter: cc.Adapter, URLEnv: cc.URLEnv, Prefix: cc.Prefix, TimeoutMs: cc.TimeoutMs, TTLSeconds: cc.TTLSeconds, Options: cc.Options}
	}
	return hc
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
func nonNilRedirects(s []config.Redirect) []config.Redirect {
	if s == nil {
		return []config.Redirect{}
	}
	return s
}
func nonNilRewrites(s []config.Rewrite) []config.Rewrite {
	if s == nil {
		return []config.Rewrite{}
	}
	return s
}
func nonNilHeaders(s []config.HeaderRule) []config.HeaderRule {
	if s == nil {
		return []config.HeaderRule{}
	}
	return s
}

// serverData is the build data inlined into the bundle (KrateServerData).
type serverData struct {
	Version         int                       `json:"version"`
	Config          handlerConfig             `json:"config"`
	Pages           []serverPage              `json:"pages"`
	Regions         map[string][]serverRegion `json:"regions"`
	AllPages        []serverAllPage           `json:"allPages"`
	Shells          map[string]string         `json:"shells"`
	Templates       []serverTemplate          `json:"templates"`
	StaticOnlyRoute []string                  `json:"staticOnlyRoutes"`
	HashedAssets    []string                  `json:"hashedAssets"`
	HTML404         string                    `json:"html404"`
	HTML500         string                    `json:"html500"`
	Segments        []SegmentEntry            `json:"segments"`
	SegmentPages    map[string]string         `json:"segmentPages"`
	StaticPages     map[string]string         `json:"staticPages,omitempty"`
	APIRoutes       []string                  `json:"apiRoutes"`
	Actions         []actionEntry             `json:"actions"`
	HasMiddleware   bool                      `json:"hasMiddleware"`
}

type serverPage struct {
	Route            string `json:"route"`
	Mode             string `json:"mode"`
	Revalidate       int    `json:"revalidate,omitempty"`
	BundlePath       string `json:"bundlePath"`
	ServerComponents bool   `json:"serverComponents,omitempty"`
	SegDir           string `json:"segDir"`
}

type serverRegion struct {
	ID           string         `json:"id"`
	Component    string         `json:"component,omitempty"`
	BundlePath   string         `json:"bundlePath,omitempty"`
	Props        map[string]any `json:"props,omitempty"`
	Suspense     bool           `json:"suspense,omitempty"`
	Revalidate   int            `json:"revalidate,omitempty"`
	ServerExport string         `json:"serverExport,omitempty"`
}

type serverAllPage struct {
	Route  string `json:"route"`
	Mode   string `json:"mode"`
	SegDir string `json:"segDir"`
}

type serverTemplate struct {
	Pattern string `json:"pattern"`
	HTML    string `json:"html"`
}

// findMiddlewareSource mirrors BuildMiddleware's lookup.
func findMiddlewareSource(root string) string {
	for _, p := range []string{
		filepath.Join(root, "middleware.ts"), filepath.Join(root, "src", "middleware.ts"),
		filepath.Join(root, "middleware.js"), filepath.Join(root, "src", "middleware.js"),
	} {
		if regularFileExists(p) {
			return p
		}
	}
	return ""
}

func regularFileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// apiRouteKeys lists the JS/TS API routes as slash paths without extension.
func apiRouteKeys(root string) (map[string]string, error) {
	dir := filepath.Join(root, filepath.FromSlash(gohost.DefaultAPIDir))
	out := map[string]string{}
	if _, err := os.Stat(dir); err != nil {
		return out, nil
	}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if strings.HasPrefix(filepath.Base(p), "_") {
			return nil
		}
		ext := filepath.Ext(p)
		if ext != ".ts" && ext != ".js" {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		key := filepath.ToSlash(strings.TrimSuffix(rel, ext))
		// A .js next to a .ts of the same name: the runtime resolves .js first.
		if prev, ok := out[key]; ok && filepath.Ext(prev) == ".js" {
			return nil
		}
		out[key] = p
		return nil
	})
	return out, err
}

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// BuildServerBundle bundles the portable server handler, the build data and
// every server module of the built project into one self-contained ESM file
// under <out>/.krate/server. Nothing is imported at request time, so the
// result runs on Node, Vercel, Netlify and Cloudflare Workers.
func BuildServerBundle(root string, cfg *config.Config, target string) (*deploy.ServerPlan, error) {
	if !IsServerTarget(target) {
		return nil, fmt.Errorf("%q does not run the portable server (want one of: %s)", target, strings.Join(ServerTargets, ", "))
	}
	outDir, err := filepath.Abs(cfg.OutDir)
	if err != nil {
		return nil, err
	}
	if problems := ServerDeployProblems(root, cfg, target); len(problems) > 0 {
		return nil, fmt.Errorf("this project cannot be deployed to %s:\n  - %s\n  Deploy with `krate deploy --target docker` (or run `krate start` on a host with the krate binary), or pass --static-only to publish only the static pages", target, strings.Join(problems, "\n  - "))
	}

	data := serverData{
		Version: 1, Config: buildHandlerConfig(cfg),
		Pages: []serverPage{}, Regions: map[string][]serverRegion{}, AllPages: []serverAllPage{},
		Shells: map[string]string{}, Templates: []serverTemplate{}, StaticOnlyRoute: []string{},
		HashedAssets: []string{}, APIRoutes: []string{}, Actions: []actionEntry{},
		Segments: []SegmentEntry{}, SegmentPages: map[string]string{},
	}
	plan := &deploy.ServerPlan{Target: target}

	var sm serverManifestFile
	if b, err := os.ReadFile(ResolveManifestPath(outDir, "server-manifest.json")); err == nil {
		if err := json.Unmarshal(b, &sm); err != nil {
			return nil, fmt.Errorf("reading server-manifest.json: %w", err)
		}
	}
	if b, err := os.ReadFile(ResolveManifestPath(outDir, "manifest.json")); err == nil {
		var m struct {
			Pages []struct {
				Route  string     `json:"route"`
				Mode   RenderMode `json:"mode"`
				SegDir string     `json:"segDir"`
			} `json:"pages"`
			StaticOnlyRoutes []string       `json:"staticOnlyRoutes"`
			HashedAssets     []string       `json:"hashedAssets"`
			Segments         []SegmentEntry `json:"segments"`
		}
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("reading manifest.json: %w", err)
		}
		for _, p := range m.Pages {
			data.AllPages = append(data.AllPages, serverAllPage{Route: p.Route, Mode: p.Mode.String(), SegDir: p.SegDir})
		}
		if m.Segments != nil {
			data.Segments = m.Segments
		}
		// The segment pages travel in the bundle: the handler picks the nearest
		// one for a request and fills it, so no file is read at request time.
		for _, e := range m.Segments {
			for _, rel := range e.pages() {
				if rel == "" {
					continue
				}
				clean := filepath.Clean(filepath.FromSlash(rel))
				if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
					return nil, fmt.Errorf("manifest.json lists segment page %q outside the output directory", rel)
				}
				b, err := os.ReadFile(filepath.Join(outDir, clean))
				if err != nil {
					return nil, fmt.Errorf("segment page %s listed in manifest.json: %w (run `krate build` again)", rel, err)
				}
				data.SegmentPages[rel] = string(b)
			}
		}
		if m.StaticOnlyRoutes != nil {
			data.StaticOnlyRoute = m.StaticOnlyRoutes
		}
		if m.HashedAssets != nil {
			data.HashedAssets = m.HashedAssets
		}
	}

	imports := &strings.Builder{}
	body := &strings.Builder{}
	n := 0
	ident := func(prefix string) string {
		n++
		return fmt.Sprintf("%s_%d", prefix, n)
	}

	pageMods := &strings.Builder{}
	for _, p := range sm.Pages {
		data.Pages = append(data.Pages, serverPage{Route: p.Route, Mode: p.Mode, Revalidate: p.Revalidate, BundlePath: p.BundlePath, ServerComponents: p.ServerComponents, SegDir: p.SegDir})
		rel := strings.Trim(strings.TrimPrefix(p.Route, "/"), "/")
		shellPath := filepath.Join(outDir, filepath.FromSlash(rel), "index.html")
		if rel == "" {
			shellPath = filepath.Join(outDir, "index.html")
		}
		if b, err := os.ReadFile(shellPath); err == nil {
			data.Shells[p.Route] = string(b)
			plan.Exclude = append(plan.Exclude, filepath.ToSlash(relOr(outDir, shellPath)))
		}
		if p.BundlePath != "" {
			id := ident("page")
			fmt.Fprintf(imports, "import * as %s from %s;\n", id, jsString(filepath.ToSlash(filepath.Join(outDir, filepath.FromSlash(p.BundlePath)))))
			fmt.Fprintf(pageMods, "  %s: %s,\n", jsString(p.BundlePath), id)
		}
	}
	runtimeMods := &strings.Builder{}
	seenRuntime := map[string]bool{}
	routes := make([]string, 0, len(sm.Regions))
	for r := range sm.Regions {
		routes = append(routes, r)
	}
	sort.Strings(routes)
	for _, route := range routes {
		for _, r := range sm.Regions[route] {
			data.Regions[route] = append(data.Regions[route], serverRegion{ID: r.ID, Component: r.Component, BundlePath: r.BundlePath, Props: r.Props, Suspense: r.Suspense, Revalidate: r.Revalidate, ServerExport: r.ServerExport})
			if r.ServerExport == "" && r.BundlePath != "" && !seenRuntime[r.BundlePath] {
				seenRuntime[r.BundlePath] = true
				code, err := os.ReadFile(filepath.Join(outDir, filepath.FromSlash(r.BundlePath)))
				if err != nil {
					return nil, fmt.Errorf("runtime component bundle %s: %w", r.BundlePath, err)
				}
				// The compiled bundle is an IIFE that defines __krate_render. Run it
				// against a shadow global so it needs neither vm nor eval.
				fmt.Fprintf(runtimeMods, "  %s: (function () { const sandbox = Object.create(globalThis); (function (globalThis) {\n%s\n;if (typeof __krate_render === 'function') globalThis.__krate_render = globalThis.__krate_render || __krate_render; }).call(sandbox, sandbox); return sandbox.__krate_render; })(),\n", jsString(r.BundlePath), string(code))
			}
		}
	}

	// Dynamic SSG templates and the error pages.
	for _, dr := range findDynamicRoutes(outDir) {
		b, err := os.ReadFile(filepath.Join(dr.dir, "index.html"))
		if err != nil {
			continue
		}
		data.Templates = append(data.Templates, serverTemplate{Pattern: dr.pattern, HTML: string(b)})
		plan.Exclude = append(plan.Exclude, dr.pattern+"/index.html")
	}
	if b, err := os.ReadFile(filepath.Join(outDir, "404.html")); err == nil {
		data.HTML404 = string(b)
	}
	if b, err := os.ReadFile(filepath.Join(outDir, "500.html")); err == nil {
		data.HTML500 = string(b)
	}

	// API routes bundle from source so third-party packages are included.
	apiMods := &strings.Builder{}
	keys, err := apiRouteKeys(root)
	if err != nil {
		return nil, fmt.Errorf("scanning API routes: %w", err)
	}
	apiKeys := make([]string, 0, len(keys))
	for k := range keys {
		apiKeys = append(apiKeys, k)
	}
	sort.Strings(apiKeys)
	for _, k := range apiKeys {
		data.APIRoutes = append(data.APIRoutes, k)
		id := ident("api")
		fmt.Fprintf(imports, "import * as %s from %s;\n", id, jsString(filepath.ToSlash(keys[k])))
		fmt.Fprintf(apiMods, "  %s: %s,\n", jsString(k), id)
	}

	// Server actions.
	actionMods := &strings.Builder{}
	if b, err := os.ReadFile(actionManifestPath(root)); err == nil {
		var am actionManifest
		if json.Unmarshal(b, &am) == nil {
			data.Actions = append(data.Actions, am.Actions...)
			seen := map[string]bool{}
			for _, a := range am.Actions {
				if seen[a.File] {
					continue
				}
				seen[a.File] = true
				id := ident("action")
				fmt.Fprintf(imports, "import * as %s from %s;\n", id, jsString(filepath.ToSlash(filepath.Join(root, ".krate", "actions", a.File))))
				fmt.Fprintf(actionMods, "  %s: %s,\n", jsString(a.File), id)
			}
		}
	}

	mw := findMiddlewareSource(root)
	mwExpr := "undefined"
	if mw != "" {
		data.HasMiddleware = true
		fmt.Fprintf(imports, "import * as middleware_mod from %s;\n", jsString(filepath.ToSlash(mw)))
		mwExpr = "middleware_mod"
	}

	// Which URL paths must reach the function on platforms that serve static files first.
	for _, p := range data.Pages {
		plan.ServerPaths = append(plan.ServerPaths, routeToGlob(p.Route))
	}
	if len(data.APIRoutes) > 0 {
		plan.ServerPaths = append(plan.ServerPaths, "/api/*")
	}
	if len(data.Actions) > 0 {
		plan.ServerPaths = append(plan.ServerPaths, "/__krate/action/*")
	}
	plan.HasMiddleware = data.HasMiddleware
	if data.HasMiddleware && (target == "vercel" || target == "netlify") {
		// The CDN would serve prerendered pages before the function and skip the
		// middleware. Those targets route page requests to the function, which
		// answers the prerendered HTML itself once the middleware has run.
		pages, exts, err := collectStaticForMiddleware(outDir, plan.Exclude)
		if err != nil {
			return nil, err
		}
		data.StaticPages = pages
		plan.AssetExts = exts
	}
	plan.HasActions = len(data.Actions) > 0
	plan.HasAPI = len(data.APIRoutes) > 0
	plan.UsesRevalidate = usesRevalidate(root, cfg)
	sort.Strings(plan.Exclude)
	plan.ShutdownTimeout = cfg.Server.ShutdownTimeout
	plan.Port = cfg.ServerPort()

	dataJSON, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	// A custom cache adapter module is imported statically (the bundle has no
	// dynamic imports of your files) and built from the cache settings.
	cacheExpr := "undefined"
	if cc := cfg.Cache; cc.Shared() && cc.Adapter != "redis" {
		mod := cc.Adapter
		if strings.HasPrefix(mod, ".") || filepath.IsAbs(mod) {
			mod = filepath.ToSlash(filepath.Join(root, mod))
			if filepath.IsAbs(cc.Adapter) {
				mod = filepath.ToSlash(cc.Adapter)
			}
		}
		fmt.Fprintf(imports, "import * as cache_mod from %s;\nimport { cacheSpecFromConfig } from '@krate/runtime/cache-adapter';\n", jsString(mod))
		cacheExpr = "(async () => { const f = cache_mod.default ?? cache_mod.createAdapter; const spec = cacheSpecFromConfig(data.config.cache); return typeof f === 'function' ? f(spec) : f; })()"
	}
	if cfg.Cache.Shared() {
		plan.SharedCache = true
		if target == "cloudflare" && cfg.Cache.Adapter == "redis" {
			plan.Warnings = append(plan.Warnings, "cache.adapter \"redis\" speaks TCP; Cloudflare Workers cannot keep that connection reliably. Use an HTTP-based adapter (a Redis REST service, KV) through a custom cache.adapter module, or deploy to node, vercel or netlify")
		}
		if cfg.Cache.URL != "" && cfg.Cache.URLEnv == "" {
			plan.Warnings = append(plan.Warnings, "cache.url is not written into the bundle; set KRATE_CACHE_URL (or name another variable with cache.urlEnv) in the deployment's environment")
		}
	}
	modules := fmt.Sprintf("const modules = {\n pages: {\n%s },\n runtime: {\n%s },\n api: {\n%s },\n actions: {\n%s },\n middleware: %s,\n};\n",
		pageMods.String(), runtimeMods.String(), apiMods.String(), actionMods.String(), mwExpr)
	fmt.Fprintf(body, "%s\nconst data = %s;\nconst __cache = %s;\n", modules, string(dataJSON), cacheExpr)

	entry := serverEntry(target, imports.String(), body.String())
	outFile := filepath.Join(outDir, ".krate", serverDeployDir, serverEntryName(target))
	if err := os.MkdirAll(filepath.Dir(outFile), 0755); err != nil {
		return nil, err
	}
	if err := bundleServerEntry(root, cfg, target, entry, outFile); err != nil {
		return nil, err
	}
	plan.Bundle = outFile
	return plan, nil
}

// collectStaticForMiddleware reads the prerendered HTML pages of the output
// (those that stay public: not request-time shells or templates) and lists the
// extensions of every other public file, which keep being served by the CDN.
func collectStaticForMiddleware(outDir string, exclude []string) (map[string]string, []string, error) {
	skip := map[string]bool{}
	for _, e := range exclude {
		skip[e] = true
	}
	pages := map[string]string{}
	extSet := map[string]bool{}
	err := filepath.Walk(outDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(outDir, p)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if info.IsDir() {
			if rel == ".krate" {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(rel))
		if ext == ".html" {
			if skip[rel] {
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			pages[rel] = string(b)
			return nil
		}
		if ext != "" {
			extSet[ext] = true
		}
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("reading prerendered pages: %w", err)
	}
	exts := make([]string, 0, len(extSet))
	for e := range extSet {
		exts = append(exts, e)
	}
	sort.Strings(exts)
	return pages, exts, nil
}

func relOr(base, p string) string {
	if rel, err := filepath.Rel(base, p); err == nil {
		return rel
	}
	return p
}

// routeToGlob turns a page route into a URL glob ("/post/[id]" -> "/post/*").
func routeToGlob(route string) string {
	segs := strings.Split(strings.Trim(route, "/"), "/")
	for i, s := range segs {
		if strings.HasPrefix(s, "[") {
			segs = append(segs[:i], "*")
			break
		}
	}
	g := "/" + strings.Join(segs, "/")
	if strings.HasSuffix(g, "*") {
		return g
	}
	return g + "{,/}"
}

func serverEntryName(target string) string {
	switch target {
	case "node":
		return "server.mjs"
	case "cloudflare":
		return "worker.mjs"
	default:
		return "index.mjs"
	}
}

// serverEntry generates the bundle entry for a target.
func serverEntry(target, imports, body string) string {
	var b strings.Builder
	b.WriteString("// Generated by `krate deploy`. Do not edit.\n")
	b.WriteString(imports)
	switch target {
	case "node":
		b.WriteString("import { createKrateHandler } from '@krate/runtime/server-handler';\nimport { startNodeServer, createNodeStaticLayer, defaultDistDir } from '@krate/runtime/node-server';\n")
		b.WriteString(body)
		b.WriteString(`const distDir = defaultDistDir(import.meta.url);
export const handler = createKrateHandler({ target: 'node', data, modules, cache: __cache, static: createNodeStaticLayer(distDir) });
await startNodeServer({ handler, distDir, trustProxy: data.config.trustProxy === true, shutdownTimeout: __SHUTDOWN__, port: __PORT__ });
`)
	case "vercel":
		b.WriteString("import { createKrateHandler } from '@krate/runtime/server-handler';\nimport { nodeRequestListener } from '@krate/runtime/node-server';\n")
		b.WriteString(body)
		b.WriteString(`export const handler = createKrateHandler({ target: 'vercel', data, modules, cache: __cache });
export const handleRequest = (request, ctx) => handler(request, ctx);
// true: keep the headers Vercel sent. The handler decides which of them it trusts.
export default nodeRequestListener(handler, true);
`)
	case "netlify":
		b.WriteString("import { createKrateHandler } from '@krate/runtime/server-handler';\n")
		b.WriteString(body)
		b.WriteString(`export const handler = createKrateHandler({ target: 'netlify', data, modules, cache: __cache });
export default async (request, context) => handler(request, { waitUntil: context && context.waitUntil ? (p) => context.waitUntil(p) : undefined });
`)
	case "cloudflare":
		// Every target follows server.trustProxy (see PLATFORMS in the handler):
		// Workers do not rewrite a client's X-Forwarded-Host/Proto and request.url
		// is already the public URL, so forwarded headers are trusted only when
		// server.trustProxy says a proxy of yours sits in front of the Worker.
		b.WriteString("import { createKrateHandler } from '@krate/runtime/server-handler';\nimport { createAssetsStaticLayer } from '@krate/runtime/assets-static';\n")
		b.WriteString(body)
		b.WriteString(`let handler;
export default {
  async fetch(request, env, ctx) {
    if (!handler) {
      // Bindings that are plain strings become process.env (secrets and vars).
      const proc = globalThis.process || (globalThis.process = { env: {} });
      proc.env = proc.env || {};
      for (const [k, v] of Object.entries(env || {})) if (typeof v === 'string' && !(k in proc.env)) proc.env[k] = v;
      handler = createKrateHandler({ target: 'cloudflare', data, modules, cache: __cache, static: env && env.ASSETS ? createAssetsStaticLayer(env.ASSETS) : undefined });
    }
    return handler(request, { waitUntil: (p) => ctx.waitUntil(p) });
  },
};
`)
	}
	return b.String()
}

// resolveRuntimePackage locates @krate/runtime the way page bundles resolve it.
func resolveRuntimePackage(root string) string {
	dir := root
	for i := 0; i < 8; i++ {
		for _, c := range []string{
			filepath.Join(dir, "node_modules", "@krate", "runtime"),
			filepath.Join(dir, "packages", "runtime"),
		} {
			if regularFileExists(filepath.Join(c, "package.json")) {
				if real, err := filepath.EvalSymlinks(c); err == nil {
					return real
				}
				return c
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

type runtimeExports map[string]string

func loadRuntimeExports(pkgDir string) runtimeExports {
	out := runtimeExports{}
	data, err := os.ReadFile(filepath.Join(pkgDir, "package.json"))
	if err != nil {
		return out
	}
	var pkg struct {
		Exports map[string]json.RawMessage `json:"exports"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return out
	}
	for sub, raw := range pkg.Exports {
		var cond struct {
			Default string `json:"default"`
		}
		if json.Unmarshal(raw, &cond) == nil && cond.Default != "" {
			out[sub] = filepath.Join(pkgDir, filepath.FromSlash(cond.Default))
		}
	}
	return out
}

// runtimePlugin pins every @krate/runtime import of the bundle to one copy.
func runtimePlugin(root string) (api.Plugin, error) {
	pkg := resolveRuntimePackage(root)
	if pkg == "" {
		return api.Plugin{}, fmt.Errorf("@krate/runtime is not installed: the server bundle needs it (run your package manager's install)")
	}
	exp := loadRuntimeExports(pkg)
	if _, ok := exp["./server-handler"]; !ok {
		return api.Plugin{}, fmt.Errorf("the installed @krate/runtime (%s) has no server-handler export; upgrade @krate/runtime to the version that matches this krate binary", pkg)
	}
	return api.Plugin{
		Name: "krate-runtime-pin",
		Setup: func(b api.PluginBuild) {
			b.OnResolve(api.OnResolveOptions{Filter: `^@krate/runtime(/.*)?$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				sub := "."
				if rest := strings.TrimPrefix(args.Path, "@krate/runtime"); rest != "" {
					sub = "." + rest
				}
				if p, ok := exp[sub]; ok {
					return api.OnResolveResult{Path: p}, nil
				}
				return api.OnResolveResult{}, fmt.Errorf("@krate/runtime has no export %q", sub)
			})
		},
	}, nil
}

// builtinGuardPlugin keeps Node built-ins out of non-Node targets. Node and
// serverless-Node targets leave them external; Cloudflare allows the
// nodejs_compat subset and fails at bundle time, naming the module, otherwise.
func builtinGuardPlugin(target string) api.Plugin {
	return api.Plugin{
		Name: "krate-node-builtins",
		Setup: func(b api.PluginBuild) {
			b.OnResolve(api.OnResolveOptions{Filter: `^(node:)?[a-z_]+(/[a-z_]+)?$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				name := strings.TrimPrefix(args.Path, "node:")
				if !nodeBuiltinNames[name] {
					return api.OnResolveResult{}, nil
				}
				if target != "cloudflare" {
					return api.OnResolveResult{Path: "node:" + name, External: true}, nil
				}
				if nodeBuiltinsOnWorkers[name] {
					return api.OnResolveResult{Path: "node:" + name, External: true}, nil
				}
				return api.OnResolveResult{}, fmt.Errorf("the cloudflare target cannot run code that imports the Node built-in %q (imported from %s): Workers have no filesystem, process or network sockets. Use fetch or a platform binding instead, or deploy with --target docker/node", args.Path, relOr("", args.Importer))
			})
		},
	}
}

func bundleServerEntry(root string, cfg *config.Config, target, entry, outFile string) error {
	rt, err := runtimePlugin(root)
	if err != nil {
		return err
	}
	entry = strings.NewReplacer("__SHUTDOWN__", fmt.Sprint(cfg.Server.ShutdownTimeout), "__PORT__", fmt.Sprint(cfg.ServerPort())).Replace(entry)
	opts := api.BuildOptions{
		AbsWorkingDir: root,
		Bundle:        true,
		Format:        api.FormatESModule,
		Target:        api.ES2022,
		Outfile:       outFile,
		Write:         true,
		LogLevel:      api.LogLevelSilent,
		Plugins:       []api.Plugin{rt, builtinGuardPlugin(target)},
		Stdin: &api.StdinOptions{
			Loader:     api.LoaderJS,
			Contents:   entry,
			ResolveDir: root,
			Sourcefile: "krate-server-entry.js",
		},
		JSX:              api.JSXAutomatic,
		JSXImportSource:  "@krate/runtime",
		Sourcemap:        sourceMapMode(false),
		MinifyWhitespace: cfg.ShouldMinifyJS(),
		MinifySyntax:     cfg.ShouldMinifyJS(),
	}
	if target == "cloudflare" {
		opts.Platform = api.PlatformNeutral
		opts.Conditions = []string{"workerd", "worker", "browser"}
		opts.MainFields = []string{"module", "main"}
	} else {
		opts.Platform = api.PlatformNode
		// CommonJS dependencies may call require() for built-ins. Platform
		// bundlers (Netlify) re-bundle the file without import.meta support, so
		// only the node target anchors the require on the bundle's own URL.
		anchor := "process.cwd() + '/'"
		if target == "node" {
			anchor = "import.meta.url"
		}
		opts.Banner = map[string]string{"js": "import { createRequire as __krateCreateRequire } from 'node:module'; const require = __krateCreateRequire(" + anchor + ");"}
	}
	res := esbuildBuild(opts)
	if len(res.Errors) > 0 {
		var msgs []string
		for _, e := range res.Errors {
			m := e.Text
			if e.Location != nil && e.Location.File != "" {
				m = fmt.Sprintf("%s:%d:%d: %s", e.Location.File, e.Location.Line, e.Location.Column, e.Text)
			}
			msgs = append(msgs, m)
		}
		return fmt.Errorf("server bundle for %s failed:\n  %s", target, strings.Join(msgs, "\n  "))
	}
	return nil
}

var _ = fsutil.WriteFileAtomic

// trustProxyOf is server.trustProxy as the handler reads it: true, false, or
// null when the project never set it.
func trustProxyOf(cfg *config.Config) *bool {
	if !cfg.Server.TrustProxySet {
		return nil
	}
	v := cfg.Server.TrustProxy
	return &v
}
