package build

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/environ"
	"github.com/kratejs/krate/packages/compiler/internal/execx"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/jseval"
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
	bindings := make(map[string]string, len(params)+1)
	objStr := make([]string, 0, len(params))
	for k, v := range params {
		bindings[k] = v
		bv, _ := json.Marshal(v)
		objStr = append(objStr, fmt.Sprintf("%q:%s", k, bv))
	}
	bindings["params"] = "{" + strings.Join(objStr, ",") + "}"
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
	bindings := make(map[string]string, len(paramNames)+1)
	objStr := make([]string, 0, len(paramNames))
	for _, name := range paramNames {
		sentinel := dynamicParamSentinel(name)
		bindings[name] = sentinel
		bv, _ := json.Marshal(sentinel)
		objStr = append(objStr, fmt.Sprintf("%q:%s", name, bv))
	}
	bindings["params"] = "{" + strings.Join(objStr, ",") + "}"
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
	for _, part := range parts {
		cleaned := strings.TrimSuffix(part, filepath.Ext(part))
		if strings.HasPrefix(cleaned, "[") && strings.HasSuffix(cleaned, "]") {
			paramName := cleaned[1 : len(cleaned)-1]
			params = append(params, paramName)
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
		Env:           b.Env,
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

// parseParamSets decodes the JSON array of param objects, coercing non-string
// values (numbers, etc.) to their JSON string form.
func parseParamSets(raw []byte) ([]map[string]string, error) {
	var paramSets []map[string]string
	if err := json.Unmarshal(raw, &paramSets); err == nil {
		return paramSets, nil
	}
	var rawSets []map[string]interface{}
	if err2 := json.Unmarshal(raw, &rawSets); err2 != nil {
		return nil, fmt.Errorf("parsing generateStaticParams output: %w\nraw: %s", err2, string(raw))
	}
	for _, rawSet := range rawSets {
		ps := make(map[string]string)
		for k, v := range rawSet {
			switch val := v.(type) {
			case string:
				ps[k] = val
			default:
				b, _ := json.Marshal(val)
				ps[k] = string(b)
			}
		}
		paramSets = append(paramSets, ps)
	}
	return paramSets, nil
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
			results[i] = b.resolveStaticParamsPage(dynamic[i])
		}(i)
	}
	wg.Wait()

	var expanded []staticParamsPage
	var errs []string
	for i := range results {
		if results[i].note {
			printf("  %s⚡%s generateStaticParams: %s\n", cCyan, cReset, filepath.Base(dynamic[i]))
		}
		expanded = append(expanded, results[i].expanded...)
		errs = append(errs, results[i].errs...)
	}

	if len(errs) > 0 {
		return expanded, fmt.Errorf("generateStaticParams resolution failed:\n  %s", strings.Join(errs, "\n  "))
	}

	return expanded, nil
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

	for _, params := range paramSets {
		rel, _ := filepath.Rel(b.Cfg.PagesDir, page)
		rel = filepath.ToSlash(rel)
		rel = strings.TrimSuffix(rel, filepath.Ext(rel))

		parts := strings.Split(rel, "/")
		for i, part := range parts {
			if strings.HasPrefix(part, "[") && strings.HasSuffix(part, "]") {
				paramName := part[1 : len(part)-1]
				if val, ok := params[paramName]; ok {
					parts[i] = val
				}
			}
		}

		res.expanded = append(res.expanded, staticParamsPage{
			PagePath: page,
			Params:   params,
			OutPath:  strings.Join(parts, "/"),
		})
	}
	return res
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

	// Run AfterParse hooks so statically generated dynamic-param pages get the
	// same AST-inspection/edit opportunity as regular pages.
	parseCtx := &plugin.ParseHookCtx{
		Page:    spp.PagePath,
		Program: entryModule.Program,
	}
	b.runPageHook("AfterParse", spp.PagePath, parseCtx, func() error { return plugin.RunAfterParse(parseCtx) })
	entryModule.Program = parseCtx.Program

	renderMode, revalidate := detectRenderMode(entryModule.Program)

	// Global streaming override: if configured, all *static* pages stream.
	// Explicit ssr/isr opts win - forcing them to streaming would silently
	// defeat a page author's per-page config.
	if b.Cfg.SSR.Streaming && renderMode == RenderSSG {
		renderMode = RenderStreaming
		fmt.Fprintf(os.Stderr, "  %s⚡%s %s → streaming (global override)\n", cCyan, cReset, filepath.Base(spp.PagePath))
	}

	b.TransformUniversalIcons(entryModule.Program)
	b.TransformUniversalImages(entryModule.Program)
	b.FlattenComponentSpreadAttrs(entryModule.Program)
	// Bind route params (e.g. slug) to their concrete values, then inline
	// getCollection so `[slug].tsx` pages can look up the matching entry at
	// build time.
	substituteParamBindings(entryModule.Program, spp.Params)
	b.InlineContent(entryModule.Program)
	pageLang := detectPageLang(entryModule.Program)

	// New pipeline: Annotate -> Build IR -> Emit
	ann := annotator.Annotate(entryModule.Program, b.Cfg, spp.PagePath, entryModule.SourceCode)
	extraPrograms := moduleSources(bundle.Modules, entryModule)
	annotator.MergeModuleFunctions(ann, extraPrograms)
	annotator.MergeImportAliases(ann, extraPrograms, annotator.ModuleSource{Program: entryModule.Program, Path: entryModule.Path, RawSource: entryModule.SourceCode})
	opts := b.irBuildOptions()
	opts.ModuleConsts, opts.ModuleConstInits = mergeModuleConstants(entryModule.Program, extraPrograms)
	tree := irtree.BuildWithOptions(entryModule.Program, ann, opts)
	if len(tree.Errors) > 0 {
		return nil, "", renderErrors(spp.PagePath, tree.Errors)
	}
	injectStaticParams(tree, spp.Params)
	emitter := renderer.NewEmitter()
	emitter.IconResolver = b.iconResolver
	emitter.EvalJS = b.jsExprEvaluator()
	emitter.CodeTheme = b.Cfg.Markdown.CodeTheme
	emitResult := emitter.Emit(tree)
	renderer.EmitMeta(tree, emitResult)

	if len(emitResult.Errors) > 0 {
		return nil, "", renderErrors(spp.PagePath, emitResult.Errors)
	}

	// Render imported server-tier Go components via the Go host and inline their
	// HTML (mirrors buildPage); keep the call-site->HTML map for SSR baking.
	var goBaked map[string]string
	if len(emitResult.GoRenders) > 0 {
		rendered, gerr := b.renderGoComponents(emitResult.GoRenders)
		if gerr != nil {
			return nil, "", gerr
		}
		emitResult.HTML = substituteGoRenders(emitResult.HTML, rendered)
		goBaked = goBakedMap(emitResult.GoRenders, rendered)
	}

	// Compile-time reactive dependency validation. Surfaced as warnings so
	// dead signals / circular effects are caught before hydration ships.
	b.printReactiveDiags(reactive.Build(emitResult.Signatures).Validate())

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
	for i := range regions {
		regions[i].Revalidate = regionRevalidate(b.Root, regions[i].SourcePath)
	}
	if renderMode == RenderSSR || renderMode == RenderISR {
		emitResult.HTML = "<!--suspense:page-->" + emitResult.HTML + "<!--/suspense:page-->"
	}

	layoutPath := findLayout(spp.PagePath, b.Cfg.PagesDir)
	var deps []string
	if layoutPath != "" {
		layoutCSS, layoutFiles, _ := b.applyLayoutStack(spp.PagePath, emitResult)
		bundle.CSS += layoutCSS
		deps = append(deps, layoutFiles...)
	}

	// Record the dynamic-route template's module graph (plus any wrapping
	// layouts) so editing blog/[slug].tsx or a layout rebuilds its expanded
	// pages instead of falling through to a full rebuild.
	deps = append(deps, spp.PagePath)
	for _, mod := range bundle.Modules {
		if mod.Path != spp.PagePath {
			deps = append(deps, mod.Path)
		}
	}
	b.recordDeps(spp.PagePath, deps)

	pageDir := filepath.Join(b.Cfg.OutDir, spp.OutPath)
	if err := os.MkdirAll(pageDir, 0755); err != nil {
		return nil, "", fmt.Errorf("creating page dir: %w", err)
	}

	jsFile := ""
	hydrationJS := ""
	hasJS := false
	needsHydrate := len(emitResult.Signatures) > 0
	if needsHydrate {
		hydrationJS = renderer.GenerateNewHydrationJS(emitResult)
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
			_ = os.WriteFile(jsPath, []byte(finalJS), 0644)
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
	pageCSS := b.pageCSSAfterDCE(renderMode, bundle.CSS, bundle.CSSModules, emitResult.HTML)
	return &PageResult{
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
	}, bundle.CSS, nil
}
