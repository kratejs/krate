package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/docs"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
)

// taxonomyEntry is one tag or category with its member pages.
type taxonomyEntry struct {
	Name  string
	Slug  string
	Pages []docs.Page
}

// taxonomyLink is a precomputed tag/category link passed to the theme. Building
// these in Go keeps the URL base and slug rules in one place and avoids
// runtime string manipulation in build-time-rendered layouts.
type taxonomyLink struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

func taxonomyLinks(routeBase, kind string, names []string) []taxonomyLink {
	base := strings.TrimRight(routeBase, "/")
	out := make([]taxonomyLink, 0, len(names))
	for _, name := range names {
		out = append(out, taxonomyLink{Name: name, URL: base + "/" + kind + "/" + slugifyName(name) + "/"})
	}
	return out
}

// generateTaxonomyPages emits an index page per tag and per category under
// /docs/tags/<slug>/ and /docs/categories/<slug>/. Pages are sorted by title.
// The generated pages go through the same generateTSX path as normal docs
// pages, so they get the same head, search assets, theme options, prepaint
// script, sidebar enrichment, and current path.
func (p *DocsPlugin) generateTaxonomyPages(ctx *BuildHookCtx, cfg *config.Config, genRoot string, theme *resolvedDocsTheme, opts *DocsPluginOptions, routeBase string, searchEnabled bool, themeOptions json.RawMessage, pages []docs.Page, sections []docs.SidebarItem) error {
	tags := map[string][]docs.Page{}
	categories := map[string][]docs.Page{}
	for _, page := range pages {
		for _, t := range page.Tags {
			tags[t] = append(tags[t], page)
		}
		for _, c := range page.Categories {
			categories[c] = append(categories[c], page)
		}
	}

	emit := func(kind string, m map[string][]docs.Page) error {
		entries := make([]taxonomyEntry, 0, len(m))
		for name, ps := range m {
			sorted := append([]docs.Page(nil), ps...)
			sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Title < sorted[j].Title })
			entries = append(entries, taxonomyEntry{Name: name, Slug: slugifyName(name), Pages: sorted})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
		for _, e := range entries {
			rel := kind + "/" + e.Slug
			route := "docs/" + rel
			tsxPath := filepath.Join(genRoot, filepath.FromSlash(route)+".tsx")
			if err := os.MkdirAll(filepath.Dir(tsxPath), 0755); err != nil {
				return err
			}
			page := docs.Page{
				Path:        rel,
				URL:         strings.TrimRight(routeBase, "/") + "/" + rel + "/",
				Title:       taxonomyTitle(kind, e.Name),
				Description: taxonomyDescription(kind, e.Name),
				Template:    "doc",
				Content:     taxonomyContent(routeBase, kind, e),
			}
			layoutRel := ""
			if theme != nil {
				layoutRel = theme.importSpecifier(filepath.Dir(tsxPath))
			}
			src := p.generateTSX(ctx, cfg, page, layoutRel, searchEnabled, sections, nil, nil, "", "", "", "", docsLocaleInfo{}, opts, themeOptions, cfg.Markdown, routeBase)
			if err := fsutil.WriteFileAtomic(tsxPath, []byte(src), 0644); err != nil {
				return err
			}
			if ctx.GeneratedPages != nil {
				*ctx.GeneratedPages = append(*ctx.GeneratedPages, GeneratedPage{Path: tsxPath, Route: route})
			}
		}
		return nil
	}

	if err := emit("tags", tags); err != nil {
		return err
	}
	return emit("categories", categories)
}

func taxonomyTitle(kind, name string) string {
	if kind == "categories" {
		return "Category: " + name
	}
	return "Tag: " + name
}

func taxonomyDescription(kind, name string) string {
	if kind == "categories" {
		return "Documentation pages in the " + name + " category."
	}
	return "Documentation pages tagged " + name + "."
}

// taxonomyContent renders the index list as the page body so the normal docs
// page generator can own the surrounding shell.
func taxonomyContent(routeBase, kind string, e taxonomyEntry) string {
	var sb strings.Builder
	sb.WriteString("<h1>")
	sb.WriteString(escape.HTML(taxonomyTitle(kind, e.Name)))
	sb.WriteString("</h1>\n")
	sb.WriteString(`<ul class="taxonomy-list">` + "\n")
	for _, p := range e.Pages {
		href := p.URL
		if href == "" {
			href = strings.TrimRight(routeBase, "/") + "/" + docs.NormalizePagePath(p.Path) + "/"
		}
		sb.WriteString(`<li><a href="`)
		sb.WriteString(escape.HTMLAttr(href))
		sb.WriteString(`">`)
		sb.WriteString(escape.HTML(p.Title))
		sb.WriteString("</a></li>\n")
	}
	sb.WriteString("</ul>")
	return sb.String()
}

// slugifyName lowercases and hyphenates a tag/category for use in a URL path.
func slugifyName(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
