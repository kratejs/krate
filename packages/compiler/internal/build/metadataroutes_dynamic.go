package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/jseval"
)

// Dynamic metadata routes. A sitemap.ts, robots.ts or manifest.ts that exports
// `dynamic = "force-dynamic"` or a `revalidate` runs per request instead of at
// build time. The build writes no static file for it. It compiles the module,
// wrapped in a small generated entry, as a Node API route under
// api-node/__krate_meta/, and records the URL to route key mapping in
// .krate/meta-routes.json. The three servers (`krate serve` and dev, the
// portable Node handler, and the platform targets) route the public URL to that
// API route.

// metaAPIKeyDir is the API route directory the generated entries live in.
const metaAPIKeyDir = "__krate_meta"

// metaRoutesFile is the manifest of dynamic metadata routes under <out>/.krate.
const metaRoutesFile = "meta-routes.json"

// metaRouteEntry maps a public URL to a dynamic metadata route.
type metaRouteEntry struct {
	Kind metaRouteKind `json:"kind"`
	// Path is the exact URL path (without the base path); Prefix and Suffix
	// match /sitemap/<id>.xml for a sitemap with generateSitemaps.
	Path   string `json:"path,omitempty"`
	Prefix string `json:"prefix,omitempty"`
	Suffix string `json:"suffix,omitempty"`
	// Key is the API route key. For a prefix entry it ends in "/[id]".
	Key string `json:"key"`
	// Source is the generated entry, relative to the project root.
	Source string `json:"source"`
	// Revalidate is the cache window in seconds; 0 renders every request.
	Revalidate int `json:"revalidate"`
}

// alias returns the API route (without "/api/") that serves URL path p.
func (e metaRouteEntry) alias(p string) (string, bool) {
	if e.Path != "" {
		if p == e.Path {
			return e.Key, true
		}
		return "", false
	}
	if !strings.HasPrefix(p, e.Prefix) || !strings.HasSuffix(p, e.Suffix) || len(p) <= len(e.Prefix)+len(e.Suffix) {
		return "", false
	}
	id := p[len(e.Prefix) : len(p)-len(e.Suffix)]
	if strings.ContainsAny(id, "/\\") || id == "." || id == ".." {
		return "", false
	}
	return strings.TrimSuffix(e.Key, "[id]") + id, true
}

// metaRouteIndex resolves public URLs to dynamic metadata routes.
type metaRouteIndex []metaRouteEntry

func (m metaRouteIndex) match(p string) (string, bool) {
	for _, e := range m {
		if k, ok := e.alias(p); ok {
			return k, true
		}
	}
	return "", false
}

// loadMetaRoutes reads the dynamic metadata route manifest of a build output.
func loadMetaRoutes(outDir string) metaRouteIndex {
	data, err := os.ReadFile(ServerOutPath(outDir, metaRoutesFile))
	if err != nil {
		return nil
	}
	var list []metaRouteEntry
	if json.Unmarshal(data, &list) != nil {
		return nil
	}
	return list
}

// metaRouteLoader serves the dynamic metadata route index of a build output.
// In dev it re-reads the manifest when a rebuild rewrote it.
type metaRouteLoader struct {
	outDir string
	dev    bool

	mu    sync.Mutex
	idx   metaRouteIndex
	mtime time.Time
	read  bool
}

func newMetaRouteLoader(outDir string, dev bool) *metaRouteLoader {
	return &metaRouteLoader{outDir: outDir, dev: dev}
}

func (l *metaRouteLoader) get() metaRouteIndex {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.read && !l.dev {
		return l.idx
	}
	var mt time.Time
	if fi, err := os.Stat(ServerOutPath(l.outDir, metaRoutesFile)); err == nil {
		mt = fi.ModTime()
	}
	if !l.read || !mt.Equal(l.mtime) {
		l.idx, l.mtime, l.read = loadMetaRoutes(l.outDir), mt, true
	}
	return l.idx
}

var (
	dynamicExportRe    = regexp.MustCompile(`(?m)^\s*export\s+const\s+dynamic\s*(?::[^=]+)?=\s*['"]([a-z-]+)['"]`)
	revalidateExportRe = regexp.MustCompile(`(?m)^\s*export\s+const\s+revalidate\s*(?::[^=]+)?=\s*(false|Infinity|[0-9][0-9_*+\s]*)\s*;?\s*$`)
	generateSitemapsRe = regexp.MustCompile(`export\s+(?:async\s+)?function\s+generateSitemaps\b|export\s+const\s+generateSitemaps\b`)
)

// metaRouteSchedule reads the `dynamic` and `revalidate` exports of a metadata
// route module. dynamic is true when the route must run per request;
// revalidate is the cache window in seconds (0 for none).
func metaRouteSchedule(src string) (dynamic bool, revalidate int) {
	mode := ""
	if m := dynamicExportRe.FindStringSubmatch(src); m != nil {
		mode = m[1]
	}
	if mode == "force-static" || mode == "error" {
		return false, 0
	}
	if m := revalidateExportRe.FindStringSubmatch(src); m != nil {
		v := strings.TrimSpace(m[1])
		if v != "false" && v != "Infinity" {
			if n, err := strconv.Atoi(strings.TrimSpace(strings.ReplaceAll(v, "_", ""))); err == nil && n >= 0 {
				revalidate = n
				dynamic = true
			}
		}
	}
	if mode == "force-dynamic" {
		return true, revalidate
	}
	return dynamic, revalidate
}

var metaEntryTemplate = `import * as mod from %s;
import { %s as render } from "@krate/runtime/server";
import { AsyncLocalStorage } from "node:async_hooks";

const TTL = %d;
const WITH_ID = %t;
const CONTENT_TYPE = %s;
const STORE = Symbol.for("krate.request.store");
const g = globalThis as Record<symbol, any>;
if (!g[STORE]) g[STORE] = new AsyncLocalStorage();
const cache = new Map<string, { at: number; body: string }>();

function parseCookies(header: string | null): Map<string, string> {
  const jar = new Map<string, string>();
  for (const part of String(header || "").split(";")) {
    const i = part.indexOf("=");
    if (i <= 0) continue;
    const name = part.slice(0, i).trim();
    if (!name || jar.has(name)) continue;
    let value = part.slice(i + 1).trim();
    try { value = decodeURIComponent(value); } catch {}
    jar.set(name, value);
  }
  return jar;
}

async function produce(id?: string): Promise<string | null> {
  if (WITH_ID) {
    const list = await (mod as any).generateSitemaps();
    if (!list.some((item: { id: unknown }) => String(item.id) === id)) return null;
  }
  const d = (mod as any).default;
  const value = typeof d === "function" ? await d(WITH_ID ? { id } : undefined) : d;
  return render(value);
}

export async function GET(request: Request, ctx?: { params?: unknown }) {
  const url = new URL(request.url);
  const params = ((await (ctx && ctx.params)) || {}) as Record<string, string>;
  const host = request.headers.get("x-forwarded-host") || request.headers.get("host") || url.host;
  const key = host + "\n" + url.pathname;
  const headers: Record<string, string> = { "content-type": CONTENT_TYPE };
  headers["cache-control"] = TTL > 0 ? "public, max-age=0, s-maxage=" + TTL : "no-store";
  if (TTL > 0) {
    const hit = cache.get(key);
    if (hit && Date.now() - hit.at < TTL * 1000) return new Response(hit.body, { headers });
  }
  const scope = {
    url: request.url,
    method: request.method,
    headers: request.headers,
    cookies: parseCookies(request.headers.get("cookie")),
    params,
    searchParams: Object.fromEntries(url.searchParams),
  };
  try {
    const body = await g[STORE].run(scope, () => produce(params.id));
    if (body === null) return new Response("Not Found", { status: 404, headers: { "content-type": "text/plain; charset=utf-8" } });
    if (TTL > 0) {
      if (cache.size >= 64) cache.delete(cache.keys().next().value as string);
      cache.set(key, { at: Date.now(), body });
    }
    return new Response(body, { headers });
  } catch (err) {
    if (err && ((err as any).digest === "NEXT_NOT_FOUND" || (err as any).__krateNotFound)) {
      return new Response("Not Found", { status: 404, headers: { "content-type": "text/plain; charset=utf-8" } });
    }
    console.error("[krate] metadata route failed: " + ((err && (err as any).stack) || err));
    return new Response("Internal Server Error", { status: 500, headers: { "content-type": "text/plain; charset=utf-8", "cache-control": "no-store" } });
  }
}
`

// dynamicMetaRoute reports whether f runs per request, with its cache window.
// A route in a dynamic segment cannot be served this way and stays static.
func (b *Builder) dynamicMetaRoute(f metaRouteFile) (bool, int) {
	src, err := os.ReadFile(f.Path)
	if err != nil {
		return false, 0
	}
	dynamic, revalidate := metaRouteSchedule(string(src))
	if dynamic && strings.Contains(f.URLDir, "[") {
		printWarningsOnce(os.Stderr, []diag.Diagnostic{{
			File: b.display(f.Path), Severity: diag.SeverityWarning, Code: diag.CodeMetadataRouteDynamic,
			Message: filepath.Base(f.Path) + " exports dynamic or revalidate, but it is in a dynamic segment (" + f.URLDir + "), so it still runs once at build time",
			Hint:    "move it to a static directory, or remove the export",
		}})
		return false, 0
	}
	return dynamic, revalidate
}

// buildDynamicDataRoute compiles a per-request sitemap, robots or manifest
// route and returns its manifest entry.
func (b *Builder) buildDynamicDataRoute(f metaRouteFile, revalidate int) (metaRouteEntry, error) {
	abs, err := filepath.Abs(f.Path)
	if err != nil {
		return metaRouteEntry{}, err
	}
	src, err := os.ReadFile(abs)
	if err != nil {
		return metaRouteEntry{}, err
	}
	withID := f.Kind == mrSitemap && generateSitemapsRe.Match(src)
	render := map[metaRouteKind]string{mrSitemap: "renderSitemapXML", mrRobots: "renderRobotsTXT", mrManifest: "renderManifestJSON"}[f.Kind]
	file := map[metaRouteKind]string{mrSitemap: "sitemap.xml", mrRobots: "robots.txt", mrManifest: "manifest.webmanifest"}[f.Kind]
	contentType := map[metaRouteKind]string{mrSitemap: "application/xml; charset=utf-8", mrRobots: "text/plain; charset=utf-8", mrManifest: "application/manifest+json; charset=utf-8"}[f.Kind]

	entry := metaRouteEntry{Kind: f.Kind, Revalidate: revalidate}
	dirKey := metaAPIKeyDir
	if f.URLDir != "" {
		dirKey += "/" + f.URLDir
	}
	if withID {
		entry.Prefix = "/" + joinURLDir(f.URLDir, "sitemap/")
		entry.Suffix = ".xml"
		entry.Key = dirKey + "/sitemap/[id]"
	} else {
		entry.Path = "/" + joinURLDir(f.URLDir, file)
		entry.Key = dirKey + "/" + string(f.Kind)
	}

	genDir := filepath.Join(b.Root, ".krate", "gen", "meta-api")
	genFile := filepath.Join(genDir, filepath.FromSlash(entry.Key)+".ts")
	if err := os.MkdirAll(filepath.Dir(genFile), 0o755); err != nil {
		return metaRouteEntry{}, err
	}
	code := fmt.Sprintf(metaEntryTemplate, strconv.Quote(filepath.ToSlash(abs)), render, revalidate, withID, strconv.Quote(contentType))
	if err := fsutil.WriteFileIfChanged(genFile, []byte(code), 0o644); err != nil {
		return metaRouteEntry{}, err
	}
	if rel, err := filepath.Rel(b.Root, genFile); err == nil {
		entry.Source = filepath.ToSlash(rel)
	}

	outName := entry.Key + ".js"
	outPath := filepath.Join(apiNodeBundleDir(b.Cfg.OutDir), filepath.FromSlash(outName))
	opts := api.BuildOptions{
		AbsWorkingDir: b.Root,
		EntryPoints:   []string{genFile},
		Bundle:        true,
		Platform:      api.PlatformNode,
		Format:        api.FormatESModule,
		Packages:      api.PackagesExternal,
		Plugins:       []api.Plugin{jseval.MarkerPackagesPlugin()},
		Outfile:       outPath,
		Write:         true,
		Sourcemap:     sourceMapMode(b.Cfg.Sourcemap),
		LogLevel:      api.LogLevelSilent,
	}
	if b.shouldMinifyJS() {
		opts.MinifyWhitespace, opts.MinifyIdentifiers, opts.MinifySyntax = true, true, true
	}
	jseval.ApplyNodeESM(&opts)
	res := esbuildBuild(opts)
	if len(res.Errors) > 0 {
		return metaRouteEntry{}, b.routeErr(f, "compiling "+filepath.Base(f.Path)+" failed:\n"+formatEsbuildErrors(res.Errors),
			"the file runs per request on Node; it must default-export a function (or a value) shaped like Next.js's MetadataRoute."+titleCase(string(f.Kind)))
	}
	printf("    %s~%s %s %s(per request)%s\n", cGreen, cReset, strings.TrimPrefix(entry.Path+entry.Prefix, "/"), cGray, cReset)
	return entry, nil
}

// writeMetaRoutes records the dynamic metadata routes for the servers and
// the Node API sidecar, replacing the entries of an earlier build.
func (b *Builder) writeMetaRoutes(entries []metaRouteEntry) error {
	manifest := ServerOutPath(b.Cfg.OutDir, metaRoutesFile)
	if len(entries) == 0 {
		_ = os.Remove(manifest)
		return b.dropMetaAPIRoutes(nil)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		return err
	}
	if err := fsutil.WriteFileIfChanged(manifest, data, 0o644); err != nil {
		return err
	}
	return b.dropMetaAPIRoutes(entries)
}

// dropMetaAPIRoutes rewrites .krate/api-routes.json: the entries of earlier
// dynamic metadata routes go, and keep (this build's routes) are added as
// Node-only routes so the Node sidecar starts and serves them.
func (b *Builder) dropMetaAPIRoutes(keep []metaRouteEntry) error {
	path := filepath.Join(b.Root, ".krate", "api-routes.json")
	var list []apiRouteMeta
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &list)
	}
	out := list[:0]
	for _, m := range list {
		if !strings.HasPrefix(m.File, metaAPIKeyDir+"/") {
			out = append(out, m)
		}
	}
	for _, e := range keep {
		out = append(out, apiRouteMeta{File: e.Key + ".js", NodeOnly: true})
	}
	if len(out) == 0 && len(list) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return writeOutput(path, data)
}
