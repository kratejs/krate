package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/content"
	"github.com/kratejs/krate/packages/compiler/internal/docs"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/markdown"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
)

type DocsPluginOptions struct {
	ContentDir   string             `json:"contentDir"`
	Title        string             `json:"title"`
	Layout       string             `json:"layout"`
	Theme        json.RawMessage    `json:"theme"` // string (path or npm specifier) or DocsThemeDescriptor
	Sidebar      []docs.SidebarItem `json:"sidebar"`
	Links        []SocialLink       `json:"links"`
	Search       *DocsSearchOptions `json:"search"`
	EditLinkBase string             `json:"editLinkBase"`
	// LastUpdated fills each page's "last updated" date from git history when
	// frontmatter omits it (default: true). Set false to skip git lookups.
	LastUpdated *bool `json:"lastUpdated,omitempty"`
}

// DocsThemeDescriptor mirrors the shape a docs theme factory returns
// (@krate/plugin DocsThemeDescriptor). Module is an absolute filesystem path
// (a file:// URL converted to a path by the config bootstrap) or a npm/relative
// specifier; Options are forwarded to the layout component as props.options.
type DocsThemeDescriptor struct {
	Name    string                 `json:"name,omitempty"`
	Module  string                 `json:"module,omitempty"`
	Layout  string                 `json:"layout,omitempty"`
	Options map[string]interface{} `json:"options,omitempty"`
}

type SocialLink struct {
	Icon string `json:"icon"`
	URL  string `json:"url"`
}

func init() {
	_ = Register(&DocsPlugin{})
}

type DocsPlugin struct{}

func (p *DocsPlugin) Name() string { return "docs" }
func (p *DocsPlugin) Order() int   { return 10 }

func (p *DocsPlugin) Hooks() PluginHooks {
	return PluginHooks{
		BeforeBuild: p.beforeBuild,
		AfterBuild:  p.afterBuild,
		Collections: p.collections,
	}
}

// collections contributes the docs content collection so docs become a proper
// content collection (validation, types via krate/content, MCP content tools)
// while the docs plugin keeps rendering the pages. Returns nil when the docs
// plugin is not configured.
func (p *DocsPlugin) collections(cfg *config.Config) []CollectionContribution {
	opts := parseDocsOptions(cfg)
	if opts == nil {
		return nil
	}
	return []CollectionContribution{{
		Name:   "docs",
		Dir:    opts.ContentDir,
		Schema: docsContentSchema(),
	}}
}

// docsContentSchema is the validation schema for docs frontmatter. It mirrors
// the typed fields the docs plugin decodes; complex/nested values (sidebar,
// toc, hero, badge, head) are intentionally untyped so they pass through.
func docsContentSchema() map[string]content.Field {
	return map[string]content.Field{
		"title":       {Type: content.TypeString},
		"description": {Type: content.TypeString},
		"template":    {Type: content.TypeString},
		"editUrl":     {Type: content.TypeString},
		"order":       {Type: content.TypeNumber},
		"draft":       {Type: content.TypeBoolean},
		"keywords":    {Type: content.TypeStringA},
		"tags":        {Type: content.TypeStringA},
		"categories":  {Type: content.TypeStringA},
	}
}

func parseDocsOptions(cfg *config.Config) *DocsPluginOptions {
	for _, pc := range cfg.Plugins {
		if pc.Name == "docs" {
			opts := &DocsPluginOptions{
				ContentDir: "src/content/docs",
				Title:      "Docs",
			}
			if pc.Options != nil {
				data, err := json.Marshal(pc.Options)
				if err != nil {
					return nil
				}
				if err := json.Unmarshal(data, opts); err != nil {
					return nil
				}
			}
			return opts
		}
	}
	return nil
}

func (p *DocsPlugin) beforeBuild(ctx *BuildHookCtx) error {
	cfg, ok := ctx.Config.(*config.Config)
	if !ok {
		return nil
	}

	opts := parseDocsOptions(cfg)
	if opts == nil {
		return nil
	}

	scanCfg := docs.Config{
		ContentDir:     opts.ContentDir,
		Root:           ctx.Root,
		MDConfig:       cfg.Markdown,
		GitLastUpdated: opts.LastUpdated == nil || *opts.LastUpdated,
	}

	pages, err := docs.Scan(scanCfg)
	if err != nil {
		return fmt.Errorf("scanning docs: %w", err)
	}
	if len(pages) == 0 {
		return nil
	}

	// Draft pages are excluded from production builds but rendered in dev.
	// Filtering here keeps them out of the nav tree, search index, and output
	// directory when they shouldn't ship.
	pages = filterDraftPages(pages, ctx.DevMode)
	if len(pages) == 0 {
		return nil
	}

	// A global `sidebar` option overrides the auto-generated tree; otherwise the
	// tree is derived from the page directory/frontmatter metadata.
	sections := docs.BuildSidebarTree(pages)
	if len(opts.Sidebar) > 0 {
		sections = opts.Sidebar
	}

	p.writeAssets(ctx, sections, pages, opts)

	// Search bar + search index (docfind WASM, embedded in-process; or the
	// opt-in Pagefind bundle indexed in AfterBuild)
	searchEnabled, searchEngine, searchMaxResults := searchConfig(opts)
	if searchEnabled {
		if err := p.buildSearchAssets(ctx, pages, searchEngine, searchMaxResults, pagefindOptions(opts)); err != nil {
			fmt.Fprintf(os.Stderr, "  Docs search warning: %v (falling back to JSON search)\n", err)
		}
	}

	genDir := filepath.Join(ctx.Root, ".krate", "gen", "docs")

	os.RemoveAll(genDir)
	_ = os.MkdirAll(genDir, 0755)

	// Resolve the docs layout/theme once up front. The result is either a bare
	// npm specifier (kept as-is so CSS/sub-components flow through the bundler
	// import graph) or an absolute layout file path (relativized per page).
	theme, err := p.resolveDocsTheme(ctx.Root, opts)
	if err != nil {
		return err
	}

	// Search is a THEME concern: the plugin emits the index + `search.js`
	// (which exposes the headless `window.__krateSearch` API), and the theme
	// renders its own UI (e.g. `<DocsSearch />`) wherever it likes. No widget
	// markup or default styling is injected into pages.

	var themeOptions json.RawMessage
	if theme != nil {
		themeOptions = theme.options
	}

	type pageGenResult struct {
		tsxPath string
		route   string
	}

	resultsCh := make(chan pageGenResult, len(pages))
	var wg sync.WaitGroup

	for i, page := range pages {
		var prevTitle, prevLink, nextTitle, nextLink string
		if i > 0 {
			prevTitle = pages[i-1].Title
			prevLink = docs.PageURL(pages[i-1].Path)
		}
		if i < len(pages)-1 {
			nextTitle = pages[i+1].Title
			nextLink = docs.PageURL(pages[i+1].Path)
		}

		wg.Add(1)
		go func(page docs.Page, prevTitle, prevLink, nextTitle, nextLink string) {
			defer wg.Done()

			tocItems := pageTocItems(page)
			breadcrumbs := docs.BuildBreadcrumbs(page.Path)

			tsxPath := filepath.Join(genDir, page.Path+".tsx")
			fileLayoutRel := theme.importSpecifier(filepath.Dir(tsxPath))
			tsxSource := p.generateTSX(ctx, cfg, page, fileLayoutRel, searchEnabled, sections, tocItems, breadcrumbs, prevTitle, prevLink, nextTitle, nextLink, opts, themeOptions, cfg.Markdown)

			_ = os.MkdirAll(filepath.Dir(tsxPath), 0755)
			_ = os.WriteFile(tsxPath, []byte(tsxSource), 0644)

			route := docs.NormalizePagePath(page.Path)
			if route == "" {
				route = "docs"
			} else {
				route = "docs/" + route
			}
			resultsCh <- pageGenResult{tsxPath: tsxPath, route: route}
		}(page, prevTitle, prevLink, nextTitle, nextLink)
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	for res := range resultsCh {
		if ctx.GeneratedPages != nil {
			*ctx.GeneratedPages = append(*ctx.GeneratedPages, GeneratedPage{Path: res.tsxPath, Route: res.route})
		}
	}

	if err := generateTaxonomyPages(ctx, genDir, theme, opts, pages, sections); err != nil {
		return fmt.Errorf("generating tag/category pages: %w", err)
	}

	return nil
}

// trimComponentExt strips a component file extension so both the raw file and
// its extensionless form resolve to the same module.
func trimComponentExt(s string) string {
	s = strings.TrimSuffix(s, ".tsx")
	s = strings.TrimSuffix(s, ".ts")
	s = strings.TrimSuffix(s, ".jsx")
	s = strings.TrimSuffix(s, ".js")
	return s
}

// resolvedDocsTheme is the outcome of resolving the docs plugin's layout/theme
// option. Exactly one of module/spec is set:
//   - module: absolute layout file path — emit a relative import from each page.
//   - spec:   bare npm specifier — keep it as-is so the theme's CSS and
//     sub-components flow through the bundler's node_modules resolution.
type resolvedDocsTheme struct {
	module  string
	spec    string
	options json.RawMessage
}

// importSpecifier returns the import path used by a generated page at genDir to
// reach this theme's layout component.
func (t *resolvedDocsTheme) importSpecifier(genDir string) string {
	if t == nil {
		return ""
	}
	if t.spec != "" {
		return t.spec
	}
	if t.module == "" {
		return ""
	}
	rel, err := filepath.Rel(genDir, t.module)
	if err != nil {
		return filepath.ToSlash(t.module)
	}
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	return rel
}

// isPathLike reports whether a specifier is a filesystem path (relative, or a
// bare npm package name with a relative/drive prefix) rather than a bare package.
func isPathLike(s string) bool {
	if strings.HasPrefix(s, ".") || strings.HasPrefix(s, "/") || strings.HasPrefix(s, "\\") {
		return true
	}
	if filepath.IsAbs(s) {
		return true
	}
	if len(s) >= 2 && s[1] == ':' {
		return true
	}
	return false
}

// resolveLayoutFile maps a root-relative layout/theme specifier to an absolute,
// extension-free file path for conflict comparisons. Bare npm specifiers are
// not path-like and map to "".
func resolveLayoutFile(root, layout string) string {
	if layout == "" {
		return ""
	}
	if isPathLike(layout) && !filepath.IsAbs(layout) {
		return filepath.Join(root, trimComponentExt(layout))
	}
	if filepath.IsAbs(layout) {
		return filepath.Clean(trimComponentExt(layout))
	}
	return ""
}

// themeLayoutConflict reports whether the legacy root-relative `layout` option
// and a path-like theme path resolve to different components. The layout option
// is always treated as a root-relative path even when it lacks a leading "./",
// so its absolute file must be compared directly rather than via
// resolveLayoutFile (which treats non-path-like strings as bare specifiers).
func themeLayoutConflict(root, layout, themeName, themePath string) error {
	if layout == "" {
		return nil
	}
	themeFile := resolveLayoutFile(root, themePath)
	if themeFile == "" {
		return nil
	}
	if layoutFile := filepath.Join(root, trimComponentExt(layout)); layoutFile != themeFile {
		return fmt.Errorf("docs plugin: both layout (%q) and theme (%q) are set but resolve to different components; set only one", layout, themeName)
	}
	return nil
}

// resolveDocsTheme resolves the docs plugin's layout/theme options into an
// importable layout component. It honors both the legacy `layout` option and
// the `theme` alias:
//
//   - theme (""| nothing) + layout -> root-relative file, like today.
//   - theme "./path" (or "/abs")  -> alias of layout; error if both are set and
//     resolve to different components.
//   - theme "npm-pkg"             -> installed docs theme, emitted as a bare
//     specifier so its CSS/sub-components bundle through node_modules.
//   - theme { module, layout, options } -> theme factory descriptor; module may
//     be an absolute path (from a file:// URL), a root-relative path, or a bare
//     npm specifier. options are forwarded to the layout as props.options.
func (p *DocsPlugin) resolveDocsTheme(root string, opts *DocsPluginOptions) (*resolvedDocsTheme, error) {
	hasTheme := len(opts.Theme) > 0 && string(opts.Theme) != "null"
	if !hasTheme {
		if opts.Layout == "" {
			return nil, nil
		}
		return &resolvedDocsTheme{module: filepath.Join(root, trimComponentExt(opts.Layout))}, nil
	}

	// String form: a component path (alias of layout) or an npm package name.
	var themeStr string
	if err := json.Unmarshal(opts.Theme, &themeStr); err == nil {
		if isPathLike(themeStr) {
			if err := themeLayoutConflict(root, opts.Layout, themeStr, themeStr); err != nil {
				return nil, err
			}
			return &resolvedDocsTheme{module: filepath.Join(root, trimComponentExt(themeStr))}, nil
		}
		if opts.Layout != "" {
			return nil, fmt.Errorf("docs plugin: both layout (%q) and theme (%q) are set but resolve to different components; set only one", opts.Layout, themeStr)
		}
		if entry := resolver.NodeModule(root, themeStr); entry == "" {
			return nil, fmt.Errorf("docs plugin: theme package %q not found in node_modules (searched from %s)", themeStr, root)
		}
		return &resolvedDocsTheme{spec: themeStr}, nil
	}

	// Descriptor (factory) form.
	var desc DocsThemeDescriptor
	if err := json.Unmarshal(opts.Theme, &desc); err != nil {
		return nil, fmt.Errorf("docs plugin: theme must be a package name, a component path, or a theme descriptor object: %w", err)
	}

	mod := desc.Module
	if mod == "" {
		mod = desc.Layout
	}
	if mod == "" {
		return nil, fmt.Errorf("docs plugin: theme descriptor %q has no module or layout to import", desc.Name)
	}

	var options json.RawMessage
	if desc.Options != nil {
		if data, err := json.Marshal(desc.Options); err == nil {
			options = data
		}
	}

	if isPathLike(mod) {
		if err := themeLayoutConflict(root, opts.Layout, desc.Name, mod); err != nil {
			return nil, err
		}
		if filepath.IsAbs(mod) {
			return &resolvedDocsTheme{module: trimComponentExt(mod), options: options}, nil
		}
		return &resolvedDocsTheme{module: filepath.Join(root, trimComponentExt(mod)), options: options}, nil
	}

	// Bare npm specifier module.
	if opts.Layout != "" {
		return nil, fmt.Errorf("docs plugin: both layout (%q) and theme (%q) are set but resolve to different components; set only one", opts.Layout, desc.Name)
	}
	if entry := resolver.NodeModule(root, mod); entry == "" {
		return nil, fmt.Errorf("docs plugin: theme descriptor module %q not found in node_modules", mod)
	}
	return &resolvedDocsTheme{spec: mod, options: options}, nil
}

// marshalJSONArray marshals a slice as a JSON array literal, emitting `[]`
// (instead of JSON's `null`) when the slice is nil. Docs pages embed these in
// TSX as `tocItems: []`; a nil slice serialized as `null` parses to a null
// literal that the compiler cannot const-fold into the child props registry.
func marshalJSONArray(v any) string {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice && rv.IsNil() {
		return "[]"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// filterDraftPages drops draft pages from the build. In dev mode (ctx.DevMode)
// drafts render normally so authors can preview work-in-progress pages.
func filterDraftPages(pages []docs.Page, devMode bool) []docs.Page {
	if devMode {
		return pages
	}
	out := pages[:0]
	for _, p := range pages {
		if !p.Draft {
			out = append(out, p)
		}
	}
	return out
}

// pageTocItems extracts the page's TOC headings, honoring per-page config
// (toc: false disables it; toc: {minLevel, maxLevel} bounds the range).
func pageTocItems(page docs.Page) []docs.TOCItem {
	if page.Toc.Disabled {
		return nil
	}
	minLevel := page.Toc.MinLevel
	maxLevel := page.Toc.MaxLevel
	if minLevel < 2 {
		minLevel = 2
	}
	if maxLevel < minLevel {
		maxLevel = minLevel
	}
	return docs.ExtractTOCLevels(page.Content, minLevel, maxLevel)
}

// applyNavOverride resolves a page's prev/next frontmatter against the
// auto-computed neighbor. Returning ok=false tells the emitter to omit the
// link entirely (e.g. prev: false on the first page).
func applyNavOverride(override *docs.NavOverride, autoTitle, autoLink string) (title, link string, ok bool) {
	if override == nil {
		return autoTitle, autoLink, autoLink != ""
	}
	if override.Disabled {
		return "", "", false
	}
	title, link = autoTitle, autoLink
	if override.Text != "" {
		title = override.Text
	}
	if override.Link != "" {
		link = override.Link
	}
	return title, link, link != ""
}

func (p *DocsPlugin) generateTSX(ctx *BuildHookCtx, cfg *config.Config, page docs.Page, layoutRel string, searchEnabled bool, sections []docs.SidebarItem, tocItems []docs.TOCItem, breadcrumbs []docs.Breadcrumb, prevTitle, prevLink, nextTitle, nextLink string, opts *DocsPluginOptions, themeOptions json.RawMessage, mdConfig markdown.Config) string {
	prevTitle, prevLink, prevOk := applyNavOverride(page.Prev, prevTitle, prevLink)
	nextTitle, nextLink, nextOk := applyNavOverride(page.Next, nextTitle, nextLink)
	siteTitle := opts.Title
	socialLinks := opts.Links

	var sb strings.Builder
	sb.WriteString("// Auto-generated by krate docs plugin\n")

	var mdxImports []string
	var segments []markdown.MDXSegment
	var rawSrc string
	if data, err := os.ReadFile(page.SourcePath); err == nil {
		rawSrc = string(data)
		mdxImports = markdown.ExtractImports(rawSrc)
		_, segments = markdown.ParseMDXSegments(rawSrc, mdConfig)
	}
	useCode := markdown.HasCodeSegments(segments)
	useAside := markdown.HasAsideSegments(segments)

	for _, imp := range mdxImports {
		sb.WriteString(imp)
		sb.WriteString("\n")
	}

	// Auto-import the built-in components referenced by the page: Code/Aside
	// segments plus the components lowered from :::directives.
	imported := map[string]bool{}
	addImport := func(name string) {
		if imported[name] {
			return
		}
		imported[name] = true
		sb.WriteString("import { " + name + " } from \"@krate/components\";\n")
	}
	if useCode {
		addImport("Code")
	}
	if useAside {
		addImport("Aside")
	}
	for _, comp := range markdown.DirectiveComponents(rawSrc) {
		addImport(comp)
	}

	if layoutRel != "" {
		sb.WriteString(fmt.Sprintf("import DocsLayout from \"%s\";\n", layoutRel))
	}

	sb.WriteString("\n")

	sidebarItems := sections
	if len(page.CustomSidebar) > 0 {
		sidebarItems = page.CustomSidebar
	}
	enriched := docs.EnrichSidebarItems(sidebarItems, page.Path)
	sidebarJSON := marshalJSONArray(enriched)
	tocJSON := marshalJSONArray(tocItems)
	breadcrumbsJSON := marshalJSONArray(breadcrumbs)
	socialJSON := marshalJSONArray(socialLinks)
	pageTitleJSON, _ := json.Marshal(page.Title)
	siteTitleJSON, _ := json.Marshal(siteTitle)
	currentPathJSON, _ := json.Marshal(page.Path)
	templateJSON, _ := json.Marshal(page.Template)

	sb.WriteString("export default function DocPage() {\n")
	sb.WriteString("  const docsProps = {\n")
	sb.WriteString("    pageTitle: ")
	sb.WriteString(string(pageTitleJSON))
	sb.WriteString(",\n")
	sb.WriteString("    siteTitle: ")
	sb.WriteString(string(siteTitleJSON))
	sb.WriteString(",\n")
	sb.WriteString("    sidebarItems: ")
	sb.WriteString(string(sidebarJSON))
	sb.WriteString(",\n")
	sb.WriteString("    tocItems: ")
	sb.WriteString(string(tocJSON))
	sb.WriteString(",\n")
	sb.WriteString("    breadcrumbs: ")
	sb.WriteString(string(breadcrumbsJSON))
	sb.WriteString(",\n")

	if page.Description != "" {
		descriptionJSON, _ := json.Marshal(page.Description)
		sb.WriteString("    description: ")
		sb.WriteString(string(descriptionJSON))
		sb.WriteString(",\n")
	}

	sb.WriteString("    template: ")
	sb.WriteString(string(templateJSON))
	sb.WriteString(",\n")

	if page.Hero != nil {
		heroJSON, _ := json.Marshal(page.Hero)
		sb.WriteString("    hero: ")
		sb.WriteString(string(heroJSON))
		sb.WriteString(",\n")
	}

	if page.Toc.Disabled {
		sb.WriteString("    tocHidden: true,\n")
	}
	if page.Toc.Label != "" {
		tocLabelJSON, _ := json.Marshal(page.Toc.Label)
		sb.WriteString("    tocLabel: ")
		sb.WriteString(string(tocLabelJSON))
		sb.WriteString(",\n")
	}

	editUrl := page.EditURL
	if editUrl == "" && opts.EditLinkBase != "" {
		if rel, err := filepath.Rel(ctx.Root, page.SourcePath); err == nil {
			editUrl = strings.TrimSuffix(opts.EditLinkBase, "/") + "/" + filepath.ToSlash(rel)
		}
	}
	if editUrl != "" {
		editUrlJSON, _ := json.Marshal(editUrl)
		sb.WriteString("    editUrl: ")
		sb.WriteString(string(editUrlJSON))
		sb.WriteString(",\n")
	}

	if len(page.Tags) > 0 {
		tagsJSON := marshalJSONArray(page.Tags)
		sb.WriteString("    tags: ")
		sb.WriteString(string(tagsJSON))
		sb.WriteString(",\n")
	}
	if len(page.Categories) > 0 {
		catsJSON := marshalJSONArray(page.Categories)
		sb.WriteString("    categories: ")
		sb.WriteString(string(catsJSON))
		sb.WriteString(",\n")
	}
	if page.Date != "" {
		dateJSON, _ := json.Marshal(page.Date)
		sb.WriteString("    date: ")
		sb.WriteString(string(dateJSON))
		sb.WriteString(",\n")
	}
	if page.Updated != "" {
		updatedJSON, _ := json.Marshal(page.Updated)
		sb.WriteString("    lastUpdated: ")
		sb.WriteString(string(updatedJSON))
		sb.WriteString(",\n")
	}

	if prevOk {
		prevTitleJSON, _ := json.Marshal(prevTitle)
		sb.WriteString("    prevTitle: ")
		sb.WriteString(string(prevTitleJSON))
		sb.WriteString(",\n")
		prevLinkJSON, _ := json.Marshal(prevLink)
		sb.WriteString("    prevLink: ")
		sb.WriteString(string(prevLinkJSON))
		sb.WriteString(",\n")
	}
	if nextOk {
		nextTitleJSON, _ := json.Marshal(nextTitle)
		sb.WriteString("    nextTitle: ")
		sb.WriteString(string(nextTitleJSON))
		sb.WriteString(",\n")
		nextLinkJSON, _ := json.Marshal(nextLink)
		sb.WriteString("    nextLink: ")
		sb.WriteString(string(nextLinkJSON))
		sb.WriteString(",\n")
	}

	sb.WriteString("    socialLinks: ")
	sb.WriteString(string(socialJSON))
	sb.WriteString(",\n")

	sb.WriteString("    currentPath: ")
	sb.WriteString(string(currentPathJSON))
	sb.WriteString(",\n")

	if len(strings.TrimSpace(string(themeOptions))) > 2 {
		sb.WriteString("    options: ")
		sb.WriteString(string(themeOptions))
		sb.WriteString(",\n")
	}

	sb.WriteString("  };\n")
	sb.WriteString("  return (\n")
	sb.WriteString("    <>\n")

	// Per-page <Head>: meta description/OG tags plus any frontmatter head tags.
	// Krate merges multiple <Head> blocks (page + layout), so emitting ours
	// alongside the layout's title composition works.
	sb.WriteString("      <Head>\n")
	// Load the headless search module (defines `window.__krateSearch`) so the
	// theme's search UI can query the index. The theme owns all markup/styles.
	if searchEnabled {
		sb.WriteString("        <script src=\"/docs/search/search.js\" defer={true}></script>\n")
	}
	if page.Description != "" {
		sb.WriteString("        <meta name=\"description\" content=")
		sb.WriteString(jsxAttrExpr(page.Description))
		sb.WriteString(" />\n")
		sb.WriteString("        <meta property=\"og:description\" content=")
		sb.WriteString(jsxAttrExpr(page.Description))
		sb.WriteString(" />\n")
	}
	sb.WriteString(docsHeadExtras(cfg, page, opts.Title))
	for _, ht := range page.Head {
		sb.WriteString("        ")
		sb.WriteString(headTagJSX(ht))
		sb.WriteString("\n")
	}
	// Pre-paint theme script: apply the persisted/preferred colour scheme before
	// the stylesheet paints, so the page never flashes the wrong theme. Runs
	// before the hydration bundle; the layout later reconciles the signal.
	themeKey := "theme"
	if len(themeOptions) > 0 {
		var to struct {
			ThemeStorageKey string `json:"themeStorageKey"`
		}
		if json.Unmarshal(themeOptions, &to) == nil && to.ThemeStorageKey != "" {
			themeKey = to.ThemeStorageKey
		}
	}
	keyJSON, _ := json.Marshal(themeKey)
	prepaint := "(function(){try{var k=" + string(keyJSON) +
		";var s=localStorage.getItem(k);if(s!=='dark'&&s!=='light'){s=window.matchMedia('(prefers-color-scheme: dark)').matches?'dark':'light';}" +
		"document.documentElement.setAttribute('data-theme',s);}catch(e){}})();"
	sb.WriteString("        <script>{`")
	sb.WriteString(escapeTemplateLit(prepaint))
	sb.WriteString("`}</script>\n")
	sb.WriteString("      </Head>\n")

	sb.WriteString("      <DocsLayout {...docsProps} >")
	if len(segments) > 0 {
		sb.WriteString("\n      <div class=\"md-content\" data-pagefind-body>\n")
		for _, seg := range segments {
			if seg.HTML != "" {
				html := seg.HTML
				if mdConfig.HeadingAnchors {
					html = docs.InjectHeadingAnchors(html)
				}
				sb.WriteString("        <div dangerouslySetInnerHTML={{__html: `")
				sb.WriteString(escapeTemplateLit(html))
				sb.WriteString("`}} />\n")
			}
			if seg.JSX != "" {
				sb.WriteString("        ")
				sb.WriteString(seg.JSX)
				sb.WriteString("\n")
			}
			if seg.Code != nil {
				sb.WriteString("        ")
				sb.WriteString(markdown.BuildCodeJSX(seg.Code.Lang, seg.Code.Code))
				sb.WriteString("\n")
			}
			if seg.Aside != nil {
				sb.WriteString("        ")
				sb.WriteString(markdown.BuildAsideJSX(seg.Aside))
				sb.WriteString("\n")
			}
		}
		sb.WriteString("      </div>\n")
	} else {
		content := page.Content
		if mdConfig.HeadingAnchors {
			content = docs.InjectHeadingAnchors(content)
		}
		sb.WriteString("<div class=\"md-content\" data-pagefind-body dangerouslySetInnerHTML={{__html: `")
		sb.WriteString(escapeTemplateLit(content))
		sb.WriteString("`}} />")
	}

	sb.WriteString("      </DocsLayout>\n")
	sb.WriteString("    </>\n")
	sb.WriteString("  );\n")
	sb.WriteString("}\n")

	return sb.String()
}

func escapeTemplateLit(s string) string {
	// Order matters: escape backslashes first so the escapes introduced for the
	// backtick and interpolation below are not themselves doubled.
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "`", "\\`")
	s = strings.ReplaceAll(s, "${", "\\${")
	return s
}

// jsxAttrExpr renders an attribute value as a JS string EXPRESSION (`{ "..." }`).
// The krate lexer reads JSX attribute strings verbatim (no escape decoding), so
// a value containing a quote would otherwise need a backslash that survives into
// the AST. An expression-based string literal is decoded by UnescapeJSString,
// so quotes/ampersands/backslashes round-trip exactly.
func jsxAttrExpr(s string) string {
	return "{" + escape.JSStringDQ(s) + "}"
}

func isVoidHeadTag(tag string) bool {
	switch tag {
	case "meta", "link", "base":
		return true
	}
	return false
}

// headTagJSX renders a frontmatter `head:` entry as a JSX element, e.g.
// `head: [["meta", {name: "robots", content: "noindex"}]]` produces
// `<meta name="robots" content="noindex" />`.
func headTagJSX(t docs.HeadTag) string {
	var sb strings.Builder
	sb.WriteString("<")
	sb.WriteString(t.Tag)
	keys := make([]string, 0, len(t.Attrs))
	for k := range t.Attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sb.WriteString(" ")
		sb.WriteString(k)
		sb.WriteString("=")
		sb.WriteString(jsxAttrExpr(t.Attrs[k]))
	}
	if isVoidHeadTag(t.Tag) {
		sb.WriteString(" />")
	} else {
		sb.WriteString("></")
		sb.WriteString(t.Tag)
		sb.WriteString(">")
	}
	return sb.String()
}

func (p *DocsPlugin) writeAssets(ctx *BuildHookCtx, sections []docs.SidebarItem, pages []docs.Page, opts *DocsPluginOptions) {
	outDir := filepath.Join(ctx.OutDir, "docs")
	_ = os.MkdirAll(filepath.Join(outDir, "data"), 0755)

	sidebarData, _ := json.Marshal(sections)
	_ = os.WriteFile(filepath.Join(outDir, "data/sidebar.json"), sidebarData, 0644)

	searchData, _ := json.Marshal(docs.BuildSearchIndex(pages))
	_ = os.WriteFile(filepath.Join(outDir, "data/search-index.json"), searchData, 0644)
}
