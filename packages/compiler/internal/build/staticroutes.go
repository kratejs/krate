package build

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/environ"
	"github.com/kratejs/krate/packages/compiler/internal/execx"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/jseval"
	"github.com/kratejs/krate/packages/compiler/internal/ogimage"
)

// Static (build-time) route handlers: GET-only route handlers that run at build
// time and write their responses to dist/ as static files. They can live as
// Next-style `src/pages/**/route.{ts,tsx,js,jsx}` files (mapped directly to
// their public URL) or in `src/api/**` when `output: "static"`, when the file
// exports `dynamic = "force-static"`, or when it exports `generateStaticParams`.

const staticRoutesFile = "static-routes.json"

// StaticRouteEntry records a static route output that needs an explicit
// Content-Type header (extensionless files or non-matching extensions), a
// rewrite (when a route path collides with a child directory), or static
// serving under /api/.
type StaticRouteEntry struct {
	URL         string `json:"url"`
	File        string `json:"file"`
	ContentType string `json:"contentType,omitempty"`
}

// staticRouteSource is one route handler module selected for build-time
// evaluation.
type staticRouteSource struct {
	Path       string
	URLPattern string // slash-separated, no leading slash (e.g. "llms.txt", "og/docs/[...slug]", "api/info")
	FromAPI    bool
}

var (
	pagesRouteHandlerExts = map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true}
	gspAnyExportRe        = regexp.MustCompile(`(?m)^\s*export\s+(?:async\s+)?(?:function\s*\*?|const|let|var)\s+generateStaticParams\b`)
)

// isPagesRouteHandlerFile reports whether base is a Next-style route handler
// file name (`route.ts`, `route.tsx`, `route.js`, `route.jsx`).
func isPagesRouteHandlerFile(base string) bool {
	ext := strings.ToLower(filepath.Ext(base))
	if !pagesRouteHandlerExts[ext] {
		return false
	}
	return strings.TrimSuffix(base, filepath.Ext(base)) == "route"
}

var (
	routeHandlerMethodRe = regexp.MustCompile(`(?m)^\s*export\s+(?:(?:async\s+)?function\s*\*?|const|let|var)\s+(?:GET|HEAD|POST|PUT|PATCH|DELETE|OPTIONS)\b|^\s*export\s*\{[^}]*\b(?:GET|HEAD|POST|PUT|PATCH|DELETE|OPTIONS)\b[^}]*\}`)
	defaultExportRe      = regexp.MustCompile(`(?m)^\s*export\s+default\b|^\s*export\s*\{[^}]*\bas\s+default\b`)
)

// isPagesRouteHandler reports whether path is a route handler: named
// route.{ts,tsx,js,jsx}, exporting an HTTP method and no default export. A
// page file that happens to be called route.tsx (a default-exported
// component served at /route) stays a page.
func isPagesRouteHandler(path string) bool {
	if !isPagesRouteHandlerFile(filepath.Base(path)) {
		return false
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return routeHandlerMethodRe.Match(src) && !defaultExportRe.Match(src)
}

// findPagesRouteHandlers lists `route.{ts,tsx,js,jsx}` files under pagesDir.
func findPagesRouteHandlers(pagesDir string) []staticRouteSource {
	var out []staticRouteSource
	_ = fsutil.WalkExt(pagesDir, pagesRouteHandlerExts, nil, func(p string, _ os.FileInfo) error {
		if !isPagesRouteHandler(p) {
			return nil
		}
		rel, err := filepath.Rel(pagesDir, p)
		if err != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if pathHasSlot(relSlash) || hasUnderscoreSegment(relSlash) {
			return nil
		}
		out = append(out, staticRouteSource{
			Path:       p,
			URLPattern: segmentURLDir(segmentDirKey(p, pagesDir)),
		})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func hasUnderscoreSegment(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if strings.HasPrefix(seg, "_") {
			return true
		}
	}
	return false
}

// shouldBuildStaticAPIRoute reports whether an API route file under src/api/
// should be evaluated at build time rather than compiled for a runtime sidecar.
func shouldBuildStaticAPIRoute(file string, staticOutput bool) bool {
	if staticOutput {
		return true
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	src := string(data)
	mode := ""
	if m := dynamicExportRe.FindStringSubmatch(src); m != nil {
		mode = m[1]
	}
	if mode == "force-dynamic" {
		return false
	}
	if mode == "force-static" {
		return true
	}
	return gspAnyExportRe.MatchString(src)
}

// apiFileToURLPattern turns a src/api-relative path (e.g. "items/[id].ts" or
// "info/index.ts") into its public URL pattern ("api/items/[id]", "api/info").
func apiFileToURLPattern(apiSrcDir, file string) (string, error) {
	rel, err := filepath.Rel(apiSrcDir, file)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	rel = strings.TrimSuffix(rel, filepath.Ext(rel))
	if rel == "index" || rel == "route" {
		return "api", nil
	}
	rel = strings.TrimSuffix(rel, "/index")
	rel = strings.TrimSuffix(rel, "/route")
	return "api/" + rel, nil
}

type staticRouteOutput struct {
	source      staticRouteSource
	urlPath     string
	contentType string
	data        []byte
}

// buildStaticRoutes evaluates every static route handler in parallel (bounded
// worker pool), writes its response bodies to dist/, and writes
// dist/.krate/static-routes.json for krate serve and deploy adapters.
func (b *Builder) buildStaticRoutes(sources []staticRouteSource) []error {
	if len(sources) == 0 {
		_ = os.Remove(ServerOutPath(b.Cfg.OutDir, staticRoutesFile))
		return nil
	}

	bnd := b.newBundler()
	workers := buildWorkerLimit()
	if workers < 4 {
		workers = 4
	}
	if workers > 16 {
		workers = 16
	}
	if len(sources) < workers {
		workers = len(sources)
	}
	pool := newWorkerPool(workers)

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		errs    []error
		outputs []staticRouteOutput
	)
	for _, src := range sources {
		src := src
		wg.Add(1)
		go func() {
			defer wg.Done()
			pool.acquire()
			defer pool.release()
			outs, err := b.evalStaticRoute(src, bnd)
			mu.Lock()
			if err != nil {
				errs = append(errs, err)
			} else {
				outputs = append(outputs, outs...)
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(errs) > 0 {
		sort.SliceStable(errs, func(i, j int) bool { return errs[i].Error() < errs[j].Error() })
		return errs
	}

	// Deterministic write order.
	sort.Slice(outputs, func(i, j int) bool { return outputs[i].urlPath < outputs[j].urlPath })

	// Detect parent-directory collisions across static route outputs (for
	// example an optional catch-all producing both "llms.mdx/docs" and
	// "llms.mdx/docs/intro") so every output writes cleanly on POSIX filesystems.
	dirs := make(map[string]bool, len(outputs))
	for _, o := range outputs {
		for d := path.Dir(o.urlPath); d != "." && d != "/" && d != ""; d = path.Dir(d) {
			dirs[d] = true
		}
	}

	var manifest []StaticRouteEntry
	seenURL := make(map[string]string, len(outputs))
	for _, o := range outputs {
		if prev, dup := seenURL[o.urlPath]; dup {
			errs = append(errs, b.staticRouteErr(o.source,
				fmt.Sprintf("duplicate static route output path /%s (also produced by %s)", o.urlPath, prev),
				"ensure generateStaticParams returns unique parameter sets and routes do not overlap"))
			continue
		}
		seenURL[o.urlPath] = b.display(o.source.Path)

		fileRel := o.urlPath
		needIndex := dirs[o.urlPath]
		if !needIndex {
			if fi, err := os.Stat(filepath.Join(b.Cfg.OutDir, filepath.FromSlash(o.urlPath))); err == nil && fi.IsDir() {
				needIndex = true
			}
		}
		if needIndex {
			fileRel = o.urlPath + "/index" + extForContentType(o.contentType)
		}

		if err := b.writeOut(fileRel, o.data); err != nil {
			errs = append(errs, err)
			continue
		}

		urlWithSlash := "/" + o.urlPath
		fileWithSlash := "/" + fileRel
		ct := strings.TrimSpace(o.contentType)
		needsCT := ct != "" && (staticHostNeedsContentType(o.urlPath, ct) || staticHostNeedsContentType(fileRel, ct))
		if needsCT || fileWithSlash != urlWithSlash || o.source.FromAPI {
			entry := StaticRouteEntry{
				URL:         urlWithSlash,
				File:        fileWithSlash,
				ContentType: ct,
			}
			manifest = append(manifest, entry)
		}
	}

	if err := b.writeStaticRoutesManifest(manifest); err != nil {
		errs = append(errs, err)
	}
	return errs
}

func (b *Builder) staticRouteErr(src staticRouteSource, msg, hint string) error {
	return diag.Diagnostic{
		File:    b.display(src.Path),
		Code:    diag.CodeStaticRouteHandler,
		Message: msg,
		Hint:    hint,
	}
}

func (b *Builder) writeStaticRoutesManifest(entries []StaticRouteEntry) error {
	dest := ServerOutPath(b.Cfg.OutDir, staticRoutesFile)
	if len(entries) == 0 {
		_ = os.Remove(dest)
		return nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].URL < entries[j].URL })
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return fsutil.WriteFileIfChanged(dest, data, 0o644)
}

// evalStaticRoute bundles and executes one static route handler module,
// returning the rendered byte outputs for every param set.
func (b *Builder) evalStaticRoute(src staticRouteSource, bnd *bundler.Bundler) ([]staticRouteOutput, error) {
	if src.URLPattern == "" {
		return nil, b.staticRouteErr(src,
			"a static route handler at the pages root cannot be built because / collides with index.html",
			"move the handler into a named segment (for example src/pages/feed.xml/route.ts) or into src/api/")
	}
	rawSrc, err := os.ReadFile(src.Path)
	if err != nil {
		return nil, err
	}
	if m := dynamicExportRe.FindStringSubmatch(string(rawSrc)); m != nil && m[1] == "force-dynamic" {
		if src.FromAPI {
			return nil, b.staticRouteErr(src,
				"API route exports dynamic = \"force-dynamic\", which needs request-time execution, but output is \"static\"",
				"remove `output: \"static\"` from the config, or export `dynamic = \"force-static\"`")
		}
		return nil, b.staticRouteErr(src,
			"route handler in pages exports dynamic = \"force-dynamic\", which cannot run at build time",
			"move request-time route handlers to src/api/, or remove `dynamic = \"force-dynamic\"`")
	}

	abs, err := filepath.Abs(src.Path)
	if err != nil {
		return nil, err
	}

	run, err := b.runStaticRouteBundle(abs, staticRouteBootstrapJS(abs, b.Root, src.URLPattern), bnd)
	if err != nil {
		return nil, b.staticRouteErr(src,
			"running static route handler failed: "+err.Error(),
			"a static route handler runs at build time on Node with the project's environment; fix the error above or use a request-time API route in src/api/")
	}

	var envelope struct {
		ErrorCode string   `json:"errorCode"`
		Reason    string   `json:"reason"`
		Methods   []string `json:"methods"`
		Status    int      `json:"status"`
		URLPath   string   `json:"urlPath"`
		Results   []struct {
			URLPath     string          `json:"urlPath"`
			ContentType string          `json:"contentType"`
			Body        string          `json:"body"`
			ImageSpec   json.RawMessage `json:"imageSpec"`
		} `json:"results"`
	}
	if err := json.Unmarshal(run.Stdout, &envelope); err != nil {
		return nil, b.staticRouteErr(src,
			"unreadable static route handler output ("+err.Error()+"): "+run.describe("exit status 0"),
			"the build-time runner must print one JSON result line on stdout; check the handler for process.exit() or output that truncates stdout")
	}

	switch envelope.ErrorCode {
	case "":
	case "NON_GET":
		methods := strings.Join(envelope.Methods, ", ")
		hint := "static route handlers support GET only; remove non-GET exports or move the handler to src/api/ without `output: \"static\"` or `dynamic = \"force-static\"`"
		return nil, b.staticRouteErr(src,
			fmt.Sprintf("static route handler exports %s, which cannot be statically generated", methods),
			hint)
	case "NO_GET":
		return nil, b.staticRouteErr(src,
			"static route handler has no GET export",
			"export `async function GET()` returning a Response (or ImageResponse)")
	case "NO_GSP":
		return nil, b.staticRouteErr(src,
			fmt.Sprintf("dynamic static route handler (/%s) has no generateStaticParams export", src.URLPattern),
			"export `generateStaticParams()` returning the parameter sets to build at build time")
	case "BAD_PARAMS":
		return nil, b.staticRouteErr(src,
			"invalid generateStaticParams output: "+envelope.Reason,
			"generateStaticParams() must return an array of objects matching the dynamic segments")
	case "DYNAMIC":
		hint := "remove `output: \"static\"` (or `dynamic = \"force-static\"`) and serve this route from src/api/, or stop reading " + envelope.Reason
		return nil, b.staticRouteErr(src,
			fmt.Sprintf("static route handler uses %s, which needs request-time execution", envelope.Reason),
			hint)
	case "BAD_RESPONSE":
		return nil, b.staticRouteErr(src,
			"GET must return a Response or ImageResponse",
			"return `new Response(...)`, `Response.json(...)`, or `new ImageResponse(...)`")
	case "BAD_STATUS":
		return nil, b.staticRouteErr(src,
			fmt.Sprintf("GET for /%s returned status %d", envelope.URLPath, envelope.Status),
			"a static route handler must return a 2xx response so its body can be written to dist/")
	case "ERROR":
		return nil, b.staticRouteErr(src,
			"GET failed: "+envelope.Reason,
			"fix the error thrown while evaluating the route at build time")
	default:
		return nil, b.staticRouteErr(src, "static route handler failed: "+envelope.Reason, "")
	}

	var outputs []staticRouteOutput
	for _, r := range envelope.Results {
		var data []byte
		if len(r.ImageSpec) > 0 && string(r.ImageSpec) != "null" {
			spec, err := ogimage.ParseSpec(r.ImageSpec)
			if err != nil {
				return nil, b.staticRouteErr(src, err.Error(), "")
			}
			png, err := ogimage.Render(spec)
			if err != nil {
				var oe *ogimage.Error
				if errors.As(err, &oe) {
					return nil, diag.Diagnostic{
						File:    b.display(src.Path),
						Code:    diag.CodeImageUnsupported,
						Message: oe.Error(),
						Hint:    "draw with the supported subset (see the Metadata routes guide), or pass a font with options.fonts for other characters",
					}
				}
				return nil, b.staticRouteErr(src, "drawing ImageResponse failed: "+err.Error(), "")
			}
			data = png
		} else {
			decoded, err := base64.StdEncoding.DecodeString(r.Body)
			if err != nil {
				return nil, b.staticRouteErr(src, "unreadable response body: "+err.Error(), "")
			}
			data = decoded
		}
		outputs = append(outputs, staticRouteOutput{
			source:      src,
			urlPath:     r.URLPath,
			contentType: r.ContentType,
			data:        data,
		})
	}
	return outputs, nil
}

// runStaticRouteBundle compiles and executes a static route handler bootstrap.
func (b *Builder) runStaticRouteBundle(abs, source string, bnd *bundler.Bundler) (scriptRun, error) {
	resolve := func(importer, spec string) (string, bool) {
		if bnd != nil {
			if p := bnd.ResolveForNode(importer, spec); p != "" {
				return p, true
			}
		}
		return "", false
	}
	js, err := b.bundleExternalizingNative(jseval.Options{
		Source:          source,
		ResolveDir:      filepath.Dir(abs),
		Sourcefile:      "krate-staticroute-bootstrap.tsx",
		Loader:          api.LoaderTSX,
		ResolveImport:   resolve,
		JSXImportSource: "@krate/runtime/server",
		SourceTransform: b.serverGlobalsTransform,
	})
	if err != nil {
		return scriptRun{}, fmt.Errorf("bundling static route: %w", err)
	}
	genDir := filepath.Join(b.Root, ".krate", "gen")
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		return scriptRun{}, fmt.Errorf("creating bootstrap directory: %w", err)
	}
	scriptPath := filepath.Join(genDir, "staticroute-"+hashContent([]byte(js))+".mjs")
	if err := fsutil.WriteFileIfChanged(scriptPath, []byte(js), 0o644); err != nil {
		return scriptRun{}, fmt.Errorf("writing bootstrap: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmdName, args := "node", []string{scriptPath}
	switch b.gspRuntime() {
	case "bun":
		cmdName, args = "bun", []string{"run", scriptPath}
	case "deno":
		cmdName, args = "deno", []string{"run", "--allow-net", "--allow-read", "--allow-env", "--allow-sys", scriptPath}
	}
	cmd := execx.Command(ctx, cmdName, args...)
	if b.Root != "" {
		cmd.Dir = b.Root
	} else {
		cmd.Dir = filepath.Dir(abs)
	}
	env := make(map[string]string, len(b.Env)+1)
	for k, v := range b.Env {
		env[k] = v
	}
	env["KRATE_PRERENDER"] = "1"
	cmd.Env = append(os.Environ(), environ.KVList(env)...)

	run, err := runBuildScript(ctx, cmd, "execution timed out after 30s")
	if err != nil {
		return run, err
	}
	// The handler may print before the result; the result is the last line.
	lines := strings.Split(strings.TrimRight(string(run.Stdout), "\r\n"), "\n")
	run.Stdout = []byte(lines[len(lines)-1])
	return run, nil
}

// staticRouteBootstrapJS generates the Node entry that validates exports,
// expands generateStaticParams, guards against request-dependent API reads,
// invokes GET, and serializes each response (or ImageResponse spec) as JSON.
func staticRouteBootstrapJS(abs, root, urlPattern string) string {
	rootJSON, _ := json.Marshal(root)
	patternJSON, _ := json.Marshal(urlPattern)
	return fmt.Sprintf(`import * as mod from %q;
import { inlineImages } from "@krate/runtime/og";
import { AsyncLocalStorage } from "node:async_hooks";
import { writeSync } from "node:fs";

const root = %s;
const pattern = %s;
const emit = (obj) => {
  // Write synchronously and in full: process.exit() would cut a large async pipe write short.
  const buf = Buffer.from(JSON.stringify(obj) + "\n");
  let off = 0;
  while (off < buf.length) {
    try { off += writeSync(1, buf, off, buf.length - off); }
    catch (err) { if (!err || err.code !== "EAGAIN") throw err; }
  }
  process.exit(0);
};

const nonGet = [];
for (const m of ["POST", "PUT", "PATCH", "DELETE"]) {
  if (typeof mod[m] === "function") nonGet.push(m);
}
if (typeof mod.default === "function") nonGet.push("default");
if (nonGet.length > 0) emit({ errorCode: "NON_GET", methods: nonGet });

const GET = mod.GET || (mod.default && typeof mod.default === "object" && mod.default.GET);
if (typeof GET !== "function") emit({ errorCode: "NO_GET" });

const parseSeg = (seg) => {
  if (seg.startsWith("[[...") && seg.endsWith("]]")) return { kind: "opt", name: seg.slice(5, -2) };
  if (seg.startsWith("[...") && seg.endsWith("]")) return { kind: "catch", name: seg.slice(4, -1) };
  if (seg.startsWith("[") && seg.endsWith("]")) return { kind: "param", name: seg.slice(1, -1) };
  return { kind: "static", text: seg };
};
const segs = pattern.split("/").filter(Boolean).map(parseSeg);
const isDynamic = segs.some((s) => s.kind !== "static");

const badPart = (p) => p === "" || p === "." || p === ".." || p.includes("/") || p.includes("\\");
const expandOne = (raw) => {
  if (!raw || typeof raw !== "object") emit({ errorCode: "BAD_PARAMS", reason: "each entry must be an object" });
  const parts = [];
  const params = {};
  for (const s of segs) {
    if (s.kind === "static") { parts.push(s.text); continue; }
    const v = raw[s.name];
    if (s.kind === "param") {
      if (v === undefined || v === null || v === "") emit({ errorCode: "BAD_PARAMS", reason: "missing param " + s.name });
      const str = String(v);
      if (badPart(str)) emit({ errorCode: "BAD_PARAMS", reason: "invalid param " + s.name + ": " + str });
      parts.push(str);
      params[s.name] = str;
    } else if (s.kind === "catch") {
      const arr = Array.isArray(v) ? v.map(String) : (typeof v === "string" ? v.split("/").filter(Boolean) : []);
      if (arr.length === 0) emit({ errorCode: "BAD_PARAMS", reason: "catch-all param " + s.name + " must be non-empty" });
      for (const p of arr) if (badPart(p)) emit({ errorCode: "BAD_PARAMS", reason: "invalid param segment: " + p });
      parts.push(...arr);
      params[s.name] = Array.isArray(v) ? arr : String(v);
    } else if (s.kind === "opt") {
      if (v === undefined || v === null || v === "" || (Array.isArray(v) && v.length === 0)) {
        if (s.name in raw) params[s.name] = Array.isArray(v) ? [] : v;
      } else {
        const arr = Array.isArray(v) ? v.map(String) : String(v).split("/").filter(Boolean);
        for (const p of arr) if (badPart(p)) emit({ errorCode: "BAD_PARAMS", reason: "invalid param segment: " + p });
        parts.push(...arr);
        params[s.name] = Array.isArray(v) ? arr : String(v);
      }
    }
  }
  const urlPath = parts.join("/");
  if (!urlPath) emit({ errorCode: "BAD_PARAMS", reason: "expanded path is empty" });
  return { urlPath, params };
};

let entries;
if (isDynamic) {
  const gsp = mod.generateStaticParams || (mod.default && mod.default.generateStaticParams);
  if (typeof gsp !== "function") emit({ errorCode: "NO_GSP" });
  let rawSets;
  try { rawSets = await gsp(); } catch (err) { emit({ errorCode: "ERROR", reason: (err && err.message) || String(err) }); }
  if (!Array.isArray(rawSets)) emit({ errorCode: "BAD_PARAMS", reason: "generateStaticParams must return an array" });
  entries = rawSets.map(expandOne);
} else {
  entries = [{ urlPath: pattern, params: {} }];
}

const STORE_KEY = Symbol.for("krate.request.store");
const g = globalThis;
if (!g[STORE_KEY]) g[STORE_KEY] = new AsyncLocalStorage();

const dynamicErr = (api) => {
  const e = new Error("Dynamic server usage: " + api);
  e.__krateDynamicUsage = api;
  e.digest = "DYNAMIC_SERVER_USAGE";
  return e;
};
const dynamicReason = (err) => {
  if (!err || typeof err !== "object") return "";
  if (typeof err.__krateDynamicUsage === "string" && err.__krateDynamicUsage) return err.__krateDynamicUsage;
  if (err.digest === "DYNAMIC_SERVER_USAGE") return err.message || "dynamic server usage";
  return "";
};

const thenable = (p) => Object.assign(Promise.resolve(p), p);
const NativeURL = globalThis.URL;

const guardHeaders = () => new Proxy(new Headers(), {
  get(_, prop) {
    if (prop === "then" || prop === Symbol.toStringTag) return undefined;
    return () => { throw dynamicErr("request.headers"); };
  },
});
const guardCookies = () => new Proxy({}, {
  get(_, prop) {
    if (prop === "then" || prop === Symbol.toStringTag) return undefined;
    throw dynamicErr("request.cookies");
  },
});
const guardURL = (u, apiPrefix) => new Proxy(u, {
  get(target, prop) {
    if (prop === "searchParams" || prop === "search") throw dynamicErr(apiPrefix + "." + String(prop));
    if (prop === "basePath") return "";
    if (prop === "clone") return () => guardURL(new NativeURL(target.href), apiPrefix);
    const v = Reflect.get(target, prop, target);
    return typeof v === "function" ? v.bind(target) : v;
  },
});

const results = [];
for (const entry of entries) {
  const reqHref = "http://localhost/" + entry.urlPath;
  const req = new Request(reqHref, { method: "GET" });
  Object.defineProperty(req, "headers", { get() { return guardHeaders(); }, configurable: true });
  Object.defineProperty(req, "cookies", { get() { return guardCookies(); }, configurable: true });
  Object.defineProperty(req, "nextUrl", { get() { return guardURL(new NativeURL(reqHref), "request.nextUrl"); }, configurable: true });

  const WrappedURL = new Proxy(NativeURL, {
    construct(target, args, newTarget) {
      const inst = Reflect.construct(target, args, newTarget);
      if (args.length > 0 && String(args[0]) === reqHref) return guardURL(inst, "request.url");
      return inst;
    },
  });

  const scope = {
    prerender: true,
    url: reqHref,
    method: "GET",
    headers: new Headers(),
    cookies: new Map(),
    params: entry.params,
    searchParams: {},
  };

  let res;
  globalThis.URL = WrappedURL;
  try {
    res = await g[STORE_KEY].run(scope, () => GET(req, { params: thenable(entry.params) }));
  } catch (err) {
    const reason = dynamicReason(err);
    if (reason) emit({ errorCode: "DYNAMIC", reason });
    emit({ errorCode: "ERROR", reason: (err && err.stack) || String(err) });
  } finally {
    globalThis.URL = NativeURL;
  }

  if (!res || (!res.__krateImage && typeof res.arrayBuffer !== "function")) {
    emit({ errorCode: "BAD_RESPONSE" });
  }
  const status = typeof res.status === "number" ? res.status : 200;
  if (status < 200 || status > 299) {
    emit({ errorCode: "BAD_STATUS", status, urlPath: entry.urlPath });
  }
  const ct = (res.headers && typeof res.headers.get === "function" ? res.headers.get("content-type") : "") || "";
  if (res.__krateImage) {
    try { await inlineImages(res.__krateImage, root); } catch (err) { emit({ errorCode: "ERROR", reason: (err && err.message) || String(err) }); }
    results.push({ urlPath: entry.urlPath, contentType: ct || "image/png", imageSpec: res.__krateImage });
  } else {
    let buf;
    try { buf = Buffer.from(await res.arrayBuffer()); } catch (err) { emit({ errorCode: "ERROR", reason: (err && err.message) || String(err) }); }
    results.push({ urlPath: entry.urlPath, contentType: ct, body: buf.toString("base64") });
  }
}
emit({ results });
`, filepath.ToSlash(abs), rootJSON, patternJSON)
}

// staticHostNeedsContentType reports whether a static host needs an explicit
// Content-Type header for fileRel (either it has no extension, or the extension
// does not imply the media type returned by the route handler).
func staticHostNeedsContentType(fileRel, contentType string) bool {
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil || media == "" {
		return false
	}
	media = strings.ToLower(media)
	ext := strings.ToLower(path.Ext(fileRel))
	if ext == "" {
		return true
	}
	known := map[string][]string{
		".txt":         {"text/plain"},
		".html":        {"text/html"},
		".htm":         {"text/html"},
		".json":        {"application/json"},
		".xml":         {"application/xml", "text/xml"},
		".rss":         {"application/rss+xml", "application/xml", "text/xml"},
		".atom":        {"application/atom+xml", "application/xml", "text/xml"},
		".css":         {"text/css"},
		".js":          {"text/javascript", "application/javascript"},
		".mjs":         {"text/javascript", "application/javascript"},
		".csv":         {"text/csv"},
		".md":          {"text/markdown"},
		".mdx":         {"text/markdown"},
		".png":         {"image/png"},
		".jpg":         {"image/jpeg"},
		".jpeg":        {"image/jpeg"},
		".gif":         {"image/gif"},
		".webp":        {"image/webp"},
		".svg":         {"image/svg+xml"},
		".ico":         {"image/x-icon", "image/vnd.microsoft.icon"},
		".webmanifest": {"application/manifest+json"},
		".pdf":         {"application/pdf"},
		".wasm":        {"application/wasm"},
	}
	if allowed, ok := known[ext]; ok {
		for _, a := range allowed {
			if media == a {
				return false
			}
		}
	}
	return true
}

// extForContentType picks a clean file extension when a static route URL
// collides with a directory of child routes and must be stored as index.<ext>.
func extForContentType(contentType string) string {
	media, _, _ := mime.ParseMediaType(contentType)
	switch strings.ToLower(media) {
	case "text/markdown", "text/x-markdown":
		return ".md"
	case "text/plain":
		return ".txt"
	case "application/json":
		return ".json"
	case "application/xml", "text/xml":
		return ".xml"
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/svg+xml":
		return ".svg"
	case "text/csv":
		return ".csv"
	case "text/html":
		return ".html"
	default:
		return ".bin"
	}
}

// staticRouteIndex resolves public URLs to their static route manifest entry.
type staticRouteIndex map[string]StaticRouteEntry

func loadStaticRoutes(outDir string) staticRouteIndex {
	data, err := os.ReadFile(ServerOutPath(outDir, staticRoutesFile))
	if err != nil {
		return nil
	}
	var list []StaticRouteEntry
	if json.Unmarshal(data, &list) != nil || len(list) == 0 {
		return nil
	}
	idx := make(staticRouteIndex, len(list))
	for _, e := range list {
		idx[e.URL] = e
	}
	return idx
}

// staticRouteLoader serves the static route manifest for krate serve / dev.
type staticRouteLoader struct {
	outDir string
	dev    bool

	mu    sync.Mutex
	idx   staticRouteIndex
	mtime time.Time
	read  bool
}

func newStaticRouteLoader(outDir string, dev bool) *staticRouteLoader {
	return &staticRouteLoader{outDir: outDir, dev: dev}
}

func (l *staticRouteLoader) get() staticRouteIndex {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.read && !l.dev {
		return l.idx
	}
	var mt time.Time
	if fi, err := os.Stat(ServerOutPath(l.outDir, staticRoutesFile)); err == nil {
		mt = fi.ModTime()
	}
	if !l.read || !mt.Equal(l.mtime) {
		l.idx, l.mtime, l.read = loadStaticRoutes(l.outDir), mt, true
	}
	return l.idx
}
