package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
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
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/content"
	"github.com/kratejs/krate/packages/compiler/internal/css"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/gohost"
	"github.com/kratejs/krate/packages/compiler/internal/gotsgen"
	"github.com/kratejs/krate/packages/compiler/internal/icons"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
	"github.com/kratejs/krate/packages/compiler/internal/plugin"
	"github.com/kratejs/krate/packages/compiler/internal/reactive"
	"github.com/kratejs/krate/packages/compiler/internal/renderer"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
	"github.com/kratejs/krate/packages/compiler/internal/safe"
	"github.com/kratejs/krate/packages/compiler/internal/syntaxhighlight"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

type PageResult struct {
	Page     string
	OutName  string
	HTML     string
	HeadHTML string
	// FontURLs are the fonts this page's own stylesheet asks to preload.
	FontURLs      []string
	ScriptHTML    string
	StyleHTML     string
	HydrationJS   string
	JSFile        string
	CSSFile       string
	CSSModernFile string // modern-only stylesheet for the UA sibling (hybrid+userAgent)
	CSSLegacyFile string // legacy-only stylesheet for the UA sibling (hybrid+userAgent)
	CSS           string // this page's own imported CSS (before minification/hashing)
	RuntimeJSFile string // shared runtime chunk path (relative to outDir), e.g. "chunks/runtime.abc.js"
	HasJS         bool
	HasCSS        bool
	HasCSSSignals bool // page emitted CSS-signal rules (drives the legacy sibling)
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
	Lang             string // per-page <html lang> override (e.g. docs locales)
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

	// Warnings are the positioned build warnings this page produced (module
	// rewrite diagnostics, IR warnings). They are stored with a disk-cached page
	// and replayed, so a warm build reports what a cold one does.
	Warnings []diag.Diagnostic

	// Program is the page's parsed (and plugin-transformed) AST. Consumed by
	// compiler-enforced quality checks; nil for plugin-generated routes.
	Program *ast.Program

	// GoBaked maps a Go-component call-site id (irtree.GoCallID) to its
	// build-time rendered HTML. Consumed by CompileServerBundles so an SSR page
	// bundle can inline server-tier Go components instead of calling the host.
	GoBaked map[string]string

	// ServerPage is true when the page component itself is async: the whole
	// page body renders per request in the SSR sidecar (no build-time body).
	ServerPage bool
	// ServerExports lists the async server components rendered as regions of
	// this page; the page server bundle exports each one (serverbundle.go).
	ServerExports []ServerExport
	// MetadataSegments are the layouts above the page that export metadata or
	// generateMetadata, and the images of the directories above it (outermost
	// first); the page server bundle exports them so the renderer can merge the
	// chain per request.
	MetadataSegments []metaSegment
	// MetadataPageFiles is the convention-file metadata (images) of the page's own route.
	MetadataPageFiles string
	// ClientBoundaries lists the "use client" modules in the module graph of a
	// page that renders server components. The page server bundle wraps their
	// exports in client boundaries (clientboundary.go).
	ClientBoundaries []ClientBoundary
	// IslandStubs replaces the islands of a page rendered per request in its
	// server bundle (nil for static pages and pages without islands).
	IslandStubs *islandServerStubs
}

type Builder struct {
	// metaImages indexes the images the opengraph-image and twitter-image
	// convention files produced in this build.
	metaImages *metaImages

	// hl holds the highlighter chunks written for code highlighted in the browser.
	hl      highlightChunks
	Root    string
	Cfg     *config.Config
	DevMode bool
	Verbose bool
	// DevReload is called by the dev watcher when a config or env file
	// changed. It reports whether the new settings load cleanly, in which case
	// the watcher asks the dev server to restart; otherwise it has already
	// surfaced the error and the current server keeps running.
	DevReload func(reason string) bool
	ctx       context.Context     // optional cancellation for long builds
	Env       map[string]string   // resolved .env values (build/serve-time only, never client-facing)
	depGraph  map[string][]string // file path -> page source paths that depend on it
	pageDeps  map[string][]string // page source path -> files it depends on
	depMu     sync.Mutex          // protects depGraph/pageDeps

	// twMu guards twPages/twWarned: Tailwind CSS-first directives collected
	// from the authored CSS that flows through processCSSDirectives (replaced
	// per page on each rebuild, cleared on a full build), and the warnings
	// already printed.
	twMu     sync.Mutex
	modCSS   moduleCSSRegistry     // layout/slot path -> its CSS Modules' scoped CSS
	twPages  map[string]twPageInfo // page source (or "") -> its collected Tailwind CSS state
	twWarned map[string]bool

	// goHostMu serialises Go host scaffolding/compilation so concurrent page
	// builds that lazily need the host (e.g. incremental dev rebuilds) don't
	// race on the generated module dir.
	goHostMu sync.Mutex
	// goParseMu guards goParseCache: parsed `.go` component files, cached per
	// build so each is parsed once instead of once per page/layout.
	goParseMu    sync.Mutex
	goParseCache map[string][]gotsgen.Component

	// SkipQualityChecks suppresses the in-build quality gates so callers that
	// run their own check pass (e.g. `krate check`, which re-reads dist/) don't
	// evaluate and report the same findings twice.
	SkipQualityChecks bool

	// fontPreloadHTML holds the `<link rel="preload">` tags for fonts declared
	// in the global stylesheet; injected into every page head.
	fontPreloadHTML string
	// globalFonts are the font URLs already preloaded by the global stylesheet.
	globalFonts []string

	workerMu  sync.Mutex
	workers   map[string]string // worker source path -> hashed site URL (/workers/...)
	workerEsm map[string]bool   // worker source path -> built as ES module

	chunkMu sync.Mutex
	chunks  map[string]string // dynamic-import source path -> hashed site URL (/chunks/...)

	// Plugin hook errors are collected here (page builds run in parallel
	// goroutines) so a failing plugin - e.g. a Go plugin whose binary is
	// missing - fails `krate build` with a non-zero exit instead of silently
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

	// staticOnlyRoutes collects dynamic route patterns (e.g. "/blog/[slug]")
	// whose params are closed, so the manifest and server can 404 unknown params.
	staticOnlyRoutes []string
	staticOnlyMu     sync.Mutex

	// Prof accumulates per-phase wall-clock timings when --profile is set.
	// Safe for concurrent use from parallel page goroutines.
	Prof *Profiler

	// moduleCache shares parsed modules across every Bundler in the build so
	// shared theme/component/runtime modules are parsed once, not once per page.
	moduleCache *bundler.ModuleCache
	// cacheContentHash keys the module cache's validity: when content
	// collections change, cached ASTs (whose getCollection calls were inlined)
	// must be re-parsed, so the cache is reset.
	cacheContentHash string
	// transformOnce guards the in-place universal transforms so a shared
	// (cached) AST is transformed exactly once per build, never concurrently.
	transformOnce sync.Map
	// collectionSites records, per program, the getCollection calls whose
	// result is left to run in the browser (see InlineContent).
	collectionSites sync.Map
	// deferInline marks entry programs whose getCollection inlining must wait
	// until route params are bound (static-params pages), so lookups like
	// `find((p) => p.slug === props.params.slug)` fold to a single entry.
	deferInline sync.Map
	// progImageDiags holds the <Image> diagnostics of each transformed program.
	progImageDiags sync.Map

	// diskCache persists per-page outputs across runs (see diskcache.go).
	diskCache *buildDiskCache
	// External React islands share one Preact build across every page. The
	// import map points the bare preact* specifiers at those shared chunks.
	islandMu        sync.Mutex
	islandImportMap string
	preactChunks    map[string]string
	// islandEntries are the island chunk entry modules of each page; they are
	// bundled together after the pages (see islandgraph.go).
	islandEntries map[string][]islandEntry
	islandDirty   bool

	// clientChunkMu guards clientChunks: "use client" module path -> URL of
	// its hydration chunk, built once per build (clientboundary.go).
	clientChunkMu sync.Mutex
	clientChunks  map[string]string

	// actions holds the discovered `"use server"` modules and their compiled
	// bundles for this build. It is refreshed by prepareActions before pages.
	actions *actionRegistry

	segmentGenMu sync.Mutex
	segmentGen   map[string]string // generated segment source path -> content written
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
	if b.DevMode {
		// Enables the SPA router's dev warnings (e.g. a layout it could not keep mounted).
		cfg["dev"] = true
	}
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
	if b.fontPreloadHTML != "" {
		sb.WriteString(b.fontPreloadHTML)
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
	// Normalise the project root into the config once, at construction. Builds
	// derive this lazily; doing it here keeps the config stable across builds so
	// the disk-cache config hash (and any other consumer) doesn't churn.
	cfg.Markdown.Root = root
	cfgHash := ""
	if data, err := json.Marshal(cfg); err == nil {
		sum := sha256.Sum256(data)
		cfgHash = hex.EncodeToString(sum[:])
	}
	// The disk cache is only safe for pure builds: per-page plugin hooks can
	// inject output the cache cannot replay, and sourcemap sidecars aren't
	// captured.
	enabled := diskCacheEnabled(cfg.Sourcemap, plugin.HasPerPageHooks(), hasCommunityPlugins(cfg))
	return &Builder{
		Root:        root,
		Cfg:         cfg,
		depGraph:    make(map[string][]string),
		pageDeps:    make(map[string][]string),
		Prof:        NewProfiler(false),
		moduleCache: bundler.NewModuleCache(),
		diskCache:   newBuildDiskCache(root, cfgHash, enabled),
	}
}

// SetContext attaches a cancellation context to a build. It is optional; a
// cancelled context stops between pages and before expensive phases.
func (b *Builder) SetContext(ctx context.Context) {
	b.ctx = ctx
}

func (b *Builder) contextErr() error {
	if b.ctx == nil {
		return nil
	}
	return b.ctx.Err()
}

// hasCommunityPlugins reports whether any configured plugin is a community
// (JS/TS module) plugin, whose hooks the disk cache can't statically reason
// about.
func hasCommunityPlugins(cfg *config.Config) bool {
	for _, p := range cfg.Plugins {
		if p.Module != "" {
			return true
		}
	}
	return false
}

// ensureTransformed applies the universal AST transforms (icons, images, spread
// flattening, content inlining) to prog exactly once per build. Cached module
// ASTs are shared across concurrent page builds, so these in-place mutations
// must be serialized per program.
func (b *Builder) ensureTransformed(prog *ast.Program, file string) error {
	if prog == nil {
		return nil
	}
	v, _ := b.transformOnce.LoadOrStore(prog, &sync.Once{})
	v.(*sync.Once).Do(func() {
		b.TransformUniversalIcons(prog)
		if ds := b.transformImages(prog); len(ds) > 0 {
			b.progImageDiags.Store(prog, ds)
			for _, d := range ds {
				if d.warn {
					fmt.Fprintf(os.Stderr, "  %sWarning:%s %s\n      hint: %s\n", cYellow, cReset, imageDiagError(b.relPath(file), d).Error(), d.hint)
				}
			}
		}
		b.FlattenComponentSpreadAttrs(prog)
		if _, deferred := b.deferInline.Load(prog); !deferred {
			b.InlineContent(prog)
		}
	})
	if v, ok := b.progImageDiags.Load(prog); ok {
		var errs []error
		for _, d := range v.([]imageDiag) {
			if !d.warn {
				errs = append(errs, imageDiagError(b.relPath(file), d))
			}
		}
		if len(errs) > 0 {
			return &diagList{errs: errs}
		}
	}
	return nil
}

// withoutTemplateSlotWarnings drops KR8011 from the template pass of a dynamic
// route: `{params.id}` has no value there, but each generated page does.
func withoutTemplateSlotWarnings(warnings []diag.Diagnostic) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, w := range warnings {
		if w.Code != diag.CodeSlotUnresolved {
			out = append(out, w)
		}
	}
	return out
}

// diagList is a set of located diagnostics returned as one build error.
type diagList struct{ errs []error }

func (d *diagList) Error() string {
	parts := make([]string, len(d.errs))
	for i, e := range d.errs {
		parts[i] = e.Error()
		if dd, ok := e.(diag.Diagnostic); ok && dd.Hint != "" {
			parts[i] += "\n      hint: " + dd.Hint
		}
	}
	return strings.Join(parts, "\n  ")
}

func (d *diagList) Unwrap() []error { return d.errs }

// errWithHint renders err for the build log. A Diagnostic wrapped by fmt.Errorf
// carries its Hint only as a field, so the error text alone would lose it.
func errWithHint(err error) string {
	msg := err.Error()
	var d diag.Diagnostic
	if errors.As(err, &d) && d.Hint != "" && !strings.Contains(msg, d.Hint) {
		msg += "\n      hint: " + d.Hint
	}
	return msg
}

// ensureTransformedModules transforms every module in a page's bundle. Page
// builds share cached ASTs, so this must finish before any AST reader runs.
func (b *Builder) ensureTransformedModules(mods []*bundler.Module) error {
	var errs []error
	for _, mod := range mods {
		if mod == nil || mod.IsExternal || mod.IsCSS || mod.Program == nil {
			continue
		}
		if err := b.ensureTransformed(mod.Program, mod.Path); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// contentHashOf returns a stable hash of the build's content collections. It is
// used to invalidate the module cache when content changes (cached ASTs have
// their getCollection calls inlined).
func (b *Builder) contentHashOf() string {
	if len(b.contentCollections) == 0 {
		return ""
	}
	names := make([]string, 0, len(b.contentCollections))
	for name := range b.contentCollections {
		names = append(names, name)
	}
	sort.Strings(names)
	h := fnv.New64a()
	for _, name := range names {
		h.Write([]byte(name))
		h.Write([]byte{0})
		for _, e := range b.contentCollections[name] {
			// json.Marshal sorts map keys, so the hash is deterministic.
			data, _ := json.Marshal(e)
			h.Write(data)
			h.Write([]byte{'\n'})
		}
	}
	return strconv.FormatUint(h.Sum64(), 16)
}

// phase records a build phase when profiling is enabled; the returned function
// ends the phase. Callers use `defer b.phase("name")()`.
func (b *Builder) phase(name string) func() {
	return b.Prof.Phase(name)
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

// recoverPage runs a page/route build, converting a panic into an error so a
// single bad page cannot crash the whole build.
func recoverPage(fn func() (*PageResult, string, error)) (result *PageResult, rawCSS string, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = safe.Recover(rec)
		}
	}()
	return fn()
}

// runPageHook runs a page-level hook: the native hook (when native is non-nil)
// followed by the same-named community plugin hook. Both are best-effort - a
// failure is logged and recorded so the build exits non-zero, but rendering
// continues so every page's problem is reported in one pass. This centralizes
// the identical AfterParse / AfterRender / AfterMarkdownParse dispatch.
func (b *Builder) runPageHook(name, page string, ctx any, native func() error) {
	if native != nil {
		if err := safe.Run(native); err != nil {
			fmt.Fprintf(os.Stderr, "  %s%s plugin error (%s):%s %v\n", cYellow, name, page, cReset, err)
			b.pluginFailed(fmt.Errorf("%s (%s): %v", name, page, err))
		}
	}
	if err := safe.Run(func() error {
		return plugin.RunCommunityPlugins(name, b.Cfg.Plugins, b.Root, b.Cfg.OutDir, ctx, b.communityEnv())
	}); err != nil {
		fmt.Fprintf(os.Stderr, "  %sCommunity plugin error %s (%s):%s %v\n", cYellow, name, page, cReset, err)
		b.pluginFailed(fmt.Errorf("%s (%s): %v", name, page, err))
	}
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
		fmt.Fprintf(os.Stderr, "  %s! %s%s\n", cYellow, d.Message, cReset)
	}
}

// BuildPages rebuilds only the specified pages (by source path).
// Unlike BuildAll, it does NOT clean the output directory.
func (b *Builder) BuildPages(pages []string) ([]*PageResult, error) {
	if len(pages) == 0 {
		return nil, nil
	}

	var pageErrs []error

	b.resetBuildCaches()
	if err := b.prepareActions(); err != nil {
		return nil, err
	}

	// A dynamic-route template (e.g. video/[id].tsx) is not the page that gets
	// served: its concrete URLs are the generateStaticParams expansions. Rebuild
	// those too, or a partial rebuild would leave every /video/abc123 variant
	// pointing at stale HTML while the [id] template itself is refreshed.
	concrete, gspErr := b.resolveStaticParamsPages(pages)
	if gspErr != nil {
		fmt.Fprintf(os.Stderr, "  %sx Error:%s %v\n", cRed, cReset, gspErr)
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
			printf("  %s>%s %s\n", cCyan, cReset, p)
			result, rawCSS, err := recoverPage(func() (*PageResult, string, error) { return b.buildPage(p) })
			resultsCh <- pageBuildResult{result, rawCSS, err, p}
		}(page)
	}

	for _, spp := range concrete {
		wg.Add(1)
		go func(s staticParamsPage) {
			defer wg.Done()
			pool.acquire()
			defer pool.release()
			printf("  %s>%s %s\n", cCyan, cReset, s.OutPath)
			result, rawCSS, err := recoverPage(func() (*PageResult, string, error) { return b.buildStaticParamsPage(s) })
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
			fmt.Fprintf(os.Stderr, "\n  %sx Error:%s %v\n", cRed, cReset, res.err)
			pageErrs = append(pageErrs, res.err)
			continue
		}
		result := res.result
		results = append(results, result)

		// Run page-level plugins (AfterPage - post-layout)
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
			// The pages exist; they failed. The pagesDir hint is only for an
			// empty directory (below).
			return nil, &multiError{summary: fmt.Sprintf("build failed: %d error(s)", len(pageErrs)), errs: pageErrs}
		}
		return nil, fmt.Errorf("no pages built successfully; check that pagesDir (%s) contains at least one page", b.Cfg.PagesDir)
	}

	// Write shared runtime chunk (extracted from per-page bundles)
	runtimeJS := writeRuntimeChunk(b.Cfg.OutDir, b.shouldMinifyJS(), b.Root, b.runtimeChunkOptions(results))
	warnMissingRuntime(runtimeJS, results)
	if b.DevMode {
		writeDevChunk(b.Cfg.OutDir, b.Root)
	}
	// Island chunks are bundled for the whole site, so a page rebuilt alone
	// refreshes the shared files its siblings import.
	if b.takeIslandDirty() {
		if err := b.writeIslandBundles(); err != nil {
			fmt.Fprintf(os.Stderr, "\n  %sx Error:%s %v\n", cRed, cReset, err)
			pageErrs = append(pageErrs, err)
		}
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

// guardOutDir refuses truly destructive output directories so the build's output
// wipe can never delete the user's sources: a filesystem root or an outDir equal
// to the project root are hard errors. An outDir that merely *contains* the
// project root (e.g. a monorepo parent) is allowed but warned about. Set
// KRATE_ALLOW_UNSAFE_OUTDIR=1 to bypass the check entirely.
func (b *Builder) guardOutDir() error {
	if os.Getenv("KRATE_ALLOW_UNSAFE_OUTDIR") != "" {
		return nil
	}
	outAbs, err1 := filepath.Abs(b.Cfg.OutDir)
	rootAbs, err2 := filepath.Abs(b.Root)
	if err1 != nil || err2 != nil {
		return nil
	}
	if filepath.Dir(outAbs) == outAbs {
		return fmt.Errorf("refusing to build: outDir %s is a filesystem root (set KRATE_ALLOW_UNSAFE_OUTDIR=1 to override)", outAbs)
	}
	if outAbs == rootAbs {
		return fmt.Errorf("refusing to build: outDir equals the project root (%s)", outAbs)
	}
	if fsutil.WithinDir(outAbs, rootAbs) {
		fmt.Fprintf(os.Stderr, "  %s! outDir %s contains the project root; the build will wipe it%s\n", cYellow, outAbs, cReset)
	}
	return nil
}

func (b *Builder) BuildAll() error {
	defer b.phase("build:total")()
	if err := b.contextErr(); err != nil {
		return err
	}
	if err := b.guardOutDir(); err != nil {
		return err
	}
	if err := os.RemoveAll(b.Cfg.OutDir); err != nil {
		return fmt.Errorf("cleaning output dir: %w", err)
	}
	if err := os.MkdirAll(b.Cfg.OutDir, 0755); err != nil {
		return fmt.Errorf("creating output dir: %w", err)
	}
	defer b.ClosePlugins()

	b.resetBuildCaches()
	b.resetIslandEntries()
	// A full build re-records every dependency; entries for deleted pages must
	// not survive it.
	b.depMu.Lock()
	b.depGraph = make(map[string][]string)
	b.pageDeps = make(map[string][]string)
	b.depMu.Unlock()
	// A full build starts from clean Tailwind state; partial rebuilds replace
	// only the pages they rebuild.
	b.twMu.Lock()
	b.twPages = nil
	b.twMu.Unlock()
	if err := b.prepareActions(); err != nil {
		return err
	}

	if err := writeConfigSchema(b.Root); err != nil {
		fmt.Fprintf(os.Stderr, "  %sWarning: failed to write .krate/config.schema.json:%s %v\n", cYellow, cReset, err)
	}

	pages, err := findPages(b.Cfg.PagesDir)
	if err != nil {
		return fmt.Errorf("finding pages: %w", err)
	}
	if err := validateRouteTree(pages, b.Cfg.PagesDir); err != nil {
		return err
	}
	// Metadata routes (sitemap, robots, manifest, opengraph-image) run first:
	// the pages' <head> carries the image URLs they produce.
	if errs := b.buildMetadataRoutes(pages); len(errs) > 0 {
		for _, err := range errs {
			fmt.Fprintf(os.Stderr, "  %sMetadata route error:%s %s\n", cRed, cReset, strings.ReplaceAll(diag.FormatDiagnostics([]error{err}), "\n", "\n    "))
		}
		return errors.Join(errs...)
	}
	// Intercepting slot pages are built as fragments the SPA router swaps
	// into their slot (see parallel.go).
	pages = append(pages, findInterceptPages(b.Cfg.PagesDir)...)
	usedEntryFallback := false
	if len(pages) == 0 {
		pages = []string{b.Cfg.Entry}
		usedEntryFallback = true
	}

	// Run BeforeBuild hooks (docs plugin generates .krategen/ pages here)
	stopPlugins := b.phase("build:plugins-before")
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
	stopPlugins()

	if usedEntryFallback && len(genPages) == 0 && len(routes) == 0 {
		if _, statErr := os.Stat(b.Cfg.Entry); statErr != nil {
			rel := func(p string) string {
				if r, err := filepath.Rel(b.Root, p); err == nil {
					return filepath.ToSlash(r)
				}
				return p
			}
			if _, dirErr := os.Stat(b.Cfg.PagesDir); dirErr != nil {
				return diag.Diagnostic{
					File:    b.Cfg.PagesDir,
					Message: "pagesDir " + rel(b.Cfg.PagesDir) + " does not exist",
					Hint:    "create " + rel(b.Cfg.PagesDir) + " with at least one page, or fix the pagesDir setting in krate.config.ts",
				}
			}
			return diag.Diagnostic{
				File:    b.Cfg.PagesDir,
				Message: "no pages found in " + rel(b.Cfg.PagesDir),
				Hint:    "add a page such as " + rel(b.Cfg.PagesDir) + "/index.tsx",
			}
		}
	}

	// Plugins generated files during BeforeBuild/GenerateRoutes; drop resolver
	// stat/package.json memos so negative probes made before those files existed
	// don't hide them from page bundling.
	resolver.ResetCaches()

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
	stopContent := b.phase("build:content")
	cres := b.prepareContent()
	stopContent()
	// Cached module ASTs have their getCollection calls inlined, so a content
	// change must invalidate the parse cache; source-only changes keep it.
	ch := b.contentHashOf()
	if ch != b.cacheContentHash {
		b.moduleCache = bundler.NewModuleCache()
		b.cacheContentHash = ch
	}
	if b.diskCache != nil {
		b.diskCache.setContentHash(ch)
	}
	for _, w := range cres.Warnings {
		fmt.Fprintf(os.Stderr, "  %sWarning: content:%s %v\n", cYellow, cReset, w)
	}
	if len(cres.Validation) > 0 {
		// Schema violations are content bugs: fail the build (and `krate
		// check`, which builds first) instead of emitting pages built from
		// invalid entries. The error also reaches the dev overlay.
		for _, ve := range cres.Validation {
			fmt.Fprintf(os.Stderr, "  %sContent error:%s %v\n", cRed, cReset, ve)
		}
		return &ContentValidationError{Errors: cres.Validation}
	}

	// Build the Go host (API routes + components) before pages, so pages that
	// import Go components can render them through the host at build time.
	if err := b.BuildAllGoAPI(); err != nil {
		return err
	}

	if err := b.contextErr(); err != nil {
		return err
	}
	totalPages := len(pages) + len(routes)
	stopPages := b.phase("build:pages")
	resultsCh := make(chan pageBuildResult, totalPages)
	var wg sync.WaitGroup
	pool := newWorkerPool(buildWorkerLimit())

	for _, page := range pages {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			pool.acquire()
			defer pool.release()
			printf("  %s>%s %s\n", cCyan, cReset, p)
			result, rawCSS, err := recoverPage(func() (*PageResult, string, error) { return b.buildPage(p) })
			resultsCh <- pageBuildResult{result, rawCSS, err, p}
		}(page)
	}

	for _, route := range routes {
		wg.Add(1)
		go func(r plugin.Route) {
			defer wg.Done()
			pool.acquire()
			defer pool.release()
			printf("  %s>%s %s (route)%s\n", cGreen, cReset, r.Path, cReset)
			result, rawCSS, err := recoverPage(func() (*PageResult, string, error) { return b.buildRoute(r) })
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
			detail := errWithHint(res.err)
			fmt.Fprintf(os.Stderr, "\n  %sx Error (%s):%s %s\n", cRed, res.page, cReset, detail)
			failureMessages = append(failureMessages, fmt.Sprintf("  %s: %s", res.page, detail))
			failureErrs = append(failureErrs, res.err)
			errorCount++
			continue
		}

		result := res.result
		results = append(results, result)

		// Run page-level plugins (AfterPage - post-layout)
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
	stopPages()

	staticParamPages, gspErr := b.resolveStaticParamsPages(pages)
	if gspErr != nil {
		fmt.Fprintf(os.Stderr, "  %sx Error:%s %v\n", cRed, cReset, gspErr)
		failureMessages = append(failureMessages, "  "+gspErr.Error())
		failureErrs = append(failureErrs, gspErr)
		errorCount++
	}
	if len(staticParamPages) > 0 {
		printf("  %s*%s Building %d statically generated pages from generateStaticParams\n", cCyan, cReset, len(staticParamPages))
		for _, spp := range staticParamPages {
			result, _, err := b.buildStaticParamsPage(spp)
			if err != nil {
				msg := fmt.Sprintf("generateStaticParams page %s: %v", spp.OutPath, err)
				fmt.Fprintf(os.Stderr, "  %sx Error:%s %v\n", cRed, cReset, msg)
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
			printf("  %s*%s Static-only dynamic routes (no fallback template): %s\n", cCyan, cReset, strings.Join(dropped, ", "))
		}
	}

	stopWrite := b.phase("build:write")
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
		runtimeJS = writeRuntimeChunk(b.Cfg.OutDir, b.shouldMinifyJS(), b.Root, b.runtimeChunkOptions(results))

		warnMissingRuntime(runtimeJS, results)
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

	// Bundle the island chunks of every page together so modules the pages
	// share (stores, query clients) are shared files.
	if err := b.writeIslandBundles(); err != nil {
		fmt.Fprintf(os.Stderr, "  %sIsland bundle error:%s %v\n", cRed, cReset, err)
		failureMessages = append(failureMessages, "  islands: "+err.Error())
		failureErrs = append(failureErrs, err)
		errorCount++
	}

	// In-memory HTML generation + string swap + single disk write per page
	b.writeHTMLPages(results, globalCSS, runtimeJS)
	stopWrite()

	// Compiler-enforced quality gates (opt-in via `checks` in krate.config.ts).
	// Runs against the final HTML so rules see exactly what ships.
	if !b.SkipQualityChecks {
		if err := b.runQualityChecks(results, runtimeJS); err != nil {
			fmt.Fprintf(os.Stderr, "  %sx Checks:%s %v\n", cRed, cReset, err)
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

	// publicFiles are the user's own static files; the build did not hash them,
	// so they must never be cached as immutable.
	publicFiles := map[string]bool{}
	if b.Cfg.PublicDir != "" {
		if info, err := os.Stat(b.Cfg.PublicDir); err == nil && info.IsDir() {
			copied, err := copyDirToOut(b.Cfg.PublicDir, b.Cfg.OutDir)
			for _, rel := range copied {
				publicFiles[rel] = true
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "  %sPublic copy error:%s %v\n", cYellow, cReset, err)
				failureMessages = append(failureMessages, "  public: "+err.Error())
				failureErrs = append(failureErrs, err)
				errorCount++
			}
		}
	}

	// Copy processed <Image> variants into the output (served at /_krate/images/...)
	if err := b.copyImageCacheToOut(); err != nil {
		fmt.Fprintf(os.Stderr, "  %sImage copy error:%s %v\n", cYellow, cReset, err)
	}

	stopTools := b.phase("build:tools")
	strays := findPagesStrays(b.Cfg.PagesDir)
	printWarningsOnce(os.Stderr, b.strayAPIWarnings(strays.apiFiles))
	for _, err := range b.buildPagesEndpoints(strays.endpoints) {
		fmt.Fprintf(os.Stderr, "  %sEndpoint error:%s %s\n", cRed, cReset, strings.ReplaceAll(diag.FormatDiagnostics([]error{err}), "\n", "\n    "))
		failureMessages = append(failureMessages, "  endpoint: "+err.Error())
		failureErrs = append(failureErrs, err)
		errorCount++
	}
	if err := b.BuildAllAPI(); err != nil {
		fmt.Fprintf(os.Stderr, "  %sAPI Build error:%s %v\n", cRed, cReset, err)
		failureMessages = append(failureMessages, "  API: "+err.Error())
		failureErrs = append(failureErrs, err)
		errorCount++
	}

	if err := b.BuildMiddleware(); err != nil {
		fmt.Fprintf(os.Stderr, "  %sMiddleware error:%s %v\n", cRed, cReset, err)
		failureMessages = append(failureMessages, "  middleware: "+err.Error())
		failureErrs = append(failureErrs, err)
		errorCount++
	}

	// Write page manifest with SSR/ISR metadata
	manifestCSS := ""
	if len(globalCSS) > 0 {
		manifestCSS = globalCSS[0]
	}
	manifest := BuildManifest(results, manifestCSS, runtimeJS)
	manifest.StaticOnlyRoutes = append([]string(nil), b.staticOnlyRoutes...)
	b.addSegmentsToManifest(manifest, results)

	// revalidatePath("/x") calls whose target page is static cannot refresh it.
	printWarningsOnce(os.Stderr, staticRevalidateTargetWarnings(filepath.Dir(b.Cfg.PagesDir), b.Root, manifest.Pages))

	// Compile server bundles for SSR/ISR/streaming pages
	serverBundles, serverBundleErrs := compileServerBundles(b.importResolver(), results, b.Root, b.Cfg.OutDir, b.Cfg.Sourcemap, b.actionModules(), b.Cfg.BaseURLPath())
	b.writeHighlightTables(serverBundles)
	for _, err := range serverBundleErrs {
		failureMessages = append(failureMessages, "  "+err.Error())
		failureErrs = append(failureErrs, err)
		errorCount++
	}
	if len(serverBundles) > 0 {
		printf("  %s*%s Compiled %d server bundles\n", cCyan, cReset, len(serverBundles))
		// Stage the bundled SSR renderer driver so `krate serve` runs it with
		// plain node instead of requiring a TypeScript runtime.
		if staged := stageServerRenderer(b.Root, b.Cfg.OutDir); staged != "" {
			printf("  %s*%s Staged SSR renderer driver\n", cCyan, cReset)
		} else {
			fmt.Fprintf(os.Stderr, "  %sWarning: SSR renderer driver not staged - server-renderer source not found under %s (ensure @krate/runtime is installed)%s\n", cYellow, b.Root, cReset)
		}
	}

	// Compile runtime server components (*.runtime.tsx, // @runtime, runtimeDirs)
	runtimeCompBundles := CompileRuntimeComponents(b.Root, b.Cfg.OutDir, b.Cfg.ServerComponents, b.Cfg.RuntimeComponents, b.Cfg.RuntimeDirs)
	if len(runtimeCompBundles) > 0 {
		printf("  %s*%s Compiled %d runtime components\n", cCyan, cReset, len(runtimeCompBundles))
	}
	manifest.SetRuntimeComponents(runtimeCompBundles)
	stopTools()

	manifest.HashedAssets = collectHashedAssets(b.Cfg.OutDir, publicFiles)
	if err := WriteManifest(manifest, b.Cfg.OutDir, serverBundles); err != nil {
		fmt.Fprintf(os.Stderr, "  %sWarning: failed to write manifest:%s %v\n", cYellow, cReset, err)
	}

	// Generate typed route declarations (.krate/types/routes.d.ts + bridge).
	// Warnings only - type generation must never fail a build.
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
		printf("  %s*%s SSR: %d, ISR: %d, Streaming: %d pages\n", cCyan, cReset, ssrCount, isrCount, streamingCount)
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

			IsErrorPage: r.IsErrorPage,
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
	// AfterRender, AfterMarkdownParse, AfterPage) must fail the build too -
	// otherwise `krate build` exits 0 while silently dropping plugin output.
	perrs := b.drainPluginErrs()
	if len(perrs) > 0 {
		failureMessages = append(failureMessages, perrs...)
		for _, pe := range perrs {
			failureErrs = append(failureErrs, fmt.Errorf("%s", pe))
		}
		errorCount += len(perrs)
	}

	if b.diskCache != nil && b.diskCache.enabled && (b.Prof.Enabled() || b.Verbose) {
		fmt.Fprintf(os.Stderr, "  build cache: %s\n", b.diskCache.string())
	}
	if b.Prof.Enabled() {
		b.Prof.Report(os.Stderr)
	}

	if errorCount > 0 {
		return &multiError{
			summary: fmt.Sprintf("build failed: %d error(s):\n%s", errorCount, strings.Join(failureMessages, "\n")),
			errs:    failureErrs,
		}
	}

	// Build-time precompression: emit .br/.gz siblings for compressible assets.
	// Skipped in dev (rebuilds must stay fast); the client/static host only uses
	// them for production delivery.
	if !b.DevMode && b.Cfg.ShouldPrecompress() {
		stopPC := b.phase("build:precompress")
		n, err := PrecompressOutput(b.Cfg.OutDir, b.Cfg.Compress.Brotli, b.Cfg.Compress.Gzip)
		stopPC()
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %sWarning: precompression failed:%s %v\n", cYellow, cReset, err)
		} else if n > 0 {
			printf("  %s*%s Precompressed %d assets\n", cCyan, cReset, n)
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
	// A rebuilt page replaces its earlier record, so a dependency it no longer
	// has stops triggering it and repeated rebuilds do not grow the graph.
	for _, old := range b.pageDeps[page] {
		pages := b.depGraph[old]
		kept := pages[:0]
		for _, pg := range pages {
			if pg != page {
				kept = append(kept, pg)
			}
		}
		if len(kept) == 0 {
			delete(b.depGraph, old)
		} else {
			b.depGraph[old] = kept
		}
	}
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

// tailwindImportMode maps the Tailwind setting to the CSS import handling.
func (b *Builder) tailwindImportMode() css.TailwindImportMode {
	if b.Cfg.Tailwind.Enabled {
		return css.TailwindKeep
	}
	return css.TailwindOff
}

// inlineImports inlines @import directives of merged authored CSS.
func (b *Builder) inlineImports(cssText string) string {
	return css.InlineImportsWith(cssText, b.Root, css.ImportOptions{Root: b.Root, Tailwind: b.tailwindImportMode()})
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

// twPageInfo is the Tailwind state one page's authored CSS contributes: its
// CSS-first directives and the @keyframes/@property blocks that @apply expanded
// (carried by the shared Tailwind sheet instead of every page sheet).
type twPageInfo struct {
	dirs   *css.TailwindDirectives
	extras []string
}

// processCSSDirectives handles Tailwind build-time directives in authored CSS.
// `@tailwind` is always stripped (Krate generates utilities itself). When
// Tailwind is enabled the CSS-first directives (`@import "tailwindcss"`,
// `@theme`, `@source`, `@utility`, `@custom-variant`, `@config`, `@plugin`)
// are collected for the generator and removed, and `@apply` is expanded;
// anything that cannot be honoured is reported with a warning. The returned
// info is what the caller records for the page via setTailwindPage.
func (b *Builder) processCSSDirectives(cssText string) (string, twPageInfo) {
	var info twPageInfo
	// `@layer` is left intact: it is valid CSS and unwrapping it would change
	// the authored cascade.
	if !b.Cfg.Tailwind.Enabled {
		return css.StripAtTailwind(cssText), info
	}
	info.dirs = b.parseTailwindDirectives(cssText)
	cssText = css.StripTailwindDirectives(cssText)
	if !strings.Contains(cssText, "@apply") && !strings.Contains(cssText, "@variant") {
		return cssText, info
	}
	opts := b.tailwindOptions()
	gen := css.NewTailwindGenerator()
	gen.MergeConfig(css.LoadTailwindConfigWithOptions(b.Root, opts))
	dirs := css.ScanTailwindDirectives(b.Root, opts)
	dirs.MergeFrom(info.dirs)
	dirs.ApplyTo(&gen.Theme)
	gen.Theme.LiveVars = dirs.LiveVars(&gen.Theme)
	out, unresolved := gen.ExpandApply(cssText)
	if len(unresolved) > 0 {
		b.warnOnce("Tailwind @apply: could not expand " + strings.Join(unresolved, ", ") + " (not a known utility; check the spelling, or define it with @utility)")
	}
	if strings.Contains(out, "@variant") {
		b.warnOnce("Tailwind @variant inside rules is not supported and was left in the CSS; use the variant:utility class form instead")
	}
	// The shared Tailwind sheet is linked on every page, so it carries the
	// keyframes instead of each page sheet repeating them.
	info.extras = splitTopLevelBlocks(gen.KeyframesCSS())
	return out, info
}

// splitTopLevelBlocks splits CSS made of top-level at-rule blocks into one
// string per block.
func splitTopLevelBlocks(text string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
			if depth == 0 {
				if blk := strings.TrimSpace(text[start : i+1]); blk != "" {
					out = append(out, blk)
				}
				start = i + 1
			}
		}
	}
	return out
}

// parseTailwindDirectives records the directives of one authored stylesheet.
func (b *Builder) parseTailwindDirectives(cssText string) *css.TailwindDirectives {
	if !strings.Contains(cssText, "@") && !strings.Contains(cssText, "var(") {
		return nil
	}
	d := css.NewTailwindDirectives(b.Root)
	d.Add(cssText, b.Root)
	return d
}

// setTailwindPage replaces the Tailwind state recorded for a page, so a
// directive removed from its CSS stops applying on the next rebuild.
func (b *Builder) setTailwindPage(page string, info twPageInfo) {
	b.twMu.Lock()
	defer b.twMu.Unlock()
	if info.dirs == nil && len(info.extras) == 0 {
		delete(b.twPages, page)
		return
	}
	if b.twPages == nil {
		b.twPages = map[string]twPageInfo{}
	}
	b.twPages[page] = info
}

// warnOnce prints a yellow Tailwind warning once per message per builder.
func (b *Builder) warnOnce(msg string) {
	b.twMu.Lock()
	if b.twWarned == nil {
		b.twWarned = map[string]bool{}
	}
	seen := b.twWarned[msg]
	b.twWarned[msg] = true
	b.twMu.Unlock()
	if !seen {
		fmt.Fprintf(os.Stderr, "  %s%s%s\n", cYellow, msg, cReset)
	}
}

// tailwindCSS generates the site-global Tailwind stylesheet honoring the
// tailwind config (scanDirs/content, darkMode, preflight, strict) and the
// CSS-first directives found in project CSS. Returns "" when Tailwind is
// disabled or nothing was generated.
func (b *Builder) tailwindCSS() string {
	if !b.Cfg.Tailwind.Enabled {
		return ""
	}
	opts := b.tailwindOptions()
	twCfg := css.LoadTailwindConfigWithOptions(b.Root, opts)
	// v4 CSS-first config: directives from project CSS files plus those seen
	// in the authored CSS processed so far (covers CSS outside the scan dirs).
	d := css.NewTailwindDirectives(b.Root)
	var extras []string
	b.twMu.Lock()
	pages := make([]string, 0, len(b.twPages))
	for p := range b.twPages {
		pages = append(pages, p)
	}
	sort.Strings(pages)
	for _, p := range pages {
		if info := b.twPages[p]; info.dirs != nil {
			d.MergeFrom(info.dirs)
		}
		extras = append(extras, b.twPages[p].extras...)
	}
	b.twMu.Unlock()
	d.MergeFrom(css.ScanTailwindDirectives(b.Root, opts))
	res, err := css.GenerateTailwindDirectives(b.Root, twCfg, opts, d)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %sTailwind error:%s %v\n", cYellow, cReset, err)
		return ""
	}
	for _, w := range res.Warnings {
		b.warnOnce("Tailwind: " + w)
	}
	if opts.Strict && len(res.Unknown) > 0 {
		b.warnOnce("Tailwind strict: " + strconv.Itoa(len(res.Unknown)) + " class candidate(s) matched no utility: " + strings.Join(res.Unknown, ", "))
	}
	return appendMissingBlocks(res.CSS, extras)
}

// appendMissingBlocks appends the at-rule blocks whose header is not already
// present in sheet (used for @keyframes that @apply pulled into page CSS).
func appendMissingBlocks(sheet string, blocks []string) string {
	seen := map[string]bool{}
	compact := strings.Join(strings.Fields(sheet), "")
	for _, blk := range blocks {
		head := blk[:strings.IndexByte(blk, '{')+1]
		key := strings.Join(strings.Fields(head), "")
		if seen[key] || strings.Contains(compact, key) {
			continue
		}
		seen[key] = true
		if sheet != "" && !strings.HasSuffix(sheet, "\n") {
			sheet += "\n"
		}
		sheet += blk + "\n"
	}
	return sheet
}

func (b *Builder) writeGlobalCSS(mergedCSS string) string {
	cssFile := ""
	if mergedCSS != "" {
		processedCSS := mergedCSS
		// Inline @import directives before minification
		processedCSS = b.inlineImports(processedCSS)
		processedCSS, _ = b.processCSSDirectives(processedCSS)
		processedCSS = injectFontDisplay(processedCSS, b.Cfg.Fonts.DisplayValue())
		if b.Cfg.Fonts.PreloadEnabled() {
			b.globalFonts = collectFontPreloads(processedCSS, b.Cfg.Fonts.PreloadLimit)
			b.fontPreloadHTML = fontPreloadHTML(b.globalFonts, b.Cfg.BaseURLPath())
		}
		if b.Cfg.ShouldMinifyCSS() {
			processedCSS = css.Minify(processedCSS)
		} else {
			processedCSS = css.RemoveDuplicateDeclarations(processedCSS)
		}
		if strings.TrimSpace(processedCSS) != "" {
			processedBytes := []byte(processedCSS)
			cssHash := hashContent(processedBytes)
			cssFile = "styles." + cssHash + ".css"
			cssPath := filepath.Join(b.Cfg.OutDir, cssFile)
			_ = fsutil.WriteFileIfChanged(cssPath, processedBytes, 0644)
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
	// Memoize the expensive inline+directive+minify pipeline: many pages share
	// byte-identical CSS (the docs theme is linked on every page), so process
	// each distinct input once.
	type processed struct {
		file       string
		data       []byte
		twInfo     twPageInfo
		modernFile string
		modernData []byte
		legacyFile string
		legacyData []byte
		fonts      []string
	}
	memo := make(map[string]processed)
	var chromaCSS string
	chromaLoaded := false
	var codeTheme syntaxhighlight.Theme
	if b.Cfg.Markdown.CodeHighlight {
		// Resolve up front so an unknown theme name is reported even on a
		// build that renders no code.
		codeTheme = b.codeTheme()
	}

	// Per-variant stylesheets: in hybrid+userAgent mode the UA sibling should
	// download only the legacy signal rules (and the default document only the
	// modern rules), so split the hybrid stylesheet per variant.
	variants := b.legacyDocEnabled()

	write := func(file string, data []byte) {
		if file == "" || written[file] {
			return
		}
		_ = fsutil.WriteFileIfChanged(filepath.Join(b.Cfg.OutDir, file), data, 0644)
		written[file] = true
	}
	var lastFonts []string
	process := func(pageCss string) (string, []byte, twPageInfo) {
		processedCSS := b.inlineImports(pageCss)
		processedCSS, info := b.processCSSDirectives(processedCSS)
		processedCSS = injectFontDisplay(processedCSS, b.Cfg.Fonts.DisplayValue())
		lastFonts = nil
		if b.Cfg.Fonts.PreloadEnabled() {
			lastFonts = collectFontPreloads(processedCSS, b.Cfg.Fonts.PreloadLimit)
		}
		if b.Cfg.ShouldMinifyCSS() {
			processedCSS = css.Minify(processedCSS)
		} else {
			processedCSS = css.RemoveDuplicateDeclarations(processedCSS)
		}
		bs := []byte(processedCSS)
		if strings.TrimSpace(processedCSS) == "" {
			return "", nil, info
		}
		return "styles." + hashContent(bs) + ".css", bs, info
	}

	for _, r := range results {
		if r.CSS == "" && !pageRendersCode(r) {
			b.setTailwindPage(r.Page, twPageInfo{})
			continue
		}
		pageCss := r.CSS
		if b.Cfg.Markdown.CodeHighlight && pageRendersCode(r) {
			if !chromaLoaded {
				chromaCSS = syntaxhighlight.ThemeCSS(codeTheme)
				chromaLoaded = true
			}
			if chromaCSS != "" {
				pageCss = chromaCSS + "\n" + pageCss
			}
		}

		key := hashContent([]byte(pageCss))
		p, ok := memo[key]
		if !ok {
			p.file, p.data, p.twInfo = process(pageCss)
			p.fonts = lastFonts
			if variants && r.HasCSSSignals {
				modernRaw, legacyRaw := splitVariantCSS(pageCss)
				p.modernFile, p.modernData, _ = process(modernRaw)
				if p.modernFile != "" {
					p.modernFile = strings.TrimSuffix(p.modernFile, ".css") + ".modern.css"
				}
				p.legacyFile, p.legacyData, _ = process(legacyRaw)
				if p.legacyFile != "" {
					p.legacyFile = strings.TrimSuffix(p.legacyFile, ".css") + ".legacy.css"
				}
			}
			memo[key] = p
		}
		r.CSSFile = p.file
		r.FontURLs = p.fonts
		b.setTailwindPage(r.Page, p.twInfo)
		write(p.file, p.data)
		if p.modernFile != "" {
			r.CSSModernFile = p.modernFile
			write(p.modernFile, p.modernData)
		}
		if p.legacyFile != "" {
			r.CSSLegacyFile = p.legacyFile
			write(p.legacyFile, p.legacyData)
		}
	}
	return written
}

// codeTheme resolves the configured light and dark code themes. An unknown
// name is reported once (KR7150) and replaced by the default for its mode.
func (b *Builder) codeTheme() syntaxhighlight.Theme {
	th, unknown := syntaxhighlight.ResolveTheme(b.Cfg.Markdown.CodeTheme, b.Cfg.Markdown.CodeThemeDark)
	var ds []diag.Diagnostic
	for _, name := range unknown {
		ds = append(ds, diag.Diagnostic{
			File:     "krate.config.ts",
			Severity: diag.SeverityWarning,
			Code:     diag.CodeUnknownCodeTheme,
			Message:  fmt.Sprintf("unknown code theme %q; using the default for that mode", name),
			Hint:     "use a chroma theme name such as " + strings.Join(syntaxhighlight.SimilarThemes(name, 3), ", ") + ", or { light, dark } to pick one per mode",
		})
	}
	if len(ds) > 0 {
		printWarningsOnce(os.Stderr, ds)
	}
	return th
}

// pageRendersCode reports whether a page shows syntax-highlighted code, in
// which case the chroma stylesheet is needed on that page: its markup has a
// highlighted block, or its script highlights code itself (the browser
// highlighter writes into blocks the markup does not mark).
func pageRendersCode(r *PageResult) bool {
	return strings.Contains(r.HTML, "chroma") || strings.Contains(r.HydrationJS, highlightAPI)
}

// writeHTMLPages builds the layout wrapper, applies placeholders, minifies,
// and saves to disk in a single parallel step (Zero Disk-I/O Amplification Fix)
// cssFiles are extra global stylesheets (e.g. Tailwind) linked on every page in
// addition to each page's own stylesheet (r.CSSFile).
// durableStateScript returns the `<script>window.__KRATE_STATE__=...</script>`
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
			defer func() {
				if rec := recover(); rec != nil {
					b.pluginFailed(safe.Recover(rec))
				}
			}()

			pageDir := b.Cfg.OutDir
			if r.OutName != "." {
				pageDir = filepath.Join(b.Cfg.OutDir, r.OutName)
			}

			if !r.IsErrorPage {
				if err := os.MkdirAll(pageDir, 0755); err != nil {
					b.pluginFailed(fmt.Errorf("creating page dir %s: %w", pageDir, err))
					return
				}
			}

			// 0. Head hygiene: the page head precedes the layout head, so the
			// first <title>/meta/canonical per key wins; site.titleTemplate
			// shapes the document title the same way it shapes og:title.
			r.HeadHTML = templateHeadTitle(dedupeHead(r.HeadHTML), b.Cfg.SEO)

			// 1. Construct structural HTML wrapper in memory.
			// Site-global stylesheets (e.g. Tailwind) are linked first, then
			// the page's own stylesheet (r.CSSFile). Tailwind output is
			// layered, so page CSS wins over preflight and utilities by
			// layer precedence, and page `@layer base` rules follow preflight.
			pageCSS := append([]string{}, cssFiles...)
			if r.CSSFile != "" {
				pageCSS = append(pageCSS, r.CSSFile)
			}
			// UA siblings link a variant-only stylesheet when the build emitted
			// one (hybrid+userAgent); otherwise they share the page sheet.
			legacyPageCSS := pageCSS
			if r.CSSLegacyFile != "" {
				legacyPageCSS = append(append([]string{}, cssFiles...), r.CSSLegacyFile)
			}
			modernPageCSS := pageCSS
			if r.CSSModernFile != "" {
				modernPageCSS = append(append([]string{}, cssFiles...), r.CSSModernFile)
			}
			devBootstrap := ""
			if b.DevMode {
				devBootstrap = b.devBootstrapJSON()
			}
			runtimeHead := b.runtimeCommonHead()
			if im := b.islandImportMapHTML(); im != "" {
				runtimeHead = im + runtimeHead
			}
			// Fonts declared by the page's own CSS (not the global sheet).
			if pf := b.pageFontPreloads(r.FontURLs); pf != "" {
				runtimeHead += pf
			}
			// Only pages that call into the runtime load it; a static page of
			// a site with islands elsewhere ships no JavaScript.
			runtimeJSFile := runtimeJSFile
			if !pageNeedsRuntime(r) {
				runtimeJSFile = ""
			}
			chunkPreloads := dynamicChunkURLs(r.HydrationJS)
			chunkPreloads = append(chunkPreloads, lazyChunkPreloads(r.HydrationJS, b.Cfg.BaseURLPath())...)
			var html string
			if r.LoadingHTML != "" {
				html = generateHTMLWithLoading(r.HTML, r.HeadHTML, r.ScriptHTML, r.StyleHTML, r.LoadingHTML, pageCSS, r.JSFile, runtimeJSFile, r.OutName, b.DevMode, devBootstrap, runtimeHead, b.Cfg.BaseURLPath(), chunkPreloads...)
			} else {
				html = generateHTML(r.HTML, r.HeadHTML, r.ScriptHTML, r.StyleHTML, pageCSS, r.JSFile, runtimeJSFile, r.OutName, b.DevMode, devBootstrap, runtimeHead, b.Cfg.BaseURLPath(), chunkPreloads...)
			}

			// Opt-in UA variants: sibling documents so `krate serve` can send a
			// smaller, variant-only document to User-Agent-classified visitors.
			// The default document stays the full hybrid (works on any host).
			legacyHTML := ""
			modernHTML := ""
			if b.legacyDocEnabled() && r.HasCSSSignals && !r.IsErrorPage {
				// Swap the gated shim for the unconditional one (legacy) or drop
				// it entirely (modern) so each sibling ships only what it needs.
				baseScript := strings.ReplaceAll(r.ScriptHTML, hybridShimScriptTag(), "")
				legacyScriptHTML := baseScript + legacyShimScriptTag()
				if r.LoadingHTML != "" {
					legacyHTML = generateHTMLWithLoading(r.HTML, r.HeadHTML, legacyScriptHTML, r.StyleHTML, r.LoadingHTML, legacyPageCSS, r.JSFile, runtimeJSFile, r.OutName, b.DevMode, devBootstrap, runtimeHead, b.Cfg.BaseURLPath(), chunkPreloads...)
					modernHTML = generateHTMLWithLoading(r.HTML, r.HeadHTML, baseScript, r.StyleHTML, r.LoadingHTML, modernPageCSS, r.JSFile, runtimeJSFile, r.OutName, b.DevMode, devBootstrap, runtimeHead, b.Cfg.BaseURLPath(), chunkPreloads...)
				} else {
					legacyHTML = generateHTML(r.HTML, r.HeadHTML, legacyScriptHTML, r.StyleHTML, legacyPageCSS, r.JSFile, runtimeJSFile, r.OutName, b.DevMode, devBootstrap, runtimeHead, b.Cfg.BaseURLPath(), chunkPreloads...)
					modernHTML = generateHTML(r.HTML, r.HeadHTML, baseScript, r.StyleHTML, modernPageCSS, r.JSFile, runtimeJSFile, r.OutName, b.DevMode, devBootstrap, runtimeHead, b.Cfg.BaseURLPath(), chunkPreloads...)
				}
			}

			// finish applies the head injections + minify shared by the default
			// and legacy documents. CSP hashes the fully assembled document, so
			// each variant's inline scripts are allow-listed individually.
			finish := func(h string) string {
				// 1. Document language: per-page lang wins (e.g. docs locales),
				// then site.locale; otherwise the default "en" stands.
				lang := r.Lang
				if lang == "" {
					lang = b.Cfg.SEO.Locale
				}
				if lang != "" {
					h = strings.Replace(h, "<html lang=\"en\">", "<html lang=\""+escape.HTMLAttr(lang)+"\">", 1)
				}
				// 1b. Site-wide <head> injection (head + analytics), added before
				// CSP so inline analytics scripts get hash-allow-listed.
				if tags := b.globalHeadHTML(h); tags != "" {
					h = strings.Replace(h, "</head>", tags+"</head>", 1)
				}
				// 2. Durable SSR state (window.__KRATE_STATE__): a page-level
				// payload that persisted signals prefer on hydration, so the
				// hydrated value matches what the server rendered.
				if state := b.durableStateScript(); state != "" {
					h = strings.Replace(h, "</head>", state+"</head>", 1)
				}
				// 2a. Segment pages (404/500) are not content: keep them out of
				// search indexes unless the page sets its own robots meta.
				if r.IsErrorPage && !hasRobotsMeta(r.HeadHTML) {
					h = strings.Replace(h, "</head>", `<meta name="robots" content="noindex">`+"</head>", 1)
				}
				// 2b. SEO meta tag injection (canonical, OG, Twitter Card)
				if b.Cfg.SEO.BaseURL != "" || b.Cfg.SEO.Description != "" {
					seoTags := generateSEOTags(r.HeadHTML, routeFromOutName(r.OutName), b.Cfg.SEO, seoCtx{BasePath: b.Cfg.BaseURLPath(), ErrorPage: r.IsErrorPage})
					if seoTags != "" {
						h = strings.Replace(h, "</head>", seoTags+"</head>", 1)
					}
				}
				// 3. Minify code in memory
				if b.Cfg.ShouldMinifyHTML() {
					h = minifyHTMLWith(h, b.linkedWhiteSpaceRules(h))
				}
				// 4. CSP meta tag injection. Hash the FINAL document (after
				// minification rewrote inline content) so every inline
				// script/style/style attribute is allow-listed.
				if b.Cfg.CSP.Enabled {
					cspMeta := generateCSPMeta(h, h, r.HydrationJS, b.Cfg.CSP.Directive)
					if cspMeta != "" {
						h = strings.Replace(h, "</head>", cspMeta+"</head>", 1)
					}
				}
				return h
			}

			html = finish(html)
			r.FinalHTML = html

			// Single write to physical media
			// Error pages (404/500) are written directly at the output root as .html
			var htmlPath string
			if r.IsErrorPage {
				htmlPath = filepath.Join(b.Cfg.OutDir, filepath.FromSlash(segmentHTMLRel(r.Page, b.Cfg.PagesDir)))
			} else {
				htmlPath = filepath.Join(pageDir, "index.html")
			}
			if err := fsutil.WriteFileIfChanged(htmlPath, []byte(html), 0644); err != nil {
				b.pluginFailed(fmt.Errorf("writing %s: %w", htmlPath, err))
				return
			}
			if legacyHTML != "" {
				if err := fsutil.WriteFileIfChanged(filepath.Join(pageDir, "index.legacy.html"), []byte(finish(legacyHTML)), 0644); err != nil {
					b.pluginFailed(fmt.Errorf("writing legacy document for %s: %w", r.Page, err))
					return
				}
			}
			if modernHTML != "" {
				if err := fsutil.WriteFileIfChanged(filepath.Join(pageDir, "index.modern.html"), []byte(finish(modernHTML)), 0644); err != nil {
					b.pluginFailed(fmt.Errorf("writing modern document for %s: %w", r.Page, err))
					return
				}
			}
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

// importResolver resolves imports with this build's tsconfig path aliases, for
// code that has no Bundler or Bundle in hand.
func (b *Builder) importResolver() *bundler.ImportResolver {
	return bundler.NewImportResolver(b.cfgPathAliasPrefixes(), b.cfgPathAliasTargets(), b.Cfg.TSBaseDir)
}

// newBundler creates a bundler configured with this build's path aliases,
// component tiers, and virtual modules (codegen'd `krate/content`).
func (b *Builder) newBundler() *bundler.Bundler {
	bnd := bundler.New(b.Root)
	bnd.SetPathAliases(b.cfgPathAliasPrefixes(), b.cfgPathAliasTargets(), b.Cfg.TSBaseDir)
	bnd.SetBaseURL(b.Cfg.TSBaseURL)
	bnd.SetServerComponents(b.Cfg.ServerComponents, b.Cfg.RuntimeComponents, b.Cfg.ServerDirs, b.Cfg.RuntimeDirs)
	bnd.SetTSOptions(tsstrip.Options{
		ExperimentalDecorators:  b.Cfg.TSCompilerOptions.ExperimentalDecorators,
		UseDefineForClassFields: b.Cfg.TSCompilerOptions.UseDefineForClassFields,
		Define:                  b.publicEnvDefines(),
	})
	if b.contentMods != nil {
		bnd.SetVirtualModules(b.contentMods)
	}
	if mods := b.actionModules(); len(mods) > 0 {
		bnd.SetActionModules(mods)
		bnd.SetActionBasePath(b.Cfg.BaseURLPath())
	}
	bnd.SetModuleCache(b.moduleCache)
	bnd.SetDevMode(b.DevMode)
	bnd.SetTailwindImports(b.tailwindImportMode())
	bnd.SetMarkdownConfig(b.Cfg.Markdown)
	bnd.SetMarkdownBasePath(b.Cfg.BaseURLPath())
	return bnd
}

func (b *Builder) buildPage(page string) (*PageResult, string, error) {
	if kind, ok := segmentKindOfFile(page); ok && kind == SegmentError {
		return b.buildErrorPage(page)
	}
	return b.buildPageOnce(page)
}

// buildErrorPage builds a segment error page. The first pass bakes
// placeholders for the error into plain HTML (the server fills them in per
// request); an error component that ships client code, or that transforms the
// error text at render time (which would break a placeholder), is built again
// reading the error the server injects, so hydration does not overwrite it and
// no placeholder text can leak.
func (b *Builder) buildErrorPage(page string) (*PageResult, string, error) {
	segmentVariants.Store(page, "static")
	defer segmentVariants.Delete(page)
	r, css, err := b.buildPageOnce(page)
	if err != nil || r == nil || (!r.HasJS && !errorPlaceholdersMangled(r.HTML+r.HeadHTML)) {
		return r, css, err
	}
	segmentVariants.Store(page, "client")
	prev := r.JSFile
	r2, css2, err2 := b.buildPageOnce(page)
	if err2 == nil && r2 != nil && prev != "" && prev != r2.JSFile {
		old := filepath.Join(b.Cfg.OutDir, filepath.FromSlash(r.OutName), prev)
		_ = os.Remove(old)
		_ = os.Remove(old + ".map")
	}
	return r2, css2, err2
}

func (b *Builder) buildPageOnce(page string) (*PageResult, string, error) {
	defer b.phase("page:total")()
	if err := b.contextErr(); err != nil {
		return nil, "", err
	}
	// The generated entry is written first: the disk cache hashes it as one of
	// the page's inputs.
	entryFile, err := b.segmentEntryFile(page)
	if err != nil {
		return nil, "", err
	}
	if pr, css, ok := b.tryDiskReplay(page); ok {
		return pr, css, nil
	}
	stopBundle := b.phase("page:bundle")
	bnd := b.newBundler()
	bundle, err := bnd.Bundle(entryFile)
	stopBundle()
	if err != nil {
		return nil, "", fmt.Errorf("bundling page %s: %w", page, err)
	}
	if err := b.contextErr(); err != nil {
		return nil, "", err
	}
	// Modules that call hooks of an npm React library run on Preact in an
	// island; the Krate lowering of their source is unused, so its warnings
	// are not shown either.
	reactIslands := detectReactIslandModules(bundle.Modules, bnd.Resolve)
	for i, m := range bundle.Modules {
		if m == nil || m.Program == nil || m.IsExternal || resolver.IsCompiledDependency(m.Path) {
			continue
		}
		if _, ok := reactIslands[m.Path]; ok {
			if len(m.Diagnostics) > 0 {
				quiet := *m
				quiet.Diagnostics = nil
				bundle.Modules[i] = &quiet
			}
		} else if ws := cssPropWarnings(m); len(ws) > 0 {
			noted := *m
			noted.Diagnostics = append(append([]diag.Diagnostic(nil), m.Diagnostics...), ws...)
			bundle.Modules[i] = &noted
		}
	}
	printModuleWarnings(os.Stderr, bundle.Modules)
	var pageWarnings []diag.Diagnostic
	for _, m := range bundle.Modules {
		pageWarnings = append(pageWarnings, m.Diagnostics...)
	}

	entryModule := findEntryModule(bundle.Modules)
	if entryModule == nil || entryModule.Program == nil {
		return nil, "", fmt.Errorf("page %s has no entry module (no default export found); a page must export a default component", page)
	}
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

	// Collect dependencies for partial reloads
	deps := []string{page}
	for _, mod := range bundle.Modules {
		if mod.Path != page {
			deps = append(deps, mod.Path)
		}
	}
	// Files the CSS was built from (@import targets, url() assets, composes
	// sources) are not modules but must invalidate the page when edited.
	deps = append(deps, bundle.CSSDeps...)
	// The modules an island bundles for Preact are not Krate source from here
	// on: the lowering, module-state and vendor passes would try to compile
	// their hooks and initializers. Their files stay in deps above.
	if len(reactIslands) > 0 {
		bundle.Modules = krateSideModules(bundle.Modules, entryModule, reactIslands, bnd.Resolve)
	}

	// Run AfterParse hooks
	parseCtx := &plugin.ParseHookCtx{
		Page:    page,
		Program: entryModule.Program,
	}
	b.runPageHook("AfterParse", page, parseCtx, func() error { return plugin.RunAfterParse(parseCtx) })
	// Plugins may hand back a replacement AST (JS plugins decode the astjson doc
	// they return; Go-style plugins swap ctx.Program directly). Point the entry
	// module at the final program so all downstream stages render the edited tree.
	entryModule.Program = parseCtx.Program

	// Universal transforms mutate shared cached ASTs. Run them for the whole
	// module graph before any pass reads those ASTs, so concurrent page builds
	// serialize on each module's sync.Once instead of racing a reader.
	if err := b.ensureTransformedModules(bundle.Modules); err != nil {
		return nil, "", err
	}

	// Detect rendering mode (SSR/ISR/Streaming) from AST exports + <Suspense>
	// usage, falling back to the page's data-cache reads (tagged/timed fetch).
	renderMode, revalidate, dataUsage := resolvePageRenderMode(page, entryModule, bundle.Modules)
	explicitStatic := renderMode == RenderSSG
	declaredMode := renderMode
	pageLang := detectPageLang(entryModule.Program)

	// If the page imports any runtime components, force streaming mode.
	// Runtime components (*.runtime.tsx) must be rendered at request time,
	// so the page must go through the streaming SSR pipeline.
	if renderMode == RenderSSG {
		for _, mod := range bundle.Modules {
			if mod.ComponentClass == bundler.ComponentClassRuntime {
				renderMode = RenderStreaming
				fmt.Fprintf(os.Stderr, "  %s*%s %s imports runtime component -> streaming\n", cCyan, cReset, filepath.Base(page))
				break
			}
		}
	}

	// A page that imports a runtime-tier Go component must stream: its region is
	// rendered per request by the Go host (via /__krate/gorender).
	pageGoComponents := b.collectGoComponents(bundle, b.Root)
	for _, ref := range pageGoComponents {
		if ref.Tier == irtree.TierRuntime && renderMode == RenderSSG {
			renderMode = RenderStreaming
			break
		}
	}

	// Global streaming override: if configured, all *static* pages stream.
	// Explicit ssr/isr opts win - forcing them to streaming would silently
	// defeat a page author's per-page config.
	if b.Cfg.SSR.Streaming && renderMode == RenderSSG {
		renderMode = RenderStreaming
		fmt.Fprintf(os.Stderr, "  %s*%s %s -> streaming (global override)\n", cCyan, cReset, filepath.Base(page))
	}

	// Static output mode disables request-time rendering entirely: a page that
	// would have been SSR/ISR/streaming is downgraded to SSG. Dynamic routes
	// are additionally closed to the params generateStaticParams returns (see
	// dynamicParams below).
	staticMode := strings.EqualFold(b.Cfg.Output, "static")
	staticWhy := "the page pins itself static"
	if staticMode && renderMode != RenderSSG {
		fmt.Fprintf(os.Stderr, "  %s*%s %s -> ssg (static output mode)\n", cCyan, cReset, filepath.Base(page))
		renderMode = RenderSSG
		staticWhy = "output: \"static\""
		explicitStatic = true
	}
	if renderMode == RenderSSG && explicitStatic {
		if d, ok := staticDataCacheWarning(page, entryModule.SourceCode, dataUsage, staticWhy); ok {
			printWarningsOnce(os.Stderr, []diag.Diagnostic{d})
			pageWarnings = append(pageWarnings, d)
		}
	}

	pageBase := strings.TrimSuffix(filepath.Base(page), filepath.Ext(page))
	isSegment := isSegmentPageBase(pageBase)
	if isSegment && renderMode != RenderSSG {
		// Segment pages are prerendered once and picked per request by the
		// server, so a request-time mode cannot apply to them.
		if declaredMode != RenderSSG || importsRuntimeComponent(bundle.Modules) {
			d := diag.New(page, 1, 1, filepath.Base(page)+" is a segment page and is always prerendered; its "+renderMode.String()+" rendering mode is ignored",
				"read request data in client code, or use a normal route for request-time content", "").WithCode(diag.CodeSegmentRequestMode)
			d.Severity = diag.SeverityWarning
			printWarningsOnce(os.Stderr, []diag.Diagnostic{d})
			pageWarnings = append(pageWarnings, d)
		}
		renderMode = RenderSSG
		revalidate = 0
	}

	isDynRoute := isDynamicRoute(page, b.Cfg.PagesDir)
	allowDynamic := dynamicParamsAllowed(entryModule.Program, staticMode)
	// A dynamic route is "static only" when it must not be served for
	// arbitrary params. Only meaningful for dynamic routes.
	staticOnly := isDynRoute && !allowDynamic && !isSegment

	stopCompile := b.phase("page:compile")

	// New pipeline: Annotate -> Build IR -> Emit
	ann := annotator.Annotate(entryModule.Program, b.Cfg, page, entryModule.SourceCode)
	extraPrograms := moduleSources(bundle, entryModule)
	annotator.MergeModuleFunctions(ann, extraPrograms)
	annotator.MergeImportAliases(ann, extraPrograms, annotator.ModuleSource{Program: entryModule.Program, Path: entryModule.Path, RawSource: entryModule.SourceCode, Resolver: bundle.Resolver})
	// Re-classify tiers for any newly discovered components
	annotator.ReclassifyTiers(ann, b.Cfg)
	serverPage, serverErr := b.applyServerComponentMode(page, entryModule, ann, staticMode, &renderMode, &revalidate)
	if serverErr == nil && !serverPage {
		serverErr = topLevelAwaitError(entryModule, extraPrograms)
	}
	if serverErr != nil {
		return nil, "", serverErr
	}
	// forbidden() and unauthorized() only run when the page renders per request.
	if renderMode == RenderSSG && !serverPage {
		for _, mod := range bundle.Modules {
			if strings.Contains(filepath.ToSlash(mod.Path), "/node_modules/") {
				continue
			}
			if d := warnStaticAccessDenied(mod.Path, mod.Program, mod.SourceCode, "page"); d != nil {
				printAccessDeniedWarning(d)
				pageWarnings = append(pageWarnings, *d)
				break
			}
		}
	}
	if serverPage && len(reactIslands) > 0 {
		return nil, "", reactIslandDiag(firstReactIsland(reactIslands), "a server-rendered page")
	}
	if errs := annotator.CheckInteractiveTierComponents(ann, bundle.ActionImports); len(errs) > 0 {
		return nil, "", errs[0]
	}
	intercept := cachedInterceptForPage(page, b.Cfg.PagesDir)
	clientBoundaries, cbErr := b.prepareClientBoundaries(bundle, ann, serverPage)
	if cbErr != nil {
		return nil, "", cbErr
	}
	var (
		tree       *irtree.ComponentTree
		emitResult *renderer.EmitResult
		choiceCSS  string
		goBaked    map[string]string
		regions    []Region
	)
	externalByLocal, externalImports, localImports := entryExternalImports(entryModule.Program)
	// A path alias ("@/components/X") looks like a package scope, so the page's
	// own import list already holds it as a package binding; the island
	// binding replaces it.
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
		// An async page renders entirely per request in the SSR sidecar: no
		// build-time body and no hydration code. The shell keeps only the
		// layout around an empty coarse page region.
		tree = &irtree.ComponentTree{}
		emitResult = &renderer.EmitResult{}
	} else {
		pageOpts := b.irBuildOptions()
		if intercept != nil {
			// An intercepting slot page hydrates next to the page it overlays,
			// so its slot ids are namespaced like a layout's.
			pageOpts.IDPrefix = b.layoutIDPrefix(page)
		}
		applyModuleBindings(&pageOpts, entryModule.Path, entryModule.Program, extraPrograms)
		pageOpts.ExternalImports = externalByLocal
		pageOpts.LocalComponentImports = localImports
		pageOpts.GoComponents = pageGoComponents
		pageOpts.ActionURLs = bundle.ActionImports
		if isDynamicRoute(page, b.Cfg.PagesDir) {
			pageOpts.PageParams = dynamicRouteSentinels(extractParamNames(page, b.Cfg.PagesDir))
		}
		if _, isSeg := segmentKindOfFile(page); !isSeg {
			pageOpts.RoutePath = routePathFor(page, b.Cfg.PagesDir, pageOpts.PageParams)
		}
		tree = irtree.BuildWithOptions(entryModule.Program, ann, pageOpts)
		// CSS primitive declarations that cannot be compiled to CSS are hard errors
		// (there is no fallback to client signals).
		if len(tree.Errors) > 0 {
			return nil, "", renderErrors(page, tree.Errors)
		}
		printTreeWarnings(os.Stderr, tree.Warnings)
		pageWarnings = append(pageWarnings, tree.Warnings...)
		// CSS stylesheets are generated during IR construction (one shared,
		// instance-agnostic stylesheet per scope). Append them to the page's own CSS
		// so they land in the hashed external stylesheet.
		choiceCSS = tree.CSSSignalsCSS
		// Dynamic-route templates are built once and served for every matching URL,
		// so bind each [param] to a replaceable sentinel the server substitutes per
		// request (see serve.go applyDynamicRouteParams). Mirrors injectStaticParams
		// and only affects static/server roots; generateStaticParams pages are built
		// separately with concrete values.
		if isDynamicRoute(page, b.Cfg.PagesDir) {
			injectDynamicRoutePlaceholders(tree, extractParamNames(page, b.Cfg.PagesDir))
		}
		emitter := renderer.NewEmitter()
		emitter.CodeTheme = b.Cfg.Markdown.CodeTheme
		emitter.IconResolver = b.iconResolver
		emitter.EvalJS = b.jsExprEvaluator()
		emitter.GoComponents = pageOpts.GoComponents
		emitResult = emitter.Emit(tree)
		tree, emitResult = applyBoundaryFallbacks(page, tree, emitResult, pageOpts, func(opts irtree.BuildOptions) (*irtree.ComponentTree, *renderer.EmitResult) {
			t := irtree.BuildWithOptions(entryModule.Program, ann, opts)
			if isDynamicRoute(page, b.Cfg.PagesDir) {
				injectDynamicRoutePlaceholders(t, extractParamNames(page, b.Cfg.PagesDir))
			}
			em := renderer.NewEmitter()
			em.CodeTheme = b.Cfg.Markdown.CodeTheme
			em.IconResolver = b.iconResolver
			em.EvalJS = b.jsExprEvaluator()
			em.GoComponents = opts.GoComponents
			return t, em.Emit(t)
		})
		renderer.EmitMeta(tree, emitResult)
		warnings := emitResult.Warnings
		if isDynRoute {
			warnings = withoutTemplateSlotWarnings(warnings)
		}
		printTreeWarnings(os.Stderr, warnings)
		pageWarnings = append(pageWarnings, warnings...)
		// Render imported Go components (server tier, slot path + SSREval'd) via the
		// Go host in one batch, then inline their HTML at the emitted markers.
		if len(emitResult.GoRenders) > 0 {
			rendered, gerr := b.renderGoComponents(emitResult.GoRenders)
			if gerr != nil {
				return nil, "", gerr
			}
			emitResult.HTML = substituteGoRenders(emitResult.HTML, rendered)
			goBaked = goBakedMap(emitResult.GoRenders, rendered)
		}
		// Regions are enumerated after emit so runtime Go components can carry their
		// build-rendered children (EmitResult.RuntimeChildren) in RegionMeta.Props.
		regions = enumerateRegions(tree)
		attachRuntimeChildren(regions, emitResult.RuntimeChildren)
		for i := range regions {
			regions[i].Revalidate = regionRevalidate(b.Root, regions[i].SourcePath)
		}
	}
	stopCompile()
	if len(clientBoundaries) > 0 {
		loader, lerr := b.islandLoaderHTML()
		if lerr != nil {
			return nil, "", lerr
		}
		emitResult.ScriptHTML += loader
	}

	if len(emitResult.Errors) > 0 {
		return nil, "", renderErrors(page, b.noteSegmentErrorPage(page, emitResult.Errors))
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
	b.runPageHook("AfterRender", page, renderCtx, func() error { return plugin.RunAfterRender(renderCtx) })
	// Apply plugin modifications back
	emitResult.HTML = renderCtx.HTML
	emitResult.HeadHTML = renderCtx.HeadHTML
	bundle.CSS = renderCtx.RawCSS
	metaSegs, metaPageFiles := b.metadataSegments(page)
	{
		var metaParams map[string]string
		if isDynRoute {
			metaParams = dynamicRouteSentinels(extractParamNames(page, b.Cfg.PagesDir))
		}
		requestTime := renderMode == RenderSSR || renderMode == RenderISR
		emitResult.HeadHTML += b.pageMetadataHeadFor(page, entryModule.SourceCode, requestTime, metaParams, bnd, metaSegs, metaPageFiles)
	}

	// Run AfterMarkdownParse hooks for markdown pages (plugins can modify the
	// rendered HTML). Markdown pages flow through the normal bundler/emitter
	// pipeline - the bundler synthesizes an MDX-style TSX bundle (bundler.go
	// .md/.mdx branch) - so the hook runs against the fully emitted markup.
	if strings.HasSuffix(page, ".md") || strings.HasSuffix(page, ".mdx") {
		outName := pageToOutput(page, b.Cfg.PagesDir)
		mdCtx := &plugin.MarkdownHookCtx{
			Page:  page,
			HTML:  emitResult.HTML,
			Route: outName,
		}
		b.runPageHook("AfterMarkdownParse", page, mdCtx, func() error { return plugin.RunAfterMarkdownParse(mdCtx) })
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

	var (
		layoutCSS        string
		layoutFiles      []string
		layoutHasSignals bool
		routeChain       []routeChainEntry
		fragmentInfo     string
	)
	if intercept != nil {
		// A fragment: only the slot content, bracketed like the live slot.
		scope := b.layoutIDPrefix(page)
		emitResult.HTML = "<!--ks:" + intercept.Slot.Key + "-->" + emitResult.HTML + "<!--/ks:" + intercept.Slot.Key + "-->"
		for i := range emitResult.Signatures {
			emitResult.Signatures[i].RouteScope = scope
		}
		fragmentInfo = fragmentInfoScript(intercept.Slot.Key, scope, intercept.Page.Target)
	} else {
		var layoutErr error
		layoutCSS, layoutFiles, layoutHasSignals, routeChain, layoutErr = b.applyLayoutStack(page, emitResult)
		if layoutErr != nil {
			return nil, "", layoutErr
		}
	}
	// Layouts come first (outermost first) so page CSS wins ties.
	bundle.CSS = b.dedupeModuleCSS(page, layoutCSS+bundle.CSS, bundle.CSSModules)
	// Generated CSS signal rules land in the page's own hashed stylesheet, after
	// layout CSS so author styles can override the defaults.
	if choiceCSS != "" {
		bundle.CSS += choiceCSS
		// Legacy/hybrid modes drive the class-based stylesheet with a scoped
		// shim; modern mode stays zero-JS.
		emitResult.ScriptHTML += b.compatShimHTML()
		// No-JS legacy engines (no :has(), scripting off) need the reveal-all
		// fallback, since the shim cannot run.
		if b.needsNoJSFallback() {
			emitResult.StyleHTML += noJSFallbackHTML(tree.NoJSFallbackCSS)
		}
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

	emitResult.ScriptHTML += routeInfoScript(routeChain, emitResult.Signatures) + fragmentInfo + pageRouteScript(page, b.Cfg.PagesDir)

	outName := pageToOutput(page, b.Cfg.PagesDir)
	pageDir := filepath.Join(b.Cfg.OutDir, outName)
	relSrc, _ := filepath.Rel(b.Root, page)

	// A static-only dynamic route has no fallback template: skip all page-dir
	// output for the canonical `[param]` result. Concrete pages expanded from
	// generateStaticParams are built separately and write normally.
	if staticOnly {
		b.recordDeps(page, deps)
		pageCSS := b.pageCSSAfterDCE(renderMode, bundle.CSS, bundle.CSSModules, emitResult.HTML)
		return &PageResult{
			Page:              page,
			OutName:           outName,
			HTML:              emitResult.HTML,
			HeadHTML:          emitResult.HeadHTML,
			ScriptHTML:        emitResult.ScriptHTML,
			StyleHTML:         emitResult.StyleHTML,
			HasCSS:            pageCSS != "",
			HasCSSSignals:     tree.HasCSSSignals || layoutHasSignals,
			IsErrorPage:       isSegment,
			Warnings:          pageWarnings,
			CSS:               pageCSS,
			UsedCSS:           emitResult.UsedCSS,
			UsedFuncs:         emitResult.UsedFuncs,
			Mode:              renderMode,
			Revalidate:        revalidate,
			Lang:              pageLang,
			SourcePath:        relSrc,
			Regions:           regions,
			DynamicParams:     allowDynamic,
			StaticOnly:        true,
			IsDynamicTemplate: true,
			Program:           entryModule.Program,
			GoBaked:           goBaked,
			ServerPage:        serverPage,
			ServerExports:     serverExportsOf(regions),
			MetadataSegments:  metaSegs,
			MetadataPageFiles: metaPageFiles,
			ClientBoundaries:  clientBoundaries,
		}, bundle.CSS, nil
	}

	if !isSegment {
		if err := os.MkdirAll(pageDir, 0755); err != nil {
			return nil, "", fmt.Errorf("creating page dir: %w", err)
		}
	}

	jsFile := ""
	hydrationJS := ""
	var jsBytes []byte
	hasJS := false
	var pageProviders *islandProviders
	if len(tree.ExternalComponents) > 0 {
		pageProviders = b.pageProviders(page)
	}
	islandBoot, islandJS, islandSSR, islandStyles, islandErr := b.buildIslands(
		page,
		tree.ExternalComponents,
		externalImports,
		entryLocalImports(entryModule.Program),
		filepath.Dir(entryModule.Path),
		pageProviders,
	)
	if islandErr != nil {
		return nil, "", islandErr
	}
	if islandStyles != nil {
		// Stylesheets imported by compiled package islands join the page CSS and
		// the page's dependencies like any other imported stylesheet.
		bundle.CSS += islandStyles.CSS
		for src, url := range islandStyles.Assets {
			bundle.AssetFiles[src] = url
		}
		deps = append(deps, islandStyles.Deps...)
	}
	if len(islandSSR) > 0 {
		emitResult.HTML = injectIslandSSR(emitResult.HTML, islandSSR)
	}
	emitResult.IslandBootJS = islandBoot
	// Request-time pages are rendered by the server, which highlights their
	// code itself; only static pages highlight in the browser.
	browserHighlight := renderMode == RenderSSG && strings.Contains(emitResult.HTML, highlightMarkup)
	needsHydrate := len(emitResult.Signatures) > 0 || islandJS != "" || islandBoot != "" || strings.Contains(emitResult.HTML, "data-krate-portal") || browserHighlight
	// Client-side JS scanned by CSS DCE for classes applied after hydration.
	dceJS := append(lazyChunkSources(emitResult.LazyChunks), islandJS)
	dceJS = append(dceJS, b.clientChunkSources(clientBoundaries)...)
	if needsHydrate {
		emitResult.TrackSources = b.Cfg.Sourcemap
		rawJS := strings.TrimSpace(renderer.GenerateNewHydrationJS(emitResult))
		if renderMode == RenderSSG {
			rawJS = b.attachHighlight(rawJS, emitResult.HTML)
		}
		b.warnShippedCollections(page, entryModule.Program, rawJS)
		dceJS = append(dceJS, rawJS)
		rawJS = b.writeLazyChunks(rawJS, emitResult.LazyChunks, pageDir, outName)
		if islandJS != "" {
			rawJS = strings.TrimSpace(rawJS + "\n" + islandJS)
		}
		if rawJS != "" {
			rawJS = neutralizeClientEnv(rawJS)
			// npm functions called by client code are bundled into the script.
			vendor := collectVendorImports(bundle)
			for _, rb := range b.routeStack(page) {
				if lv, ok := layoutVendorImports.Load(rb.Path); ok {
					vendor = append(vendor, lv.([]vendorImport)...)
				}
			}
			var verr error
			if rawJS, verr = b.withVendorImports(rawJS, vendor, tree.ExternalComponents); verr != nil {
				return nil, "", verr
			}
			hasJS = true
			// Source markers become a map from the generated code back to the
			// original .tsx positions.
			inputMap := ""
			if len(emitResult.SrcMarks) > 0 {
				rawJS, inputMap = buildHydrationMap(rawJS, emitResult.SrcMarks, pageDir)
			}
			finalJS := rawJS
			mapJSON := ""
			if b.shouldMinifyJS() {
				if b.Cfg.Sourcemap && inputMap != "" {
					finalJS, mapJSON = minifyJSWithInputMap(rawJS, inputMap)
				} else if b.Cfg.Sourcemap {
					// Minify with esbuild's own map so the minified output can be
					// decoded back to the generated code. The hydration bundle is
					// compiler-generated (no literal user TSX), so `sources` labels
					// it as such rather than pretending to be the page source.
					finalJS, mapJSON = minifyJSWithMap(rawJS, "krate-hydration.js")
				} else {
					finalJS = minifyJS(rawJS)
				}
			}

			// Write page-specific hydration code only (runtime is in a shared chunk)
			finalJS = strings.TrimSpace(finalJS)
			jsHash := hashContent([]byte(finalJS))
			jsFile = "index." + jsHash + ".js"
			finalJS = substituteImportMetaURL(finalJS, outName, jsFile, b.Cfg.BaseURLPath())
			jsPath := filepath.Join(pageDir, jsFile)
			_ = fsutil.WriteFileIfChanged(jsPath, []byte(finalJS), 0644)

			if b.Cfg.Sourcemap {
				sm := mapJSON
				if sm == "" && inputMap != "" {
					sm = inputMap
				}
				if sm == "" {
					// Unminified output: emit a valid line-level map with the
					// generated code embedded so devtools shows a coherent file.
					sm = generateSourcemap(finalJS, "krate-hydration.js", finalJS)
				}
				_ = fsutil.WriteFileAtomic(jsPath+".map", []byte(sm), 0644)
				finalJS = appendSourceMappingURL(finalJS, jsFile+".map")
				_ = fsutil.WriteFileIfChanged(jsPath, []byte(finalJS), 0644)
			}

			if b.DevMode {
				pruneStaleHydration(pageDir, jsFile)
			}

			// Keep the CSP hash and any other ingest in sync with the bytes
			// actually written (which may differ from hydrationJS if
			// import.meta.url was substituted or a sourcemap comment appended).
			hydrationJS = finalJS
			jsBytes = []byte(finalJS)
		} else {
			hydrationJS = ""
		}
	}

	if err := b.writeAssetFiles(bundle.AssetFiles); err != nil {
		return nil, "", fmt.Errorf("writing assets for %s: %w", page, err)
	}
	b.registerWorkers(bundle.WorkerFiles, bundle.WorkerEsm)
	b.registerDynamicChunks(bundle.DynImportFiles)

	printResult(outName, jsFile, pageBase)

	loadingHTML := b.renderLoadingComponent(page)

	pageCSS := b.pageCSSAfterDCE(renderMode, bundle.CSS, bundle.CSSModules, emitResult.HTML, dceJS...)

	// Return data structures without triggering an intermediate disk write
	result := &PageResult{
		Page:          page,
		OutName:       outName,
		HTML:          emitResult.HTML,
		HeadHTML:      emitResult.HeadHTML,
		ScriptHTML:    emitResult.ScriptHTML,
		StyleHTML:     emitResult.StyleHTML,
		HydrationJS:   hydrationJS,
		HasJS:         hasJS,
		JSFile:        jsFile,
		HasCSS:        pageCSS != "",
		HasCSSSignals: tree.HasCSSSignals || layoutHasSignals,
		IsErrorPage:   isSegment,
		Warnings:      pageWarnings,
		CSS:           pageCSS,
		UsedCSS:       emitResult.UsedCSS,
		UsedFuncs:     emitResult.UsedFuncs,
		LoadingHTML:   loadingHTML,

		Mode:       renderMode,
		Revalidate: revalidate,
		Lang:       pageLang,
		SourcePath: relSrc,
		Regions:    regions,

		DynamicParams:     allowDynamic,
		StaticOnly:        staticOnly,
		IsDynamicTemplate: isDynRoute,
		Program:           entryModule.Program,
		GoBaked:           goBaked,
		ServerPage:        serverPage,
		ServerExports:     serverExportsOf(regions),
		MetadataSegments:  metaSegs,
		MetadataPageFiles: metaPageFiles,
		ClientBoundaries:  clientBoundaries,
	}
	if renderMode != RenderSSG {
		result.IslandStubs = newIslandServerStubs(entryModule.Program, tree.ExternalComponents, islandSSR)
	}
	b.maybeStoreDiskPage(page, result, deps, bundle, jsBytes)
	return result, bundle.CSS, nil
}

// tryDiskReplay returns a cached PageResult for page when every input file is
// unchanged since it was written, replaying the page's disk side effects
// (hydration JS, asset copies, worker/chunk registration, dep graph).
func (b *Builder) tryDiskReplay(page string) (*PageResult, string, bool) {
	c := b.diskCache
	if c == nil || !c.enabled {
		return nil, "", false
	}
	// Dynamic-route templates bake param placeholders and are cheap; keep them
	// off the cache to avoid replaying placeholder injection.
	if isDynamicRoute(page, b.Cfg.PagesDir) {
		return nil, "", false
	}
	e, ok := c.load(page)
	if !ok || e.Key == "" {
		c.markMiss()
		return nil, "", false
	}
	key, _, _, ok := c.computeKey(e.Deps, b.routeSignature(page))
	if !ok || key != e.Key {
		c.markMiss()
		return nil, "", false
	}

	pageDir := filepath.Join(b.Cfg.OutDir, e.OutName)
	if !e.IsErrorPage {
		_ = os.MkdirAll(pageDir, 0755)
	}
	if len(e.JSBytes) > 0 && e.JSFile != "" {
		_ = fsutil.WriteFileIfChanged(filepath.Join(pageDir, e.JSFile), e.JSBytes, 0644)
	}
	if len(e.AssetFiles) > 0 {
		_ = b.writeAssetFiles(e.AssetFiles)
	}
	b.registerWorkers(e.WorkerFiles, trueSet(e.WorkerEsm))
	b.registerDynamicChunks(e.DynImports)
	b.setIslandEntries(page, e.IslandEntries)
	b.recordDeps(page, e.Deps)
	c.markHit()
	// A replayed page reports the warnings its cold build did (once per build).
	printWarningsOnce(os.Stderr, e.Warnings)

	return &PageResult{
		Page:          page,
		OutName:       e.OutName,
		HTML:          e.HTML,
		HeadHTML:      e.HeadHTML,
		ScriptHTML:    e.ScriptHTML,
		StyleHTML:     e.StyleHTML,
		HydrationJS:   e.HydrationJS,
		HasJS:         e.HasJS,
		JSFile:        e.JSFile,
		HasCSS:        e.HasCSS,
		HasCSSSignals: e.HasCSSSignals,
		IsErrorPage:   e.IsErrorPage,
		CSS:           e.CSS,
		UsedCSS:       setFromSlice(e.UsedCSS),
		UsedFuncs:     setFromSlice(e.UsedFuncs),
		LoadingHTML:   e.LoadingHTML,
		Mode:          RenderMode(e.Mode),
		Revalidate:    e.Revalidate,
		Lang:          e.Lang,
		SourcePath:    e.SourcePath,

		DynamicParams:     e.DynamicParams,
		StaticOnly:        e.StaticOnly,
		IsDynamicTemplate: e.IsDynamicTemplate,
		Warnings:          e.Warnings,
		// Program stays nil; checks re-parse the source (attachPrograms).
	}, e.CSS, true
}

// maybeStoreDiskPage persists a freshly built page result when caching is
// enabled and the page is a plain SSG page.
func (b *Builder) maybeStoreDiskPage(page string, r *PageResult, deps []string, bundle *bundler.Bundle, jsBytes []byte) {
	c := b.diskCache
	if c == nil || !c.enabled || bundle == nil {
		return
	}
	if r.Mode != RenderSSG || len(r.Regions) > 0 || r.Program == nil {
		return
	}
	if isDynamicRoute(page, b.Cfg.PagesDir) {
		return
	}
	absDeps := make([]string, 0, len(deps)+1)
	for _, d := range deps {
		if !filepath.IsAbs(d) {
			d = filepath.Join(b.Root, d)
		}
		absDeps = append(absDeps, filepath.Clean(d))
	}
	key, sorted, hashes, ok := c.computeKey(absDeps, b.routeSignature(page))
	if !ok {
		return
	}
	c.save(page, &diskPageEntry{
		Key:           key,
		OutName:       r.OutName,
		HTML:          r.HTML,
		HeadHTML:      r.HeadHTML,
		ScriptHTML:    r.ScriptHTML,
		StyleHTML:     r.StyleHTML,
		HydrationJS:   r.HydrationJS,
		JSFile:        r.JSFile,
		JSBytes:       jsBytes,
		CSS:           r.CSS,
		HasJS:         r.HasJS,
		HasCSS:        r.HasCSS,
		HasCSSSignals: r.HasCSSSignals,
		IsErrorPage:   r.IsErrorPage,
		UsedCSS:       sliceFromSet(r.UsedCSS),
		UsedFuncs:     sliceFromSet(r.UsedFuncs),
		LoadingHTML:   r.LoadingHTML,
		Mode:          int(r.Mode),
		Revalidate:    r.Revalidate,
		Lang:          r.Lang,
		SourcePath:    r.SourcePath,

		DynamicParams:     r.DynamicParams,
		StaticOnly:        r.StaticOnly,
		IsDynamicTemplate: r.IsDynamicTemplate,

		Warnings:    r.Warnings,
		Deps:        sorted,
		DepHashes:   hashes,
		AssetFiles:  bundle.AssetFiles,
		WorkerFiles: bundle.WorkerFiles,
		WorkerEsm:   sliceFromSet(bundle.WorkerEsm),
		DynImports:  bundle.DynImportFiles,

		IslandEntries: b.islandEntriesOf(page),
	})
}

func sliceFromSet(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func setFromSlice(s []string) map[string]bool {
	if len(s) == 0 {
		return nil
	}
	out := make(map[string]bool, len(s))
	for _, k := range s {
		out[k] = true
	}
	return out
}

func trueSet(s []string) map[string]bool { return setFromSlice(s) }

// buildRoute generates a full HTML page from a plugin-generated virtual route.
func (b *Builder) buildRoute(route plugin.Route) (result *PageResult, rawCSS string, err error) {
	outName := strings.Trim(route.Path, "/")
	if outName == "" {
		outName = "."
	}

	// Use our new shared pipeline
	// This works for plugins because we pass route.Data (the props) directly
	layoutRes, rawCSS, _, _, err := b.executeLayoutPipeline(route.Layout, route.Content, route.Data)
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
	// hasSignals records whether the layout (and its components) compiled any
	// CSS signal rules, so wrapped pages know to emit UA siblings/fallbacks.
	hasSignals bool
	// Client hydration metadata, merged into each wrapped page so the layout's
	// interactive components hydrate (their JS is generated alongside the page's).
	signatures     []irtree.ComponentSignature
	listComponents []*ast.FnDecl
	hasLinks       bool
	boundaryLink   *renderer.BoundaryLink
	deps           []string
	depTimes       map[string]time.Time
	// providers are the provider wrappers lifted out of the layout for the
	// islands of its pages (nil when there are none).
	providers *layoutProviders
}

// layoutIDPrefix returns a stable, collision-free namespace for a layout's
// compact slot IDs. It starts with "_" (outside the base62 alphabet the page
// builder uses) so a merged layout + page can never share a data-k ID. The
// hash covers the path relative to the project root so builds are identical
// across checkouts; the same id names the layout's route boundary
// (<!--kl:ID--> markers and the route scope of its hydration code).
func (b *Builder) layoutIDPrefix(layoutPath string) string {
	key := filepath.ToSlash(layoutPath)
	if rel, err := filepath.Rel(b.Root, layoutPath); err == nil && !strings.HasPrefix(rel, "..") {
		key = filepath.ToSlash(rel)
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return "_l" + strconv.FormatUint(uint64(h.Sum32()), 36) + "_"
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
	// One instant for this build: new Date() and Date.now() in static and
	// server components read it (SOURCE_DATE_EPOCH when set).
	jsvalue.StartBuildClock()
	layoutEmitCache.Clear()
	b.modCSS.clear()
	loadingEmitCache.Clear()
	layoutCache.Clear()
	routeDirSigCache.Clear()
	interceptCache.Clear()
	slotCache.Clear()
	// Re-arm the per-program transform guard for this build. The parsed module
	// cache persists across builds; re-running the (idempotent) transforms on
	// already-transformed ASTs is a no-op, and the guard keeps it race-free.
	b.transformOnce = sync.Map{}
	b.collectionSites = sync.Map{}
	// Go component parses are per-build.
	b.goParseCache = nil
	// Client boundary chunks are per-build: a dev rebuild must pick up edits
	// to a "use client" module or its imports.
	b.clientChunkMu.Lock()
	b.clientChunks = nil
	b.clientChunkMu.Unlock()
	// Filesystem-existence and import-resolution memos are per-build: a file
	// added/removed between dev rebuilds must not be served from a stale entry.
	// The parse cache is keyed by mtime+size, so it stays valid and is kept.
	b.moduleCache.ResetResolver()
	bundler.ResetResolveCache()
	// The resolver's stat/package.json memos are per-build too (shared by the
	// bundler and docs plugin).
	resolver.ResetCaches()
	// Runtime/dev bundle disk lookups are memoized per build.
	resetRuntimeBundleMemo()
	// Module warnings are printed once per build.
	resetModuleWarnings()
	// Baked collection-chain folds are per-build (a content edit changes the key).
	resetContentFoldCache()
}

var layoutEmitCache sync.Map
var loadingEmitCache sync.Map

// emitCacheLocks maps a layout/loading path to the mutex serializing its
// bundle->emit pipeline. Concurrent buildPage goroutines sharing the same layout
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
// dependencies - editing a component a layout imports must rebuild the pages the
// layout wraps, even though the layout file itself is unchanged.
func (b *Builder) executeLayoutPipeline(layoutPath string, content string, props map[string]string) (*renderer.EmitResult, string, []string, bool, error) {
	var css string

	if layoutPath == "" {
		return &renderer.EmitResult{HTML: content}, "", nil, false, nil
	}

	// Cache hit: reuse the layout skeleton and inject the page content. The
	// cache is only valid while every file in the layout's module graph is
	// unchanged; a stale entry (e.g. an imported component edited during a
	// long-lived `krate dev` process) is ignored and re-rendered.
	if cached, ok := layoutEmitCache.Load(layoutPath); ok {
		entry := cached.(*layoutEmitCacheEntry)
		if layoutCacheFresh(entry) {
			layoutProviderCache.Store(layoutPath, entry.providers)
			return &renderer.EmitResult{
				HTML:           strings.Replace(entry.html, "<!--__children__-->", content, -1),
				HeadHTML:       entry.headHTML,
				ScriptHTML:     entry.scriptHTML,
				StyleHTML:      entry.styleHTML,
				Signatures:     entry.signatures,
				ListComponents: entry.listComponents,
				HasLinks:       entry.hasLinks,
				BoundaryLink:   entry.boundaryLink,
			}, entry.css, entry.deps, entry.hasSignals, nil
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
			layoutProviderCache.Store(layoutPath, entry.providers)
			return &renderer.EmitResult{
				HTML:           strings.Replace(entry.html, "<!--__children__-->", content, -1),
				HeadHTML:       entry.headHTML,
				ScriptHTML:     entry.scriptHTML,
				StyleHTML:      entry.styleHTML,
				Signatures:     entry.signatures,
				ListComponents: entry.listComponents,
				HasLinks:       entry.hasLinks,
				BoundaryLink:   entry.boundaryLink,
			}, entry.css, entry.deps, entry.hasSignals, nil
		}
	}

	bnd := b.newBundler()
	layoutBundle, err := bnd.Bundle(layoutPath)
	if err != nil {
		return nil, "", nil, false, fmt.Errorf("bundling layout %s: %w", layoutPath, err)
	}
	// Every file the layout reads, including the modules of provider wrappers
	// lifted out below, invalidates it when edited.
	allLayoutDeps := layoutDepPaths(layoutPath, layoutBundle)
	var lifted *layoutProviders
	if isLayoutFile(layoutPath) {
		if lm := findEntryModule(layoutBundle.Modules); lm != nil {
			plan, notes := b.planLayoutProviders(layoutPath, lm, layoutBundle.Modules, bnd.Resolve)
			if plan != nil {
				if why := b.verifyProviderChain(plan.providers); why != nil {
					notes = append(notes, *why)
				} else {
					for i, m := range layoutBundle.Modules {
						if m == lm {
							layoutBundle.Modules[i] = plan.module
						}
					}
					layoutBundle.Modules = krateSideModules(layoutBundle.Modules, plan.module, nil, bnd.Resolve)
					lifted = plan.providers
				}
			}
			if lifted == nil && len(notes) > 0 {
				lifted = &layoutProviders{Layout: layoutPath, Notes: notes}
			}
			// Said now: when the wrapper then fails to compile as part of the
			// layout, this is the explanation that matters.
			printWarningsOnce(os.Stderr, notes)
		}
	}
	layoutProviderCache.Store(layoutPath, lifted)
	if islands := detectReactIslandModules(layoutBundle.Modules, bnd.Resolve); len(islands) > 0 {
		return nil, "", nil, false, reactIslandDiag(firstReactIsland(islands), "a layout")
	}
	layoutVendorImports.Store(layoutPath, collectVendorImports(layoutBundle))
	printModuleWarnings(os.Stderr, layoutBundle.Modules)

	b.modCSS.store(layoutPath, layoutBundle.CSSModules)
	layoutModule := findEntryModule(layoutBundle.Modules)
	if layoutModule == nil || layoutModule.Program == nil {
		return nil, "", nil, false, fmt.Errorf("layout module invalid or empty: %s (ensure the layout has a default export)", layoutPath)
	}
	if !hasDefaultExportStmt(layoutModule.Program) {
		return nil, "", nil, false, diag.Diagnostic{
			File:    layoutPath,
			Line:    1,
			Col:     1,
			Message: "no default export: a layout or template must default-export a component",
			Hint:    "add `export default function Layout({ children }) { return <>{children}</> }`",
		}
	}

	// The layout's full module graph is both its cache-invalidation key and a
	// dependency of every page it wraps.
	layoutDeps := allLayoutDeps

	if err := b.ensureTransformed(layoutModule.Program, layoutModule.Path); err != nil {
		return nil, "", nil, false, err
	}
	if isLayoutFile(layoutPath) {
		slots, serr := cachedSlots(filepath.Dir(layoutPath), b.Cfg.PagesDir)
		if serr != nil {
			return nil, "", nil, false, serr
		}
		if len(slots) > 0 {
			names := map[string]bool{}
			for _, sl := range slots {
				names[sl.Name] = true
			}
			rewriteSlotProps(layoutModule.Program, names)
		}
	}

	if isLayoutFile(layoutPath) {
		passLayoutDepth(layoutModule.Program, layoutURLDepth(layoutPath, b.Cfg.PagesDir))
	}
	ann := annotator.Annotate(layoutModule.Program, b.Cfg, layoutPath, layoutModule.SourceCode)
	extraLayoutPrograms := moduleSources(layoutBundle, layoutModule)
	for _, mp := range extraLayoutPrograms {
		if err := b.ensureTransformed(mp.Program, mp.Path); err != nil {
			return nil, "", nil, false, err
		}
	}
	annotator.MergeModuleFunctions(ann, extraLayoutPrograms)
	annotator.MergeImportAliases(ann, extraLayoutPrograms, annotator.ModuleSource{Program: layoutModule.Program, Path: layoutModule.Path, RawSource: layoutModule.SourceCode, Resolver: layoutBundle.Resolver})
	annotator.ReclassifyTiers(ann, b.Cfg)
	if err := asyncLayoutError(layoutPath, ann, layoutModule.SourceCode); err != nil {
		return nil, "", nil, false, err
	}
	printAccessDeniedWarning(warnStaticAccessDenied(layoutPath, layoutModule.Program, layoutModule.SourceCode, "layout"))
	opts := b.irBuildOptions()
	opts.IDPrefix = b.layoutIDPrefix(layoutPath)
	applyModuleBindings(&opts, layoutModule.Path, layoutModule.Program, extraLayoutPrograms)
	opts.GoComponents = b.collectGoComponents(layoutBundle, b.Root)
	opts.ActionURLs = layoutBundle.ActionImports
	tree := irtree.BuildWithOptions(layoutModule.Program, ann, opts)
	if len(tree.Errors) > 0 {
		return nil, "", nil, false, renderErrors(layoutPath, tree.Errors)
	}
	emitter := renderer.NewEmitter()
	emitter.CodeTheme = b.Cfg.Markdown.CodeTheme
	emitter.IconResolver = b.iconResolver
	emitter.EvalJS = b.jsExprEvaluator()
	emitter.GoComponents = opts.GoComponents
	emitResult := emitter.Emit(tree)
	renderer.EmitMeta(tree, emitResult)
	if len(emitResult.GoRenders) > 0 {
		rendered, gerr := b.renderGoComponents(emitResult.GoRenders)
		if gerr != nil {
			return nil, "", nil, false, gerr
		}
		emitResult.HTML = substituteGoRenders(emitResult.HTML, rendered)
	}

	if len(emitResult.Errors) > 0 {
		return nil, "", nil, false, renderErrors(layoutPath, emitResult.Errors)
	}

	// Compile-time reactive dependency validation. Surfaced as warnings so
	// dead signals / circular effects are caught before hydration ships.
	b.printReactiveDiags(reactive.Build(emitResult.Signatures).Validate())

	css = layoutBundle.CSS
	// CSS signal rules for the layout and its components are generated during IR
	// construction and appended to the layout's own module CSS.
	if tree.CSSSignalsCSS != "" {
		css += tree.CSSSignalsCSS
		emitResult.ScriptHTML += b.compatShimHTML()
		if b.needsNoJSFallback() {
			emitResult.StyleHTML += noJSFallbackHTML(tree.NoJSFallbackCSS)
		}
	}
	if tree.NeedsCSSARIA {
		emitResult.ScriptHTML += "<script>" + renderer.CSSARIAJS + "</script>"
	}

	boundaryLink := renderer.FindBoundaryLink(tree)
	layoutEmitCache.Store(layoutPath, &layoutEmitCacheEntry{
		boundaryLink:   boundaryLink,
		html:           emitResult.HTML,
		headHTML:       emitResult.HeadHTML,
		scriptHTML:     emitResult.ScriptHTML,
		styleHTML:      emitResult.StyleHTML,
		signatures:     emitResult.Signatures,
		listComponents: emitResult.ListComponents,
		hasLinks:       emitResult.HasLinks,
		css:            css,
		hasSignals:     tree.HasCSSSignals,
		deps:           layoutDeps,
		depTimes:       modTimesOf(layoutDeps),
		providers:      lifted,
	})

	// Inject page content at {children} slot
	return &renderer.EmitResult{
		HTML:           strings.Replace(emitResult.HTML, "<!--__children__-->", content, -1),
		HeadHTML:       emitResult.HeadHTML,
		ScriptHTML:     emitResult.ScriptHTML,
		StyleHTML:      emitResult.StyleHTML,
		Signatures:     emitResult.Signatures,
		ListComponents: emitResult.ListComponents,
		HasLinks:       emitResult.HasLinks,
		BoundaryLink:   boundaryLink,
	}, css, layoutDeps, tree.HasCSSSignals, nil
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

// modTimesOf stats each path, returning a path->modtime map used to detect
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

// New IR Tree Pipeline

// NewRenderPipeline runs the new annotator + irtree + emitter pipeline.
// It returns an EmitResult containing HTML + hydration metadata.
// This is an alternative to the old renderPage pipeline and can be
// tested side-by-side before full migration.
func (b *Builder) NewRenderPipeline(entryModule *bundler.Module, page string) (*renderer.EmitResult, error) {
	if entryModule == nil || entryModule.Program == nil {
		return nil, fmt.Errorf("entry module invalid or empty (ensure the page has a default export)")
	}

	ann := annotator.Annotate(entryModule.Program, b.Cfg, page, entryModule.SourceCode)

	opts := b.irBuildOptions()
	opts.ModuleConsts, opts.ModuleConstInits, opts.ModuleRun = mergeModuleConstants(entryModule.Program, nil)
	tree := irtree.BuildWithOptions(entryModule.Program, ann, opts)
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
		// Files under a parallel-route slot (@name) render inside their
		// layout; intercepting slot pages are added by findInterceptPages.
		if rel, err := filepath.Rel(dir, path); err == nil && pathHasSlot(rel) {
			return nil
		}
		if rel, err := filepath.Rel(dir, path); err == nil && isStrayAPIFile(filepath.ToSlash(rel)) {
			return nil // reported by findPagesStrays, never a page
		}
		if isPagesEndpointFile(base) {
			return nil // a prerendered endpoint, built by buildPagesEndpoints
		}
		if isMetadataRouteFile(base) {
			return nil // sitemap.ts, robots.ts, opengraph-image.tsx...: built by buildMetadataRoutes
		}
		if !strings.HasPrefix(base, "_") && !isRouteConventionFile(base) && !isUseServerFile(path) {
			pages = append(pages, path)
		}
		return nil
	})
	return pages, nil
}

// hasDefaultExportStmt reports whether a module has an `export default`.
func hasDefaultExportStmt(prog *ast.Program) bool {
	for _, stmt := range prog.Body {
		if exp, ok := stmt.(*ast.ExportStmt); ok && exp.Default {
			return true
		}
	}
	return false
}

// isRouteConventionFile reports whether a file name is a routing convention
// (loading UI, template) rather than a page: loading.tsx and template.tsx
// never become /loading or /template routes.
func isRouteConventionFile(base string) bool {
	for _, names := range [][]string{loadingNames, templateNames} {
		for _, n := range names {
			if base == n {
				return true
			}
		}
	}
	return false
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
	extraPrograms := moduleSources(bundle, entryModule)
	annotator.MergeModuleFunctions(ann, extraPrograms)
	annotator.MergeImportAliases(ann, extraPrograms, annotator.ModuleSource{Program: entryModule.Program, Path: entryModule.Path, RawSource: entryModule.SourceCode, Resolver: bundle.Resolver})
	opts := b.irBuildOptions()
	opts.ModuleConsts, opts.ModuleConstInits, opts.ModuleRun = mergeModuleConstants(entryModule.Program, extraPrograms)
	tree := irtree.BuildWithOptions(entryModule.Program, ann, opts)
	if len(tree.Errors) > 0 {
		for _, e := range tree.Errors {
			fmt.Fprintf(os.Stderr, "  %sx Error (loading %s):%s %v\n", cRed, loadingPath, cReset, e)
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

// routeBoundary is one layout or template wrapping a page.
type routeBoundary struct {
	Path string
	// Kind is "layout", "template" or "error" (a segment's error.tsx, run as a
	// client error boundary; Path is then the source error page and
	// Builder.routeStack swaps in the generated boundary layout).
	Kind string
	// Dir is the directory the boundary was found in.
	Dir string
	// Origin is the user's file when Path is a generated stand-in.
	Origin string
}

// src returns the file the user wrote for the boundary.
func (rb routeBoundary) src() string {
	if rb.Origin != "" {
		return rb.Origin
	}
	return rb.Path
}

// findLayoutStack returns every layout wrapping pagePath, ordered innermost
// first (see findRouteStack, which also returns templates).
func findLayoutStack(pagePath, pagesDir string) []string {
	var out []string
	for _, rb := range findRouteStack(pagePath, pagesDir) {
		if rb.Kind == "layout" {
			out = append(out, rb.Path)
		}
	}
	return out
}

// findRouteStack returns every layout and template wrapping pagePath, ordered
// innermost first: walking up from the page's directory, each directory
// contributes its template (inner) and then its layout (outer), so the
// document order is layout > template > page. A page under blog/ with
// blog/_layout and _layout at pagesDir gets [blog/_layout, pagesDir/_layout]
// so the root shell (nav/footer) can wrap a nested section layout. Pages
// outside pagesDir (e.g. .krate/gen plugin pages) only collect boundaries
// found in their own tree and never fall back to the app root layout.
func findRouteStack(pagePath, pagesDir string) []routeBoundary {
	var stack []routeBoundary
	dir := filepath.Dir(pagePath)
	// A segment's own error page renders outside that segment's boundary: an
	// error it throws goes to the next boundary up.
	ownDir := ""
	if kind, ok := segmentKindOfFile(pagePath); ok && kind == SegmentError {
		ownDir = dir
	}
	for {
		if tpl := firstExisting(dir, templateNames); tpl != "" {
			stack = append(stack, routeBoundary{Path: tpl, Kind: "template", Dir: dir})
		}
		if dir != ownDir {
			if ep := firstExisting(dir, errorBoundaryNames); ep != "" {
				stack = append(stack, routeBoundary{Path: ep, Kind: "error", Dir: dir})
			}
		}
		for _, name := range layoutNames {
			if p := firstExisting(dir, []string{name}); p != "" {
				stack = append(stack, routeBoundary{Path: p, Kind: "layout", Dir: dir})
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

// firstExisting returns the first dir/name that exists (memoized in
// layoutCache), or "".
func firstExisting(dir string, names []string) string {
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
	return ""
}

// routeChainEntry describes one layout/template boundary in the per-page
// route info the SPA router reads (see routeInfoScript).
type routeChainEntry struct {
	ID   string `json:"id"`
	Kind string `json:"k"`
	// Depth is the number of URL segments the boundary's directory covers,
	// -1 when a catch-all segment makes it cover the rest of the path. The
	// router keeps a layout mounted only while those segments are unchanged.
	Depth   int    `json:"d"`
	Persist bool   `json:"p"`
	Why     string `json:"why,omitempty"`
	File    string `json:"f,omitempty"`
	// Slots are the parallel-route slots the layout renders.
	Slots []routeSlotInfo `json:"s,omitempty"`
}

// routeSlotInfo describes one rendered slot in the route info.
type routeSlotInfo struct {
	Name string `json:"n"`
	Key  string `json:"key"`
	// Scope is the route scope (id prefix) of the rendered slot content.
	Scope string `json:"scope"`
	// Src is "page" for a matching slot page, "default" for default.tsx.
	Src string `json:"src"`
	// Intercepts are the URL patterns (no leading slash) the slot intercepts
	// on soft navigation.
	Intercepts []string `json:"i,omitempty"`
}

// routeDepth counts the URL segments of dir relative to pagesDir. Route
// groups "(name)" and parallel-route slots "@name" do not appear in URLs.
func routeDepth(dir, pagesDir string) int {
	rel, err := filepath.Rel(pagesDir, dir)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return 0
	}
	n := 0
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if isRouteGroup(seg) || strings.HasPrefix(seg, "@") {
			continue
		}
		if strings.HasPrefix(seg, "[...") || strings.HasPrefix(seg, "[[...") {
			return -1
		}
		n++
	}
	return n
}

// stripRouteGroups removes route-group segments from a slash path.
func stripRouteGroups(p string) string {
	if !strings.Contains(p, "(") {
		return p
	}
	parts := strings.Split(p, "/")
	out := parts[:0]
	for _, seg := range parts {
		if !isRouteGroup(seg) {
			out = append(out, seg)
		}
	}
	if len(out) == 0 {
		return "index"
	}
	return strings.Join(out, "/")
}

// isRouteGroup reports whether a directory name is a route group "(name)".
// Intercepting-route prefixes "(.)" / "(..)" / "(...)" are not groups.
func isRouteGroup(seg string) bool {
	return len(seg) > 2 && seg[0] == '(' && seg[len(seg)-1] == ')' && !strings.HasPrefix(seg, "(.")
}

// applyLayoutStack wraps emitResult.HTML in every layout and template
// returned by findRouteStack, innermost first. Each boundary's children are
// bracketed by <!--kl:ID--> markers so the SPA router can swap only the
// deepest boundary that differs between two routes. Each boundary's metadata
// (head/script/style) is merged after the inner content so an outer layout's
// own head/style lands after the nested section's. A boundary that fails to
// build fails the page with a diagnostic. Returns the CSS contributed by the
// boundaries, the files used (page dependencies), whether any uses CSS
// signals, and the route chain (outermost first).
func (b *Builder) applyLayoutStack(page string, emitResult *renderer.EmitResult) (string, []string, bool, []routeChainEntry, error) {
	var css string
	var used []string
	hasSignals := false
	var chain []routeChainEntry
	for _, rb := range b.routeStack(page) {
		id := b.layoutIDPrefix(rb.Path)
		open, end := "<!--kl:"+id+"-->", "<!--/kl:"+id+"-->"
		var slotCSS string
		layoutRes, layoutCSS, layoutDeps, layoutHasSignals, err := b.executeLayoutPipeline(rb.Path, open+emitResult.HTML+end, nil)
		relPath := rb.Path
		if rel, rerr := filepath.Rel(b.Root, rb.src()); rerr == nil {
			relPath = filepath.ToSlash(rel)
		}
		if err != nil {
			var d diag.Diagnostic
			var re *renderError
			if errors.As(err, &d) || errors.As(err, &re) {
				return "", nil, false, nil, err
			}
			return "", nil, false, nil, diag.Diagnostic{
				File:    rb.src(),
				Message: fmt.Sprintf("%s %s failed to build: %v", rb.Kind, relPath, err),
				Hint:    "fix the " + rb.Kind + " (it must default-export a component that renders {children}); every page under its directory depends on it",
			}
		}
		entry := routeChainEntry{ID: id, Kind: rb.Kind, Depth: routeDepth(rb.Dir, b.Cfg.PagesDir), File: relPath}
		switch n := strings.Count(layoutRes.HTML, open); {
		case n == 0:
			b.printRouteWarning(diag.Diagnostic{
				File:     rb.src(),
				Message:  fmt.Sprintf("%s %s does not render {children}; the page content of %s is dropped", rb.Kind, relPath, filepath.Base(page)),
				Hint:     "render {children} (or props.children) where the page should appear",
				Severity: diag.SeverityWarning,
			})
			entry.Why = "the " + rb.Kind + " does not render {children}"
		case n > 1:
			entry.Why = "the " + rb.Kind + " renders {children} more than once"
		}
		if rb.Kind == "layout" {
			slotRes, serr := b.renderSlots(page, rb.Path, layoutRes)
			if serr != nil {
				return "", nil, false, nil, serr
			}
			entry.Slots = slotRes.info
			slotCSS = slotRes.css
			used = append(used, slotRes.deps...)
			hasSignals = hasSignals || slotRes.hasSignals
		}
		hasSignals = hasSignals || layoutHasSignals
		emitResult.HTML = layoutRes.HTML
		emitResult.HeadHTML = emitResult.HeadHTML + layoutRes.HeadHTML
		emitResult.ScriptHTML = emitResult.ScriptHTML + layoutRes.ScriptHTML
		emitResult.StyleHTML = emitResult.StyleHTML + layoutRes.StyleHTML
		// Carry the boundary's client components into the page so its
		// interactivity hydrates. Their compact slot IDs are namespaced
		// (IDPrefix) so they can't collide with the page's; RouteScope lets the
		// router skip them while the layout stays mounted.
		layoutSigs := make([]irtree.ComponentSignature, 0, len(layoutRes.Signatures))
		for _, sig := range layoutRes.Signatures {
			if sig.RouteScope == "" {
				sig.RouteScope = id
			}
			layoutSigs = append(layoutSigs, sig)
		}
		if rb.Kind == "error" && layoutRes.BoundaryLink != nil {
			// Everything inside the boundary (the page and nested layouts) hangs
			// under it in the owner tree, and the boundary's scope runs first so
			// those owners find it.
			renderer.LinkUnderBoundary(emitResult.Signatures, layoutRes.BoundaryLink)
			emitResult.Signatures = append(layoutSigs, emitResult.Signatures...)
		} else {
			emitResult.Signatures = append(emitResult.Signatures, layoutSigs...)
		}
		emitResult.ListComponents = append(emitResult.ListComponents, layoutRes.ListComponents...)
		if layoutRes.HasLinks {
			emitResult.HasLinks = true
		}
		// Boundaries are visited innermost first; prepend so the outermost
		// layout ends up first and inner ones override it.
		css = layoutCSS + slotCSS + css
		used = append(used, layoutDeps...)
		chain = append([]routeChainEntry{entry}, chain...)
	}
	return css, used, hasSignals, chain, nil
}

// printRouteWarning prints a non-fatal routing diagnostic.
func (b *Builder) printRouteWarning(d diag.Diagnostic) {
	fmt.Fprintf(os.Stderr, "  %sWarning:%s %s\n", cYellow, cReset, d.Error())
	if d.Hint != "" {
		fmt.Fprintf(os.Stderr, "      hint: %s\n", d.Hint)
	}
}

// routeInfoScript returns the per-page route info consumed by the SPA router
// (a JSON <script> after #root). Each boundary is persistable when it is a
// layout whose hydration scope can be skipped (see RouteScopeBlockers).
func routeInfoScript(chain []routeChainEntry, sigs []irtree.ComponentSignature) string {
	if len(chain) == 0 {
		return ""
	}
	blockers := renderer.RouteScopeBlockers(sigs)
	out := make([]routeChainEntry, len(chain))
	for i, e := range chain {
		if e.Why == "" {
			e.Why = blockers[e.ID]
		}
		e.Persist = (e.Kind == "layout" || e.Kind == "error") && e.Why == ""
		out[i] = e
	}
	data, err := json.Marshal(map[string]interface{}{"v": 1, "chain": out})
	if err != nil {
		return ""
	}
	return `<script type="application/json" id="__krate_route">` + escapeJSONForScript(string(data)) + `</script>`
}

// routePathFor is the URL path of a page with its dynamic segments filled from
// params ("/blog/hello"), or "" for a page that has no route of its own.
func routePathFor(page, pagesDir string, params map[string]string) string {
	out := pageToOutput(page, pagesDir)
	if out == "" {
		return ""
	}
	var parts []string
	for _, seg := range strings.Split(strings.Trim(routeFromOutName(out), "/"), "/") {
		if seg == "" {
			continue
		}
		if rs := parseRouteSeg(seg); rs.kind != segStatic {
			v, ok := params[rs.text]
			if !ok {
				if rs.kind == segOptionalCatchAll {
					continue
				}
				v = dynamicParamSentinel(rs.text)
			}
			parts = append(parts, v)
			continue
		}
		parts = append(parts, seg)
	}
	return "/" + strings.Join(parts, "/")
}

// pageRouteScript carries the route pattern of a dynamic page ("/blog/[slug]")
// so useParams() can match the address bar against it in the browser. Pages
// without dynamic segments need none: their params are empty.
func pageRouteScript(page, pagesDir string) string {
	if !isDynamicRoute(page, pagesDir) {
		return ""
	}
	data, err := json.Marshal(map[string]string{"r": routeFromOutName(pageToOutput(page, pagesDir))})
	if err != nil {
		return ""
	}
	return `<script type="application/json" id="__krate_page">` + escapeJSONForScript(string(data)) + `</script>`
}

// fragmentInfoScript is the route info of an intercepting-route fragment.
func fragmentInfoScript(key, scope, target string) string {
	data, err := json.Marshal(map[string]interface{}{"v": 1, "frag": map[string]string{"key": key, "scope": scope, "target": target}})
	if err != nil {
		return ""
	}
	return `<script type="application/json" id="__krate_route">` + escapeJSONForScript(string(data)) + `</script>`
}

// escapeJSONForScript keeps JSON inert inside a <script> element.
func escapeJSONForScript(s string) string {
	s = strings.ReplaceAll(s, "<", "\\u003c")
	s = strings.ReplaceAll(s, ">", "\\u003e")
	return strings.ReplaceAll(s, "&", "\\u0026")
}

var (
	layoutNames   = []string{"_layout.tsx", "_layout.ts", "_layout.jsx", "_layout.js"}
	templateNames = []string{"_template.tsx", "_template.ts", "_template.jsx", "_template.js", "template.tsx", "template.ts", "template.jsx", "template.js"}
	loadingNames  = []string{"loading.tsx", "loading.ts", "loading.jsx", "loading.js"}
	// errorBoundaryNames are the files that become a client error boundary. 500.tsx
	// is only the server error page, so existing static 500 pages stay static.
	errorBoundaryNames = []string{"error.tsx", "error.ts", "error.jsx", "error.js"}
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
func (b *Builder) BuildMiddleware() error {
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
		_ = os.Remove(filepath.Join(b.Root, ".krate", "middleware.js"))
		return nil
	}

	printf("  %s> Middleware%s %s\n", cGreen, cReset, filepath.Base(middlewarePath))

	outDir := filepath.Join(b.Root, ".krate")
	_ = os.MkdirAll(outDir, 0755)
	outPath := filepath.Join(outDir, "middleware.js")

	result := esbuildBuild(api.BuildOptions{
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
		return fmt.Errorf("middleware build failed: %s", result.Errors[0].Text)
	}

	if len(result.OutputFiles) == 0 {
		return fmt.Errorf("middleware build produced no output")
	}
	if err := fsutil.WriteFileAtomic(outPath, result.OutputFiles[0].Contents, 0644); err != nil {
		return fmt.Errorf("writing middleware: %w", err)
	}
	return nil
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
func moduleSources(bundle *bundler.Bundle, entry *bundler.Module) []annotator.ModuleSource {
	var out []annotator.ModuleSource
	for _, mod := range bundle.Modules {
		if mod == nil || mod.Program == nil || mod.IsCSS || mod.IsExternal || mod == entry || mod.Path == entry.Path {
			continue
		}
		out = append(out, annotator.ModuleSource{
			Program:   mod.Program,
			Path:      mod.Path,
			RawSource: mod.SourceCode,
			Resolver:  bundle.Resolver,
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
	// Trailing "/index" is stripped so "docs/index" -> output at docs/index.html (route /docs/)
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
	if pathHasSlot(rel) {
		if info := cachedInterceptForPage(page, pagesDir); info != nil {
			return info.OutDir
		}
	}
	// Normalize to forward slashes so the returned OutName is a portable URL
	// path ("video/[id]" on every OS) - it feeds manifest routes, shell reads,
	// and region requests. Callers that touch the filesystem join it with
	// filepath.Join, which re-applies the OS separator. Route groups
	// "(name)" organize files without appearing in the URL.
	if _, ok := segmentKindOfFile(page); ok {
		// A segment page owns no URL; its assets live next to its HTML.
		return segmentAssetOut(page, pagesDir)
	}
	name := stripRouteGroups(filepath.ToSlash(strings.TrimSuffix(rel, filepath.Ext(rel))))
	// A nested index page (blog/index.tsx) is the directory's default page: it
	// maps to the parent route (blog) so /blog/ serves it - not the odd
	// /blog/index URL (which would 404 into a directory listing at /blog/).
	// Matches the .krate/gen behavior just above.
	name = strings.TrimSuffix(name, "/index")
	if name == "index" || name == "home" {
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
// directory at its hashed /workers/... URL. Workers are produced with esbuild so
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

		result := esbuildBuild(api.BuildOptions{
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
		_ = fsutil.WriteFileAtomic(ServerOutPath(b.Cfg.OutDir, "workers.json"), idx, 0644)
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
// the output directory at its hashed /chunks/... URL. Chunks are produced with
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

		result := esbuildBuild(api.BuildOptions{
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
// relative to the current module - substituting the script's real served URL
// keeps those resolutions working. jsFile is the hashed script filename.
func substituteImportMetaURL(hydrationJS, outName, jsFile, basePath string) string {
	if !strings.Contains(hydrationJS, "import.meta.url") {
		return hydrationJS
	}
	return strings.ReplaceAll(hydrationJS, "import.meta.url", strconv.Quote(pageScriptSrc(outName, jsFile, basePath)))
}

// writeAssetFiles copies every registered asset into the output directory at
// its hashed /assets/... URL path. Safe to call once per page build: the copy is
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
		if err := fsutil.WriteFileAtomic(dst, data, 0644); err != nil {
			return err
		}
	}
	return nil
}

// pruneStaleHydration removes superseded index.<hash>.js files (and their
// sourcemaps) from a page directory. Every dev edit writes a new content-hashed
// bundle, so without this they pile up in dist/ for the whole session.
func pruneStaleHydration(pageDir, keep string) {
	matches, err := filepath.Glob(filepath.Join(pageDir, "index.*.js*"))
	if err != nil {
		return
	}
	for _, m := range matches {
		base := filepath.Base(m)
		if base == keep || base == keep+".map" {
			continue
		}
		_ = os.Remove(m)
	}
}

func printResult(outName, jsFile, pageBase string) {
	if kind, ok := segmentKindOfBase(pageBase); ok {
		printf("    %s+%s %s (segment page)\n", cGreen, cReset, strings.TrimPrefix(strings.TrimSuffix(outName, kind.assetDirName())+kind.htmlName(), "/"))
	} else if outName == "." {
		printf("    %s+%s %s/index.html %s(root)%s\n", cGreen, cReset, outName, cGray, cReset)
	} else {
		printf("    %s+%s %s/index.html\n", cGreen, cReset, outName)
	}
	if jsFile != "" {
		printf("    %s%s (hydration)%s\n", cGray, jsFile, cReset)
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
		// `{cond && <Icon .../>}` (and other operators) - recurse both sides so
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
// static string literal - the only form transformIconExpr can resolve to an
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
		// the compiler. JSX text is escaped by the emitters, so the
		// (possibly attacker-influenced) name cannot inject markup.
		return &ast.JSXElement{
			Opening:  &ast.JSXOpening{Name: "span", Attributes: forwardedAttrs, SelfClosing: false},
			Children: []ast.JSXChild{&ast.JSXText{Value: fmt.Sprintf("[%v]", err)}},
			Closing:  &ast.JSXClosing{Name: "span"},
		}
	}

	// Base structural SVG properties. User-provided attributes take precedence
	// over the defaults, so any default whose name appears in the forwarded
	// attributes is dropped (duplicate attributes in HTML resolve to the first,
	// which would silently ignore the user's override).
	var baseSvgAttrs []*ast.JSXAttr
	for _, kv := range iconBaseAttrs(icon) {
		baseSvgAttrs = append(baseSvgAttrs, &ast.JSXAttr{Name: kv[0], Value: &ast.Literal{Kind: ast.StringLit, Value: kv[1]}})
	}

	// Combine default engine SVG properties with user-defined attributes,
	// letting the user override any default. `size` is shorthand for both
	// width and height.
	allAttrs := append(baseSvgAttrs, iconSizeAttrs(forwardedAttrs)...)
	allAttrs = append(allAttrs, withoutSize(forwardedAttrs)...)
	// The icon body is trusted SVG markup from the icon set, injected raw.
	allAttrs = append(allAttrs, &ast.JSXAttr{Name: "dangerouslySetInnerHTML", Value: &ast.ObjectExpr{
		Properties: []*ast.ObjectProp{{Key: "__html", Value: &ast.Literal{Kind: ast.StringLit, Value: icon.Inner}}},
	}})

	return &ast.JSXElement{
		Position: orig.Position,
		Opening: &ast.JSXOpening{
			Name:        "svg",
			Attributes:  dedupeAttrs(allAttrs),
			SelfClosing: false,
		},
		Closing: &ast.JSXClosing{
			Name: "svg",
		},
	}
}

// iconBaseAttrs returns the default attributes of an inlined icon's <svg>: the
// namespace, the source geometry, a 1em size so the icon scales with text, and
// the paint the source intended (see icons.Icon.PaintAttrs).
func iconBaseAttrs(icon icons.Icon) [][2]string {
	viewBox := icon.ViewBox
	if viewBox == "" {
		viewBox = "0 0 24 24"
	}
	attrs := [][2]string{
		{"xmlns", "http://www.w3.org/2000/svg"},
		{"viewBox", viewBox},
		{"width", "1em"},
		{"height", "1em"},
	}
	return append(attrs, icon.PaintAttrs()...)
}

// iconSizeAttrs expands a `size` attribute into width and height, and removes
// it from attrs so no meaningless size attribute reaches the SVG.
func iconSizeAttrs(attrs []*ast.JSXAttr) []*ast.JSXAttr {
	var out []*ast.JSXAttr
	for _, a := range attrs {
		if a.Name == "size" && !a.Spread && a.Value != nil {
			out = append(out,
				&ast.JSXAttr{Name: "width", Value: a.Value},
				&ast.JSXAttr{Name: "height", Value: a.Value})
		}
	}
	return out
}

// withoutSize returns attrs minus any `size` attribute.
func withoutSize(attrs []*ast.JSXAttr) []*ast.JSXAttr {
	out := make([]*ast.JSXAttr, 0, len(attrs))
	for _, a := range attrs {
		if a.Name == "size" && !a.Spread {
			continue
		}
		out = append(out, a)
	}
	return out
}

// dedupeAttrs keeps the LAST occurrence of each attribute name, so user-supplied
// attributes override the engine's defaults. Duplicate attributes in HTML are
// resolved to the FIRST occurrence by browsers, so dropping earlier defaults is
// the only reliable way to let callers override them.
func dedupeAttrs(attrs []*ast.JSXAttr) []*ast.JSXAttr {
	var out []*ast.JSXAttr
	seen := map[string]bool{}
	for i := len(attrs) - 1; i >= 0; i-- {
		if !attrs[i].Spread {
			// strokeWidth and stroke-width are the same attribute.
			key := ast.HTMLAttrName(attrs[i].Name)
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		out = append([]*ast.JSXAttr{attrs[i]}, out...)
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

	var b2 strings.Builder
	b2.WriteString("<svg")
	// Defaults first, then size, then the caller's own attributes; the last
	// occurrence of a name wins.
	var attrs2 []*ast.JSXAttr
	for _, kv := range iconBaseAttrs(icon) {
		attrs2 = append(attrs2, &ast.JSXAttr{Name: kv[0], Value: &ast.Literal{Kind: ast.StringLit, Value: kv[1]}})
	}
	attrs2 = append(attrs2, iconSizeAttrs(forwardedAttrs)...)
	attrs2 = append(attrs2, withoutSize(forwardedAttrs)...)
	for _, attr := range dedupeAttrs(attrs2) {
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
	_, err := copyDirToOut(src, dst)
	return err
}

func (b *Builder) BuildAllAPI() error {
	apiSrcDir := filepath.Join(b.Root, filepath.FromSlash(gohost.DefaultAPIDir))
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
	apiSrcDir := filepath.Join(b.Root, filepath.FromSlash(gohost.DefaultAPIDir))
	pool := newWorkerPool(buildWorkerLimit())

	var metaMu sync.Mutex
	metas := make(map[string]apiRouteMeta, len(routes))

	for _, route := range routes {
		wg.Add(1)
		go func(r string) {
			defer wg.Done()
			pool.acquire()
			defer pool.release()
			printf("  %s> API%s %s\n", cGreen, cReset, filepath.Base(r))
			meta, err := func() (m apiRouteMeta, err error) {
				err = safe.Run(func() error {
					var e error
					m, e = b.compileSingleRoute(r, apiSrcDir)
					return e
				})
				return
			}()
			if err != nil {
				fmt.Fprintf(os.Stderr, "  %sx API Error:%s %s: %v\n", cRed, cReset, r, err)
				errCh <- err
				return
			}
			metaMu.Lock()
			metas[meta.File] = meta
			metaMu.Unlock()
		}(route)
	}

	wg.Wait()
	close(errCh)
	if len(errCh) > 0 {
		return <-errCh
	}
	b.mergeAPIRouteMeta(metas)
	return nil
}

// apiRouteMeta records one compiled JS API route so the dev/prod server can
// route Node-only routes (those importing external modules / node builtins that
// the embedded QuickJS runtime cannot run) to the Node sidecar.
type apiRouteMeta struct {
	// File is the out-dir relative output, e.g. "users/[id].js".
	File string `json:"file"`
	// NodeOnly is true when the bundle still has external imports.
	NodeOnly bool `json:"nodeOnly"`
}

func (b *Builder) compileSingleRoute(file string, apiSrcDir string) (apiRouteMeta, error) {
	relPath, err := filepath.Rel(apiSrcDir, file)
	if err != nil {
		return apiRouteMeta{}, err
	}
	outName := strings.TrimSuffix(relPath, filepath.Ext(relPath)) + ".js"

	// Two artifacts so each runtime gets the shape it wants:
	//   dist/.krate/api/<name>.js       IIFE + bundled  -> embedded QuickJS
	//   dist/.krate/api-node/<name>.js  ESM  + externals -> Node sidecar
	quickjsPath := filepath.Join(apiBundleDir(b.Cfg.OutDir), outName)
	nodePath := filepath.Join(apiNodeBundleDir(b.Cfg.OutDir), outName)
	for _, p := range []string{quickjsPath, nodePath} {
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return apiRouteMeta{}, err
		}
	}

	// QuickJS artifact: IIFE with a global name so the runtime loads it with a
	// plain eval and reads exports off `__krate_route` (no runtime regex
	// export-stripping); deps are bundled so the route is self-contained.
	quickjsOpts := api.BuildOptions{
		EntryPoints: []string{file},
		Bundle:      true,
		Platform:    api.PlatformNode,
		Format:      api.FormatIIFE,
		GlobalName:  "__krate_route",
		Outfile:     quickjsPath,
		Write:       true,
		Sourcemap:   sourceMapMode(b.Cfg.Sourcemap),
		Metafile:    true,
	}
	// Node artifact: ESM with third-party packages external (Node resolves them
	// from node_modules at runtime, including native addons).
	nodeOpts := api.BuildOptions{
		EntryPoints: []string{file},
		Bundle:      true,
		Platform:    api.PlatformNode,
		Format:      api.FormatESModule,
		Packages:    api.PackagesExternal,
		Outfile:     nodePath,
		Write:       true,
		Sourcemap:   sourceMapMode(b.Cfg.Sourcemap),
		Metafile:    true,
	}

	if b.Cfg.ShouldMinifyJS() {
		quickjsOpts.MinifyWhitespace = true
		quickjsOpts.MinifyIdentifiers = true
		quickjsOpts.MinifySyntax = true
		nodeOpts.MinifyWhitespace = true
		nodeOpts.MinifyIdentifiers = true
		nodeOpts.MinifySyntax = true
	}

	quickjsResult := esbuildBuild(quickjsOpts)
	if len(quickjsResult.Errors) > 0 {
		return apiRouteMeta{}, fmt.Errorf("esbuild compilation failed:\n%s", formatEsbuildErrors(quickjsResult.Errors))
	}
	nodeResult := esbuildBuild(nodeOpts)
	if len(nodeResult.Errors) > 0 {
		return apiRouteMeta{}, fmt.Errorf("esbuild compilation failed:\n%s", formatEsbuildErrors(nodeResult.Errors))
	}

	meta := apiRouteMeta{File: filepath.ToSlash(outName)}

	// Node-only = imports still external after bundling (node builtins / native
	// deps the embedded runtime cannot provide). Package deps that bundle cleanly
	// stay embedded, so dep-using routes still run in QuickJS.
	if quickjsResult.Metafile != "" {
		type esbuildMeta struct {
			Inputs  map[string]interface{} `json:"inputs"`
			Outputs map[string]struct {
				Imports []struct {
					External bool `json:"external"`
				} `json:"imports"`
			} `json:"outputs"`
		}
		var m esbuildMeta
		if err := json.Unmarshal([]byte(quickjsResult.Metafile), &m); err == nil {
			var deps []string
			for inputPath := range m.Inputs {
				// Convert to pristine workspace absolute paths for the file watcher
				deps = append(deps, filepath.Clean(filepath.Join(b.Root, inputPath)))
			}
			// Re-inject back into your framework's reactive watch graph
			b.recordDeps(file, deps)

			for _, out := range m.Outputs {
				for _, imp := range out.Imports {
					if imp.External {
						meta.NodeOnly = true
					}
				}
			}
		}
	}

	return meta, nil
}

// formatEsbuildErrors renders esbuild messages into a single styled string.
func formatEsbuildErrors(errs []api.Message) string {
	formatted := api.FormatMessages(errs, api.FormatMessagesOptions{
		Kind:  api.ErrorMessage,
		Color: true, // Retains your pretty terminal compiler colors
	})
	var sb strings.Builder
	for _, msg := range formatted {
		sb.WriteString(msg)
	}
	return sb.String()
}

// mergeAPIRouteMeta writes/updates `.krate/api-routes.json`, preserving entries
// for routes not touched by this (possibly incremental) build.
func (b *Builder) mergeAPIRouteMeta(updates map[string]apiRouteMeta) {
	if len(updates) == 0 {
		return
	}
	manifestPath := filepath.Join(b.Root, ".krate", "api-routes.json")
	merged := map[string]apiRouteMeta{}
	if data, err := os.ReadFile(manifestPath); err == nil {
		var existing []apiRouteMeta
		if json.Unmarshal(data, &existing) == nil {
			for _, m := range existing {
				merged[m.File] = m
			}
		}
	}
	for k, v := range updates {
		merged[k] = v
	}
	files := make([]string, 0, len(merged))
	for f := range merged {
		files = append(files, f)
	}
	sort.Strings(files)
	list := make([]apiRouteMeta, 0, len(files))
	for _, f := range files {
		list = append(list, merged[f])
	}
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0755); err != nil {
		return
	}
	if data, err := json.MarshalIndent(list, "", "  "); err == nil {
		_ = fsutil.WriteFileAtomic(manifestPath, data, 0644)
	}
}

// pageFontPreloads renders preload links for fonts the global stylesheet has
// not already preloaded.
func (b *Builder) pageFontPreloads(urls []string) string {
	if len(urls) == 0 {
		return ""
	}
	have := map[string]bool{}
	for _, u := range b.globalFonts {
		have[u] = true
	}
	var rest []string
	for _, u := range urls {
		if !have[u] {
			rest = append(rest, u)
		}
	}
	return fontPreloadHTML(rest, b.Cfg.BaseURLPath())
}

// importsRuntimeComponent reports whether any module is a runtime component.
func importsRuntimeComponent(mods []*bundler.Module) bool {
	for _, m := range mods {
		if m.ComponentClass == bundler.ComponentClassRuntime {
			return true
		}
	}
	return false
}
