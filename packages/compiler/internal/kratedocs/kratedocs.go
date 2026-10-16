// Package kratedocs embeds Krate's own framework documentation (the content
// that powers the docs site) into the compiler so tooling - notably the MCP
// search_docs tool and the krate://docs/{slug} resource - can answer questions
// about Krate itself, independent of the project it is running in.
// The docs/ directory is generated at build time from
// packages/web/src/content/docs by scripts/sync-krate-docs.mjs and is
// gitignored; a checked-in .gitkeep keeps the embed compilable before the sync
// has run.
package kratedocs

import (
	"embed"
	"io/fs"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/docs"
	"github.com/kratejs/krate/packages/compiler/internal/markdown"
)

// The all: prefix includes the committed .gitkeep so the pattern still matches
// in a fresh checkout before scripts/sync-krate-docs.mjs has populated docs/.
//
//go:embed all:docs
var content embed.FS

// docsRoot is the embedded directory holding the framework docs.
const docsRoot = "docs"

// Doc pairs a parsed framework page with its raw markdown source.
type Doc struct {
	// Slug is the source-relative path without the extension, e.g. "cli" or
	// "features/mcp".
	Slug string
	// Page is the parsed docs page (title, URL path, tags, rendered text).
	Page docs.Page
	// Markdown is the raw source including frontmatter.
	Markdown string
}

var cached []Doc

// Docs returns every embedded framework doc, parsed once and cached.
func Docs() []Doc {
	if cached != nil {
		return cached
	}
	mdCfg := markdown.DefaultConfig()
	var out []Doc
	_ = fs.WalkDir(content, docsRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, ".md") && !strings.HasSuffix(p, ".mdx") {
			return nil
		}
		data, readErr := content.ReadFile(p)
		if readErr != nil {
			return nil
		}
		rel := strings.TrimPrefix(p, docsRoot+"/")
		slug := strings.TrimSuffix(strings.TrimSuffix(rel, ".md"), ".mdx")

		var html string
		var fm *docs.Frontmatter
		if strings.HasSuffix(rel, ".mdx") {
			html, fm = docs.ParseMDX(string(data), mdCfg)
		} else {
			html, fm = docs.ParseMD(string(data), mdCfg)
		}
		title := fm.Title
		if title == "" {
			title = docs.PathToTitle(slug)
		}
		dir := ""
		if i := strings.LastIndex(slug, "/"); i >= 0 {
			dir = slug[:i]
		}
		out = append(out, Doc{
			Slug:     slug,
			Markdown: string(data),
			Page: docs.Page{
				Path:       slug,
				Title:      title,
				Content:    html,
				Dir:        dir,
				Order:      fm.Order,
				Keywords:   fm.Keywords,
				Tags:       fm.Tags,
				Categories: fm.Categories,
				Template:   fm.Template,
			},
		})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	cached = out
	return cached
}

// Pages returns the parsed pages, ready for the search index builders.
func Pages() []docs.Page {
	docsList := Docs()
	out := make([]docs.Page, 0, len(docsList))
	for _, d := range docsList {
		out = append(out, d.Page)
	}
	return out
}

// Lookup returns the framework doc for slug (with or without a leading slash
// or a ".md"/".mdx" extension). The bool is false when no such doc exists.
func Lookup(slug string) (Doc, bool) {
	clean := strings.Trim(strings.TrimSpace(slug), "/")
	clean = strings.TrimSuffix(strings.TrimSuffix(clean, ".md"), ".mdx")
	if clean == "" {
		return Doc{}, false
	}
	for _, d := range Docs() {
		if d.Slug == clean {
			return d, true
		}
	}
	return Doc{}, false
}

// Slugs returns every doc slug, sorted.
func Slugs() []string {
	list := Docs()
	out := make([]string, 0, len(list))
	for _, d := range list {
		out = append(out, d.Slug)
	}
	return out
}

// Count returns the number of embedded docs.
func Count() int { return len(Docs()) }
