package build

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/content"
	"github.com/kratejs/krate/packages/compiler/internal/css"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/icons"
	"github.com/kratejs/krate/packages/compiler/internal/imageproc"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
	"github.com/kratejs/krate/packages/compiler/internal/plugin"
	"github.com/kratejs/krate/packages/compiler/internal/reactive"
	"github.com/kratejs/krate/packages/compiler/internal/renderer"
	"github.com/kratejs/krate/packages/compiler/internal/syntaxhighlight"
)

type PageResult struct {
	Page          string
	OutName       string
	HTML          string
	HeadHTML      string
	ScriptHTML    string
	StyleHTML     string
	HydrationJS   string
	JSFile        string
	CSSFile       string
	CSS           string // this page's own imported CSS (before minification/hashing)
	RuntimeJSFile string // shared runtime chunk path (relative to outDir), e.g. "chunks/runtime.abc.js"
	HasJS         bool
	HasCSS        bool
	IsErrorPage   bool
	UsedCSS       map[string]bool
	UsedFuncs     map[string]bool
	LoadingHTML   string // rendered loading.tsx fallback for SPA transitions

	// FinalHTML is the fully assembled document written to disk (head + body,
	// SEO injection, minification). Populated by writeHTMLPages; consumed by
	// compiler-enforced quality checks.
	FinalHTML string

	// SSR/ISR/Streaming metadata
	Mode             RenderMode
	Revalidate       int
	SourcePath       string // relative path to source file
	ServerBundlePath string // path to server bundle (for SSR/ISR pages)

	// DynamicParams is false when a dynamic route must 404 for params other
	// than those returned by generateStaticParams.
	DynamicParams bool // StaticOnly marks a dynamic route template (e.g. blog/[slug].tsx) whose
	// valid params are closed: the build must not emit its `[param]` fallback
	// HTML. The concrete generateStaticParams pages are emitted normally.
	StaticOnly bool
	// IsDynamicTemplate is true for the canonical `[...]` template page result
	// (as opposed to the concrete pages expanded from generateStaticParams).
	IsDynamicTemplate bool

	// Regions lists this page's dynamic regions (Suspense primaries + runtime
	// components) discovered from the IR tree. Populated for non-SSG pages.
	Regions []Region

	// Program is the page's parsed (and plugin-transformed) AST. Consumed by
	// compiler-enforced quality checks; nil for plugin-generated routes.
	Program *ast.Program
}

type Builder struct {
	Root     string
	Cfg      *config.Config
	DevMode  bool
	Verbose  bool
	Env      map[string]string   // resolved .env values (build/serve-time only, never client-facing)
	depGraph map[string][]string // file path → page source paths that depend on it
	pageDeps map[string][]string // page source path → files it depends on
	depMu    sync.Mutex          // protects depGraph/pageDeps

	// SkipQualityChecks suppresses the in-build quality gates so callers that
	// run their own check pass (e.g. `krate check`, which re-reads dist/) don't
	// evaluate and report the same findings twice.
	SkipQualityChecks bool

	workerMu  sync.Mutex
	workers   map[string]string // worker source path → hashed site URL (/workers/…)
	workerEsm map[string]bool   // worker source path → built as ES module

	chunkMu sync.Mutex
	chunks  map[string]string // dynamic-import source path → hashed site URL (/chunks/…)

	// Plugin hook errors are collected here (page builds run in parallel
	// goroutines) so a failing plugin — e.g. a Go plugin whose binary is
	// missing — fails `krate build` with a non-zero exit instead of silently
	// dropping the plugin's contribution and exiting 0.
	pluginErrs []string
	pluginMu   sync.Mutex

	// contentMods maps virtual import specifiers (e.g. "krate/content") to the
	// codegen'd module on disk. Populated by prepareContent before page builds
	// and installed on every bundler instance.
	contentMods map[string]string

	// contentCollections maps a collection name to its entries, used to inline
	// `getCollection("...")` into literal arrays for build-time folding.
	contentCollections map[string][]content.Entry

	// contentModuleDTS holds ambient `krate/content` declarations, appended to
	// the generated krate-env.d.ts bridge.
	contentModuleDTS string

	// stateOnce/stateScript cache the durable SSR state payload injected into
	// every page as `window.__KRATE_STATE__` (from krate.state.json). Signals
	// created with `{ persist }` prefer this server-chosen value on hydration.
	stateOnce   sync.Once
	stateScript string

	// tsxTsconfig is the absolute path to the generated `.krate/tsconfig.json`
	// used by `npx tsx` bootstraps so user source resolves Krate aliases
	// (krate/content) and the project's own path aliases.
	tsxTsconfig string

	// staticOnlyRoutes collects dynamic route patterns (e.g. "/blog/[slug]")
	// whose params are closed, so the manifest and server can 404 unknown params.
	staticOnlyRoutes []string
	staticOnlyMu     sync.Mutex
}

// shouldMinifyJS reports whether generated JavaScript should be minified.
// Development builds emit readable JS (newlines, indentation, component
// comments) so devtools and view-source are usable; production still minifies.
func (b *Builder) shouldMinifyJS() bool {
	return b.Cfg.ShouldMinifyJS() && !b.DevMode
}

// runtimeCommonHead returns the per-page <head> extras: the site runtime config
// (window.__KRATE_CFG__) for the SPA router, and the View Transitions CSS when
// enabled. Emitted on every page (production and dev).
func (b *Builder) runtimeCommonHead() string {
	var sb strings.Builder
	cfg := map[string]interface{}{"basePath": b.Cfg.BaseURLPath()}
	if b.Cfg.ViewTransitions == "off" {
		cfg["viewTransitions"] = false
	} else {
		cfg["viewTransitions"] = "auto"
	}
	if data, err := json.Marshal(cfg); err == nil {
		sb.WriteString("<script>window.__KRATE_CFG__=")
		sb.Write(data)
		sb.WriteString(";</script>\n")
	}
	if b.Cfg.ViewTransitions != "off" {
		sb.WriteString("<style>@view-transition{navigation:auto}")
		sb.WriteString("@media (prefers-reduced-motion: reduce){")
		sb.WriteString("::view-transition-group(*),::view-transition-old(*),::view-transition-new(*){animation:none!important}}")
		sb.WriteString("</style>\n")
	}
	return sb.String()
}

// devBootstrapJSON is the config the dev overlay bundle reads from
// window.__KRATE_DEV__ (endpoints + feature toggles).
func (b *Builder) devBootstrapJSON() string {
	cfg := map[string]interface{}{
		"sse":     "/__krate/hotreload",
		"errors":  "/__krate/client-error",
		"open":    "/__krate/open",
		"overlay": b.Cfg.DevServer.OverlayEnabled(),
		"toolbar": b.Cfg.DevServer.ToolbarEnabled(),
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return ""
	}
	return string(data)
}

func New(root string, cfg *config.Config) *Builder {
	// Set KrateRoot so the bundler can resolve krate/* virtual packages
	bundler.KrateRoot = findKrateRoot(root)
	return &Builder{
		Root:     root,
		Cfg:      cfg,
		depGraph: make(map[string][]string),
		pageDeps: make(map[string][]string),
	}
}

// pluginFailed records a plugin hook error so the overall build fails with a
// non-zero exit code. Returns the message recorded ("" when nil).
func (b *Builder) pluginFailed(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	b.pluginMu.Lock()
	b.pluginErrs = append(b.pluginErrs, msg)
	b.pluginMu.Unlock()
	return msg
}

// drainPluginErrs returns and clears the accumulated plugin hook errors.
func (b *Builder) drainPluginErrs() []string {
	b.pluginMu.Lock()
	defer b.pluginMu.Unlock()
	errs := b.pluginErrs
	b.pluginErrs = nil
	return errs
}

// communityEnv is the build-wide context handed to community plugin hooks so
// the richer `krate` object reflects the project layout and config.
func (b *Builder) communityEnv() plugin.CommunityEnv {
	return plugin.CommunityEnv{
		PagesDir: b.Cfg.PagesDir,
		DevMode:  b.DevMode,
		Config:   b.Cfg,
	}
}

// findKrateRoot walks up from the project root to find the krate compiler's root directory.
func findKrateRoot(projectRoot string) string {
	dir := projectRoot
	for {
		// Check for compiler marker: internal/ast/ast.go relative to dir
		candidate := filepath.Join(dir, "packages", "compiler")
		if _, err := os.Stat(filepath.Join(candidate, "internal", "ast", "ast.go")); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// printReactiveDiags surfaces reactive validation diagnostics. These are
// informational ("reads no signals / runs exactly once, may be intended") and
// are only shown in verbose mode to keep the default build output quiet.
func (b *Builder) printReactiveDiags(diags []reactive.Diagnostic) {
	if !b.Verbose {
		return
	}
	for _, d := range diags {
		fmt.Fprintf(os.Stderr, "  %s⚠ %s%s\n", cYellow, d.Message, cReset)
	}
}

// BuildPages rebuilds only the specified pages (by source path).
// Unlike BuildAll, it does NOT clean the output directory.
func (b *Builder) BuildPages(pages []string) ([]*PageResult, error) {
	if len(pages) == 0 {
		return nil, nil
	}

	var pageErrs []error

	b.Cfg.Markdown.Root = b.Root
	b.resetBuildCaches()

	// A dynamic-route template (e.g. video/[id].tsx) is not the page that gets
	// served: its concrete URLs are the generateStaticParams expansions. Rebuild
	// those too, or a partial rebuild would leave every /video/abc123 variant
	// pointing at stale HTML while the [id] template itself is refreshed.
	concrete, gspErr := b.resolveStaticParamsPages(pages)
	if gspErr != nil {
		fmt.Fprintf(os.Stderr, "  %s✗ Error:%s %v\n", cRed, cReset, gspErr)
		pageErrs = append(pageErrs, gspErr)
	}

	type pageBuildResult struct {
		result *PageResult
		rawCSS string
		err    error
		page   string
	}

	resultsCh := make(chan pageBuildResult, len(pages)+len(concrete))
	var wg sync.WaitGroup
	pool := newWorkerPool(buildWorkerLimit())

	for _, page := range pages {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			pool.acquire()
			defer pool.release()
			fmt.Printf("  %s▶%s %s\n", cCyan, cReset, p)
			result, rawCSS, err := b.buildPage(p)
			resultsCh <- pageBuildResult{result, rawCSS, err, p}
		}(page)
	}

	for _, spp := range concrete {
		wg.Add(1)
		go func(s staticParamsPage) {
			defer wg.Done()
			pool.acquire()
			defer pool.release()
			fmt.Printf("  %s▶%s %s\n", cCyan, cReset, s.OutPath)
			result, rawCSS, err := b.buildStaticParamsPage(s)
			resultsCh <- pageBuildResult{result, rawCSS, err, s.OutPath}
		}(spp)
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	var results []*PageResult

	for res := range resultsCh {
		if res.err != nil {
			fmt.Fprintf(os.Stderr, "\n  %s✗ Error:%s %v\n", cRed, cReset, res.err)
			pageErrs = append(pageErrs, res.err)
			continue
		}
		result := res.result
		results = append(results, result)

		// Run page-level plugins (AfterPage — post-layout)
		pageHTML := result.HTML
		pageHeadHTML := result.HeadHTML
		afterPageCtx := &plugin.PageHookCtx{
			Page:     result.Page,
			OutName:  result.OutName,
			HTML:     pageHTML,
			HeadHTML: pageHeadHTML,
			HasJS:    result.HasJS,
		}
		if err := plugin.RunAfterPage(afterPageCtx); err != nil {
			fmt.Fprintf(os.Stderr, "  %sPlugin error (AfterPage: %s):%s %v\n", cYellow, res.page, cReset, err)
			b.pluginFailed(err)
		}
		if err := plugin.RunCommunityPlugins("AfterPage", b.Cfg.Plugins, b.Root, b.Cfg.OutDir, afterPageCtx, b.communityEnv()); err != nil {
			fmt.Fprintf(os.Stderr, "  %sCommunity plugin error (AfterPage: %s):%s %v\n", cYellow, res.page, cReset, err)
			b.pluginFailed(err)
		}
		// Apply plugin modifications back
		result.HTML = afterPageCtx.HTML
		result.HeadHTML = afterPageCtx.HeadHTML
	}

	if len(results) == 0 {
		if len(pageErrs) > 0 {
			return nil, &multiError{summary: fmt.Sprintf("build failed: no pages built successfully (%d error(s))", len(pageErrs)), errs: pageErrs}
		}
		return nil, fmt.Errorf("no pages built successfully")
	}

	// Write shared runtime chunk (extracted from per-page bundles)
	runtimeJS := writeRuntimeChunk(b.Cfg.OutDir, b.shouldMinifyJS(), b.Root)
	if b.DevMode {
		writeDevChunk(b.Cfg.OutDir, b.Root)
	}

	// Per-page stylesheets: each page links only the CSS its own module graph
	// imported (deduplicated across pages sharing identical CSS).
	b.writePageCSS(results)

	// Optional site-global stylesheet (Tailwind) linked on every page.
	var globalCSS []string
	if twCSS := b.tailwindCSS(); twCSS != "" {
		if f := b.writeGlobalCSS(twCSS); f != "" {
			globalCSS = append(globalCSS, f)
		}
	}

	// In-memory HTML generation + string swap + single disk write per page
	b.writeHTMLPages(results, globalCSS, runtimeJS)

	// Refresh the sidecar artifacts this subset of pages owns: an SSR/streaming
	// page's server bundle, and any runtime components it references. Done here
	// (rather than by rebuilding the whole site) so a dev-mode edit stays a
	// single-page rebuild even on a server-rendered site.
	b.refreshPageServerArtifacts(results, runtimeJS, globalCSS)

	if perrs := b.drainPluginErrs(); len(perrs) > 0 {
		return results, fmt.Errorf("build failed: %d plugin error(s):\n  %s", len(perrs), strings.Join(perrs, "\n  "))
	}

	// Surface page failures even when some pages succeeded, so a dev rebuild
	// with a mix of good and bad pages still reports the errors (and the
	// browser overlay shows them) instead of silently succeeding.
	if len(pageErrs) > 0 {
		return results, &multiError{summary: fmt.Sprintf("build failed: %d page error(s)", len(pageErrs)), errs: pageErrs}
	}

	return results, nil
}

func (b *Builder) BuildAll() error {
	if err := os.RemoveAll(b.Cfg.OutDir); err != nil {
		return fmt.Errorf("cleaning output dir: %w", err)
	}
	if err := os.MkdirAll(b.Cfg.OutDir, 0755); err != nil {
		return fmt.Errorf("creating output dir: %w", err)
	}
	defer b.ClosePlugins()

	b.Cfg.Markdown.Root = b.Root
	b.resetBuildCaches()

	// Generate `.krate/tsconfig.json` so `npx tsx` bootstraps (config load,
	// generateStaticParams) resolve `krate/content` and the project's path
	// aliases. Non-fatal on failure.
	if tsPath, err := b.writeTsxTsconfig(); err != nil {
		fmt.Fprintf(os.Stderr, "  %sWarning: failed to write .krate/tsconfig.json:%s %v\n", cYellow, cReset, err)
	} else {
		b.tsxTsconfig = tsPath
	}

	pages, err := findPages(b.Cfg.PagesDir)
	if err != nil {
		return fmt.Errorf("finding pages: %w", err)
	}
	if len(pages) == 0 {
		pages = []string{b.Cfg.Entry}
	}

	// Run BeforeBuild hooks (docs plugin generates .krategen/ pages here)
	genPages := make([]plugin.GeneratedPage, 0)
	beforeBuildCtx := &plugin.BuildHookCtx{
		Root:           b.Root,
		OutDir:         b.Cfg.OutDir,
		Config:         b.Cfg,
		Pages:          pages,
		GeneratedPages: &genPages,
		DevMode:        b.DevMode,
	}
	if err := plugin.RunBeforeBuild(beforeBuildCtx); err != nil {
		fmt.Fprintf(os.Stderr, "  %sPlugin error (BeforeBuild):%s %v\n", cYellow, cReset, err)
		b.pluginFailed(err)
	}
	if err := plugin.RunCommunityPlugins("BeforeBuild", b.Cfg.Plugins, b.Root, b.Cfg.OutDir, beforeBuildCtx, b.communityEnv()); err != nil {
		fmt.Fprintf(os.Stderr, "  %sCommunity plugin error (BeforeBuild):%s %v\n", cYellow, cReset, err)
		b.pluginFailed(err)
	}

	// Add generated pages (from BeforeBuild hooks like docs plugin)
	for _, gp := range genPages {
		pages = append(pages, gp.Path)
	}

	// Run GenerateRoutes hooks for plugin-generated virtual pages
	routeCtx := &plugin.BuildHookCtx{
		Root:           b.Root,
		OutDir:         b.Cfg.OutDir,
		Config:         b.Cfg,
		Pages:          pages,
		GeneratedPages: &genPages,
		DevMode:        b.DevMode,
	}
	routes, err := plugin.RunGenerateRoutes(routeCtx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %sPlugin error (GenerateRoutes):%s %v\n", cYellow, cReset, err)
		b.pluginFailed(err)
	}
	if err := plugin.RunCommunityPlugins("GenerateRoutes", b.Cfg.Plugins, b.Root, b.Cfg.OutDir, routeCtx, b.communityEnv()); err != nil {
		fmt.Fprintf(os.Stderr, "  %sCommunity plugin error (GenerateRoutes):%s %v\n", cYellow, cReset, err)
		b.pluginFailed(err)
	}

	type pageBuildResult struct {
		result *PageResult
		rawCSS string
		err    error
		page   string
	}

	// Prepare content collections before building pages: this validates
	// frontmatter, emits `.krate/types/content.d.ts`, and codegens the
	// `krate/content` module pages can import. Schema violations are build
	// errors (a content bug); IO problems are warnings.
	cres := b.prepareContent()
	for _, w := range cres.Warnings {
		fmt.Fprintf(os.Stderr, "  %sWarning: content:%s %v\n", cYellow, cReset, w)
	}
	for _, ve := range cres.Validation {
		fmt.Fprintf(os.Stderr, "  %sContent error:%s %v\n", cRed, cReset, ve)
	}

	totalPages := len(pages) + len(routes)
	resultsCh := make(chan pageBuildResult, totalPages)
	var wg sync.WaitGroup
	pool := newWorkerPool(buildWorkerLimit())

	for _, page := range pages {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			pool.acquire()
			defer pool.release()
			fmt.Printf("  %s▶%s %s\n", cCyan, cReset, p)
			result, rawCSS, err := b.buildPage(p)
			resultsCh <- pageBuildResult{result, rawCSS, err, p}
		}(page)
	}

	for _, route := range routes {
		wg.Add(1)
		go func(r plugin.Route) {
			defer wg.Done()
			pool.acquire()
			defer pool.release()
			fmt.Printf("  %s▶%s %s (route)%s\n", cGreen, cReset, r.Path, cReset)
			result, rawCSS, err := b.buildRoute(r)
			resultsCh <- pageBuildResult{result, rawCSS, err, "(route) " + r.Path}
		}(route)
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	var results []*PageResult
	errorCount := 0
	var failureMessages []string
	var failureErrs []error

	for res := range resultsCh {
		if res.err != nil {
			fmt.Fprintf(os.Stderr, "\n  %s✗ Error (%s):%s %v\n", cRed, res.page, cReset, res.err)
			failureMessages = append(failureMessages, fmt.Sprintf("  %s: %v", res.page, res.err))
			failureErrs = append(failureErrs, res.err)
			errorCount++
			continue
		}

		result := res.result
		results = append(results, result)

		// Run page-level plugins (AfterPage — post-layout)
		pageHTML := result.HTML
		pageHeadHTML := result.HeadHTML
		afterPageCtx := &plugin.PageHookCtx{
			Page:     result.Page,
			OutName:  result.OutName,
			HTML:     pageHTML,
			HeadHTML: pageHeadHTML,
			HasJS:    result.HasJS,
		}
		if err := plugin.RunAfterPage(afterPageCtx); err != nil {
			fmt.Fprintf(os.Stderr, "  %sPlugin error (AfterPage: %s):%s %v\n", cYellow, res.page, cReset, err)
			b.pluginFailed(err)
		}
		if err := plugin.RunCommunityPlugins("AfterPage", b.Cfg.Plugins, b.Root, b.Cfg.OutDir, afterPageCtx, b.communityEnv()); err != nil {
			fmt.Fprintf(os.Stderr, "  %sCommunity plugin error (AfterPage: %s):%s %v\n", cYellow, res.page, cReset, err)
			b.pluginFailed(err)
		}
		// Apply plugin modifications back to result
		result.HTML = afterPageCtx.HTML
		result.HeadHTML = afterPageCtx.HeadHTML
	}

	staticParamPages, gspErr := b.resolveStaticParamsPages(pages)
	if gspErr != nil {
		fmt.Fprintf(os.Stderr, "  %s✗ Error:%s %v\n", cRed, cReset, gspErr)
		failureMessages = append(failureMessages, "  "+gspErr.Error())
		failureErrs = append(failureErrs, gspErr)
		errorCount++
	}
	if len(staticParamPages) > 0 {
		fmt.Printf("  %s⚡%s Building %d statically generated pages from generateStaticParams\n", cCyan, cReset, len(staticParamPages))
		for _, spp := range staticParamPages {
			result, _, err := b.buildStaticParamsPage(spp)
			if err != nil {
				msg := fmt.Sprintf("generateStaticParams page %s: %v", spp.OutPath, err)
				fmt.Fprintf(os.Stderr, "  %s✗ Error:%s %v\n", cRed, cReset, msg)
				failureMessages = append(failureMessages, "  "+msg)
				failureErrs = append(failureErrs, fmt.Errorf("generateStaticParams page %s: %w", spp.OutPath, err))
				errorCount++
				continue
			}
			results = append(results, result)
			// Run AfterPage plugins for the generated page
			afterPageCtx := &plugin.PageHookCtx{
				Page:     result.Page,
				OutName:  result.OutName,
				HTML:     result.HTML,
				HeadHTML: result.HeadHTML,
				HasJS:    result.HasJS,
			}
			if err := plugin.RunAfterPage(afterPageCtx); err != nil {
				fmt.Fprintf(os.Stderr, "  %sPlugin error (AfterPage: %s):%s %v\n", cYellow, result.Page, cReset, err)
				b.pluginFailed(err)
			}
			if err := plugin.RunCommunityPlugins("AfterPage", b.Cfg.Plugins, b.Root, b.Cfg.OutDir, afterPageCtx, b.communityEnv()); err != nil {
				fmt.Fprintf(os.Stderr, "  %sCommunity plugin error (AfterPage: %s):%s %v\n", cYellow, result.Page, cReset, err)
				b.pluginFailed(err)
			}
			result.HTML = afterPageCtx.HTML
			result.HeadHTML = afterPageCtx.HeadHTML
		}
	}

	// Static-only dynamic routes: drop their canonical `[param]` fallback
	// template so the output contains only the concrete pages baked from
	// generateStaticParams. On a plain static host, any other param therefore
	// 404s with no server involvement.
	{
		filtered := results[:0]
		var dropped []string
		for _, r := range results {
			if r != nil && r.StaticOnly && r.IsDynamicTemplate {
				pat := routeFromOutName(r.OutName)
				dropped = append(dropped, pat)
				b.staticOnlyMu.Lock()
				b.staticOnlyRoutes = append(b.staticOnlyRoutes, pat)
				b.staticOnlyMu.Unlock()
				continue
			}
			filtered = append(filtered, r)
		}
		results = filtered
		if len(dropped) > 0 {
			fmt.Printf("  %s⚡%s Static-only dynamic routes (no fallback template): %s\n", cCyan, cReset, strings.Join(dropped, ", "))
		}
	}

	// Write shared runtime chunk only if at least one page needs client JS.
	// Fully static sites (no signals/handlers anywhere) ship zero JavaScript.
	anyPageHasJS := false
	for _, r := range results {
		if r.HasJS {
			anyPageHasJS = true
			break
		}
	}
	runtimeJS := ""
	if anyPageHasJS {
		runtimeJS = writeRuntimeChunk(b.Cfg.OutDir, b.shouldMinifyJS(), b.Root)
	}
	if b.DevMode {
		writeDevChunk(b.Cfg.OutDir, b.Root)
	}

	// Per-page stylesheets: each page links only the CSS its own module graph
	// imported (deduplicated across pages sharing identical CSS).
	b.writePageCSS(results)

	// Optional site-global stylesheet (Tailwind) linked on every page.
	var globalCSS []string
	if twCSS := b.tailwindCSS(); twCSS != "" {
		if f := b.writeGlobalCSS(twCSS); f != "" {
			globalCSS = append(globalCSS, f)
		}
	}

	// In-memory HTML generation + string swap + single disk write per page
	b.writeHTMLPages(results, globalCSS, runtimeJS)

	// Compiler-enforced quality gates (opt-in via `checks` in krate.config.ts).
	// Runs against the final HTML so rules see exactly what ships.
	if !b.SkipQualityChecks {
		if err := b.runQualityChecks(results, runtimeJS); err != nil {
			fmt.Fprintf(os.Stderr, "  %s✗ Checks:%s %v\n", cRed, cReset, err)
			failureMessages = append(failureMessages, "  checks: "+err.Error())
			failureErrs = append(failureErrs, err)
			errorCount++
		}
	}

	// Compile and emit any registered web workers to /workers/.
	if err := b.writeWorkerBundles(); err != nil {
		fmt.Fprintf(os.Stderr, "  %sWorker bundle error:%s %v\n", cYellow, cReset, err)
		failureMessages = append(failureMessages, "  workers: "+err.Error())
		failureErrs = append(failureErrs, err)
		errorCount++
	}

	// Compile and emit any registered dynamic-import chunks to /chunks/.
	if err := b.writeDynamicChunkBundles(); err != nil {
		fmt.Fprintf(os.Stderr, "  %sDynamic import chunk error:%s %v\n", cYellow, cReset, err)
		failureMessages = append(failureMessages, "  chunks: "+err.Error())
		failureErrs = append(failureErrs, err)
		errorCount++
	}

	if b.Cfg.PublicDir != "" {
		if info, err := os.Stat(b.Cfg.PublicDir); err == nil && info.IsDir() {
			_ = copyDirToOut(b.Cfg.PublicDir, b.Cfg.OutDir)
		}
	}

	// Copy processed <Image> variants into the output (served at /_krate/images/...)
	if err := b.copyImageCacheToOut(); err != nil {
		fmt.Fprintf(os.Stderr, "  %sImage copy error:%s %v\n", cYellow, cReset, err)
	}

	if err := b.BuildAllAPI(); err != nil {
		fmt.Fprintf(os.Stderr, "  %sAPI Build error:%s %v\n", cRed, cReset, err)
		failureMessages = append(failureMessages, "  API: "+err.Error())
		failureErrs = append(failureErrs, err)
		errorCount++
	}

	b.BuildMiddleware()

	// Write page manifest with SSR/ISR metadata
	manifestCSS := ""
	if len(globalCSS) > 0 {
		manifestCSS = globalCSS[0]
	}
	manifest := BuildManifest(results, manifestCSS, runtimeJS)
	manifest.StaticOnlyRoutes = append([]string(nil), b.staticOnlyRoutes...)

	// Compile server bundles for SSR/ISR/streaming pages
	serverBundles := CompileServerBundles(results, b.Root, b.Cfg.OutDir)
	if len(serverBundles) > 0 {
		fmt.Printf("  %s⚡%s Compiled %d server bundles\n", cCyan, cReset, len(serverBundles))
		// Stage the bundled SSR renderer driver so `krate serve` runs it with
		// plain node instead of npx tsx on the TS source.
		if staged := stageServerRenderer(b.Root, b.Cfg.OutDir); staged != "" {
			fmt.Printf("  %s⚡%s Staged SSR renderer driver\n", cCyan, cReset)
		} else {
			fmt.Fprintf(os.Stderr, "  %sWarning: SSR renderer driver not staged — server-renderer source not found under %s (ensure @krate/runtime is installed)%s\n", cYellow, b.Root, cReset)
		}
	}

	// Compile runtime server components (*.runtime.tsx, // @runtime, runtimeDirs)
	runtimeCompBundles := CompileRuntimeComponents(b.Root, b.Cfg.OutDir, b.Cfg.ServerComponents, b.Cfg.RuntimeComponents, b.Cfg.RuntimeDirs)
	if len(runtimeCompBundles) > 0 {
		fmt.Printf("  %s⚡%s Compiled %d runtime components\n", cCyan, cReset, len(runtimeCompBundles))
	}
	manifest.SetRuntimeComponents(runtimeCompBundles)

	if err := WriteManifest(manifest, b.Cfg.OutDir, serverBundles); err != nil {
		fmt.Fprintf(os.Stderr, "  %sWarning: failed to write manifest:%s %v\n", cYellow, cReset, err)
	}

	// Generate typed route declarations (.krate/types/routes.d.ts + bridge).
	// Warnings only — type generation must never fail a build.
	if err := b.writeRouteTypes(results); err != nil {
		fmt.Fprintf(os.Stderr, "  %sWarning: failed to generate route types:%s %v\n", cYellow, cReset, err)
	}

	// Report SSR/ISR page count
	ssrCount, isrCount, streamingCount := 0, 0, 0
	for _, r := range results {
		switch r.Mode {
		case RenderSSR:
			ssrCount++
		case RenderISR:
			isrCount++
		case RenderStreaming:
			streamingCount++
		}
	}
	if ssrCount+isrCount+streamingCount > 0 {
		fmt.Printf("  %s⚡%s SSR: %d, ISR: %d, Streaming: %d pages\n", cCyan, cReset, ssrCount, isrCount, streamingCount)
	}

	// Run build-level plugins (after all pages are done and aggregate work is complete)
	pageResults := make([]plugin.PageResult, len(results))
	seenCSS := make(map[string]bool)
	var mergedPageCSS strings.Builder
	for i, r := range results {
		pageResults[i] = plugin.PageResult{
			Page:     r.Page,
			OutName:  r.OutName,
			HTML:     r.HTML,
			HeadHTML: r.HeadHTML,
			HasJS:    r.HasJS,
		}
		if r.CSSFile != "" && !seenCSS[r.CSSFile] {
			seenCSS[r.CSSFile] = true
			if data, err := os.ReadFile(filepath.Join(b.Cfg.OutDir, r.CSSFile)); err == nil {
				mergedPageCSS.Write(data)
				mergedPageCSS.WriteString("\n")
			}
		}
	}
	afterBuildCtx := &plugin.BuildResultHookCtx{
		Root:    b.Root,
		OutDir:  b.Cfg.OutDir,
		Config:  b.Cfg,
		Pages:   pageResults,
		CSS:     mergedPageCSS.String(),
		DevMode: b.DevMode,
	}
	if err := plugin.RunAfterBuild(afterBuildCtx); err != nil {
		fmt.Fprintf(os.Stderr, "  %sPlugin error (AfterBuild):%s %v\n", cYellow, cReset, err)
		b.pluginFailed(err)
	}
	if err := plugin.RunCommunityPlugins("AfterBuild", b.Cfg.Plugins, b.Root, b.Cfg.OutDir, afterBuildCtx, b.communityEnv()); err != nil {
		fmt.Fprintf(os.Stderr, "  %sCommunity plugin error (AfterBuild):%s %v\n", cYellow, cReset, err)
		b.pluginFailed(err)
	}

	// Plugin hook errors recorded in parallel page goroutines (AfterParse,
	// AfterRender, AfterMarkdownParse, AfterPage) must fail the build too —
	// otherwise `krate build` exits 0 while silently dropping plugin output.
	perrs := b.drainPluginErrs()
	if len(perrs) > 0 {
		failureMessages = append(failureMessages, perrs...)
		for _, pe := range perrs {
			failureErrs = append(failureErrs, fmt.Errorf("%s", pe))
		}
		errorCount += len(perrs)
	}

	if errorCount > 0 {
		return &multiError{
			summary: fmt.Sprintf("build failed: %d error(s):\n%s", errorCount, strings.Join(failureMessages, "\n")),
			errs:    failureErrs,
		}
	}

	return nil
}

// ClosePlugins shuts down any running Go plugin subprocesses.
func (b *Builder) ClosePlugins() {
	plugin.CloseGoPlugins()
}

// recordDeps records the dependency mapping between a page and the files it depends on.
func (b *Builder) recordDeps(page string, pageDeps []string) {
	b.depMu.Lock()
	defer b.depMu.Unlock()
	b.pageDeps[page] = pageDeps
	for _, dep := range pageDeps {
		b.depGraph[dep] = append(b.depGraph[dep], page)
	}
}

// affectedPages returns the set of page source paths that depend on the given changed files.
func (b *Builder) affectedPages(changedFiles []string) []string {
	b.depMu.Lock()
	defer b.depMu.Unlock()
	seen := make(map[string]bool)
	var pages []string
	for _, f := range changedFiles {
		for _, p := range b.depGraph[f] {
			if !seen[p] {
				seen[p] = true
				pages = append(pages, p)
			}
		}
	}
	return pages
}

// tailwindOptions builds the Tailwind options from config.
func (b *Builder) tailwindOptions() css.TailwindOptions {
	return css.TailwindOptions{
		ScanDirs:         b.Cfg.Tailwind.ScanDirs,
		ContentOverrides: b.Cfg.Tailwind.Content,
		Preflight:        b.Cfg.Tailwind.Preflight,
		Strict:           b.Cfg.Tailwind.Strict,
		DarkMode:         b.Cfg.Tailwind.DarkMode,
		ExecuteConfig:    b.Cfg.Tailwind.ExecuteConfig,
	}
}

// processCSSDirectives handles Tailwind build-time directives in authored CSS:
// `@tailwind` is stripped (Krate generates utilities itself) and `@layer` is
// unwrapped to its inner rules. When Tailwind is enabled, plain `@apply`
// utilities are expanded; unsupported ones are reported with a warning.
func (b *Builder) processCSSDirectives(cssText string) string {
	// `@layer` is left intact: it is valid CSS and unwrapping it would change
	// the authored cascade.
	cssText = css.StripAtTailwind(cssText)
	if !b.Cfg.Tailwind.Enabled || !strings.Contains(cssText, "@apply") {
		return cssText
	}
	opts := b.tailwindOptions()
	gen := css.NewTailwindGenerator()
	gen.MergeConfig(css.LoadTailwindConfigWithOptions(b.Root, opts))
	out, unresolved := css.ExpandApply(cssText, gen.Theme)
	if len(unresolved) > 0 {
		fmt.Fprintf(os.Stderr, "  %sTailwind @apply: could not expand %s%s\n", cYellow, strings.Join(unresolved, ", "), cReset)
	}
	return out
}

// tailwindCSS generates the site-global Tailwind stylesheet honoring the
// tailwind config (scanDirs/content, darkMode, preflight, strict). Returns ""
// when Tailwind is disabled or no classes were found.
func (b *Builder) tailwindCSS() string {
	if !b.Cfg.Tailwind.Enabled {
		return ""
	}
	opts := b.tailwindOptions()
	twCfg := css.LoadTailwindConfigWithOptions(b.Root, opts)
	// v4 CSS-first config: merge `@theme { --… }` variables found in project CSS.
	atTheme := css.ScanAtTheme(b.Root, opts)
	twCSS, err := css.GenerateTailwindWithTheme(b.Root, twCfg, opts, atTheme)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %sTailwind error:%s %v\n", cYellow, cReset, err)
		return ""
	}
	return twCSS
}

func (b *Builder) writeGlobalCSS(mergedCSS string) string {
	cssFile := ""
	if mergedCSS != "" {
		processedCSS := mergedCSS
		// Inline @import directives before minification
		processedCSS = css.InlineImports(processedCSS, b.Root)
		processedCSS = b.processCSSDirectives(processedCSS)
		if b.Cfg.ShouldMinifyCSS() {
			processedCSS = css.Minify(processedCSS)
		}
		if strings.TrimSpace(processedCSS) != "" {
			processedBytes := []byte(processedCSS)
			cssHash := hashContent(processedBytes)
			cssFile = "styles." + cssHash + ".css"
			cssPath := filepath.Join(b.Cfg.OutDir, cssFile)
			_ = os.WriteFile(cssPath, processedBytes, 0644)
		}
	}
	return cssFile
}

// writePageCSS writes one hashed stylesheet per page from the page's own CSS.
// Pages sharing identical CSS (e.g. every docs page under the same theme, or
// every page wrapped by the same layout) collapse onto one shared file, so each
// page only downloads the CSS its own module graph imported. Syntax-highlight
// (chroma) CSS is prepended only when the page actually renders highlighted
// code. Returns the set of stylesheet filenames written.
func (b *Builder) writePageCSS(results []*PageResult) map[string]bool {
	written := make(map[string]bool)
	for _, r := range results {
		if r.CSS == "" && !pageRendersCode(r.HTML) {
			continue
		}
		pageCss := r.CSS
		if b.Cfg.Markdown.CodeHighlight && pageRendersCode(r.HTML) {
			chromaCSS := syntaxhighlight.CSSForTheme(b.Cfg.Markdown.CodeTheme)
			if chromaCSS != "" {
				pageCss = chromaCSS + "\n" + pageCss
			}
		}
		processedCSS := css.InlineImports(pageCss, b.Root)
		processedCSS = b.processCSSDirectives(processedCSS)
		if b.Cfg.ShouldMinifyCSS() {
			processedCSS = css.Minify(processedCSS)
		}
		if strings.TrimSpace(processedCSS) == "" {
			continue
		}
		processedBytes := []byte(processedCSS)
		cssHash := hashContent(processedBytes)
		cssFile := "styles." + cssHash + ".css"
		r.CSSFile = cssFile
		if !written[cssFile] {
			_ = os.WriteFile(filepath.Join(b.Cfg.OutDir, cssFile), processedBytes, 0644)
			written[cssFile] = true
		}
	}
	return written
}

// pageRendersCode reports whether a page's markup contains syntax-highlighted
// code, in which case the chroma stylesheet is needed on that page.
func pageRendersCode(html string) bool {
	return strings.Contains(html, "chroma")
}

// writeHTMLPages builds the layout wrapper, applies placeholders, minifies,
// and saves to disk in a single parallel step (Zero Disk-I/O Amplification Fix)
// cssFiles are extra global stylesheets (e.g. Tailwind) linked on every page in
// addition to each page's own stylesheet (r.CSSFile).
// durableStateScript returns the `<script>window.__KRATE_STATE__=…</script>`
// tag for the project's durable state, read once from `krate.state.json` (or
// `.krate/state.json`). Returns "" when neither exists or the JSON is invalid.
// Signals created with `{ persist }` prefer this server-chosen value over
// browser storage on hydration, so SSR and client agree.
func (b *Builder) durableStateScript() string {
	b.stateOnce.Do(func() {
		for _, name := range []string{"krate.state.json", filepath.Join(".krate", "state.json")} {
			data, err := os.ReadFile(filepath.Join(b.Root, name))
			if err != nil || len(data) == 0 {
				continue
			}
			if !json.Valid(data) {
				continue
			}
			// Escape characters that could terminate the inline <script> or
			// break the JS (json.Valid does not escape them). `\u003c` etc. are
			// valid inside JSON strings and JS source alike.
			state := string(data)
			state = strings.ReplaceAll(state, "<", "\\u003c")
			state = strings.ReplaceAll(state, ">", "\\u003e")
			state = strings.ReplaceAll(state, "&", "\\u0026")
			state = strings.ReplaceAll(state, "\u2028", "\\u2028")
			state = strings.ReplaceAll(state, "\u2029", "\\u2029")
			b.stateScript = "<script>window.__KRATE_STATE__=" + state + "</script>"
			return
		}
	})
	return b.stateScript
}

func (b *Builder) writeHTMLPages(results []*PageResult, cssFiles []string, runtimeJSFile string) {
	var wg sync.WaitGroup
	pool := newWorkerPool(buildWorkerLimit())
	for _, r := range results {
		wg.Add(1)
		go func(r *PageResult) {
			defer wg.Done()
			pool.acquire()
			defer pool.release()

			pageDir := b.Cfg.OutDir
			if r.OutName != "." {
				pageDir = filepath.Join(b.Cfg.OutDir, r.OutName)
			}

			_ = os.MkdirAll(pageDir, 0755)

			// 1. Construct structural HTML wrapper in memory.
			// A page links its own stylesheet (r.CSSFile) first, then any
			// site-global stylesheets (e.g. Tailwind) so utility classes can
			// override page/component styles.
			pageCSS := []string{}
			if r.CSSFile != "" {
				pageCSS = append(pageCSS, r.CSSFile)
			}
			pageCSS = append(pageCSS, cssFiles...)
			devBootstrap := ""
			if b.DevMode {
				devBootstrap = b.devBootstrapJSON()
			}
			runtimeHead := b.runtimeCommonHead()
			var html string
			if r.LoadingHTML != "" {
				html = generateHTMLWithLoading(r.HTML, r.HeadHTML, r.ScriptHTML, r.StyleHTML, r.LoadingHTML, pageCSS, r.JSFile, runtimeJSFile, r.OutName, b.DevMode, devBootstrap, runtimeHead, b.Cfg.BaseURLPath())
			} else {
				html = generateHTML(r.HTML, r.HeadHTML, r.ScriptHTML, r.StyleHTML, pageCSS, r.JSFile, runtimeJSFile, r.OutName, b.DevMode, devBootstrap, runtimeHead, b.Cfg.BaseURLPath())
			}

			// 2. Durable SSR state (window.__KRATE_STATE__): a page-level
			// payload that persisted signals prefer on hydration, so the
			// hydrated value matches what the server rendered. Inserted before
			// the CSP hash pass so its inline script is allow-listed.
			if state := b.durableStateScript(); state != "" {
				html = strings.Replace(html, "</head>", state+"</head>", 1)
			}

			// 2b. SEO meta tag injection (canonical, OG, Twitter Card)
			if b.Cfg.SEO.BaseURL != "" {
				seoTags := generateSEOTags(r.HeadHTML, routeFromOutName(r.OutName), b.Cfg.SEO.BaseURL, b.Cfg.SEO.SiteName, b.Cfg.SEO.Description, b.Cfg.SEO.Image)
				if seoTags != "" {
					html = strings.Replace(html, "</head>", seoTags+"</head>", 1)
				}
			}

			// 2c. CSP meta tag injection. Hash against the FULLY ASSEMBLED
			// document so every inline script/style — including the runtime
			// config (`__KRATE_CFG__`), View Transitions CSS, and durable state
			// — is allow-listed. External (src/href) tags are skipped by the
			// extractor, and `script-src 'self'` covers the hashed bundles.
			if b.Cfg.CSP.Enabled {
				cspMeta := generateCSPMeta(html, html, r.HydrationJS, b.Cfg.CSP.Directive)
				if cspMeta != "" {
					html = strings.Replace(html, "</head>", cspMeta+"</head>", 1)
				}
			}

			// 3. Minify code in memory
			if b.Cfg.ShouldMinifyHTML() {
				html = minifyHTML(html)
			}

			r.FinalHTML = html

			// 4. Single Write to physical media
			// Error pages (404/500) are written directly at the output root as .html
			var htmlPath string
			if r.IsErrorPage {
				base := strings.TrimSuffix(filepath.Base(r.Page), filepath.Ext(r.Page))
				htmlPath = filepath.Join(b.Cfg.OutDir, base+".html")
			} else {
				htmlPath = filepath.Join(pageDir, "index.html")
			}
			_ = os.WriteFile(htmlPath, []byte(html), 0644)
		}(r)
	}
	wg.Wait()
}

// cfgPathAliasPrefixes returns the prefix strings from path aliases for the bundler.
func (b *Builder) cfgPathAliasPrefixes() []string {
	prefixes := make([]string, len(b.Cfg.PathAliases))
	for i, a := range b.Cfg.PathAliases {
		prefixes[i] = a.Prefix
	}
	return prefixes
}

// cfgPathAliasTargets returns the target slices from path aliases for the bundler.
func (b *Builder) cfgPathAliasTargets() [][]string {
	targets := make([][]string, len(b.Cfg.PathAliases))
	for i, a := range b.Cfg.PathAliases {
		targets[i] = a.Targets
	}
	return targets
}

// newBundler creates a bundler configured with this build's path aliases,
// component tiers, and virtual modules (codegen'd `krate/content`).
func (b *Builder) newBundler() *bundler.Bundler {
	bnd := bundler.New(b.Root)
	bnd.SetPathAliases(b.cfgPathAliasPrefixes(), b.cfgPathAliasTargets(), b.Cfg.TSBaseDir)
	bnd.SetServerComponents(b.Cfg.ServerComponents, b.Cfg.RuntimeComponents, b.Cfg.ServerDirs, b.Cfg.RuntimeDirs)
	if b.contentMods != nil {
		bnd.SetVirtualModules(b.contentMods)
	}
	return bnd
}

func (b *Builder) buildPage(page string) (*PageResult, string, error) {
	bnd := b.newBundler()
	bundle, err := bnd.Bundle(page)
	if err != nil {
		return nil, "", fmt.Errorf("bundling page %s: %w", page, err)
	}

	entryModule := findEntryModule(bundle.Modules)
	if entryModule == nil || entryModule.Program == nil {
		return nil, "", fmt.Errorf("page %s has no entry module (no default export found); a page must export a default component", page)
	}

	// Collect dependencies for partial reloads
	deps := []string{page}
	for _, mod := range bundle.Modules {
		if mod.Path != page {
			deps = append(deps, mod.Path)
		}
	}

	// Run AfterParse hooks
	parseCtx := &plugin.ParseHookCtx{
		Page:    page,
		Program: entryModule.Program,
	}
	if err := plugin.RunAfterParse(parseCtx); err != nil {
		fmt.Fprintf(os.Stderr, "  %sAfterParse plugin error (%s):%s %v\n", cYellow, page, cReset, err)
		b.pluginFailed(fmt.Errorf("AfterParse (%s): %v", page, err))
	}
	if err := plugin.RunCommunityPlugins("AfterParse", b.Cfg.Plugins, b.Root, b.Cfg.OutDir, parseCtx, b.communityEnv()); err != nil {
		fmt.Fprintf(os.Stderr, "  %sCommunity plugin error AfterParse (%s):%s %v\n", cYellow, page, cReset, err)
		b.pluginFailed(fmt.Errorf("AfterParse (%s): %v", page, err))
	}
	// Plugins may hand back a replacement AST (JS plugins decode the astjson doc
	// they return; Go-style plugins swap ctx.Program directly). Point the entry
	// module at the final program so all downstream stages render the edited tree.
	entryModule.Program = parseCtx.Program

	// Detect rendering mode (SSR/ISR/Streaming) from AST exports + <Suspense> usage
	renderMode, revalidate := detectRenderMode(entryModule.Program)

	// If the page imports any runtime components, force streaming mode.
	// Runtime components (*.runtime.tsx) must be rendered at request time,
	// so the page must go through the streaming SSR pipeline.
	if renderMode == RenderSSG {
		for _, mod := range bundle.Modules {
			if mod.ComponentClass == bundler.ComponentClassRuntime {
				renderMode = RenderStreaming
				fmt.Fprintf(os.Stderr, "  %s⚡%s %s imports runtime component → streaming\n", cCyan, cReset, filepath.Base(page))
				break
			}
		}
	}

	// Global streaming override: if configured, all *static* pages stream.
	// Explicit ssr/isr opts win — forcing them to streaming would silently
	// defeat a page author's per-page config.
	if b.Cfg.SSR.Streaming && renderMode == RenderSSG {
		renderMode = RenderStreaming
		fmt.Fprintf(os.Stderr, "  %s⚡%s %s → streaming (global override)\n", cCyan, cReset, filepath.Base(page))
	}

	// Static output mode disables request-time rendering entirely: a page that
	// would have been SSR/ISR/streaming is downgraded to SSG. Dynamic routes
	// are additionally closed to the params generateStaticParams returns (see
	// dynamicParams below).
	staticMode := strings.EqualFold(b.Cfg.Output, "static")
	if staticMode && renderMode != RenderSSG {
		fmt.Fprintf(os.Stderr, "  %s⚡%s %s → ssg (static output mode)\n", cCyan, cReset, filepath.Base(page))
		renderMode = RenderSSG
	}

	isDynRoute := isDynamicRoute(page, b.Cfg.PagesDir)
	allowDynamic := dynamicParamsAllowed(entryModule.Program, staticMode)
	// A dynamic route is "static only" when it must not be served for
	// arbitrary params. Only meaningful for dynamic routes.
	staticOnly := isDynRoute && !allowDynamic

	b.TransformUniversalIcons(entryModule.Program)
	b.TransformUniversalImages(entryModule.Program)
	b.FlattenComponentSpreadAttrs(entryModule.Program)
	b.InlineContent(entryModule.Program)

	// ─── New pipeline: Annotate → Build IR → Emit ──────────────────────────
	// Transform <Icon>/<Image> in imported component modules too. These are
	// universal built-in components, so their JSX must be compiled to concrete
	// SVG/picture markup wherever they appear — including inside library
	// components like LinkCard that the page imports. Skipping imported modules
	// leaves <Icon> as an unresolved component slot that renders nothing.
	ann := annotator.Annotate(entryModule.Program, b.Cfg, page, entryModule.SourceCode)
	extraPrograms := moduleSources(bundle.Modules, entryModule)
	for _, mp := range extraPrograms {
		b.TransformUniversalIcons(mp.Program)
		b.TransformUniversalImages(mp.Program)
		b.FlattenComponentSpreadAttrs(mp.Program)
		b.InlineContent(mp.Program)
	}
	annotator.MergeModuleFunctions(ann, extraPrograms)
	annotator.MergeImportAliases(ann, extraPrograms, annotator.ModuleSource{Program: entryModule.Program, Path: entryModule.Path, RawSource: entryModule.SourceCode})
	// Re-classify tiers for any newly discovered components
	annotator.ReclassifyTiers(ann, b.Cfg)
	tree := irtree.BuildWithOptions(entryModule.Program, ann, irtree.BuildOptions{CodeTheme: b.Cfg.Markdown.CodeTheme})
	// CSS primitive declarations that cannot be compiled to CSS are hard errors
	// (there is no fallback to client signals).
	if len(tree.Errors) > 0 {
		return nil, "", renderErrors(page, tree.Errors)
	}
	// CSS stylesheets are generated during IR construction (one shared,
	// instance-agnostic stylesheet per scope). Append them to the page's own CSS
	// so they land in the hashed external stylesheet.
	choiceCSS := tree.CSSSignalsCSS
	// Dynamic-route templates are built once and served for every matching URL,
	// so bind each [param] to a replaceable sentinel the server substitutes per
	// request (see serve.go applyDynamicRouteParams). Mirrors injectStaticParams
	// and only affects static/server roots; generateStaticParams pages are built
	// separately with concrete values.
	if isDynamicRoute(page, b.Cfg.PagesDir) {
		injectDynamicRoutePlaceholders(tree, extractParamNames(page, b.Cfg.PagesDir))
	}
	regions := enumerateRegions(tree)
	for i := range regions {
		regions[i].Revalidate = regionRevalidate(b.Root, regions[i].SourcePath)
	}
	emitter := renderer.NewEmitter()
	emitter.CodeTheme = b.Cfg.Markdown.CodeTheme
	emitter.IconResolver = b.iconResolver
	emitter.EvalJS = b.jsExprEvaluator()
	emitResult := emitter.Emit(tree)
	renderer.EmitMeta(tree, emitResult)

	if len(emitResult.Errors) > 0 {
		return nil, "", renderErrors(page, emitResult.Errors)
	}

	// Compile-time reactive dependency validation. Surfaced as warnings so
	// dead signals / circular effects are caught before hydration ships.
	b.printReactiveDiags(reactive.Build(emitResult.Signatures).Validate())

	// Run AfterRender hooks (pre-layout, plugins can modify HTML/head/CSS)
	renderCtx := &plugin.RenderHookCtx{
		Page:     page,
		HTML:     emitResult.HTML,
		HeadHTML: emitResult.HeadHTML,
		HasJS:    len(emitResult.Signatures) > 0,
		RawCSS:   bundle.CSS,
	}
	if err := plugin.RunAfterRender(renderCtx); err != nil {
		fmt.Fprintf(os.Stderr, "  %sAfterRender plugin error (%s):%s %v\n", cYellow, page, cReset, err)
		b.pluginFailed(fmt.Errorf("AfterRender (%s): %v", page, err))
	}
	if err := plugin.RunCommunityPlugins("AfterRender", b.Cfg.Plugins, b.Root, b.Cfg.OutDir, renderCtx, b.communityEnv()); err != nil {
		fmt.Fprintf(os.Stderr, "  %sCommunity plugin error AfterRender (%s):%s %v\n", cYellow, page, cReset, err)
		b.pluginFailed(fmt.Errorf("AfterRender (%s): %v", page, err))
	}
	// Apply plugin modifications back
	emitResult.HTML = renderCtx.HTML
	emitResult.HeadHTML = renderCtx.HeadHTML
	bundle.CSS = renderCtx.RawCSS

	// Run AfterMarkdownParse hooks for markdown pages (plugins can modify the
	// rendered HTML). Markdown pages flow through the normal bundler/emitter
	// pipeline — the bundler synthesizes an MDX-style TSX bundle (bundler.go
	// .md/.mdx branch) — so the hook runs against the fully emitted markup.
	if strings.HasSuffix(page, ".md") || strings.HasSuffix(page, ".mdx") {
		outName := pageToOutput(page, b.Cfg.PagesDir)
		mdCtx := &plugin.MarkdownHookCtx{
			Page:  page,
			HTML:  emitResult.HTML,
			Route: outName,
		}
		if err := plugin.RunAfterMarkdownParse(mdCtx); err != nil {
			fmt.Fprintf(os.Stderr, "  %sAfterMarkdownParse plugin error (%s):%s %v\n", cYellow, page, cReset, err)
			b.pluginFailed(fmt.Errorf("AfterMarkdownParse (%s): %v", page, err))
		}
		if err := plugin.RunCommunityPlugins("AfterMarkdownParse", b.Cfg.Plugins, b.Root, b.Cfg.OutDir, mdCtx, b.communityEnv()); err != nil {
			fmt.Fprintf(os.Stderr, "  %sCommunity plugin error AfterMarkdownParse (%s):%s %v\n", cYellow, page, cReset, err)
			b.pluginFailed(fmt.Errorf("AfterMarkdownParse (%s): %v", page, err))
		}
		emitResult.HTML = mdCtx.HTML
	}

	// SSR/ISR pages have no component-level regions (any runtime component or
	// <Suspense> forces streaming). Their whole page body is therefore ONE
	// coarse region: the shell is baked with the page body wrapped in a splice
	// marker, and the Go server replaces that body with a request-time render
	// from the sidecar (/__krate/regions). Wrapping before the layout merge
	// places the marker around the page body wherever the layout injects
	// {children}. Reuses the suspense marker kind so a failed region render
	// falls back to the baked (stale/placeholder) body.
	if renderMode == RenderSSR || renderMode == RenderISR {
		emitResult.HTML = "<!--suspense:page-->" + emitResult.HTML + "<!--/suspense:page-->"
	}

	layoutCSS, layoutFiles := b.applyLayoutStack(page, emitResult)
	bundle.CSS += layoutCSS
	// Generated CSS signal rules land in the page's own hashed stylesheet, after
	// layout CSS so author styles can override the defaults.
	if choiceCSS != "" {
		bundle.CSS += choiceCSS
	}
	// ARIA roles that need synthesized state (tabs/listbox/disclosure) inject a
	// tiny inline synchroniser. Every other CSS-signal page stays zero-JS.
	if tree.NeedsCSSARIA {
		emitResult.ScriptHTML += "<script>" + renderer.CSSARIAJS + "</script>"
	}

	// Layouts wrap the page and are not part of its module graph, so record
	// them explicitly: editing _layout.tsx must rebuild every page it wraps.
	deps = append(deps, layoutFiles...)
	// The loading.tsx fallback is also a file the user edits directly.
	if lp := findLoading(page, b.Cfg.PagesDir); lp != "" {
		deps = append(deps, lp)
	}

	b.recordDeps(page, deps)

	outName := pageToOutput(page, b.Cfg.PagesDir)
	pageDir := filepath.Join(b.Cfg.OutDir, outName)
	pageBase := strings.TrimSuffix(filepath.Base(page), filepath.Ext(page))
	relSrc, _ := filepath.Rel(b.Root, page)

	// A static-only dynamic route has no fallback template: skip all page-dir
	// output for the canonical `[param]` result. Concrete pages expanded from
	// generateStaticParams are built separately and write normally.
	if staticOnly {
		b.recordDeps(page, deps)
		return &PageResult{
			Page:              page,
			OutName:           outName,
			HTML:              emitResult.HTML,
			HeadHTML:          emitResult.HeadHTML,
			ScriptHTML:        emitResult.ScriptHTML,
			StyleHTML:         emitResult.StyleHTML,
			HasCSS:            bundle.CSS != "",
			IsErrorPage:       pageBase == "404" || pageBase == "500",
			CSS:               bundle.CSS,
			UsedCSS:           emitResult.UsedCSS,
			UsedFuncs:         emitResult.UsedFuncs,
			Mode:              renderMode,
			Revalidate:        revalidate,
			SourcePath:        relSrc,
			Regions:           regions,
			DynamicParams:     allowDynamic,
			StaticOnly:        true,
			IsDynamicTemplate: true,
			Program:           entryModule.Program,
		}, bundle.CSS, nil
	}

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

			// Write page-specific hydration code only (runtime is in a shared chunk)
			finalJS := strings.TrimSpace(hydrationJS)
			jsHash := hashContent([]byte(finalJS))
			jsFile = "index." + jsHash + ".js"
			finalJS = substituteImportMetaURL(finalJS, outName, jsFile, b.Cfg.BaseURLPath())
			jsPath := filepath.Join(pageDir, jsFile)
			_ = os.WriteFile(jsPath, []byte(finalJS), 0644)

			if b.Cfg.Sourcemap {
				// The hydration bundle is compiler-generated, so the "source" is
				// the page file; embed both the generated code and the page
				// label so devtools can show a coherent (if synthetic) file.
				sm := generateSourcemap(finalJS, outName, hydrationJS)
				_ = os.WriteFile(jsPath+".map", []byte(sm), 0644)
				finalJS = appendSourceMappingURL(finalJS, jsFile+".map")
				_ = os.WriteFile(jsPath, []byte(finalJS), 0644)
			}

			// Keep the CSP hash and any other ingest in sync with the bytes
			// actually written (which may differ from hydrationJS if
			// import.meta.url was substituted or a sourcemap comment appended).
			hydrationJS = finalJS
		} else {
			hydrationJS = ""
		}
	}

	if err := b.writeAssetFiles(bundle.AssetFiles); err != nil {
		return nil, "", fmt.Errorf("writing assets for %s: %w", page, err)
	}
	b.registerWorkers(bundle.WorkerFiles, bundle.WorkerEsm)
	b.registerDynamicChunks(bundle.DynImportFiles)

	printResult(outName, jsFile)

	loadingHTML := b.renderLoadingComponent(page)

	// Return data structures without triggering an intermediate disk write
	return &PageResult{
		Page:        page,
		OutName:     outName,
		HTML:        emitResult.HTML,
		HeadHTML:    emitResult.HeadHTML,
		ScriptHTML:  emitResult.ScriptHTML,
		StyleHTML:   emitResult.StyleHTML,
		HydrationJS: hydrationJS,
		HasJS:       hasJS,
		JSFile:      jsFile,
		HasCSS:      bundle.CSS != "",
		IsErrorPage: pageBase == "404" || pageBase == "500",
		CSS:         bundle.CSS,
		UsedCSS:     emitResult.UsedCSS,
		UsedFuncs:   emitResult.UsedFuncs,
		LoadingHTML: loadingHTML,

		Mode:       renderMode,
		Revalidate: revalidate,
		SourcePath: relSrc,
		Regions:    regions,

		DynamicParams:     allowDynamic,
		StaticOnly:        staticOnly,
		IsDynamicTemplate: isDynRoute,
		Program:           entryModule.Program,
	}, bundle.CSS, nil
}

// buildRoute generates a full HTML page from a plugin-generated virtual route.
func (b *Builder) buildRoute(route plugin.Route) (result *PageResult, rawCSS string, err error) {
	outName := strings.Trim(route.Path, "/")
	if outName == "" {
		outName = "."
	}

	// Use our new shared pipeline
	// This works for plugins because we pass route.Data (the props) directly
	layoutRes, rawCSS, _, err := b.executeLayoutPipeline(route.Layout, route.Content, route.Data)
	if err != nil {
		return nil, "", err
	}

	// Deferred generation structure
	return &PageResult{
		Page:        route.Path,
		OutName:     outName,
		HTML:        layoutRes.HTML,
		HeadHTML:    layoutRes.HeadHTML,
		ScriptHTML:  layoutRes.ScriptHTML,
		StyleHTML:   layoutRes.StyleHTML,
		HydrationJS: "",
		HasJS:       false,
		JSFile:      "",
		HasCSS:      rawCSS != "",
		CSS:         rawCSS,
		UsedCSS:     layoutRes.UsedCSS,
		UsedFuncs:   make(map[string]bool),
	}, rawCSS, nil
}

// layoutEmitCacheEntry is the layout skeleton (pre-children-injection) keyed by
// layout path + the modtimes of every file in the layout's module graph. The
// layout is content-independent of its pages, so the expensive
// bundle/annotate/emit pipeline runs once per layout version instead of once per
// page. deps records the layout's full module graph so that editing a component
// the layout imports (which leaves the layout file itself untouched) is detected
// and forces a rebuild instead of reusing a stale skeleton.
type layoutEmitCacheEntry struct {
	html, headHTML, scriptHTML, styleHTML, css string
	deps                                       []string
	depTimes                                   map[string]time.Time
}

// layoutCacheFresh reports whether every file the layout was built from is
// still unchanged. A missing file, or any modtime change, invalidates the entry.
func layoutCacheFresh(e *layoutEmitCacheEntry) bool {
	if e == nil {
		return false
	}
	for _, dep := range e.deps {
		info, err := os.Stat(dep)
		if err != nil {
			return false
		}
		if !info.ModTime().Equal(e.depTimes[dep]) {
			return false
		}
	}
	return true
}

// resetBuildCaches clears the in-process layout/loading emit caches. They are
// only valid within a single build pass: a long-lived `krate dev` process would
// otherwise keep serving a layout rendered before a component it imports was
// edited, and would keep negative-caching a `_layout.tsx`/`loading.tsx` that was
// created after the first build. Cleared in place (sync.Map.Clear) so it is safe
// even if a build goroutine from a previous pass is still winding down.
func (b *Builder) resetBuildCaches() {
	layoutEmitCache.Clear()
	loadingEmitCache.Clear()
	layoutCache.Clear()
}

var layoutEmitCache sync.Map
var loadingEmitCache sync.Map

// emitCacheLocks maps a layout/loading path to the mutex serializing its
// bundle→emit pipeline. Concurrent buildPage goroutines sharing the same layout
// all miss the cache at once; the mutex makes the first goroutine do the work
// while the rest wait and then read the filled cache (singleflight without a
// new dependency).
var emitCacheLocks sync.Map

func emitCacheLock(path string) *sync.Mutex {
	mu, _ := emitCacheLocks.LoadOrStore(path, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

// executeLayoutPipeline renders (or reuses) a layout and injects page content.
// It returns the emitted result, the layout's CSS, and the full set of files the
// layout was built from (its module graph), so callers can record them as page
// dependencies — editing a component a layout imports must rebuild the pages the
// layout wraps, even though the layout file itself is unchanged.
func (b *Builder) executeLayoutPipeline(layoutPath string, content string, props map[string]string) (*renderer.EmitResult, string, []string, error) {
	var css string

	if layoutPath == "" {
		return &renderer.EmitResult{HTML: content}, "", nil, nil
	}

	// Cache hit: reuse the layout skeleton and inject the page content. The
	// cache is only valid while every file in the layout's module graph is
	// unchanged; a stale entry (e.g. an imported component edited during a
	// long-lived `krate dev` process) is ignored and re-rendered.
	if cached, ok := layoutEmitCache.Load(layoutPath); ok {
		entry := cached.(*layoutEmitCacheEntry)
		if layoutCacheFresh(entry) {
			return &renderer.EmitResult{
				HTML:       strings.Replace(entry.html, "<!--__children__-->", content, -1),
				HeadHTML:   entry.headHTML,
				ScriptHTML: entry.scriptHTML,
				StyleHTML:  entry.styleHTML,
			}, entry.css, entry.deps, nil
		}
	}

	// Serialize concurrent misses for the same layout so the expensive
	// bundle/annotate/emit pipeline runs exactly once per layout version.
	lock := emitCacheLock(layoutPath)
	lock.Lock()
	defer lock.Unlock()

	// Re-check the cache after acquiring the lock: another goroutine may have
	// filled it while we waited.
	if cached, ok := layoutEmitCache.Load(layoutPath); ok {
		entry := cached.(*layoutEmitCacheEntry)
		if layoutCacheFresh(entry) {
			return &renderer.EmitResult{
				HTML:       strings.Replace(entry.html, "<!--__children__-->", content, -1),
				HeadHTML:   entry.headHTML,
				ScriptHTML: entry.scriptHTML,
				StyleHTML:  entry.styleHTML,
			}, entry.css, entry.deps, nil
		}
	}

	bnd := b.newBundler()
	layoutBundle, err := bnd.Bundle(layoutPath)
	if err != nil {
		return nil, "", nil, fmt.Errorf("bundling layout %s: %w", layoutPath, err)
	}

	layoutModule := findEntryModule(layoutBundle.Modules)
	if layoutModule == nil || layoutModule.Program == nil {
		return nil, "", nil, fmt.Errorf("layout module invalid or empty: %s", layoutPath)
	}

	// The layout's full module graph is both its cache-invalidation key and a
	// dependency of every page it wraps.
	layoutDeps := layoutDepPaths(layoutPath, layoutBundle)

	b.TransformUniversalIcons(layoutModule.Program)
	b.TransformUniversalImages(layoutModule.Program)
	b.FlattenComponentSpreadAttrs(layoutModule.Program)

	ann := annotator.Annotate(layoutModule.Program, b.Cfg, layoutPath, layoutModule.SourceCode)
	extraLayoutPrograms := moduleSources(layoutBundle.Modules, layoutModule)
	annotator.MergeModuleFunctions(ann, extraLayoutPrograms)
	annotator.MergeImportAliases(ann, extraLayoutPrograms, annotator.ModuleSource{Program: layoutModule.Program, Path: layoutModule.Path, RawSource: layoutModule.SourceCode})
	annotator.ReclassifyTiers(ann, b.Cfg)
	tree := irtree.BuildWithOptions(layoutModule.Program, ann, irtree.BuildOptions{CodeTheme: b.Cfg.Markdown.CodeTheme})
	if len(tree.Errors) > 0 {
		return nil, "", nil, renderErrors(layoutPath, tree.Errors)
	}
	emitter := renderer.NewEmitter()
	emitter.CodeTheme = b.Cfg.Markdown.CodeTheme
	emitter.IconResolver = b.iconResolver
	emitter.EvalJS = b.jsExprEvaluator()
	emitResult := emitter.Emit(tree)
	renderer.EmitMeta(tree, emitResult)

	if len(emitResult.Errors) > 0 {
		return nil, "", nil, renderErrors(layoutPath, emitResult.Errors)
	}

	// Compile-time reactive dependency validation. Surfaced as warnings so
	// dead signals / circular effects are caught before hydration ships.
	b.printReactiveDiags(reactive.Build(emitResult.Signatures).Validate())

	css = layoutBundle.CSS
	// CSS signal rules for the layout and its components are generated during IR
	// construction and appended to the layout's own module CSS.
	if tree.CSSSignalsCSS != "" {
		css += tree.CSSSignalsCSS
	}
	if tree.NeedsCSSARIA {
		emitResult.ScriptHTML += "<script>" + renderer.CSSARIAJS + "</script>"
	}

	layoutEmitCache.Store(layoutPath, &layoutEmitCacheEntry{
		html:       emitResult.HTML,
		headHTML:   emitResult.HeadHTML,
		scriptHTML: emitResult.ScriptHTML,
		styleHTML:  emitResult.StyleHTML,
		css:        css,
		deps:       layoutDeps,
		depTimes:   modTimesOf(layoutDeps),
	})

	// Inject page content at {children} slot
	return &renderer.EmitResult{
		HTML:       strings.Replace(emitResult.HTML, "<!--__children__-->", content, -1),
		HeadHTML:   emitResult.HeadHTML,
		ScriptHTML: emitResult.ScriptHTML,
		StyleHTML:  emitResult.StyleHTML,
	}, css, layoutDeps, nil
}

// layoutDepPaths returns the absolute paths of every real file in a layout's
// module graph, including the layout entry itself. External/bare module
// specifiers (node builtins, `react`, virtual modules) are skipped: they have no
// filesystem modtime and would otherwise keep the layout cache permanently
// stale.
func layoutDepPaths(layoutPath string, bundle *bundler.Bundle) []string {
	deps := []string{layoutPath}
	seen := map[string]bool{layoutPath: true}
	for _, mod := range bundle.Modules {
		if mod == nil || mod.IsExternal || mod.Path == "" || seen[mod.Path] {
			continue
		}
		if !filepath.IsAbs(mod.Path) {
			continue
		}
		seen[mod.Path] = true
		deps = append(deps, mod.Path)
	}
	return deps
}

// modTimesOf stats each path, returning a path→modtime map used to detect
// changes to a cached layout's dependencies. Paths that cannot be statted are
// omitted so layoutCacheFresh treats them as changed.
func modTimesOf(paths []string) map[string]time.Time {
	out := make(map[string]time.Time, len(paths))
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil {
			out[p] = info.ModTime()
		}
	}
	return out
}

// ─── New IR Tree Pipeline ──────────────────────────────────────────────────

// NewRenderPipeline runs the new annotator + irtree + emitter pipeline.
// It returns an EmitResult containing HTML + hydration metadata.
// This is an alternative to the old renderPage pipeline and can be
// tested side-by-side before full migration.
func (b *Builder) NewRenderPipeline(entryModule *bundler.Module, page string) (*renderer.EmitResult, error) {
	if entryModule == nil || entryModule.Program == nil {
		return nil, fmt.Errorf("entry module invalid or empty")
	}

	ann := annotator.Annotate(entryModule.Program, b.Cfg, page, entryModule.SourceCode)

	tree := irtree.BuildWithOptions(entryModule.Program, ann, irtree.BuildOptions{CodeTheme: b.Cfg.Markdown.CodeTheme})
	if len(tree.Errors) > 0 {
		return nil, renderErrors(page, tree.Errors)
	}
	emitter := renderer.NewEmitter()
	emitter.CodeTheme = b.Cfg.Markdown.CodeTheme
	result := emitter.Emit(tree)

	if len(result.Errors) > 0 {
		return nil, renderErrors(page, result.Errors)
	}

	return result, nil
}

// renderErrors folds renderer diagnostics into a single clear build error while
// preserving the individual diagnostics for structured display.
func renderErrors(page string, errs []error) error {
	msgs := make([]string, 0, len(errs))
	for _, e := range errs {
		msgs = append(msgs, e.Error())
	}
	return &renderError{
		page:    page,
		summary: fmt.Sprintf("render failed (%s): %s", page, strings.Join(msgs, "; ")),
		errs:    errs,
	}
}

func findPages(dir string) ([]string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}

	var pages []string
	exts := map[string]bool{".tsx": true, ".ts": true, ".jsx": true, ".js": true, ".md": true, ".mdx": true}
	_ = fsutil.WalkExt(dir, exts, nil, func(path string, _ os.FileInfo) error {
		base := filepath.Base(path)
		if !strings.HasPrefix(base, "_") {
			pages = append(pages, path)
		}
		return nil
	})
	return pages, nil
}

// renderLoadingComponent finds and renders loading.tsx for a page's directory.
// The loading component is content-independent of the page, so its render is
// cached by path + modtime and reused across pages.
func (b *Builder) renderLoadingComponent(pagePath string) string {
	loadingPath := findLoading(pagePath, b.Cfg.PagesDir)
	if loadingPath == "" {
		return ""
	}

	// Cache hit: reuse the rendered loading fallback only while every file in
	// its module graph is unchanged.
	if cached, ok := loadingEmitCache.Load(loadingPath); ok {
		entry := cached.(*layoutEmitCacheEntry)
		if layoutCacheFresh(entry) {
			return entry.html
		}
	}

	// Serialize concurrent misses for the same loading component, then re-check
	// the cache once the lock is held.
	lock := emitCacheLock(loadingPath)
	lock.Lock()
	defer lock.Unlock()
	if cached, ok := loadingEmitCache.Load(loadingPath); ok {
		entry := cached.(*layoutEmitCacheEntry)
		if layoutCacheFresh(entry) {
			return entry.html
		}
	}

	bnd := b.newBundler()
	bundle, err := bnd.Bundle(loadingPath)
	if err != nil {
		return ""
	}
	entryModule := findEntryModule(bundle.Modules)
	if entryModule == nil || entryModule.Program == nil {
		return ""
	}
	deps := layoutDepPaths(loadingPath, bundle)
	ann := annotator.Annotate(entryModule.Program, b.Cfg, loadingPath, entryModule.SourceCode)
	extraPrograms := moduleSources(bundle.Modules, entryModule)
	annotator.MergeModuleFunctions(ann, extraPrograms)
	annotator.MergeImportAliases(ann, extraPrograms, annotator.ModuleSource{Program: entryModule.Program, Path: entryModule.Path, RawSource: entryModule.SourceCode})
	tree := irtree.BuildWithOptions(entryModule.Program, ann, irtree.BuildOptions{CodeTheme: b.Cfg.Markdown.CodeTheme})
	if len(tree.Errors) > 0 {
		for _, e := range tree.Errors {
			fmt.Fprintf(os.Stderr, "  %s✗ Error (loading %s):%s %v\n", cRed, loadingPath, cReset, e)
		}
		return ""
	}
	emitter := renderer.NewEmitter()
	emitter.CodeTheme = b.Cfg.Markdown.CodeTheme
	emitter.EvalJS = b.jsExprEvaluator()
	emitResult := emitter.Emit(tree)

	loadingEmitCache.Store(loadingPath, &layoutEmitCacheEntry{
		html:     emitResult.HTML,
		deps:     deps,
		depTimes: modTimesOf(deps),
	})
	return emitResult.HTML
}

// findLoading walks up from pagePath to pagesDir looking for loading.tsx/ts/jsx/js.
func findLoading(pagePath, pagesDir string) string {
	return findLayoutFile(pagePath, pagesDir, loadingNames)
}

// findLayout returns the layout .tsx|.ts|.jsx|.js nearest to pagePath, walking
// up from the page's directory toward pagesDir.
func findLayout(pagePath, pagesDir string) string {
	return findLayoutFile(pagePath, pagesDir, layoutNames)
}

// findLayoutStack returns every layout wrapping pagePath, ordered innermost
// first: the layout nearest to the page, then each ancestor layout up to (and
// including) the pagesDir root. A page under blog/ with both blog/_layout and
// _layout at pagesDir gets [blog/_layout, pagesDir/_layout] so the root shell
// (nav/footer) can wrap a nested section layout. Pages outside pagesDir (e.g.
// .krate/gen plugin pages) only collect layouts found in their own tree and
// never fall back to the app root layout.
func findLayoutStack(pagePath, pagesDir string) []string {
	var stack []string
	dir := filepath.Dir(pagePath)
	for {
		for _, name := range layoutNames {
			candidate := filepath.Join(dir, name)
			if cached, ok := layoutCache.Load(candidate); ok {
				if cached.(string) != "" {
					stack = append(stack, cached.(string))
				}
				continue
			}
			if _, err := os.Stat(candidate); err == nil {
				layoutCache.Store(candidate, candidate)
				stack = append(stack, candidate)
			} else {
				layoutCache.Store(candidate, "")
			}
		}
		if dir == pagesDir {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return stack
}

// applyLayoutStack wraps emitResult.HTML in every layout returned by
// findLayoutStack, innermost first. Each layout's metadata (head/script/style)
// is merged after the inner content so an outer layout's own head/style lands
// after (and thus after the nested section's). A layout that fails to build is
// skipped so the page still emits unwrapped. Returns the CSS contributed by the
// applied layouts and the layout paths used, so the caller can record them as
// page dependencies (a layout edit must rebuild every page it wraps).
func (b *Builder) applyLayoutStack(page string, emitResult *renderer.EmitResult) (string, []string) {
	var css string
	var used []string
	for _, layoutPath := range findLayoutStack(page, b.Cfg.PagesDir) {
		layoutRes, layoutCSS, layoutDeps, err := b.executeLayoutPipeline(layoutPath, emitResult.HTML, nil)
		if err != nil {
			continue
		}
		emitResult.HTML = layoutRes.HTML
		emitResult.HeadHTML = emitResult.HeadHTML + layoutRes.HeadHTML
		emitResult.ScriptHTML = emitResult.ScriptHTML + layoutRes.ScriptHTML
		emitResult.StyleHTML = emitResult.StyleHTML + layoutRes.StyleHTML
		css += layoutCSS
		// Record the layout's full module graph (not just the layout file) so
		// editing a component the layout imports rebuilds every wrapped page.
		used = append(used, layoutDeps...)
	}
	return css, used
}

var (
	layoutNames  = []string{"_layout.tsx", "_layout.ts", "_layout.jsx", "_layout.js"}
	loadingNames = []string{"loading.tsx", "loading.ts", "loading.jsx", "loading.js"}
)

// findLayoutFile walks up from pagePath toward pagesDir checking each directory
// for any of the given filenames. Results are cached in layoutCache to avoid
// repeated os.Stat calls.
func findLayoutFile(pagePath, pagesDir string, names []string) string {
	dir := filepath.Dir(pagePath)
	for {
		for _, name := range names {
			candidate := filepath.Join(dir, name)
			if cached, ok := layoutCache.Load(candidate); ok {
				if cached.(string) != "" {
					return cached.(string)
				}
				continue
			}
			if _, err := os.Stat(candidate); err == nil {
				layoutCache.Store(candidate, candidate)
				return candidate
			}
			layoutCache.Store(candidate, "")
		}
		if dir == pagesDir {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// layoutCache caches findLayout results to avoid repeated os.Stat calls.
var layoutCache sync.Map

// BuildMiddleware compiles middleware.ts (if present) to .krate/middleware.js.
func (b *Builder) BuildMiddleware() {
	middlewarePaths := []string{
		filepath.Join(b.Root, "middleware.ts"),
		filepath.Join(b.Root, "src", "middleware.ts"),
		filepath.Join(b.Root, "middleware.js"),
		filepath.Join(b.Root, "src", "middleware.js"),
	}

	var middlewarePath string
	for _, p := range middlewarePaths {
		if _, err := os.Stat(p); err == nil {
			middlewarePath = p
			break
		}
	}
	if middlewarePath == "" {
		return
	}

	fmt.Printf("  %s▶ Middleware%s %s\n", cGreen, cReset, filepath.Base(middlewarePath))

	outDir := filepath.Join(b.Root, ".krate")
	_ = os.MkdirAll(outDir, 0755)
	outPath := filepath.Join(outDir, "middleware.js")

	result := api.Build(api.BuildOptions{
		EntryPoints: []string{middlewarePath},
		Outdir:      outDir,
		Bundle:      true,
		Platform:    api.PlatformNode,
		Format:      api.FormatESModule,
		Write:       false,
		LogLevel:    api.LogLevelError,
	})

	if len(result.Errors) > 0 {
		for _, e := range result.Errors {
			fmt.Fprintf(os.Stderr, "  %sMiddleware error:%s %s\n", cRed, cReset, e.Text)
		}
		return
	}

	if len(result.OutputFiles) > 0 {
		_ = os.WriteFile(outPath, result.OutputFiles[0].Contents, 0644)
	}
}

// extractBuildComponentName extracts the component name from a file path for server component marking.
func extractBuildComponentName(filePath string) string {
	base := filePath
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	if idx := strings.LastIndex(base, "\\"); idx >= 0 {
		base = base[idx+1:]
	}
	for _, ext := range []string{".tsx", ".ts", ".jsx", ".js"} {
		if strings.HasSuffix(base, ext) {
			base = strings.TrimSuffix(base, ext)
			break
		}
	}
	base = strings.TrimSuffix(base, ".server")
	base = strings.TrimSuffix(base, ".runtime")
	return base
}

func findEntryModule(modules []*bundler.Module) *bundler.Module {
	for _, m := range modules {
		if m.IsEntry {
			return m
		}
	}
	return nil
}

// moduleSources converts a bundle's non-entry, non-CSS module programs (plus
// their source paths and raw text) into annotator.ModuleSource values so the
// annotator can classify imported components against their own files.
func moduleSources(modules []*bundler.Module, entry *bundler.Module) []annotator.ModuleSource {
	var out []annotator.ModuleSource
	for _, mod := range modules {
		if mod == nil || mod.Program == nil || mod.IsCSS || mod.IsExternal || mod == entry {
			continue
		}
		out = append(out, annotator.ModuleSource{
			Program:   mod.Program,
			Path:      mod.Path,
			RawSource: mod.SourceCode,
		})
	}
	return out
}

// jsExprEvaluator returns a function that evaluates self-contained JS
// expressions with the embedded QuickJS engine (full ECMAScript built-ins:
// Date, Math, String, Number, Array, Object, ...). It's wired into the SSR
// evaluator so server-component globals like Date.now() are computed by the
// real JS engine at compile time rather than Go approximations.
func (b *Builder) jsExprEvaluator() func(code string) (string, error) {
	return func(code string) (string, error) {
		return jsruntime.EvaluateExpr(code)
	}
}

func pageToOutput(page, pagesDir string) string {
	// Generated pages in .krategen/ use route derived from file path relative to .krategen/
	// Trailing "/index" is stripped so "docs/index" → output at docs/index.html (route /docs/)
	if strings.Contains(page, ".krate/gen") || strings.Contains(page, ".krate\\gen") {
		idx := strings.Index(page, ".krate/gen")
		if idx == -1 {
			idx = strings.Index(page, ".krate\\gen")
		}
		baseDir := filepath.Join(page[:idx], ".krate", "gen")
		rel, err := filepath.Rel(baseDir, page)
		if err != nil {
			return ""
		}
		name := strings.TrimSuffix(rel, filepath.Ext(rel))
		name = filepath.ToSlash(name)
		name = strings.TrimSuffix(name, "/index")
		if name == "index" || name == "home" || name == "" {
			return "."
		}
		return name
	}

	rel, err := filepath.Rel(pagesDir, page)
	if err != nil {
		return ""
	}
	// Normalize to forward slashes so the returned OutName is a portable URL
	// path ("video/[id]" on every OS) — it feeds manifest routes, shell reads,
	// and region requests. Callers that touch the filesystem join it with
	// filepath.Join, which re-applies the OS separator.
	name := filepath.ToSlash(strings.TrimSuffix(rel, filepath.Ext(rel)))
	// A nested index page (blog/index.tsx) is the directory's default page: it
	// maps to the parent route (blog) so /blog/ serves it — not the odd
	// /blog/index URL (which would 404 into a directory listing at /blog/).
	// Matches the .krate/gen behavior just above.
	name = strings.TrimSuffix(name, "/index")
	if name == "index" || name == "home" {
		return "."
	}
	// Error pages output at root level (like index). name is slash-normalized,
	// so use path.Base (slash-aware), not filepath.Base.
	base := path.Base(name)
	if base == "404" || base == "500" {
		return "."
	}
	return name
}

func hashContent(data []byte) string {
	h := fnv.New32a()
	h.Write(data)
	v := h.Sum32()
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	var buf [6]byte
	for i := 5; i >= 0; i-- {
		buf[i] = chars[v%36]
		v /= 36
	}
	return string(buf[:])
}

// registerWorkers collects worker registrations from a page bundle into the
// build-wide worker set. Safe to call concurrently from page goroutines.
func (b *Builder) registerWorkers(files map[string]string, esm map[string]bool) {
	if len(files) == 0 {
		return
	}
	b.workerMu.Lock()
	defer b.workerMu.Unlock()
	if b.workers == nil {
		b.workers = make(map[string]string)
		b.workerEsm = make(map[string]bool)
	}
	for src, url := range files {
		b.workers[src] = url
	}
	for src, isEsm := range esm {
		if isEsm {
			b.workerEsm[src] = true
		}
	}
}

// writeWorkerBundles compiles every registered worker source into the output
// directory at its hashed /workers/… URL. Workers are produced with esbuild so
// their own relative imports are bundled into a single browser-safe file.
func (b *Builder) writeWorkerBundles() error {
	b.workerMu.Lock()
	workers := make(map[string]string, len(b.workers))
	for src, url := range b.workers {
		workers[src] = url
	}
	esm := make(map[string]bool, len(b.workerEsm))
	for src, v := range b.workerEsm {
		if v {
			esm[src] = true
		}
	}
	b.workerMu.Unlock()

	if len(workers) == 0 {
		return nil
	}

	built := make(map[string]string)
	for src, url := range workers {
		rel := strings.TrimPrefix(url, "/")
		if rel == "" || strings.HasPrefix(rel, "..") {
			continue
		}
		outPath := filepath.Join(b.Cfg.OutDir, filepath.FromSlash(rel))
		if _, err := os.Stat(outPath); err == nil {
			built[src] = url
			continue
		}
		parent := filepath.Dir(outPath)
		if err := os.MkdirAll(parent, 0755); err != nil {
			return err
		}

		loader := api.LoaderJS
		ext := strings.ToLower(filepath.Ext(src))
		switch ext {
		case ".ts":
			loader = api.LoaderTS
		case ".tsx":
			loader = api.LoaderTSX
		case ".jsx":
			loader = api.LoaderJSX
		}

		format := api.FormatIIFE
		if esm[src] {
			format = api.FormatESModule
		}

		result := api.Build(api.BuildOptions{
			AbsWorkingDir:    b.Root,
			EntryPoints:      []string{src},
			Bundle:           true,
			Format:           format,
			Platform:         api.PlatformBrowser,
			Target:           api.ES2020,
			Outfile:          outPath,
			Write:            true,
			Loader:           map[string]api.Loader{ext: loader},
			MinifyWhitespace: b.Cfg.ShouldMinifyJS(),
			Sourcemap:        sourceMapMode(b.Cfg.Sourcemap),
			LogLevel:         api.LogLevelSilent,
		})
		if len(result.Errors) > 0 {
			msgs := make([]string, 0, len(result.Errors))
			for _, e := range result.Errors {
				msgs = append(msgs, e.Text)
			}
			return fmt.Errorf("worker bundle (%s): %s", src, strings.Join(msgs, "; "))
		}
		built[src] = url
	}

	// Emit a tiny index of emitted workers for tooling/debugging.
	if len(built) > 0 {
		idx, _ := json.MarshalIndent(built, "", "  ")
		_ = os.WriteFile(filepath.Join(b.Cfg.OutDir, "workers.json"), idx, 0644)
	}
	return nil
}

// sourceMapMode maps Krate's sourcemap config to esbuild's mode. Linked maps
// are written next to the output and referenced by a comment; when disabled,
// none are produced.
func sourceMapMode(enabled bool) api.SourceMap {
	if enabled {
		return api.SourceMapLinked
	}
	return api.SourceMapNone
}

// registerDynamicChunks collects dynamic-import chunk registrations from a page
// bundle into the build-wide chunk set. Safe to call concurrently from page
// goroutines.
func (b *Builder) registerDynamicChunks(files map[string]string) {
	if len(files) == 0 {
		return
	}
	b.chunkMu.Lock()
	defer b.chunkMu.Unlock()
	if b.chunks == nil {
		b.chunks = make(map[string]string)
	}
	for src, url := range files {
		b.chunks[src] = url
	}
}

// writeDynamicChunkBundles compiles every registered dynamic-import chunk into
// the output directory at its hashed /chunks/… URL. Chunks are produced with
// esbuild as ES modules (the browser's `import()` returns their namespace), with
// their own relative imports bundled into a single browser-safe file.
func (b *Builder) writeDynamicChunkBundles() error {
	b.chunkMu.Lock()
	chunks := make(map[string]string, len(b.chunks))
	for src, url := range b.chunks {
		chunks[src] = url
	}
	b.chunkMu.Unlock()

	if len(chunks) == 0 {
		return nil
	}

	for src, url := range chunks {
		rel := strings.TrimPrefix(url, "/")
		if rel == "" || strings.HasPrefix(rel, "..") {
			continue
		}
		outPath := filepath.Join(b.Cfg.OutDir, filepath.FromSlash(rel))
		if _, err := os.Stat(outPath); err == nil {
			continue
		}
		parent := filepath.Dir(outPath)
		if err := os.MkdirAll(parent, 0755); err != nil {
			return err
		}

		loader := api.LoaderJS
		ext := strings.ToLower(filepath.Ext(src))
		switch ext {
		case ".ts":
			loader = api.LoaderTS
		case ".tsx":
			loader = api.LoaderTSX
		case ".jsx":
			loader = api.LoaderJSX
		}

		result := api.Build(api.BuildOptions{
			AbsWorkingDir:    b.Root,
			EntryPoints:      []string{src},
			Bundle:           true,
			Format:           api.FormatESModule,
			Platform:         api.PlatformBrowser,
			Target:           api.ES2020,
			Outfile:          outPath,
			Write:            true,
			Loader:           map[string]api.Loader{ext: loader},
			MinifyWhitespace: b.Cfg.ShouldMinifyJS(),
			Sourcemap:        sourceMapMode(b.Cfg.Sourcemap),
			LogLevel:         api.LogLevelSilent,
		})
		if len(result.Errors) > 0 {
			msgs := make([]string, 0, len(result.Errors))
			for _, e := range result.Errors {
				msgs = append(msgs, e.Text)
			}
			return fmt.Errorf("dynamic import chunk (%s): %s", src, strings.Join(msgs, "; "))
		}
	}
	return nil
}

// substituteImportMetaURL replaces `import.meta.url` in generated hydration JS
// with the URL of the page's own hydration script. Hydration scripts are plain
// classic <script> files where `import.meta` would be a SyntaxError, but
// `new URL('../x', import.meta.url)` is a common way to reference assets
// relative to the current module — substituting the script's real served URL
// keeps those resolutions working. jsFile is the hashed script filename.
func substituteImportMetaURL(hydrationJS, outName, jsFile, basePath string) string {
	if !strings.Contains(hydrationJS, "import.meta.url") {
		return hydrationJS
	}
	return strings.ReplaceAll(hydrationJS, "import.meta.url", strconv.Quote(pageScriptSrc(outName, jsFile, basePath)))
}

// writeAssetFiles copies every registered asset into the output directory at
// its hashed /assets/… URL path. Safe to call once per page build: the copy is
// content-addressed so repeated writes are idempotent.
func (b *Builder) writeAssetFiles(assets map[string]string) error {
	for src, url := range assets {
		rel := strings.TrimPrefix(url, "/")
		if rel == "" || strings.HasPrefix(rel, "..") {
			continue
		}
		dst := filepath.Join(b.Cfg.OutDir, filepath.FromSlash(rel))
		parent := filepath.Dir(dst)
		if err := os.MkdirAll(parent, 0755); err != nil {
			return err
		}
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0644); err != nil {
			return err
		}
	}
	return nil
}

func printResult(outName, jsFile string) {
	if outName == "." {
		fmt.Printf("    %s✓%s %s/index.html %s(root)%s\n", cGreen, cReset, outName, cGray, cReset)
	} else {
		fmt.Printf("    %s✓%s %s/index.html\n", cGreen, cReset, outName)
	}
	if jsFile != "" {
		fmt.Printf("    %s%s (hydration)%s\n", cGray, jsFile, cReset)
	}
}

func (b *Builder) TransformUniversalIcons(prog *ast.Program) {
	for _, stmt := range prog.Body {
		b.walkAndTransformIcons(stmt)
	}
}

func (b *Builder) walkAndTransformIcons(node ast.Node) {
	if node == nil {
		return
	}

	switch n := node.(type) {
	case *ast.ExportStmt:
		// `export default function Foo()` wraps the FnDecl in an ExportStmt.
		if n.Declaration != nil {
			b.walkAndTransformIcons(n.Declaration)
		}
	case *ast.FnDecl:
		for _, stmt := range n.Body {
			b.walkAndTransformIcons(stmt)
		}
	case *ast.ReturnStmt:
		n.Value = b.transformIconExpr(n.Value)
	case *ast.ExprStmt:
		n.Expression = b.transformIconExpr(n.Expression)
	case *ast.ArrowFn:
		for _, stmt := range n.Body {
			b.walkAndTransformIcons(stmt)
		}
	case *ast.IfStmt:
		b.walkAndTransformIcons(n.Test)
		for _, s := range n.Consequent {
			b.walkAndTransformIcons(s)
		}
		for _, s := range n.Alternate {
			b.walkAndTransformIcons(s)
		}
	case *ast.BlockStmt:
		for _, s := range n.Body {
			b.walkAndTransformIcons(s)
		}
	case *ast.VarStmt:
		for _, d := range n.Decls {
			if d.Init != nil {
				d.Init = b.transformIconExpr(d.Init)
			}
		}
	}
}

func (b *Builder) transformIconExpr(expr ast.Expr) ast.Expr {
	if expr == nil {
		return nil
	}

	switch e := expr.(type) {
	case *ast.JSXElement:
		if e.Opening.Name == "Icon" && iconNameIsLiteral(e) {
			return b.compileIconToSVG(e)
		}

		for i, child := range e.Children {
			switch c := child.(type) {
			case *ast.JSXElementChild:
				c.Element = b.transformIconExpr(c.Element).(*ast.JSXElement)
				e.Children[i] = c
			case *ast.JSXExprContainer:
				if c.Expression != nil {
					c.Expression = b.transformIconExpr(c.Expression)
				}
			}
		}
	case *ast.ConditionalExpr:
		e.Test = b.transformIconExpr(e.Test)
		e.Consequent = b.transformIconExpr(e.Consequent)
		e.Alternate = b.transformIconExpr(e.Alternate)
	case *ast.JSXFragment:
		for i, child := range e.Children {
			switch c := child.(type) {
			case *ast.JSXElementChild:
				c.Element = b.transformIconExpr(c.Element).(*ast.JSXElement)
				e.Children[i] = c
			case *ast.JSXExprContainer:
				if c.Expression != nil {
					c.Expression = b.transformIconExpr(c.Expression)
				}
			}
		}
	case *ast.BinaryExpr:
		// `{cond && <Icon .../>}` (and other operators) — recurse both sides so
		// icons inside short-circuit guards still compile to SVG.
		e.Left = b.transformIconExpr(e.Left)
		e.Right = b.transformIconExpr(e.Right)
	case *ast.CallExpr:
		// `{items.map((x) => <Icon .../>)}` and similar call-argument JSX.
		e.Callee = b.transformIconExpr(e.Callee)
		for i := range e.Args {
			e.Args[i] = b.transformIconExpr(e.Args[i])
		}
	case *ast.ArrowFn:
		for _, stmt := range e.Body {
			b.walkAndTransformIcons(stmt)
		}
	}
	return expr
}

// iconNameIsLiteral reports whether an <Icon> element's `name` attribute is a
// static string literal — the only form transformIconExpr can resolve to an
// SVG at build time. Icons with a dynamic name (e.g. name={icon} or a ternary)
// are deliberately left untouched so the SSREval/runtime path can resolve them
// against the real prop values instead of falling back to an empty name error.
func iconNameIsLiteral(el *ast.JSXElement) bool {
	for _, attr := range el.Opening.Attributes {
		if attr.Spread || attr.Name != "name" {
			continue
		}
		_, ok := attr.Value.(*ast.Literal)
		return ok
	}
	return false
}

func (b *Builder) compileIconToSVG(orig *ast.JSXElement) *ast.JSXElement {
	var iconName string
	var forwardedAttrs []*ast.JSXAttr

	// Separate the 'name' attribute from styling properties (className, style, etc.)
	for _, attr := range orig.Opening.Attributes {
		if attr.Name == "name" {
			if lit, ok := attr.Value.(*ast.Literal); ok {
				iconName = lit.Value
			}
		} else {
			forwardedAttrs = append(forwardedAttrs, attr)
		}
	}

	// Fetch or read from local disk cache / the project's icons/ directory.
	icon, err := icons.ResolveIcon(b.Root, iconName)
	if err != nil {
		// Return a graceful error fallback inside the HTML rather than crashing
		// the compiler. The name is escaped because it may be attacker-influenced
		// and the value is written into the page as-is.
		return &ast.JSXElement{
			Opening:  &ast.JSXOpening{Name: "span", Attributes: forwardedAttrs, SelfClosing: false},
			Children: []ast.JSXChild{&ast.JSXText{Value: escape.HTMLAttr(fmt.Sprintf("[%v]", err))}},
			Closing:  &ast.JSXClosing{Name: "span"},
		}
	}

	// Base structural SVG properties. User-provided attributes take precedence
	// over the defaults, so any default whose name appears in the forwarded
	// attributes is dropped (duplicate attributes in HTML resolve to the first,
	// which would silently ignore the user's override).
	viewBox := icon.ViewBox
	if viewBox == "" {
		viewBox = "0 0 24 24"
	}
	baseSvgAttrs := []*ast.JSXAttr{
		{Name: "xmlns", Value: &ast.Literal{Kind: ast.StringLit, Value: "http://www.w3.org/2000/svg"}},
		{Name: "viewBox", Value: &ast.Literal{Kind: ast.StringLit, Value: viewBox}},
		{Name: "fill", Value: &ast.Literal{Kind: ast.StringLit, Value: "none"}},
		{Name: "stroke", Value: &ast.Literal{Kind: ast.StringLit, Value: "currentColor"}},
		{Name: "stroke-width", Value: &ast.Literal{Kind: ast.StringLit, Value: "2"}},
	}

	// Combine default engine SVG properties with user-defined attributes,
	// letting the user override any default.
	allAttrs := append(baseSvgAttrs, forwardedAttrs...)

	return &ast.JSXElement{
		Position: orig.Position,
		Opening: &ast.JSXOpening{
			Name:        "svg",
			Attributes:  dedupeAttrs(allAttrs),
			SelfClosing: false,
		},
		Children: []ast.JSXChild{&ast.JSXText{Value: icon.Inner}},
		Closing: &ast.JSXClosing{
			Name: "svg",
		},
	}
}

// dedupeAttrs keeps the LAST occurrence of each attribute name, so user-supplied
// attributes override the engine's defaults. Duplicate attributes in HTML are
// resolved to the FIRST occurrence by browsers, so dropping earlier defaults is
// the only reliable way to let callers override them.
func dedupeAttrs(attrs []*ast.JSXAttr) []*ast.JSXAttr {
	var out []*ast.JSXAttr
	for i := len(attrs) - 1; i >= 0; i-- {
		name := attrs[i].Name
		dup := false
		for _, keep := range out {
			if keep.Name == name {
				dup = true
				break
			}
		}
		if !dup {
			out = append([]*ast.JSXAttr{attrs[i]}, out...)
		}
	}
	return out
}

// iconResolver compiles a resolved <Icon name="..."> to SVG HTML at emit
// time. It's wired into the emitter so components whose icon `name` is a
// build-time-evaluated expression (e.g. <Icon name={icon}/> where icon is a
// local var from props) still produce SVG markup. Returns handled=false when
// the icon can't be fetched so the caller falls back to default handling.
func (b *Builder) iconResolver(iconName string, attrs []*ast.JSXAttr) (string, bool) {
	var forwardedAttrs []*ast.JSXAttr
	for _, attr := range attrs {
		if attr.Name == "name" {
			continue
		}
		forwardedAttrs = append(forwardedAttrs, attr)
	}

	icon, err := icons.ResolveIcon(b.Root, iconName)
	if err != nil {
		return fmt.Sprintf("<span>%s</span>", escape.HTMLAttr(fmt.Sprintf("[%v]", err))), false
	}

	viewBox := icon.ViewBox
	if viewBox == "" {
		viewBox = "0 0 24 24"
	}

	var b2 strings.Builder
	b2.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="`)
	b2.WriteString(escape.HTMLAttr(viewBox))
	b2.WriteString(`" fill="none" stroke="currentColor" stroke-width="2"`)
	for _, attr := range dedupeAttrs(forwardedAttrs) {
		if attr.Spread {
			continue
		}
		val := ""
		if attr.Value != nil {
			if lit, ok := attr.Value.(*ast.Literal); ok {
				val = lit.Value
			}
		} else {
			val = "true"
		}
		if val == "" || val == "false" || val == "null" || val == "undefined" {
			continue
		}
		b2.WriteByte(' ')
		b2.WriteString(ast.HTMLAttrName(attr.Name))
		b2.WriteString(`="`)
		b2.WriteString(escape.HTMLAttr(val))
		b2.WriteByte('"')
	}
	b2.WriteByte('>')
	b2.WriteString(icon.Inner)
	b2.WriteString("</svg>")
	return b2.String(), true
}

func (b *Builder) TransformUniversalImages(prog *ast.Program) {
	for _, stmt := range prog.Body {
		b.walkAndTransformImages(stmt)
	}
}

func (b *Builder) walkAndTransformImages(node ast.Node) {
	if node == nil {
		return
	}
	switch n := node.(type) {
	case *ast.FnDecl:
		for _, stmt := range n.Body {
			b.walkAndTransformImages(stmt)
		}
	case *ast.ExportStmt:
		if n.Declaration != nil {
			b.walkAndTransformImages(n.Declaration)
		}
	case *ast.ReturnStmt:
		n.Value = b.transformImageExpr(n.Value)
	case *ast.ExprStmt:
		n.Expression = b.transformImageExpr(n.Expression)
	case *ast.IfStmt:
		for _, s := range n.Consequent {
			b.walkAndTransformImages(s)
		}
		for _, s := range n.Alternate {
			b.walkAndTransformImages(s)
		}
	case *ast.ForStmt:
		for _, stmt := range n.Body {
			b.walkAndTransformImages(stmt)
		}
	case *ast.WhileStmt:
		for _, stmt := range n.Body {
			b.walkAndTransformImages(stmt)
		}
	case *ast.VarStmt:
		for _, decl := range n.Decls {
			if decl.Init != nil {
				decl.Init = b.transformImageExpr(decl.Init)
			}
		}
	}
}

func (b *Builder) transformImageExpr(expr ast.Expr) ast.Expr {
	if expr == nil {
		return nil
	}

	switch e := expr.(type) {
	case *ast.JSXElement:
		if e.Opening.Name == "Image" {
			return b.compileImageToPicture(e)
		}
		b.transformJSXChildren(e.Children)
	case *ast.JSXFragment:
		b.transformJSXChildren(e.Children)
	case *ast.CallExpr:
		for i, arg := range e.Args {
			e.Args[i] = b.transformImageExpr(arg)
		}
	}
	return expr
}

// transformJSXChildren rewrites any <Image> descendants within a JSX child list.
func (b *Builder) transformJSXChildren(children []ast.JSXChild) {
	for i, child := range children {
		if elChild, ok := child.(*ast.JSXElementChild); ok {
			transformed := b.transformImageExpr(elChild.Element)
			if jsxEl, ok := transformed.(*ast.JSXElement); ok {
				elChild.Element = jsxEl
				children[i] = elChild
			}
		}
	}
}

func (b *Builder) compileImageToPicture(orig *ast.JSXElement) *ast.JSXElement {
	var src string
	var reqW, reqH int
	var alt, loading, sizes, placeholder string
	var priority bool
	var quality int
	var className string
	var extraAttrs []*ast.JSXAttr

	for _, attr := range orig.Opening.Attributes {
		if attr.Spread {
			extraAttrs = append(extraAttrs, attr)
			continue
		}
		if attr.Value == nil {
			// Bare boolean attribute (e.g. <Image priority />)
			if attr.Name == "priority" {
				priority = true
			} else {
				extraAttrs = append(extraAttrs, attr)
			}
			continue
		}
		val, _ := evalAttrString(attr.Value)
		switch attr.Name {
		case "src":
			src = val
		case "width":
			reqW, _ = strconv.Atoi(val)
		case "height":
			reqH, _ = strconv.Atoi(val)
		case "alt":
			alt = val
		case "loading":
			loading = val
		case "priority":
			priority = val == "true"
		case "quality":
			quality, _ = strconv.Atoi(val)
		case "sizes":
			sizes = val
		case "placeholder":
			placeholder = val
		case "className":
			className = val
		default:
			extraAttrs = append(extraAttrs, attr)
		}
	}

	if src == "" {
		return imageErrorSpan("[Image: missing src]")
	}

	publicDir := b.Cfg.PublicDir
	if publicDir == "" {
		publicDir = filepath.Join(b.Root, "public")
	}

	// `src` is a URL, not a filesystem path. A leading "/" is the site root on
	// every platform — do NOT let filepath.IsAbs treat it as an OS-absolute
	// path (it does on Linux, which made `/hero.png` resolve to the drive root).
	// Only a genuinely absolute filesystem path (e.g. `C:\assets\hero.png`) is
	// used verbatim.
	resolvedSrc := src
	if strings.HasPrefix(src, "/") {
		resolvedSrc = filepath.Join(publicDir, strings.TrimPrefix(src, "/"))
	} else if !filepath.IsAbs(src) {
		resolvedSrc = filepath.Join(publicDir, src)
	}

	result, err := imageproc.ProcessImage(b.Root, resolvedSrc, reqW, reqH, quality, placeholder != "empty")
	if err != nil {
		return imageErrorSpan(fmt.Sprintf("[Image: %v]", err))
	}
	result.Src = "/" + strings.TrimPrefix(src, "/")

	if loading == "" {
		loading = "lazy"
	}
	if priority {
		loading = "eager"
	}
	if sizes == "" {
		sizes = "(max-width: 768px) 100vw, 50vw"
	}

	// Vector sources (e.g. SVG) are passed through as-is: no decoding, no
	// responsive variants, no compression. Emit a plain <img> pointing at the
	// original file.
	if len(result.WebP) == 0 && len(result.Fallback) == 0 {
		imgAttrs := []*ast.JSXAttr{
			{Name: "src", Value: &ast.Literal{Kind: ast.StringLit, Value: result.Src}},
			{Name: "alt", Value: &ast.Literal{Kind: ast.StringLit, Value: alt}},
			{Name: "loading", Value: &ast.Literal{Kind: ast.StringLit, Value: loading}},
			{Name: "decoding", Value: &ast.Literal{Kind: ast.StringLit, Value: "async"}},
		}
		if result.Width > 0 {
			imgAttrs = append(imgAttrs, &ast.JSXAttr{Name: "width", Value: &ast.Literal{Kind: ast.NumberLit, Value: strconv.Itoa(result.Width)}})
		}
		if result.Height > 0 {
			imgAttrs = append(imgAttrs, &ast.JSXAttr{Name: "height", Value: &ast.Literal{Kind: ast.NumberLit, Value: strconv.Itoa(result.Height)}})
		}
		if className != "" {
			imgAttrs = append(imgAttrs, &ast.JSXAttr{Name: "className", Value: &ast.Literal{Kind: ast.StringLit, Value: className}})
		}
		if priority {
			imgAttrs = append(imgAttrs, &ast.JSXAttr{Name: "fetchpriority", Value: &ast.Literal{Kind: ast.StringLit, Value: "high"}})
		}
		imgAttrs = append(imgAttrs, extraAttrs...)
		return &ast.JSXElement{
			Position: orig.Position,
			Opening:  &ast.JSXOpening{Name: "img", Attributes: imgAttrs, SelfClosing: true},
			Children: []ast.JSXChild{
				&ast.JSXText{Value: fmt.Sprintf("Your browser does not support the image element. Original: %s", src)},
			},
		}
	}

	// CLS mitigation: reserve the intrinsic aspect ratio so the browser can
	// size the image before it loads, plus a blurred LQIP background. Vectors
	// (e.g. SVG) are passed through without decoding, so they may not know
	// their intrinsic dimensions — don't force an aspect-ratio in that case.
	style := ""
	if result.Width > 0 && result.Height > 0 {
		style = fmt.Sprintf("width:100%%;height:auto;aspect-ratio:%d/%d", result.Width, result.Height)
		if result.Placeholder != "" {
			style += ";background-image:url(" + result.Placeholder + ");background-size:cover;background-position:center"
		}
	}

	pictureAttrs := []*ast.JSXAttr{}
	if className != "" {
		pictureAttrs = append(pictureAttrs, &ast.JSXAttr{
			Name:  "className",
			Value: &ast.Literal{Kind: ast.StringLit, Value: className},
		})
	}

	webpSrcset := srcsetString(result.WebP)
	fallbackSrcset := srcsetString(result.Fallback)

	webpSource := &ast.JSXElement{
		Opening: &ast.JSXOpening{
			Name: "source",
			Attributes: []*ast.JSXAttr{
				{Name: "type", Value: &ast.Literal{Kind: ast.StringLit, Value: "image/webp"}},
				{Name: "srcSet", Value: &ast.Literal{Kind: ast.StringLit, Value: webpSrcset}},
				{Name: "sizes", Value: &ast.Literal{Kind: ast.StringLit, Value: sizes}},
			},
			SelfClosing: true,
		},
	}
	fallbackSource := &ast.JSXElement{
		Opening: &ast.JSXOpening{
			Name: "source",
			Attributes: []*ast.JSXAttr{
				{Name: "type", Value: &ast.Literal{Kind: ast.StringLit, Value: result.FallbackMime}},
				{Name: "srcSet", Value: &ast.Literal{Kind: ast.StringLit, Value: fallbackSrcset}},
				{Name: "sizes", Value: &ast.Literal{Kind: ast.StringLit, Value: sizes}},
			},
			SelfClosing: true,
		},
	}

	imgAttrs := []*ast.JSXAttr{
		{Name: "src", Value: &ast.Literal{Kind: ast.StringLit, Value: result.Src}},
		{Name: "alt", Value: &ast.Literal{Kind: ast.StringLit, Value: alt}},
		{Name: "loading", Value: &ast.Literal{Kind: ast.StringLit, Value: loading}},
		{Name: "decoding", Value: &ast.Literal{Kind: ast.StringLit, Value: "async"}},
	}
	if result.Width > 0 {
		imgAttrs = append(imgAttrs, &ast.JSXAttr{Name: "width", Value: &ast.Literal{Kind: ast.NumberLit, Value: strconv.Itoa(result.Width)}})
	}
	if result.Height > 0 {
		imgAttrs = append(imgAttrs, &ast.JSXAttr{Name: "height", Value: &ast.Literal{Kind: ast.NumberLit, Value: strconv.Itoa(result.Height)}})
	}
	if style != "" {
		imgAttrs = append(imgAttrs, &ast.JSXAttr{Name: "style", Value: &ast.Literal{Kind: ast.StringLit, Value: style}})
	}
	if priority {
		imgAttrs = append(imgAttrs, &ast.JSXAttr{Name: "fetchpriority", Value: &ast.Literal{Kind: ast.StringLit, Value: "high"}})
	}
	if className != "" {
		imgAttrs = append(imgAttrs, &ast.JSXAttr{
			Name:  "className",
			Value: &ast.Literal{Kind: ast.StringLit, Value: className},
		})
	}
	if fallbackSrcset != "" {
		imgAttrs = append(imgAttrs, &ast.JSXAttr{
			Name:  "srcSet",
			Value: &ast.Literal{Kind: ast.StringLit, Value: fallbackSrcset},
		})
		imgAttrs = append(imgAttrs, &ast.JSXAttr{
			Name:  "sizes",
			Value: &ast.Literal{Kind: ast.StringLit, Value: sizes},
		})
	}
	imgAttrs = append(imgAttrs, extraAttrs...)

	imgEl := &ast.JSXElement{
		Opening: &ast.JSXOpening{
			Name:        "img",
			Attributes:  imgAttrs,
			SelfClosing: true,
		},
		Children: []ast.JSXChild{
			&ast.JSXText{Value: fmt.Sprintf("Your browser does not support the image element. Original: %s", src)},
		},
	}

	return &ast.JSXElement{
		Position: orig.Position,
		Opening: &ast.JSXOpening{
			Name:        "picture",
			Attributes:  pictureAttrs,
			SelfClosing: false,
		},
		Children: []ast.JSXChild{
			&ast.JSXElementChild{Element: webpSource},
			&ast.JSXElementChild{Element: fallbackSource},
			&ast.JSXElementChild{Element: imgEl},
		},
		Closing: &ast.JSXClosing{Name: "picture"},
	}
}

// imageErrorSpan renders an inline error marker in place of a broken <Image>.
func imageErrorSpan(msg string) *ast.JSXElement {
	return &ast.JSXElement{
		Opening:  &ast.JSXOpening{Name: "span", Attributes: nil, SelfClosing: false},
		Children: []ast.JSXChild{&ast.JSXText{Value: msg}},
		Closing:  &ast.JSXClosing{Name: "span"},
	}
}

// srcsetString renders a responsive srcset attribute ("path 640w, path 1024w").
func srcsetString(entries []imageproc.SrcsetEntry) string {
	if len(entries) == 0 {
		return ""
	}
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		parts = append(parts, fmt.Sprintf("%s %dw", e.FilePath, e.Width))
	}
	return strings.Join(parts, ", ")
}

// copyImageCacheToOut copies processed <Image> variants from the build cache
// into the output directory so they're served at /_krate/images/....
func (b *Builder) copyImageCacheToOut() error {
	src := filepath.Join(b.Root, ".krate", "cache", "images")
	if _, err := os.Stat(src); err != nil {
		return nil // no images processed
	}
	dst := filepath.Join(b.Cfg.OutDir, "_krate", "images")
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	return copyDirToOut(src, dst)
}

func evalAttrString(expr ast.Expr) (string, bool) {
	if lit, ok := expr.(*ast.Literal); ok {
		return lit.Value, true
	}
	if ident, ok := expr.(*ast.Identifier); ok {
		return ident.Name, false
	}
	return "", false
}

func (b *Builder) BuildAllAPI() error {
	apiSrcDir := filepath.Join(b.Root, "src", "api")
	if _, err := os.Stat(apiSrcDir); os.IsNotExist(err) {
		return nil // No API directory present, skip silently
	}

	var jsRoutes []string
	err := filepath.Walk(apiSrcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasPrefix(filepath.Base(path), "_") {
			return nil
		}
		if strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".js") {
			jsRoutes = append(jsRoutes, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("scanning api routes: %w", err)
	}

	if len(jsRoutes) > 0 {
		if err := b.CompileAPIRoutes(jsRoutes); err != nil {
			return err
		}
	}
	// Always run Go API build: it no-ops when no .go routes exist and removes
	// stale artifacts when routes were deleted.
	if err := b.BuildAllGoAPI(); err != nil {
		return err
	}
	return nil
}

// CompileAPIRoutes processes the specified list of API paths in parallel.
func (b *Builder) CompileAPIRoutes(routes []string) error {
	if len(routes) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(routes))
	apiSrcDir := filepath.Join(b.Root, "src", "api")
	pool := newWorkerPool(buildWorkerLimit())

	for _, route := range routes {
		wg.Add(1)
		go func(r string) {
			defer wg.Done()
			pool.acquire()
			defer pool.release()
			fmt.Printf("  %s▶ API%s %s\n", cGreen, cReset, filepath.Base(r))
			if err := b.compileSingleRoute(r, apiSrcDir); err != nil {
				fmt.Fprintf(os.Stderr, "  %s✗ API Error:%s %s: %v\n", cRed, cReset, r, err)
				errCh <- err
			}
		}(route)
	}

	wg.Wait()
	close(errCh)
	if len(errCh) > 0 {
		return <-errCh
	}
	return nil
}

func (b *Builder) compileSingleRoute(file string, apiSrcDir string) error {
	relPath, err := filepath.Rel(apiSrcDir, file)
	if err != nil {
		return err
	}
	outName := strings.TrimSuffix(relPath, filepath.Ext(relPath)) + ".js"
	outPath := filepath.Join(b.Cfg.OutDir, "api", outName)

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}

	opts := api.BuildOptions{
		EntryPoints: []string{file},
		Bundle:      true,
		Platform:    api.PlatformNode,
		Format:      api.FormatESModule,   // Compiles cleanly to ES modules matching your runner
		Packages:    api.PackagesExternal, // Keeps third-party node_modules completely external
		Outfile:     outPath,
		Write:       true,
		Metafile:    true, // Generates the dep-graph string in-memory
	}

	if b.Cfg.ShouldMinifyJS() {
		opts.MinifyWhitespace = true
		opts.MinifyIdentifiers = true
		opts.MinifySyntax = true
	}

	result := api.Build(opts)

	if len(result.Errors) > 0 {
		formatted := api.FormatMessages(result.Errors, api.FormatMessagesOptions{
			Kind:  api.ErrorMessage,
			Color: true, // Retains your pretty terminal compiler colors
		})
		var sb strings.Builder
		for _, msg := range formatted {
			sb.WriteString(msg)
		}
		return fmt.Errorf("esbuild compilation failed:\n%s", sb.String())
	}

	// Extract dependency tracking from the in-memory metafile string
	if result.Metafile != "" {
		type esbuildMeta struct {
			Inputs map[string]interface{} `json:"inputs"`
		}
		var meta esbuildMeta
		if err := json.Unmarshal([]byte(result.Metafile), &meta); err == nil {
			var deps []string
			for inputPath := range meta.Inputs {
				// Convert to pristine workspace absolute paths for the file watcher
				deps = append(deps, filepath.Clean(filepath.Join(b.Root, inputPath)))
			}
			// Re-inject back into your framework's reactive watch graph
			b.recordDeps(file, deps)
		}
	}

	return nil
}
