package build

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/environ"
	"github.com/kratejs/krate/packages/compiler/internal/execx"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/jseval"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
	"github.com/kratejs/krate/packages/compiler/internal/plugin"
	"github.com/kratejs/krate/packages/compiler/internal/reactive"
	"github.com/kratejs/krate/packages/compiler/internal/renderer"
)

// injectStaticParams seeds the root page component with concrete dynamic-route
// params so a statically generated page ([id].tsx via generateStaticParams)
// can render them. Params are exposed to the component's SSR evaluation via:
// - `params` - a JSON object (e.g. { params } destructuring -> params.id)
// - each param key - a direct binding (e.g. { id } destructuring -> id)
// Only pure, signal-less root components are SSREval'd this way; interactive
// (client) roots keep their normal hydration-driven emission.
func injectStaticParams(tree *irtree.ComponentTree, params map[string]string) {
	root := tree.Root
	if root == nil || params == nil || len(params) == 0 {
		return
	}
	if root.Tier != irtree.TierStatic && root.Tier != irtree.TierServer {
		// Interactive roots render their params reactively via the served props
		// script; injecting static bindings here would conflict with hydration.
		return
	}
	if irtree.HasClientComponent(root.Children) {
		// Evaluating the root at build time cannot hydrate the interactive
		// components it renders (their createSignal has no build-time value);
		// the tree path emits them with their hydration markers instead.
		return
	}
	bindings := make(map[string]jsvalue.Value, len(params)+1)
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	paramsObj := jsvalue.NewObject()
	for _, k := range keys {
		bindings[k] = jsvalue.String(params[k])
		paramsObj.Set(k, jsvalue.String(params[k]))
	}
	bindings["params"] = paramsObj
	if len(bindings) > 0 {
		if len(root.SSREvalBindings) > 0 {
			for k, v := range root.SSREvalBindings {
				if _, ok := bindings[k]; !ok {
					bindings[k] = v
				}
			}
		}
		root.SSREvalBindings = bindings
		root.IsSSREval = true
	}
}

// dynamicParamSentinelPrefix marks the build-time placeholder emitted for a
// dynamic-route parameter in a statically built template. The dev/preview
// server replaces each sentinel with the matched URL segment at request time
// (see serve.go handleDynamicRoute), so text, <title>, and attribute positions
// all carry a real value - not just signal-bound text nodes.
const dynamicParamSentinelPrefix = "__KRATE_PARAM_"

// dynamicParamSentinel returns the unique token emitted for the named route
// parameter. The trailing "__" keeps names unambiguous (id vs idx).
func dynamicParamSentinel(name string) string {
	return dynamicParamSentinelPrefix + name + "__"
}

// dynamicRouteSentinels maps each route param to its placeholder token.
func dynamicRouteSentinels(paramNames []string) map[string]string {
	out := make(map[string]string, len(paramNames))
	for _, name := range paramNames {
		out[name] = dynamicParamSentinel(name)
	}
	return out
}

// injectDynamicRoutePlaceholders binds each dynamic-route [param] to a unique
// sentinel so a statically built template's evaluated output (body text, title,
// meta attributes) contains a marker the server substitutes per request.
// Mirrors injectStaticParams: it SSREval's the root with the sentinel bindings,
// and only for pure static/server roots. Interactive client roots render params
// reactively after hydration and must not be frozen to a sentinel.
func injectDynamicRoutePlaceholders(tree *irtree.ComponentTree, paramNames []string) {
	root := tree.Root
	if root == nil || len(paramNames) == 0 {
		return
	}
	if root.Tier != irtree.TierStatic && root.Tier != irtree.TierServer {
		return
	}
	if irtree.HasClientComponent(root.Children) {
		return
	}
	bindings := make(map[string]jsvalue.Value, len(paramNames)+1)
	paramsObj := jsvalue.NewObject()
	for _, name := range paramNames {
		sentinel := jsvalue.String(dynamicParamSentinel(name))
		bindings[name] = sentinel
		paramsObj.Set(name, sentinel)
	}
	bindings["params"] = paramsObj
	for k, v := range root.SSREvalBindings {
		if _, ok := bindings[k]; !ok {
			bindings[k] = v
		}
	}
	root.SSREvalBindings = bindings
	root.IsSSREval = true
}

// extractParamNames extracts parameter names from a dynamic route filename.
// e.g. "video/[id].tsx" -> ["id"], "user/[username]/posts/[postId].tsx" -> ["username", "postId"]
func extractParamNames(pagePath, pagesDir string) []string {
	rel, err := filepath.Rel(pagesDir, pagePath)
	if err != nil {
		return nil
	}
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	var params []string
	for i, part := range parts {
		cleaned := part
		if i == len(parts)-1 {
			cleaned = strings.TrimSuffix(part, filepath.Ext(part))
		}
		if seg := parseRouteSeg(cleaned); seg.kind != segStatic {
			params = append(params, seg.text)
		}
	}
	return params
}

// hasGenerateStaticParams checks if the program exports a generateStaticParams function.
func hasGenerateStaticParams(prog *ast.Program) bool {
	for _, stmt := range prog.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok {
			continue
		}
		fn, ok := exp.Declaration.(*ast.FnDecl)
		if ok && fn.Name == "generateStaticParams" {
			return true
		}
	}
	return false
}

// executeGenerateStaticParams runs the page's generateStaticParams and returns
// the parsed param combinations. It evaluates the function on the embedded
// esbuild+QuickJS runtime first. If the module needs Node built-ins or other
// runtime APIs QuickJS cannot provide, it bundles the bootstrap to ESM and runs
// it with the configured runtime. bnd supplies the bundler's resolution rules
// (virtual modules such as `krate/content`, tsconfig path aliases, krate/*
// packages).
func (b *Builder) executeGenerateStaticParams(pagePath string, bnd *bundler.Bundler) ([]map[string]string, error) {
	abs, err := filepath.Abs(pagePath)
	if err != nil {
		return nil, err
	}

	if raw, eerr := b.gspEmbedded(abs, bnd); eerr == nil {
		if sets, perr := parseParamSets(raw); perr == nil {
			return sets, nil
		}
	}

	output, err := b.runGSPBundle(abs, bnd)
	if err != nil {
		return nil, fmt.Errorf("generateStaticParams execution: %w", err)
	}
	return parseParamSets(output)
}

// runGSPBundle bundles the generateStaticParams bootstrap to ESM JavaScript and
// runs it with the configured runtime. Bundling with esbuild preserves TSX,
// path aliases, and krate/* resolution while avoiding tsx entirely.
func (b *Builder) runGSPBundle(abs string, bnd *bundler.Bundler) ([]byte, error) {
	bootstrap := gspBootstrapJS(filepath.ToSlash(abs))
	resolve := func(importer, spec string) (string, bool) {
		if bnd != nil {
			if p := bnd.Resolve(importer, spec); p != "" {
				return p, true
			}
		}
		return "", false
	}
	js, err := jseval.Bundle(jseval.Options{
		Source:        bootstrap,
		ResolveDir:    filepath.Dir(abs),
		Sourcefile:    "krate-gsp-bootstrap.ts",
		Loader:        api.LoaderTS,
		ResolveImport: resolve,
	})
	if err != nil {
		return nil, fmt.Errorf("bundling bootstrap: %w", err)
	}
	genDir := filepath.Join(b.Root, ".krate", "gen")
	if err := os.MkdirAll(genDir, 0755); err != nil {
		return nil, fmt.Errorf("creating bootstrap directory: %w", err)
	}
	name := "gsp-" + hashContent([]byte(js)) + ".mjs"
	path := filepath.Join(genDir, name)
	if err := fsutil.WriteFileIfChanged(path, []byte(js), 0644); err != nil {
		return nil, fmt.Errorf("writing bootstrap: %w", err)
	}

	runtime := b.gspRuntime()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var args []string
	cmdName := "node"
	switch runtime {
	case "", "node":
		args = []string{path}
	case "bun":
		cmdName = "bun"
		args = []string{"run", path}
	case "deno":
		cmdName = "deno"
		args = []string{"run", "--allow-net", "--allow-read", "--allow-env", "--allow-sys", path}
	default:
		return nil, fmt.Errorf("unsupported runtime %q for generateStaticParams (want node, bun, or deno)", runtime)
	}
	cmd := execx.Command(ctx, cmdName, args...)
	cmd.Dir = filepath.Dir(abs)
	if len(b.Env) > 0 {
		cmd.Env = append(os.Environ(), environ.KVList(b.Env)...)
	}
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, errors.New("execution timed out after 30s")
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("execution failed:\n%s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

// gspEmbedded evaluates the page's generateStaticParams on the embedded
// esbuild+QuickJS runtime and returns its JSON result. bnd is consulted to
// resolve imports esbuild can't see (krate/content, path aliases).
func (b *Builder) gspEmbedded(abs string, bnd *bundler.Bundler) (json.RawMessage, error) {
	entry := fmt.Sprintf(`import * as mod from %q;
const fn = mod.generateStaticParams || (mod.default && mod.default.generateStaticParams);
if (typeof fn !== "function") { throw new Error("generateStaticParams is not a function"); }
Promise.resolve(fn()).then(function (r) { globalThis.%s = r; }, function () {});
`, filepath.ToSlash(abs), jseval.ResultGlobal)

	resolve := func(importer, spec string) (string, bool) {
		if bnd != nil {
			if p := bnd.Resolve(importer, spec); p != "" {
				return p, true
			}
		}
		return "", false
	}

	return jseval.Eval(jseval.Options{
		Source:        entry,
		ResolveDir:    filepath.Dir(abs),
		Env:           b.scriptEnv(),
		Await:         true,
		ResolveImport: resolve,
	})
}

// gspRuntime picks the build-time runtime for bundled generateStaticParams.
// The SSR runtime is the explicit runtime setting; the global runtime is used
// only when it names a supported sidecar (quickjs cannot run this bundle).
func (b *Builder) gspRuntime() string {
	if b.Cfg == nil {
		return "node"
	}
	for _, rt := range []string{b.Cfg.SSR.SSRRuntime, b.Cfg.Runtime} {
		switch strings.ToLower(strings.TrimSpace(rt)) {
		case "node", "bun", "deno":
			return strings.ToLower(strings.TrimSpace(rt))
		}
	}
	return "node"
}

// gspBootstrapJS imports the page and prints the JSON result of
// generateStaticParams. It is bundled to ESM before execution.
func gspBootstrapJS(importPath string) string {
	return fmt.Sprintf(
		`import * as mod from %q;
const fn = mod.generateStaticParams || (mod.default && mod.default.generateStaticParams);
if (typeof fn !== 'function') {
  process.stderr.write('generateStaticParams is not a function\n');
  process.exit(1);
}
const result = await fn();
console.log(JSON.stringify(result));
`,
		importPath,
	)
}

// parseParamSets decodes the JSON array of param objects. Numbers and booleans
// become their string form, and an array of scalars (a catch-all value such as
// ["a", "b"]) becomes its "/"-joined path. Any other value shape is an error.
func parseParamSets(raw []byte) ([]map[string]string, error) {
	var rawSets []map[string]interface{}
	if err := json.Unmarshal(raw, &rawSets); err != nil {
		return nil, fmt.Errorf("parsing generateStaticParams output: %w\nraw: %s", err, string(raw))
	}
	paramSets := make([]map[string]string, 0, len(rawSets))
	for i, rawSet := range rawSets {
		ps := make(map[string]string, len(rawSet))
		for k, v := range rawSet {
			s, ok := paramScalar(v)
			if !ok {
				return nil, fmt.Errorf("generateStaticParams entry #%d: the value of %q must be a string, a number or an array of them, got %s", i+1, k, entryJSON(map[string]string{k: fmt.Sprint(v)}))
			}
			ps[k] = s
		}
		paramSets = append(paramSets, ps)
	}
	return paramSets, nil
}

// paramScalar renders one generateStaticParams value as a path string.
func paramScalar(v interface{}) (string, bool) {
	switch val := v.(type) {
	case string:
		return val, true
	case float64, bool:
		b, _ := json.Marshal(val)
		return string(b), true
	case []interface{}:
		parts := make([]string, 0, len(val))
		for _, item := range val {
			switch item.(type) {
			case string, float64, bool:
				s, _ := paramScalar(item)
				parts = append(parts, s)
			default:
				return "", false
			}
		}
		return strings.Join(parts, "/"), true
	}
	return "", false
}

// staticParamsPage represents a page with params for static generation.
type staticParamsPage struct {
	PagePath string
	Params   map[string]string
	OutPath  string // e.g. "video/abc123"
}

// isDynamicRoute checks if a page path contains [param] segments.
func isDynamicRoute(pagePath, pagesDir string) bool {
	rel, err := filepath.Rel(pagesDir, pagePath)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return strings.Contains(rel, "[") && strings.Contains(rel, "]")
}

// staticParamsResolution is the per-dynamic-route outcome of expanding a route
// with generateStaticParams.
type staticParamsResolution struct {
	expanded []staticParamsPage
	note     bool
	errs     []string
}

// resolveStaticParamsPages expands dynamic routes using generateStaticParams.
// It returns additional pages to build, each with concrete param values, plus
// any errors encountered while resolving dynamic routes (bundling or executing
// generateStaticParams). A dynamic route that lacks generateStaticParams is
// left to runtime resolution and is not an error. Dynamic routes are independent,
// so they are bundled and evaluated in parallel.
func (b *Builder) resolveStaticParamsPages(pages []string) ([]staticParamsPage, error) {
	var dynamic []string
	for _, page := range pages {
		// Intercepting-route fragments are served from their dynamic template.
		if cachedInterceptForPage(page, b.Cfg.PagesDir) != nil {
			continue
		}
		if isDynamicRoute(page, b.Cfg.PagesDir) {
			dynamic = append(dynamic, page)
		}
	}
	if len(dynamic) == 0 {
		return nil, nil
	}

	results := make([]staticParamsResolution, len(dynamic))
	pool := newWorkerPool(buildWorkerLimit())
	var wg sync.WaitGroup
	for i := range dynamic {
		wg.Add(1)
		pool.acquire()
		go func(i int) {
			defer wg.Done()
			defer pool.release()
			results[i] = b.resolveStaticParamsPageOnce(dynamic[i])
		}(i)
	}
	wg.Wait()

	var expanded []staticParamsPage
	var errs []string
	for i := range results {
		if results[i].note {
			printf("  %s*%s generateStaticParams: %s\n", cCyan, cReset, filepath.Base(dynamic[i]))
		}
		expanded = append(expanded, results[i].expanded...)
		errs = append(errs, results[i].errs...)
	}

	if len(errs) > 0 {
		return expanded, fmt.Errorf("generateStaticParams resolution failed:\n  %s", strings.Join(errs, "\n  "))
	}

	return expanded, nil
}

type gspOnce struct {
	once sync.Once
	res  staticParamsResolution
}

// resolveStaticParamsPageOnce is resolveStaticParamsPage memoized for the
// current build, so the template pass and the expansion pass of one route agree
// on the params and generateStaticParams runs once.
func (b *Builder) resolveStaticParamsPageOnce(page string) staticParamsResolution {
	v, _ := b.gspMemo.LoadOrStore(page, &gspOnce{})
	m := v.(*gspOnce)
	m.once.Do(func() { m.res = b.resolveStaticParamsPage(page) })
	return m.res
}

// resolveStaticParamsPage bundles one dynamic route and expands its concrete
// param combinations.
func (b *Builder) resolveStaticParamsPage(page string) staticParamsResolution {
	var res staticParamsResolution

	bnd := b.newBundler()
	bundle, err := bnd.Bundle(page)
	if err != nil {
		res.errs = append(res.errs, fmt.Sprintf("failed to bundle %s for generateStaticParams: %v", page, err))
		return res
	}

	entryModule := findEntryModule(bundle.Modules)
	if entryModule == nil || entryModule.Program == nil {
		return res
	}
	if !hasGenerateStaticParams(entryModule.Program) {
		return res
	}
	res.note = true

	paramSets, err := b.executeGenerateStaticParams(page, bnd)
	if err != nil {
		res.errs = append(res.errs, fmt.Sprintf("generateStaticParams (%s): %v", filepath.Base(page), err))
		return res
	}

	rel, _ := filepath.Rel(b.Cfg.PagesDir, page)
	pages, errs := expandStaticParams(page, filepath.ToSlash(rel), paramSets)
	if len(errs) > 0 {
		res.errs = append(res.errs, errs...)
		return res
	}
	res.expanded = pages

	// The metadata of every entry resolves in one run (see metadataeval.go).
	b.prefetchMetadata(page, paramSets, bnd)
	return res
}

// expandStaticParams turns the entries returned by generateStaticParams into
// one page each. rel is the route file relative to the pages directory. An
// entry that cannot produce a page (a route param without a value, or a value
// that is not a safe path segment) or that collides with a different entry is
// an error naming the entry; an exact duplicate is dropped. The result has one
// page per output path, so the number built is the number written.
func expandStaticParams(page, rel string, paramSets []map[string]string) ([]staticParamsPage, []string) {
	rel = stripRouteGroups(strings.TrimSuffix(rel, filepath.Ext(rel)))
	rel = strings.TrimSuffix(rel, "/index")
	parts := strings.Split(rel, "/")
	name := filepath.Base(page)

	var pages []staticParamsPage
	var errs []string
	fail := func(n int, params map[string]string, why, hint string) {
		errs = append(errs, fmt.Sprintf("%s: [%s] generateStaticParams entry #%d %s %s (hint: %s)",
			name, diag.CodeStaticParamsEntry, n, entryJSON(params), why, hint))
	}
	seen := map[string]int{}
	for idx, params := range paramSets {
		n := idx + 1
		out := parts[:0:0]
		bad := false
		for _, part := range parts {
			seg := parseRouteSeg(part)
			if seg.kind == segStatic {
				out = append(out, part)
				continue
			}
			val, ok := params[seg.text]
			if !ok {
				fail(n, params, "has no value for ["+seg.text+"], so it would write the literal path "+part,
					"return an object with a "+seg.text+" key for every entry, for example { "+seg.text+": \"my-post\" }")
				bad = true
				break
			}
			if why := unsafeParamValue(seg, val); why != "" {
				fail(n, params, why, "param values become directory names; use plain segments such as \"my-post\"")
				bad = true
				break
			}
			// Catch-all values are "/"-joined segments; an empty optional
			// catch-all renders at the parent path.
			if val != "" || seg.kind != segOptionalCatchAll {
				out = append(out, val)
			}
		}
		if bad {
			continue
		}
		outPath := strings.Join(out, "/")
		if outPath == "" {
			outPath = "."
		}
		if first, dup := seen[outPath]; dup {
			if !sameParams(paramSets[first-1], params) {
				fail(n, params, "writes the same page as entry #"+strconv.Itoa(first)+" "+entryJSON(paramSets[first-1])+" (/"+outPath+")",
					"make the entries produce different paths, or remove one")
			}
			continue
		}
		seen[outPath] = n
		pages = append(pages, staticParamsPage{PagePath: page, Params: params, OutPath: outPath})
	}
	return pages, errs
}

// unsafeParamValue explains why a generateStaticParams value cannot be a path
// segment, or returns "" when it can. A catch-all value is several segments
// joined with "/".
func unsafeParamValue(seg routeSeg, val string) string {
	if val == "" {
		if seg.kind == segOptionalCatchAll {
			return ""
		}
		return "has an empty value for [" + seg.text + "]"
	}
	if strings.ContainsAny(val, "\\\x00") {
		return "has a value for [" + seg.text + "] containing a backslash or NUL"
	}
	segs := []string{val}
	if seg.kind == segCatchAll || seg.kind == segOptionalCatchAll {
		segs = strings.Split(val, "/")
	}
	for _, sg := range segs {
		switch sg {
		case "", ".", "..":
			return "has a value for [" + seg.text + "] (" + strconv.Quote(val) + ") that is not a plain path segment"
		}
		if strings.Contains(sg, "/") {
			return "has a value for [" + seg.text + "] containing \"/\"; only a catch-all route takes several segments"
		}
	}
	return ""
}

func sameParams(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

func entryJSON(params map[string]string) string {
	data, err := json.Marshal(params)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// buildStaticParamsPage builds a single page with its params injected into the
// page component's props (via injectStaticParams).
func (b *Builder) buildStaticParamsPage(spp staticParamsPage) (*PageResult, string, error) {
	bnd := b.newBundler()

	bundle, err := bnd.Bundle(spp.PagePath)
	if err != nil {
		return nil, "", err
	}

	entryModule := findEntryModule(bundle.Modules)
	if entryModule == nil || entryModule.Program == nil {
		return nil, "", fmt.Errorf("no entry module found")
	}

	// Modules that call hooks of an npm React library run on Preact in an
	// island, exactly as in buildPage. The module list is captured before the
	// island modules leave it: their files stay dependencies of the page.
	reactIslands := b.detectReactIslandModules(bundle.Modules, bnd.Resolve)
	var reactIslandImps []irtree.ExternalImport
	if len(reactIslands) > 0 {
		reactIslandImps = reactIslandImports(bundle.Modules, reactIslands, bnd.Resolve)
		if im, ok := reactIslands[entryModule.Path]; ok {
			rewritten, why := reactIslandPage(entryModule, im.Pos)
			if why != "" {
				return nil, "", diag.Diagnostic{
					File: entryModule.Path, Line: im.Pos.Line, Col: im.Pos.Col,
					Message: "`" + im.Name + "` from \"" + im.Pkg + "\" needs React's render model, so this page would run as a Preact island, but it cannot",
					Hint:    why,
				}.WithCode(diag.CodeReactLibNotInIsland)
			}
			for i, m := range bundle.Modules {
				if m == entryModule {
					bundle.Modules[i] = rewritten
				}
			}
			entryModule = rewritten
			reactIslandImps = append(reactIslandImps, irtree.ExternalImport{
				Source: rewritten.Path, Local: reactIslandPageLocal, Default: true, ReactIsland: true, ClientOnly: im.ClientOnly,
			})
		}
	}
	pageDeps := []string{spp.PagePath}
	for _, mod := range bundle.Modules {
		if mod.Path != spp.PagePath {
			pageDeps = append(pageDeps, mod.Path)
		}
	}
	pageDeps = append(pageDeps, bundle.CSSDeps...)
	layoutCut, cutErr := b.prepareLayoutCut(spp.PagePath)
	if cutErr != nil {
		return nil, "", cutErr
	}
	if len(reactIslands) > 0 && !entryIsAsync(entryModule.Program) && layoutCut == nil {
		bundle.Modules = krateSideModules(bundle.Modules, entryModule, reactIslands, bnd.Resolve)
	}

	// Run AfterParse hooks so statically generated dynamic-param pages get the
	// same AST-inspection/edit opportunity as regular pages.
	parseCtx := &plugin.ParseHookCtx{
		Page:    spp.PagePath,
		Program: entryModule.Program,
	}
	b.runPageHook("AfterParse", spp.PagePath, parseCtx, func() error { return plugin.RunAfterParse(parseCtx) })
	entryModule.Program = parseCtx.Program

	// The entry program is fresh per generated page, so its getCollection
	// calls are inlined after the route params are bound (below), not here.
	b.deferInline.Store(entryModule.Program, true)
	if err := b.ensureTransformedModules(bundle.Modules); err != nil {
		return nil, "", err
	}
	renderMode, revalidate, _ := resolvePageRenderMode(spp.PagePath, entryModule, bundle.Modules)

	// Global streaming override: if configured, all *static* pages stream.
	// Explicit ssr/isr opts win - forcing them to streaming would silently
	// defeat a page author's per-page config.
	if b.Cfg.SSR.Streaming && renderMode == RenderSSG {
		renderMode = RenderStreaming
		fmt.Fprintf(os.Stderr, "  %s*%s %s -> streaming (global override)\n", cCyan, cReset, filepath.Base(spp.PagePath))
	}

	// Bind route params (e.g. slug) to their concrete values, then inline
	// getCollection so `[slug].tsx` pages can look up the matching entry at
	// build time.
	substituteParamBindings(entryModule.Program, spp.Params)
	b.paramBindings.Store(entryModule.Program, paramPatternBindings(entryModule.Program, spp.Params))
	b.InlineContent(entryModule.Program)
	pageLang := detectPageLang(entryModule.Program)

	// New pipeline: Annotate -> Build IR -> Emit
	ann := annotator.Annotate(entryModule.Program, b.Cfg, spp.PagePath, entryModule.SourceCode)
	extraPrograms := moduleSources(bundle, entryModule)
	annotator.MergeModuleFunctions(ann, extraPrograms)
	annotator.MergeImportAliases(ann, extraPrograms, annotator.ModuleSource{Program: entryModule.Program, Path: entryModule.Path, RawSource: entryModule.SourceCode, Resolver: bundle.Resolver})
	annotator.ReclassifyTiers(ann, b.Cfg)
	serverPage, serverErr := b.applyServerComponentMode(spp.PagePath, entryModule, ann, strings.EqualFold(b.Cfg.Output, "static"), layoutCut, &renderMode, &revalidate)
	if serverErr == nil && !serverPage {
		serverErr = topLevelAwaitError(entryModule, extraPrograms)
	}
	if serverErr != nil {
		return nil, "", serverErr
	}
	clientBoundaries, cbErr := b.prepareClientBoundaries(bundle, ann, serverPage)
	if cbErr != nil {
		return nil, "", cbErr
	}
	if serverPage || len(serverComponentNames(ann)) > 0 {
		pkgBoundaries, pbErr := b.serverTreeBoundaries(bundle, reactIslands, bnd.Resolve, serverPage)
		if pbErr != nil {
			return nil, "", pbErr
		}
		clientBoundaries = append(clientBoundaries, pkgBoundaries...)
		if layoutCut != nil {
			clientBoundaries = mergeBoundaries(clientBoundaries, layoutCut.Boundaries)
		}
	}
	var (
		tree       *irtree.ComponentTree
		emitResult *renderer.EmitResult
		goBaked    map[string]string
	)
	externalByLocal, externalImports, localImports := entryExternalImports(entryModule.Program)
	for _, imp := range reactIslandImps {
		if _, dup := externalByLocal[imp.Local]; dup {
			kept := externalImports[:0]
			for _, old := range externalImports {
				if old.Local != imp.Local {
					kept = append(kept, old)
				}
			}
			externalImports = kept
		}
		externalByLocal[imp.Local] = imp
		externalImports = append(externalImports, imp)
	}
	if serverPage {
		// An async page renders per request; its params arrive with the request.
		tree = &irtree.ComponentTree{}
		emitResult = &renderer.EmitResult{}
	} else {
		opts := b.irBuildOptions()
		opts.PageParams = spp.Params
		opts.RoutePath = routePathFor(spp.PagePath, b.Cfg.PagesDir, spp.Params)
		applyModuleBindings(&opts, entryModule.Path, entryModule.Program, extraPrograms)
		opts.ExternalImports = externalByLocal
		opts.LocalComponentImports = localImports
		opts.ActionURLs = bundle.ActionImports
		tree = b.buildTree(entryModule.Program, ann, opts)
		if len(tree.Errors) > 0 {
			return nil, "", renderErrors(spp.PagePath, tree.Errors)
		}
		injectStaticParams(tree, spp.Params)
		emitter := renderer.NewEmitter()
		emitter.IconResolver = b.iconResolver
		emitter.EvalJS = b.jsExprEvaluator()
		emitter.BasePath = b.Cfg.BaseURLPath()
		emitter.CodeTheme = b.Cfg.Markdown.CodeTheme
		emitResult = emitter.Emit(tree)
		renderer.EmitMeta(tree, emitResult)

		if len(emitResult.Errors) > 0 {
			return nil, "", renderErrors(spp.PagePath, emitResult.Errors)
		}

		// Render imported server-tier Go components via the Go host and inline their
		// HTML (mirrors buildPage); keep the call-site->HTML map for SSR baking.
		if len(emitResult.GoRenders) > 0 {
			rendered, gerr := b.renderGoComponents(emitResult.GoRenders)
			if gerr != nil {
				return nil, "", gerr
			}
			emitResult.HTML = substituteGoRenders(emitResult.HTML, rendered)
			goBaked = goBakedMap(emitResult.GoRenders, rendered)
		}
	}

	if anyBoundaryHydrates(clientBoundaries) {
		loader, lerr := b.islandLoaderHTML()
		if lerr != nil {
			return nil, "", lerr
		}
		emitResult.ScriptHTML += loader
	}

	// Compile-time reactive dependency validation. Surfaced as warnings so
	// dead signals / circular effects are caught before hydration ships.
	b.printReactiveDiags(reactive.Build(emitResult.Signatures).Validate())

	requestTime := renderMode == RenderSSR || renderMode == RenderISR
	metaSegs, metaPageFiles := b.metadataSegments(spp.PagePath)
	emitResult.HeadHTML += b.pageMetadataHeadFor(spp.PagePath, entryModule.SourceCode, requestTime, spp.Params, bnd, metaSegs, metaPageFiles)

	// Run AfterRender hooks (pre-layout) so plugins can modify HTML/head/CSS for
	// statically generated dynamic-param pages, matching buildPage.
	renderCtx := &plugin.RenderHookCtx{
		Page:     spp.PagePath,
		HTML:     emitResult.HTML,
		HeadHTML: emitResult.HeadHTML,
		HasJS:    len(emitResult.Signatures) > 0,
		RawCSS:   bundle.CSS,
	}
	b.runPageHook("AfterRender", spp.PagePath, renderCtx, func() error { return plugin.RunAfterRender(renderCtx) })
	emitResult.HTML = renderCtx.HTML
	emitResult.HeadHTML = renderCtx.HeadHTML
	bundle.CSS = renderCtx.RawCSS

	// Run AfterMarkdownParse hooks for markdown/MDX dynamic-param pages.
	if strings.HasSuffix(spp.PagePath, ".md") || strings.HasSuffix(spp.PagePath, ".mdx") {
		mdCtx := &plugin.MarkdownHookCtx{
			Page:  spp.PagePath,
			HTML:  emitResult.HTML,
			Route: spp.OutPath,
		}
		b.runPageHook("AfterMarkdownParse", spp.PagePath, mdCtx, func() error { return plugin.RunAfterMarkdownParse(mdCtx) })
		emitResult.HTML = mdCtx.HTML
	}

	// Dynamic regions + the coarse page-region wrap, mirroring buildPage so
	// generateStaticParams pages PPR-stream consistently with their template.
	regions := enumerateRegions(tree)
	attachRuntimeChildren(regions, emitResult.RuntimeChildren)
	for i := range regions {
		regions[i].Revalidate = regionRevalidate(b.Root, regions[i].SourcePath)
	}
	if renderMode == RenderSSR || renderMode == RenderISR {
		emitResult.HTML = "<!--suspense:page-->" + emitResult.HTML + "<!--/suspense:page-->"
	}

	var deps []string
	layoutCSS, layoutFiles, _, routeChain, layoutErr := b.applyLayoutStack(spp.PagePath, emitResult, layoutCut)
	if layoutErr != nil {
		return nil, "", layoutErr
	}
	bundle.CSS = b.dedupeModuleCSS(spp.PagePath, layoutCSS+bundle.CSS, bundle.CSSModules)
	deps = append(deps, layoutFiles...)
	emitResult.ScriptHTML += routeInfoScript(routeChain, emitResult.Signatures) + pageRouteScript(spp.PagePath, b.Cfg.PagesDir)

	// Record the dynamic-route template's module graph (plus any wrapping
	// layouts) so editing blog/[slug].tsx or a layout rebuilds its expanded
	// pages instead of falling through to a full rebuild.
	deps = append(deps, pageDeps...)

	pageDir := filepath.Join(b.Cfg.OutDir, spp.OutPath)
	if err := os.MkdirAll(pageDir, 0755); err != nil {
		return nil, "", fmt.Errorf("creating page dir: %w", err)
	}

	jsFile := ""
	hydrationJS := ""
	hasJS := false
	var pageProviders *islandProviders
	pageIslands := tree.ExternalComponents
	if layoutExts, layoutImps := b.layoutIslandsFor(spp.PagePath); len(layoutExts) > 0 {
		pageIslands = append(append([]irtree.ExternalComponent(nil), layoutExts...), pageIslands...)
		externalImports = append(append([]irtree.ExternalImport(nil), externalImports...), layoutImps...)
	}
	if len(pageIslands) > 0 {
		pageProviders = b.pageProviders(spp.PagePath)
	}
	// Every generated page owns its island entries: the template and each
	// param entry share a source file, and one owner would keep only the last.
	islandBoot, islandJS, islandSSR, islandStyles, islandErr := b.buildIslands(
		spp.PagePath+"\x00"+spp.OutPath,
		pageIslands,
		externalImports,
		entryLocalImports(entryModule.Program),
		filepath.Dir(entryModule.Path),
		pageProviders,
	)
	if islandErr != nil {
		return nil, "", islandErr
	}
	if islandStyles != nil {
		bundle.CSS += islandStyles.CSS
		for src, url := range islandStyles.Assets {
			bundle.AssetFiles[src] = url
		}
		deps = append(deps, islandStyles.Deps...)
	}
	b.recordDeps(spp.PagePath, deps)
	if len(islandSSR) > 0 {
		emitResult.HTML = injectIslandSSR(emitResult.HTML, islandSSR)
	}
	emitResult.IslandBootJS = islandBoot
	needsHydrate := len(emitResult.Signatures) > 0 || islandJS != "" || islandBoot != "" || strings.Contains(emitResult.HTML, "data-krate-portal")
	dceJS := append(lazyChunkSources(emitResult.LazyChunks), islandJS)
	dceJS = append(dceJS, b.clientChunkSources(clientBoundaries)...)
	if needsHydrate {
		hydrationJS = renderer.GenerateNewHydrationJS(emitResult)
		b.warnShippedCollections(spp.PagePath, entryModule.Program, hydrationJS, 2)
		dceJS = append(dceJS, hydrationJS)
		var lazyErr error
		if hydrationJS, lazyErr = b.writeLazyChunks(hydrationJS, emitResult.LazyChunks, pageDir, spp.OutPath); lazyErr != nil {
			return nil, "", lazyErr
		}
		if islandJS != "" {
			hydrationJS = strings.TrimSpace(hydrationJS + "\n" + islandJS)
		}
		if strings.TrimSpace(hydrationJS) != "" {
			hydrationJS = neutralizeClientEnv(hydrationJS)
			vendor := collectVendorImports(bundle)
			for _, rb := range b.routeStack(spp.PagePath) {
				if lv, ok := layoutVendorImports.Load(rb.Path); ok {
					vendor = append(vendor, lv.([]vendorImport)...)
				}
			}
			var verr error
			if hydrationJS, verr = b.withVendorImports(hydrationJS, vendor, tree.ExternalComponents); verr != nil {
				return nil, "", verr
			}
		}
		if strings.TrimSpace(hydrationJS) != "" {
			hasJS = true
			if b.shouldMinifyJS() {
				hydrationJS = minifyJS(hydrationJS)
			}
			finalJS := strings.TrimSpace(hydrationJS)
			jsHash := hashContent([]byte(finalJS))
			jsFile = "index." + jsHash + ".js"
			finalJS = substituteImportMetaURL(finalJS, spp.OutPath, jsFile, b.Cfg.BaseURLPath())
			jsPath := filepath.Join(pageDir, jsFile)
			if err := writeOutput(jsPath, []byte(finalJS)); err != nil {
				return nil, "", err
			}
			hydrationJS = finalJS
		} else {
			hydrationJS = ""
		}
	}

	relSrc, _ := filepath.Rel(b.Root, spp.PagePath)
	if err := b.writeAssetFiles(bundle.AssetFiles); err != nil {
		return nil, "", fmt.Errorf("writing assets for %s: %w", spp.PagePath, err)
	}
	b.registerWorkers(bundle.WorkerFiles, bundle.WorkerEsm)
	b.registerDynamicChunks(bundle.DynImportFiles)
	pageCSS := b.pageCSSAfterDCE(renderMode, bundle.CSS, bundle.CSSModules, emitResult.HTML, dceJS...)
	result := &PageResult{
		Page:        spp.PagePath,
		OutName:     spp.OutPath,
		HTML:        emitResult.HTML,
		HeadHTML:    emitResult.HeadHTML,
		ScriptHTML:  emitResult.ScriptHTML,
		StyleHTML:   emitResult.StyleHTML,
		HydrationJS: hydrationJS,
		HasJS:       hasJS,
		JSFile:      jsFile,
		HasCSS:      pageCSS != "",
		CSS:         pageCSS,
		UsedCSS:     emitResult.UsedCSS,
		UsedFuncs:   emitResult.UsedFuncs,
		Mode:        renderMode,
		Revalidate:  revalidate,
		Lang:        pageLang,
		SourcePath:  relSrc,
		Program:     entryModule.Program,
		Regions:     regions,
		GoBaked:     goBaked,

		ServerPage:        serverPage,
		ServerExports:     serverExportsOf(regions),
		MetadataSegments:  metaSegs,
		MetadataPageFiles: metaPageFiles,
		ClientBoundaries:  clientBoundaries,
		ComposedLayouts:   layoutCutLayouts(layoutCut),
	}
	if renderMode != RenderSSG {
		result.IslandStubs = newIslandServerStubs(entryModule.Program, tree.ExternalComponents, islandSSR)
	}
	return result, bundle.CSS, nil
}
