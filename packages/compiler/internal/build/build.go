package build

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/evanw/esbuild/pkg/api"
	"krate-compiler/internal/annotator"
	"krate-compiler/internal/ast"
	"krate-compiler/internal/bundler"
	"krate-compiler/internal/config"
	"krate-compiler/internal/css"
	"krate-compiler/internal/escape"
	"krate-compiler/internal/fsutil"
	"krate-compiler/internal/icons"
	"krate-compiler/internal/imageproc"
	"krate-compiler/internal/irtree"
	"krate-compiler/internal/jsruntime"
	"krate-compiler/internal/markdown"
	"krate-compiler/internal/plugin"
	"krate-compiler/internal/reactive"
	"krate-compiler/internal/renderer"
	"krate-compiler/internal/syntaxhighlight"
)

type cssModuleBinding struct {
	LocalVar string
	Mappings map[string]string
}

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
	RuntimeJSFile string // shared runtime chunk path (relative to outDir), e.g. "chunks/runtime.abc.js"
	HasJS         bool
	HasCSS        bool
	IsErrorPage   bool
	UsedCSS       map[string]bool
	UsedFuncs     map[string]bool
	LoadingHTML   string // rendered loading.tsx fallback for SPA transitions

	// SSR/ISR/Streaming metadata
	Mode             RenderMode
	Revalidate       int
	SourcePath       string // relative path to source file
	ServerBundlePath string // path to server bundle (for SSR/ISR pages)
}

type Builder struct {
	Root     string
	Cfg      *config.Config
	DevMode  bool
	depGraph map[string][]string // file path → page source paths that depend on it
	pageDeps map[string][]string // page source path → files it depends on
	depMu    sync.Mutex          // protects depGraph/pageDeps
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

// BuildPages rebuilds only the specified pages (by source path).
// Unlike BuildAll, it does NOT clean the output directory.
func (b *Builder) BuildPages(pages []string) error {
	if len(pages) == 0 {
		return nil
	}

	errorCount := 0

	b.Cfg.Markdown.Root = b.Root

	type pageBuildResult struct {
		result *PageResult
		rawCSS string
		err    error
		page   string
	}

	resultsCh := make(chan pageBuildResult, len(pages))
	var wg sync.WaitGroup

	for _, page := range pages {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			fmt.Printf("  %s▶%s %s\n", cCyan, cReset, p)
			result, rawCSS, err := b.buildPage(p)
			resultsCh <- pageBuildResult{result, rawCSS, err, p}
		}(page)
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	var results []*PageResult
	mergedCSS := ""
	docsCSS := ""
	cssRuleSeen := make(map[string]bool)
	docsCSSRuleSeen := make(map[string]bool)

	for res := range resultsCh {
		if res.err != nil {
			fmt.Fprintf(os.Stderr, "\n  %s✗ Error:%s %v\n", cRed, cReset, res.err)
			errorCount++
			continue
		}
		result := res.result
		results = append(results, result)

		isDocsPage := strings.HasPrefix(result.OutName, "docs/") || result.OutName == "docs"
		if res.rawCSS != "" {
			if isDocsPage {
				docsCSS = mergeCSS(docsCSS, res.rawCSS, docsCSSRuleSeen)
			} else {
				mergedCSS = mergeCSS(mergedCSS, res.rawCSS, cssRuleSeen)
			}
		}

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
		}
		if err := plugin.RunCommunityPlugins("AfterPage", b.Cfg.Plugins, b.Root, b.Cfg.OutDir, afterPageCtx); err != nil {
			fmt.Fprintf(os.Stderr, "  %sCommunity plugin error (AfterPage: %s):%s %v\n", cYellow, res.page, cReset, err)
		}
		// Apply plugin modifications back
		result.HTML = afterPageCtx.HTML
		result.HeadHTML = afterPageCtx.HeadHTML
	}

	if len(results) == 0 {
		return fmt.Errorf("no pages built successfully")
	}

	if b.Cfg.Tailwind.Enabled {
		twCfg := css.LoadTailwindConfig(b.Root)
		twCSS, err := css.GenerateTailwind(b.Root, twCfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %sTailwind error:%s %v\n", cYellow, cReset, err)
		} else if twCSS != "" {
			mergedCSS = mergeCSS(mergedCSS, twCSS, cssRuleSeen)
		}
	}

	// Inject chroma syntax highlighting CSS if code highlighting is enabled
	if b.Cfg.Markdown.CodeHighlight {
		chromaCSS := syntaxhighlight.CSSForTheme(b.Cfg.Markdown.CodeTheme)
		if chromaCSS != "" {
			mergedCSS = chromaCSS + "\n" + mergedCSS
		}
	}

	// Regenerate global CSS (if multiple pages, need new CSS hash)
	cssFile := b.writeGlobalCSS(mergedCSS)

	// Write shared runtime chunk (extracted from per-page bundles)
	runtimeJS := writeRuntimeChunk(b.Cfg.OutDir, b.Cfg.ShouldMinifyJS(), b.Root)

	// In-memory HTML generation + string swap + single disk write per page
	b.writeHTMLPages(results, cssFile, runtimeJS)

	return nil
}

func (b *Builder) BuildAll() error {
	if err := os.RemoveAll(b.Cfg.OutDir); err != nil {
		return fmt.Errorf("cleaning output dir: %w", err)
	}
	if err := os.MkdirAll(b.Cfg.OutDir, 0755); err != nil {
		return fmt.Errorf("creating output dir: %w", err)
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
	}
	if err := plugin.RunCommunityPlugins("BeforeBuild", b.Cfg.Plugins, b.Root, b.Cfg.OutDir, beforeBuildCtx); err != nil {
		fmt.Fprintf(os.Stderr, "  %sCommunity plugin error (BeforeBuild):%s %v\n", cYellow, cReset, err)
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
	}
	if err := plugin.RunCommunityPlugins("GenerateRoutes", b.Cfg.Plugins, b.Root, b.Cfg.OutDir, routeCtx); err != nil {
		fmt.Fprintf(os.Stderr, "  %sCommunity plugin error (GenerateRoutes):%s %v\n", cYellow, cReset, err)
	}

	type pageBuildResult struct {
		result *PageResult
		rawCSS string
		err    error
		page   string
	}

	totalPages := len(pages) + len(routes)
	resultsCh := make(chan pageBuildResult, totalPages)
	var wg sync.WaitGroup

	for _, page := range pages {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			fmt.Printf("  %s▶%s %s\n", cCyan, cReset, p)
			result, rawCSS, err := b.buildPage(p)
			resultsCh <- pageBuildResult{result, rawCSS, err, p}
		}(page)
	}

	for _, route := range routes {
		wg.Add(1)
		go func(r plugin.Route) {
			defer wg.Done()
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
	mergedCSS := ""
	docsCSS := ""
	cssRuleSeen := make(map[string]bool)
	docsCSSRuleSeen := make(map[string]bool)
	errorCount := 0

	for res := range resultsCh {
		if res.err != nil {
			fmt.Fprintf(os.Stderr, "\n  %s✗ Error:%s %v\n", cRed, cReset, res.err)
			errorCount++
			continue
		}

		result := res.result
		results = append(results, result)

		isDocsPage := strings.HasPrefix(result.OutName, "docs/") || result.OutName == "docs"
		if res.rawCSS != "" {
			if isDocsPage {
				docsCSS = mergeCSS(docsCSS, res.rawCSS, docsCSSRuleSeen)
			} else {
				mergedCSS = mergeCSS(mergedCSS, res.rawCSS, cssRuleSeen)
			}
		}

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
		}
		if err := plugin.RunCommunityPlugins("AfterPage", b.Cfg.Plugins, b.Root, b.Cfg.OutDir, afterPageCtx); err != nil {
			fmt.Fprintf(os.Stderr, "  %sCommunity plugin error (AfterPage: %s):%s %v\n", cYellow, res.page, cReset, err)
		}
		// Apply plugin modifications back to result
		result.HTML = afterPageCtx.HTML
		result.HeadHTML = afterPageCtx.HeadHTML
	}

	if b.Cfg.Tailwind.Enabled {
		twCfg := css.LoadTailwindConfig(b.Root)
		twCSS, err := css.GenerateTailwind(b.Root, twCfg)
		if err == nil && twCSS != "" {
			mergedCSS = mergeCSS(mergedCSS, twCSS, cssRuleSeen)
		}
	}

	staticParamPages := b.resolveStaticParamsPages(pages)
	if len(staticParamPages) > 0 {
		fmt.Printf("  %s⚡%s Building %d statically generated pages from generateStaticParams\n", cCyan, cReset, len(staticParamPages))
		for _, spp := range staticParamPages {
			result, rawCSS, err := b.buildStaticParamsPage(spp)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  %s✗ Error building static params page (%s):%s %v\n", cRed, spp.OutPath, cReset, err)
				continue
			}
			results = append(results, result)
			if rawCSS != "" {
				mergedCSS = mergeCSS(mergedCSS, rawCSS, cssRuleSeen)
			}
			// Run AfterPage plugins for the generated page
			afterPageCtx := &plugin.PageHookCtx{
				Page:     result.Page,
				OutName:  result.OutName,
				HTML:     result.HTML,
				HeadHTML: result.HeadHTML,
				HasJS:    result.HasJS,
			}
			plugin.RunAfterPage(afterPageCtx)
			plugin.RunCommunityPlugins("AfterPage", b.Cfg.Plugins, b.Root, b.Cfg.OutDir, afterPageCtx)
			result.HTML = afterPageCtx.HTML
			result.HeadHTML = afterPageCtx.HeadHTML
		}
	}

	// Inject chroma syntax highlighting CSS if code highlighting is enabled
	if b.Cfg.Markdown.CodeHighlight {
		chromaCSS := syntaxhighlight.CSSForTheme(b.Cfg.Markdown.CodeTheme)
		if chromaCSS != "" {
			mergedCSS = chromaCSS + "\n" + mergedCSS
		}
	}

	// Write global CSS with hash-based naming
	cssFile := b.writeGlobalCSS(mergedCSS)

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
		runtimeJS = writeRuntimeChunk(b.Cfg.OutDir, b.Cfg.ShouldMinifyJS(), b.Root)
	}

	// In-memory HTML generation + string swap + single disk write per page
	b.writeHTMLPages(results, cssFile, runtimeJS)

	if b.Cfg.PublicDir != "" {
		if info, err := os.Stat(b.Cfg.PublicDir); err == nil && info.IsDir() {
			copyDirToOut(b.Cfg.PublicDir, b.Cfg.OutDir)
		}
	}

	// Copy processed <Image> variants into the output (served at /_krate/images/...)
	if err := b.copyImageCacheToOut(); err != nil {
		fmt.Fprintf(os.Stderr, "  %sImage copy error:%s %v\n", cYellow, cReset, err)
	}

	// Append docs-specific component CSS + chroma CSS to docs-styles.css
	if docsCSS != "" || b.Cfg.Markdown.CodeHighlight {
		extra := docsCSS
		if b.Cfg.Markdown.CodeHighlight {
			chromaCSS := syntaxhighlight.CSSForTheme(b.Cfg.Markdown.CodeTheme)
			if chromaCSS != "" {
				if extra != "" {
					extra = chromaCSS + "\n" + extra
				} else {
					extra = chromaCSS
				}
			}
		}
		b.writeDocsCSS(extra)
	}

	if err := b.BuildAllAPI(); err != nil {
		fmt.Fprintf(os.Stderr, "  %sAPI Build error:%s %v\n", cRed, cReset, err)
		os.Exit(1)
	}

	b.BuildMiddleware()

	// Write page manifest with SSR/ISR metadata
	manifest := BuildManifest(results, cssFile, runtimeJS)

	// Compile server bundles for SSR/ISR/streaming pages
	serverBundles := CompileServerBundles(results, b.Root, b.Cfg.OutDir)
	if len(serverBundles) > 0 {
		fmt.Printf("  %s⚡%s Compiled %d server bundles\n", cCyan, cReset, len(serverBundles))
	}

	// Compile runtime server components (*.runtime.tsx, // @runtime, runtimeDirs)
	runtimeCompBundles := CompileRuntimeComponents(b.Root, b.Cfg.OutDir, b.Cfg.ServerComponents, b.Cfg.RuntimeComponents, b.Cfg.RuntimeDirs)
	if len(runtimeCompBundles) > 0 {
		fmt.Printf("  %s⚡%s Compiled %d runtime components\n", cCyan, cReset, len(runtimeCompBundles))
	}
	manifest.SetRuntimeComponents(runtimeCompBundles)

	// Compile SSR bundles for QuickJS rendering
	ssrBundles := CompileSSRPageBundles(results, b.Root, b.Cfg.OutDir)
	_ = ssrBundles // used by embedded QuickJS runtime

	if err := WriteManifest(manifest, b.Cfg.OutDir, serverBundles); err != nil {
		fmt.Fprintf(os.Stderr, "  %sWarning: failed to write manifest:%s %v\n", cYellow, cReset, err)
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
	for i, r := range results {
		pageResults[i] = plugin.PageResult{
			Page:     r.Page,
			OutName:  r.OutName,
			HTML:     r.HTML,
			HeadHTML: r.HeadHTML,
			HasJS:    r.HasJS,
		}
	}
	afterBuildCtx := &plugin.BuildResultHookCtx{
		Root:   b.Root,
		OutDir: b.Cfg.OutDir,
		Config: b.Cfg,
		Pages:  pageResults,
		CSS:    mergedCSS,
	}
	if err := plugin.RunAfterBuild(afterBuildCtx); err != nil {
		fmt.Fprintf(os.Stderr, "  %sPlugin error (AfterBuild):%s %v\n", cYellow, cReset, err)
	}
	if err := plugin.RunCommunityPlugins("AfterBuild", b.Cfg.Plugins, b.Root, b.Cfg.OutDir, afterBuildCtx); err != nil {
		fmt.Fprintf(os.Stderr, "  %sCommunity plugin error (AfterBuild):%s %v\n", cYellow, cReset, err)
	}

	return nil
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

func (b *Builder) writeGlobalCSS(mergedCSS string) string {
	cssFile := ""
	if mergedCSS != "" {
		processedCSS := mergedCSS
		// Inline @import directives before minification
		processedCSS = css.InlineImports(processedCSS, b.Root)
		if b.Cfg.ShouldMinifyCSS() {
			processedCSS = css.Minify(processedCSS)
		}
		if strings.TrimSpace(processedCSS) != "" {
			processedBytes := []byte(processedCSS)
			cssHash := hashContent(processedBytes)
			cssFile = "styles." + cssHash + ".css"
			cssPath := filepath.Join(b.Cfg.OutDir, cssFile)
			os.WriteFile(cssPath, processedBytes, 0644)
		}
	}
	return cssFile
}

// writeDocsCSS appends component CSS to the docs-styles.css file in the output directory.
func (b *Builder) writeDocsCSS(docsCSS string) {
	docsCSSPath := filepath.Join(b.Cfg.OutDir, "docs-styles.css")
	existing, _ := os.ReadFile(docsCSSPath)
	combined := string(existing) + "\n" + docsCSS
	os.WriteFile(docsCSSPath, []byte(combined), 0644)
}

// writeHTMLPages builds the layout wrapper, applies placeholders, minifies,
// and saves to disk in a single parallel step (Zero Disk-I/O Amplification Fix)
func (b *Builder) writeHTMLPages(results []*PageResult, cssFile string, runtimeJSFile string) {
	var wg sync.WaitGroup
	for _, r := range results {
		wg.Add(1)
		go func(r *PageResult) {
			defer wg.Done()

			pageDir := b.Cfg.OutDir
			if r.OutName != "." {
				pageDir = filepath.Join(b.Cfg.OutDir, r.OutName)
			}

			os.MkdirAll(pageDir, 0755)

			// 1. Construct structural HTML wrapper in memory.
			// A page links the global stylesheet when it has its own imported CSS
			// OR when a merged stylesheet (e.g. Tailwind-generated) was written.
			hasCSS := r.HasCSS || cssFile != ""
			var html string
			if r.LoadingHTML != "" {
				html = generateHTMLWithLoading(r.HTML, r.HeadHTML, r.ScriptHTML, r.StyleHTML, r.LoadingHTML, hasCSS, r.JSFile, runtimeJSFile, r.OutName, b.DevMode)
			} else {
				html = generateHTML(r.HTML, r.HeadHTML, r.ScriptHTML, r.StyleHTML, hasCSS, r.JSFile, runtimeJSFile, r.OutName, b.DevMode)
			}

			// 2. CSP meta tag injection
			if b.Cfg.CSP.Enabled {
				cspMeta := generateCSPMeta(r.ScriptHTML, r.StyleHTML, r.HydrationJS, b.Cfg.CSP.Directive)
				if cspMeta != "" {
					html = strings.Replace(html, "</head>", cspMeta+"</head>", 1)
				}
			}

			// 2b. SEO meta tag injection (canonical, OG, Twitter Card)
			if b.Cfg.SEO.BaseURL != "" {
				seoTags := generateSEOTags(r.HeadHTML, routeFromOutName(r.OutName), b.Cfg.SEO.BaseURL, b.Cfg.SEO.SiteName, b.Cfg.SEO.Description, b.Cfg.SEO.Image)
				if seoTags != "" {
					html = strings.Replace(html, "</head>", seoTags+"</head>", 1)
				}
			}

			// 3. Perform placeholder swap in memory
			if cssFile != "" {
				html = strings.ReplaceAll(html, cssPlaceholder, cssFile)
			}

			// 3. Minify code in memory
			if b.Cfg.ShouldMinifyHTML() {
				html = minifyHTML(html)
			}

			// 4. Single Write to physical media
			// Error pages (404/500) are written directly at the output root as .html
			var htmlPath string
			if r.IsErrorPage {
				base := strings.TrimSuffix(filepath.Base(r.Page), filepath.Ext(r.Page))
				htmlPath = filepath.Join(b.Cfg.OutDir, base+".html")
			} else {
				htmlPath = filepath.Join(pageDir, "index.html")
			}
			os.WriteFile(htmlPath, []byte(html), 0644)
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

func (b *Builder) buildPage(page string) (*PageResult, string, error) {
	bnd := bundler.New(b.Root)
	bnd.SetEmitReact(b.Cfg.EmitReact)
	bnd.SetPathAliases(b.cfgPathAliasPrefixes(), b.cfgPathAliasTargets(), b.Cfg.TSBaseDir)
	bnd.SetServerComponents(b.Cfg.ServerComponents, b.Cfg.RuntimeComponents, b.Cfg.ServerDirs, b.Cfg.RuntimeDirs)
	bundle, err := bnd.Bundle(page)
	if err != nil {
		return nil, "", err
	}

	entryModule := findEntryModule(bundle.Modules)
	if entryModule == nil || entryModule.Program == nil {
		return nil, "", fmt.Errorf("no entry module found")
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
	}
	if err := plugin.RunCommunityPlugins("AfterParse", b.Cfg.Plugins, b.Root, b.Cfg.OutDir, parseCtx); err != nil {
		fmt.Fprintf(os.Stderr, "  %sCommunity plugin error AfterParse (%s):%s %v\n", cYellow, page, cReset, err)
	}

	// Detect rendering mode (SSR/ISR/Streaming) from AST exports
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

	// Global streaming override: if configured, force all pages to stream.
	if b.Cfg.SSR.Streaming && renderMode != RenderStreaming {
		renderMode = RenderStreaming
		fmt.Fprintf(os.Stderr, "  %s⚡%s %s → streaming (global override)\n", cCyan, cReset, filepath.Base(page))
	}

	// Handle plain Markdown pages directly (skip JSX renderer).
	// MDX files now generate proper TSX with imports and JSX, so they go through
	// the regular rendering pipeline below.
	if strings.HasSuffix(page, ".md") {
		return b.buildMarkdownPage(page, bundle)
	}

	b.TransformUniversalIcons(entryModule.Program)
	b.TransformUniversalImages(entryModule.Program)

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
	}
	annotator.MergeModuleFunctions(ann, extraPrograms)
	// Re-classify tiers for any newly discovered components
	annotator.ReclassifyTiers(ann, b.Cfg)
	tree := irtree.Build(entryModule.Program, ann)
	emitter := renderer.NewEmitter()
	emitter.IconResolver = b.iconResolver
	emitter.EvalJS = b.jsExprEvaluator()
	emitResult := emitter.Emit(tree)
	renderer.EmitMeta(tree, emitResult)

	// Compile-time reactive dependency validation. Surfaced as warnings so
	// dead signals / circular effects are caught before hydration ships.
	for _, d := range reactive.Build(emitResult.Signatures).Validate() {
		fmt.Fprintf(os.Stderr, "  %s⚠ %s%s\n", cYellow, d.Message, cReset)
	}

	// Include the runtime component props script (krate-id → props JSON) so
	// serve-time rendering can resolve runtime component props from the page.
	if emitResult.RuntimeHTML != "" {
		emitResult.ScriptHTML += emitResult.RuntimeHTML
	}

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
	}
	if err := plugin.RunCommunityPlugins("AfterRender", b.Cfg.Plugins, b.Root, b.Cfg.OutDir, renderCtx); err != nil {
		fmt.Fprintf(os.Stderr, "  %sCommunity plugin error AfterRender (%s):%s %v\n", cYellow, page, cReset, err)
	}
	// Apply plugin modifications back
	emitResult.HTML = renderCtx.HTML
	emitResult.HeadHTML = renderCtx.HeadHTML
	bundle.CSS = renderCtx.RawCSS

	layoutPath := findLayout(page, b.Cfg.PagesDir)
	if layoutPath != "" {
		layoutRes, layoutCSS, err := b.executeLayoutPipeline(layoutPath, emitResult.HTML, nil)
		if err == nil {
			emitResult.HTML = layoutRes.HTML
			// Merge metadata from layout
			emitResult.HeadHTML = emitResult.HeadHTML + layoutRes.HeadHTML
			emitResult.ScriptHTML = emitResult.ScriptHTML + layoutRes.ScriptHTML
			emitResult.StyleHTML = emitResult.StyleHTML + layoutRes.StyleHTML
			bundle.CSS += layoutCSS
		}
	}

	b.recordDeps(page, deps)

	outName := pageToOutput(page, b.Cfg.PagesDir)
	pageDir := filepath.Join(b.Cfg.OutDir, outName)

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
			if b.Cfg.ShouldMinifyJS() {
				hydrationJS = minifyJS(hydrationJS)
			}

			// Write page-specific hydration code only (runtime is in a shared chunk)
			finalJS := strings.TrimSpace(hydrationJS)
			jsHash := hashContent([]byte(finalJS))
			jsFile = "index." + jsHash + ".js"
			jsPath := filepath.Join(pageDir, jsFile)
			os.WriteFile(jsPath, []byte(finalJS), 0644)

			if b.Cfg.Sourcemap {
				sm := generateSourcemap(finalJS, hydrationJS, page)
				smPath := jsPath + ".map"
				os.WriteFile(smPath, []byte(sm), 0644)
			}
		} else {
			hydrationJS = ""
		}
	}

	printResult(outName, jsFile)

	loadingHTML := b.renderLoadingComponent(page)

	// Return data structures without triggering an intermediate disk write
	pageBase := strings.TrimSuffix(filepath.Base(page), filepath.Ext(page))
	relSrc, _ := filepath.Rel(b.Root, page)
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
		UsedCSS:     emitResult.UsedCSS,
		UsedFuncs:   emitResult.UsedFuncs,
		LoadingHTML: loadingHTML,

		Mode:       renderMode,
		Revalidate: revalidate,
		SourcePath: relSrc,
	}, bundle.CSS, nil
}

// buildMarkdownPage renders a .md or .mdx file as a static HTML page.
func (b *Builder) buildMarkdownPage(page string, bundle *bundler.Bundle) (*PageResult, string, error) {
	data, err := os.ReadFile(page)
	if err != nil {
		return nil, "", fmt.Errorf("reading markdown: %w", err)
	}

	var htmlContent string
	props := make(map[string]string)

	if strings.HasSuffix(page, ".mdx") {
		mdCfg := b.Cfg.Markdown
		result := markdown.ParseMDX(string(data), mdCfg)
		htmlContent = result.ReinsertJSXBlocks()
		for k, v := range result.Frontmatter {
			props[k] = v
		}
	} else {
		mdCfg := b.Cfg.Markdown
		htmlContent = markdown.RenderToHTML(string(data), mdCfg)
	}

	bodyHTML := "<div class=\"md-content\">" + htmlContent + "</div>"

	// Run AfterMarkdownParse hooks (plugins can modify rendered HTML)
	outName := pageToOutput(page, b.Cfg.PagesDir)
	mdCtx := &plugin.MarkdownHookCtx{
		Page:  page,
		HTML:  bodyHTML,
		Route: outName,
	}
	if err := plugin.RunAfterMarkdownParse(mdCtx); err != nil {
		fmt.Fprintf(os.Stderr, "  %sAfterMarkdownParse plugin error (%s):%s %v\n", cYellow, page, cReset, err)
	}
	if err := plugin.RunCommunityPlugins("AfterMarkdownParse", b.Cfg.Plugins, b.Root, b.Cfg.OutDir, mdCtx); err != nil {
		fmt.Fprintf(os.Stderr, "  %sCommunity plugin error AfterMarkdownParse (%s):%s %v\n", cYellow, page, cReset, err)
	}
	// Apply plugin modifications back
	bodyHTML = mdCtx.HTML

	deps := []string{page}
	for _, mod := range bundle.Modules {
		if mod.Path != page {
			deps = append(deps, mod.Path)
		}
	}

	layoutPath := findLayout(page, b.Cfg.PagesDir)
	if layoutPath != "" {
		layoutRes, layoutCSS, err := b.executeLayoutPipeline(layoutPath, bodyHTML, props)
		if err == nil {
			bodyHTML = layoutRes.HTML
			bundle.CSS += layoutCSS
		}
	}

	// Save data structures without writing to media yet
	return &PageResult{
		Page:        page,
		OutName:     outName,
		HTML:        bodyHTML,
		HeadHTML:    "",
		ScriptHTML:  "",
		StyleHTML:   "",
		HydrationJS: "",
		HasJS:       false,
		JSFile:      "",
		HasCSS:      bundle.CSS != "",
		UsedCSS:     make(map[string]bool),
		UsedFuncs:   make(map[string]bool),
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
	layoutRes, rawCSS, err := b.executeLayoutPipeline(route.Layout, route.Content, route.Data)
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
		UsedCSS:     layoutRes.UsedCSS,
		UsedFuncs:   make(map[string]bool),
	}, rawCSS, nil
}

// layoutEmitCacheEntry is the layout skeleton (pre-children-injection) keyed by
// layout path + modtime. The layout is content-independent of its pages, so the
// expensive bundle/annotate/emit pipeline runs once per layout version instead
// of once per page.
type layoutEmitCacheEntry struct {
	modTime                                    time.Time
	html, headHTML, scriptHTML, styleHTML, css string
}

var layoutEmitCache sync.Map
var loadingEmitCache sync.Map

func (b *Builder) executeLayoutPipeline(layoutPath string, content string, props map[string]string) (*renderer.EmitResult, string, error) {
	var css string

	if layoutPath == "" {
		return &renderer.EmitResult{HTML: content}, "", nil
	}

	var modTime time.Time
	if info, err := os.Stat(layoutPath); err == nil {
		modTime = info.ModTime()
	}

	// Cache hit: reuse the layout skeleton and inject the page content.
	if cached, ok := layoutEmitCache.Load(layoutPath); ok {
		entry := cached.(*layoutEmitCacheEntry)
		if entry.modTime.Equal(modTime) {
			return &renderer.EmitResult{
				HTML:       strings.Replace(entry.html, "<!--__children__-->", content, -1),
				HeadHTML:   entry.headHTML,
				ScriptHTML: entry.scriptHTML,
				StyleHTML:  entry.styleHTML,
			}, entry.css, nil
		}
	}

	bnd := bundler.New(b.Root)
	bnd.SetEmitReact(b.Cfg.EmitReact)
	bnd.SetPathAliases(b.cfgPathAliasPrefixes(), b.cfgPathAliasTargets(), b.Cfg.TSBaseDir)
	bnd.SetServerComponents(b.Cfg.ServerComponents, b.Cfg.RuntimeComponents, b.Cfg.ServerDirs, b.Cfg.RuntimeDirs)
	layoutBundle, err := bnd.Bundle(layoutPath)
	if err != nil {
		return nil, "", fmt.Errorf("bundling layout %s: %w", layoutPath, err)
	}

	layoutModule := findEntryModule(layoutBundle.Modules)
	if layoutModule == nil || layoutModule.Program == nil {
		return nil, "", fmt.Errorf("layout module invalid or empty: %s", layoutPath)
	}

	b.TransformUniversalIcons(layoutModule.Program)
	b.TransformUniversalImages(layoutModule.Program)

	ann := annotator.Annotate(layoutModule.Program, b.Cfg, layoutPath, layoutModule.SourceCode)
	extraLayoutPrograms := moduleSources(layoutBundle.Modules, layoutModule)
	annotator.MergeModuleFunctions(ann, extraLayoutPrograms)
	annotator.ReclassifyTiers(ann, b.Cfg)
	tree := irtree.Build(layoutModule.Program, ann)
	emitter := renderer.NewEmitter()
	emitter.IconResolver = b.iconResolver
	emitter.EvalJS = b.jsExprEvaluator()
	emitResult := emitter.Emit(tree)
	renderer.EmitMeta(tree, emitResult)

	// Compile-time reactive dependency validation. Surfaced as warnings so
	// dead signals / circular effects are caught before hydration ships.
	for _, d := range reactive.Build(emitResult.Signatures).Validate() {
		fmt.Fprintf(os.Stderr, "  %s⚠ %s%s\n", cYellow, d.Message, cReset)
	}

	css = layoutBundle.CSS

	layoutEmitCache.Store(layoutPath, &layoutEmitCacheEntry{
		modTime:    modTime,
		html:       emitResult.HTML,
		headHTML:   emitResult.HeadHTML,
		scriptHTML: emitResult.ScriptHTML,
		styleHTML:  emitResult.StyleHTML,
		css:        css,
	})

	// Inject page content at {children} slot
	return &renderer.EmitResult{
		HTML:       strings.Replace(emitResult.HTML, "<!--__children__-->", content, -1),
		HeadHTML:   emitResult.HeadHTML,
		ScriptHTML: emitResult.ScriptHTML,
		StyleHTML:  emitResult.StyleHTML,
	}, css, nil
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

	tree := irtree.Build(entryModule.Program, ann)

	emitter := renderer.NewEmitter()
	result := emitter.Emit(tree)

	return result, nil
}

// mergeCSS merges new CSS into the accumulated CSS, deduplicating by individual rule.
// Rules are split respecting brace depth so @keyframes/@media rules with nested
// braces are treated as a single unit.
func mergeCSS(merged, newCSS string, seen map[string]bool) string {
	if newCSS == "" {
		return merged
	}
	var b strings.Builder
	b.Grow(len(merged) + len(newCSS))
	b.WriteString(merged)
	if merged != "" && !strings.HasSuffix(merged, "\n") {
		b.WriteByte('\n')
	}
	// Split newCSS into rules respecting brace depth
	i := 0
	for i < len(newCSS) {
		openIdx := strings.IndexByte(newCSS[i:], '{')
		if openIdx < 0 {
			// No more rules — append any trailing text
			rest := newCSS[i:]
			if strings.TrimSpace(rest) != "" {
				b.WriteString(rest)
				b.WriteByte('\n')
			}
			break
		}
		openIdx += i
		// Find matching close brace (respecting nested braces and strings)
		depth := 1
		j := openIdx + 1
		inStr := byte(0)
		for j < len(newCSS) && depth > 0 {
			ch := newCSS[j]
			if inStr != 0 {
				if ch == '\\' && j+1 < len(newCSS) {
					j += 2
					continue
				}
				if ch == inStr {
					inStr = 0
				}
			} else {
				switch ch {
				case '"', '\'':
					inStr = ch
				case '{':
					depth++
				case '}':
					depth--
				}
			}
			j++
		}
		if depth > 0 {
			// Unmatched brace — append the rest and stop
			rest := newCSS[i:]
			if strings.TrimSpace(rest) != "" {
				b.WriteString(rest)
				b.WriteByte('\n')
			}
			break
		}
		// j points just past the closing '}'; rule is [i, j)
		rule := newCSS[i:j]
		key := strings.TrimSpace(rule)
		if key != "" && !seen[key] {
			seen[key] = true
			b.WriteString(rule)
			b.WriteByte('\n')
		}
		i = j
	}
	return b.String()
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
	fsutil.WalkExt(dir, exts, nil, func(path string, _ os.FileInfo) error {
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

	var modTime time.Time
	if info, err := os.Stat(loadingPath); err == nil {
		modTime = info.ModTime()
	}
	if cached, ok := loadingEmitCache.Load(loadingPath); ok {
		entry := cached.(*layoutEmitCacheEntry)
		if entry.modTime.Equal(modTime) {
			return entry.html
		}
	}

	bnd := bundler.New(b.Root)
	bnd.SetEmitReact(b.Cfg.EmitReact)
	bnd.SetPathAliases(b.cfgPathAliasPrefixes(), b.cfgPathAliasTargets(), b.Cfg.TSBaseDir)
	bnd.SetServerComponents(b.Cfg.ServerComponents, b.Cfg.RuntimeComponents, b.Cfg.ServerDirs, b.Cfg.RuntimeDirs)
	bundle, err := bnd.Bundle(loadingPath)
	if err != nil {
		return ""
	}
	entryModule := findEntryModule(bundle.Modules)
	if entryModule == nil || entryModule.Program == nil {
		return ""
	}
	ann := annotator.Annotate(entryModule.Program, b.Cfg, loadingPath, entryModule.SourceCode)
	extraPrograms := moduleSources(bundle.Modules, entryModule)
	annotator.MergeModuleFunctions(ann, extraPrograms)
	tree := irtree.Build(entryModule.Program, ann)
	emitter := renderer.NewEmitter()
	emitter.EvalJS = b.jsExprEvaluator()
	emitResult := emitter.Emit(tree)

	loadingEmitCache.Store(loadingPath, &layoutEmitCacheEntry{
		modTime: modTime,
		html:    emitResult.HTML,
	})
	return emitResult.HTML
}

// findLoading walks up from pagePath to pagesDir looking for loading.tsx/ts/jsx/js.
func findLoading(pagePath, pagesDir string) string {
	return findLayoutFile(pagePath, pagesDir, loadingNames)
}

func findLayout(pagePath, pagesDir string) string {
	return findLayoutFile(pagePath, pagesDir, layoutNames)
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

func extractCSSModuleBindings(prog *ast.Program, entryPath string, cssModules map[string]*bundler.CSSModuleInfo) []cssModuleBinding {
	var bindings []cssModuleBinding
	entryDir := filepath.Dir(entryPath)
	for _, stmt := range prog.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok || imp.Default == "" {
			continue
		}
		src := strings.Trim(imp.Source, "\"'")
		if !strings.Contains(src, ".module.css") {
			continue
		}
		resolved := filepath.Clean(filepath.Join(entryDir, src))
		if info, ok := cssModules[resolved]; ok {
			bindings = append(bindings, cssModuleBinding{
				LocalVar: imp.Default,
				Mappings: info.Mappings,
			})
		}
	}
	return bindings
}

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
	os.MkdirAll(outDir, 0755)
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
		os.WriteFile(outPath, result.OutputFiles[0].Contents, 0644)
	}
}
func extractLiteralValue(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Literal:
		return e.Value
	case *ast.Identifier:
		return e.Name
	}
	return ""
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
	name := strings.TrimSuffix(rel, filepath.Ext(rel))
	if name == "index" || name == "home" {
		return "."
	}
	// Error pages output at root level (like index)
	base := filepath.Base(name)
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
	}
}

func (b *Builder) transformIconExpr(expr ast.Expr) ast.Expr {
	if expr == nil {
		return nil
	}

	switch e := expr.(type) {
	case *ast.JSXElement:
		if e.Opening.Name == "Icon" {
			return b.compileIconToSVG(e)
		}

		for i, child := range e.Children {
			if elChild, ok := child.(*ast.JSXElementChild); ok {
				elChild.Element = b.transformIconExpr(elChild.Element).(*ast.JSXElement)
				e.Children[i] = elChild
			}
		}
	}
	return expr
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

	resolvedSrc := src
	if !filepath.IsAbs(src) {
		resolvedSrc = filepath.Join(b.Root, "public", src)
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

	// CLS mitigation: reserve the intrinsic aspect ratio so the browser can
	// size the image before it loads, plus a blurred LQIP background.
	style := fmt.Sprintf("width:100%%;height:auto;aspect-ratio:%d/%d", result.Width, result.Height)
	if result.Placeholder != "" {
		style += ";background-image:url(" + result.Placeholder + ");background-size:cover;background-position:center"
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
		{Name: "width", Value: &ast.Literal{Kind: ast.NumberLit, Value: strconv.Itoa(result.Width)}},
		{Name: "height", Value: &ast.Literal{Kind: ast.NumberLit, Value: strconv.Itoa(result.Height)}},
		{Name: "alt", Value: &ast.Literal{Kind: ast.StringLit, Value: alt}},
		{Name: "loading", Value: &ast.Literal{Kind: ast.StringLit, Value: loading}},
		{Name: "decoding", Value: &ast.Literal{Kind: ast.StringLit, Value: "async"}},
		{Name: "style", Value: &ast.Literal{Kind: ast.StringLit, Value: style}},
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

	for _, route := range routes {
		wg.Add(1)
		go func(r string) {
			defer wg.Done()
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
