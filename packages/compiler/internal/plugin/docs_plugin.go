package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"krate-compiler/internal/config"
	"krate-compiler/internal/docs"
	"krate-compiler/internal/markdown"
)

type DocsPluginOptions struct {
	ContentDir string             `json:"contentDir"`
	Title      string             `json:"title"`
	Layout     string             `json:"layout"`
	Sidebar    []docs.SidebarItem `json:"sidebar"`
	Links      []SocialLink       `json:"links"`
	Search     *DocsSearchOptions `json:"search"`
}

type SocialLink struct {
	Icon string `json:"icon"`
	URL  string `json:"url"`
}

func init() {
	Register(&DocsPlugin{})
}

type DocsPlugin struct{}

func (p *DocsPlugin) Name() string { return "docs" }
func (p *DocsPlugin) Order() int   { return 10 }

func (p *DocsPlugin) Hooks() PluginHooks {
	return PluginHooks{
		BeforeBuild: p.beforeBuild,
	}
}

func parseDocsOptions(cfg *config.Config) *DocsPluginOptions {
	for _, pc := range cfg.Plugins {
		if pc.Name == "docs" {
			opts := &DocsPluginOptions{
				ContentDir: "content/docs",
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
		ContentDir: opts.ContentDir,
		Root:       ctx.Root,
		MDConfig: markdown.Config{
			GFM:            true,
			HeadingAnchors: true,
			Admonitions:    true,
		},
	}

	pages, err := docs.Scan(scanCfg)
	if err != nil {
		return fmt.Errorf("scanning docs: %w", err)
	}
	if len(pages) == 0 {
		return nil
	}

	sections := docs.BuildSidebarTree(pages)

	p.writeAssets(ctx, sections, pages, opts)

	// Search bar + search index (docfind WASM, embedded in-process)
	searchEnabled, searchEngine, searchMaxResults := searchConfig(opts)
	if searchEnabled {
		if err := p.buildSearchAssets(ctx, pages, searchEngine, searchMaxResults); err != nil {
			fmt.Fprintf(os.Stderr, "  Docs search warning: %v (falling back to JSON search)\n", err)
		}
	}

	genDir := filepath.Join(ctx.Root, ".krate", "gen", "docs")

	os.RemoveAll(genDir)
	os.MkdirAll(genDir, 0755)

	// Generate the SearchBar component once into the gen dir (shared by all pages)
	if searchEnabled {
		os.WriteFile(filepath.Join(genDir, "SearchBar.tsx"), []byte(generateSearchBarTSX()), 0644)
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

			tocItems := docs.ExtractTOC(page.Content)
			breadcrumbs := docs.BuildBreadcrumbs(page.Path)

			tsxPath := filepath.Join(genDir, page.Path+".tsx")
			fileLayoutRel := resolveLayoutImport(filepath.Dir(tsxPath), ctx.Root, opts.Layout)
			var searchBarRel string
			if searchEnabled {
				searchBarRel = searchBarImportRel(tsxPath, genDir)
			}
			tsxSource := p.generateTSX(ctx, page, fileLayoutRel, searchBarRel, sections, tocItems, breadcrumbs, prevTitle, prevLink, nextTitle, nextLink, opts.Title, opts.Links)

			os.MkdirAll(filepath.Dir(tsxPath), 0755)
			os.WriteFile(tsxPath, []byte(tsxSource), 0644)

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

	return nil
}

func resolveLayoutImport(genDir, root, layout string) string {
	if layout == "" {
		return ""
	}
	layout = strings.TrimSuffix(layout, ".tsx")
	layout = strings.TrimSuffix(layout, ".ts")
	layout = strings.TrimSuffix(layout, ".jsx")
	layout = strings.TrimSuffix(layout, ".js")
	layoutPath := filepath.Join(root, layout)
	rel, err := filepath.Rel(genDir, layoutPath)
	if err != nil {
		return layout
	}
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	return rel
}

func (p *DocsPlugin) generateTSX(ctx *BuildHookCtx, page docs.Page, layoutRel, searchBarRel string, sections []docs.SidebarItem, tocItems []docs.TOCItem, breadcrumbs []docs.Breadcrumb, prevTitle, prevLink, nextTitle, nextLink, siteTitle string, socialLinks []SocialLink) string {
	var sb strings.Builder
	sb.WriteString("// Auto-generated by krate docs plugin\n")

	var mdxImports []string
	var segments []markdown.MDXSegment
	if strings.HasSuffix(page.SourcePath, ".mdx") {
		if data, err := os.ReadFile(page.SourcePath); err == nil {
			src := string(data)
			mdxImports = markdown.ExtractImports(src)
			mdCfg := markdown.Config{GFM: true, HeadingAnchors: true, Admonitions: true}
			_, segments = markdown.ParseMDXSegments(src, mdCfg)
		}
	}

	for _, imp := range mdxImports {
		sb.WriteString(imp)
		sb.WriteString("\n")
	}

	genDir := filepath.Join(ctx.Root, ".krate", "gen", "docs")
	compDir := filepath.Join(ctx.Root, "src", "components", "docs")
	compRel, _ := filepath.Rel(genDir, compDir)
	compRel = filepath.ToSlash(compRel)
	if !strings.HasPrefix(compRel, ".") {
		compRel = "./" + compRel
	}

	if layoutRel != "" {
		sb.WriteString(fmt.Sprintf("import DocsLayout from \"%s\";\n", layoutRel))
	}
	if searchBarRel != "" {
		sb.WriteString(fmt.Sprintf("import SearchBar from \"%s\";\n", searchBarRel))
	}
	sb.WriteString(fmt.Sprintf("import SidebarNav from \"%s/SidebarNav\";\n", compRel))
	sb.WriteString(fmt.Sprintf("import TOCNav from \"%s/TOCNav\";\n", compRel))
	sb.WriteString(fmt.Sprintf("import Breadcrumbs from \"%s/Breadcrumbs\";\n", compRel))
	sb.WriteString(fmt.Sprintf("import PrevNext from \"%s/PrevNext\";\n", compRel))
	sb.WriteString(fmt.Sprintf("import SocialLinks from \"%s/SocialLinks\";\n", compRel))

	sb.WriteString("\n")

	sidebarItems := sections
	if len(page.CustomSidebar) > 0 {
		sidebarItems = page.CustomSidebar
	}
	enriched := docs.EnrichSidebarItems(sidebarItems, page.Path)
	sidebarJSON, _ := json.Marshal(enriched)
	tocJSON, _ := json.Marshal(tocItems)
	breadcrumbsJSON, _ := json.Marshal(breadcrumbs)
	socialJSON, _ := json.Marshal(socialLinks)

	sb.WriteString("export default function DocPage() {\n")
	sb.WriteString("  return (\n")
	sb.WriteString("    <>\n")
	if searchBarRel != "" {
		sb.WriteString("      <SearchBar />\n")
	}
	sb.WriteString("      <DocsLayout\n")
	sb.WriteString("    pageTitle=\"")
	sb.WriteString(page.Title)
	sb.WriteString("\"\n")
	sb.WriteString("    siteTitle=\"")
	sb.WriteString(siteTitle)
	sb.WriteString("\"\n")

	sb.WriteString("    sidebarItems={")
	sb.WriteString(string(sidebarJSON))
	sb.WriteString("}\n")

	sb.WriteString("    tocItems={")
	sb.WriteString(string(tocJSON))
	sb.WriteString("}\n")

	sb.WriteString("    breadcrumbs={")
	sb.WriteString(string(breadcrumbsJSON))
	sb.WriteString("}\n")

	if prevTitle != "" {
		sb.WriteString("    prevTitle=\"")
		sb.WriteString(escapeJSXAttr(prevTitle))
		sb.WriteString("\"\n")
	}
	if prevLink != "" {
		sb.WriteString("    prevLink=\"")
		sb.WriteString(escapeJSXAttr(prevLink))
		sb.WriteString("\"\n")
	}
	if nextTitle != "" {
		sb.WriteString("    nextTitle=\"")
		sb.WriteString(escapeJSXAttr(nextTitle))
		sb.WriteString("\"\n")
	}
	if nextLink != "" {
		sb.WriteString("    nextLink=\"")
		sb.WriteString(escapeJSXAttr(nextLink))
		sb.WriteString("\"\n")
	}

	sb.WriteString("    socialLinks={")
	sb.WriteString(string(socialJSON))
	sb.WriteString("}\n")

	sb.WriteString("    currentPath=\"")
	sb.WriteString(page.Path)
	sb.WriteString("\"\n")

	sb.WriteString("  >")

	if len(segments) > 0 {
		sb.WriteString("\n      <div class=\"md-content\">\n")
		for _, seg := range segments {
			if seg.HTML != "" {
				sb.WriteString("        <div dangerouslySetInnerHTML={{__html: `")
				sb.WriteString(escapeTemplateLit(seg.HTML))
				sb.WriteString("`}} />\n")
			}
			if seg.JSX != "" {
				sb.WriteString("        ")
				sb.WriteString(seg.JSX)
				sb.WriteString("\n")
			}
		}
		sb.WriteString("      </div>\n")
	} else {
		content := page.Content
		sb.WriteString("<div class=\"md-content\" dangerouslySetInnerHTML={{__html: `")
		sb.WriteString(escapeTemplateLit(content))
		sb.WriteString("`}} />")
	}

	sb.WriteString("      </DocsLayout>\n")
	sb.WriteString("    </>\n")
	sb.WriteString("  );\n")
	sb.WriteString("}\n")

	return sb.String()
}

func escapeJSXAttr(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func escapeTemplateLit(s string) string {
	s = strings.ReplaceAll(s, "`", "\\`")
	s = strings.ReplaceAll(s, "${", "\\${")
	return s
}

func (p *DocsPlugin) writeAssets(ctx *BuildHookCtx, sections []docs.SidebarItem, pages []docs.Page, opts *DocsPluginOptions) {
	outDir := filepath.Join(ctx.OutDir, "docs")
	os.MkdirAll(filepath.Join(outDir, "data"), 0755)

	sidebarData, _ := json.Marshal(sections)
	os.WriteFile(filepath.Join(outDir, "data/sidebar.json"), sidebarData, 0644)

	searchData, _ := json.Marshal(docs.BuildSearchIndex(pages))
	os.WriteFile(filepath.Join(outDir, "data/search-index.json"), searchData, 0644)
}
