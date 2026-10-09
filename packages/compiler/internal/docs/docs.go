package docs

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/internal/docfind"
	"github.com/kratejs/krate/packages/compiler/internal/frontmatter"
	"github.com/kratejs/krate/packages/compiler/internal/gitinfo"
	"github.com/kratejs/krate/packages/compiler/internal/markdown"
	"github.com/kratejs/krate/packages/compiler/internal/pluginutil"
)

// Badge is a small colored chip rendered next to a sidebar link or page heading.
type Badge struct {
	Text    string `json:"text"`
	Variant string `json:"variant,omitempty"`
}

// SidebarItem represents a single entry in a recursive sidebar tree.
// Items with Children act as expandable folder sections.
// Items with URL (and no Children) act as clickable links.
// Items with neither are plain text headers.
type SidebarItem struct {
	Title       string        `json:"title"`
	URL         string        `json:"url"`
	Active      bool          `json:"active"`
	Children    []SidebarItem `json:"children,omitempty"`
	IndexURL    string        `json:"indexURL,omitempty"`
	Collapsible bool          `json:"collapsible,omitempty"`
	Expanded    bool          `json:"expanded,omitempty"`
	Icon        string        `json:"icon,omitempty"`
	Badge       *Badge        `json:"badge,omitempty"`
}

// Page represents a single documentation page.
type Page struct {
	Path          string        `json:"path"`
	Title         string        `json:"title"`
	Order         int           `json:"order"`
	Content       string        `json:"content"`
	Dir           string        `json:"dir"`
	Sidebar       string        `json:"sidebar"`
	SourcePath    string        `json:"sourcePath"`
	Keywords      []string      `json:"keywords,omitempty"`
	CustomSidebar []SidebarItem `json:"customSidebar"`

	// Phase 1 — content fields
	Description string       `json:"description,omitempty"`
	Toc         TocConfig    `json:"-"`
	Hero        *HeroConfig  `json:"-"`
	Template    string       `json:"template,omitempty"`
	Head        []HeadTag    `json:"-"`
	Prev        *NavOverride `json:"-"`
	Next        *NavOverride `json:"-"`

	// Phase 2 — navigation surface
	SidebarCfg *SidebarNavConfig `json:"-"`
	Badge      *Badge            `json:"badge,omitempty"`

	// Phase 3 — workflow
	Draft   bool   `json:"draft,omitempty"`
	EditURL string `json:"editUrl,omitempty"`

	// Phase 4 — search/org
	Tags       []string `json:"tags,omitempty"`
	Categories []string `json:"categories,omitempty"`

	// Phase 5 — dates (RFC3339 or YYYY-MM-DD strings). Date is the published
	// date; Updated is the last-modified date (frontmatter, else git).
	Date    string `json:"date,omitempty"`
	Updated string `json:"updated,omitempty"`

	// Authors lists recent contributors to the source file (from git), most
	// recent first. Populated when Config.GitLastUpdated is set.
	Authors []string `json:"authors,omitempty"`

	// Phase 6 — i18n/versioning. Locale is the page's locale ("" when i18n is
	// not configured); Version is its docs version ("" when unversioned); URL is
	// the page's absolute site URL, set by the docs plugin (locale/version
	// aware).
	Locale  string `json:"locale,omitempty"`
	Version string `json:"version,omitempty"`
	URL     string `json:"url,omitempty"`
}

// TocConfig controls the page's table of contents.
type TocConfig struct {
	Disabled bool
	MinLevel int
	MaxLevel int
	Label    string
}

// HeroAction is a button rendered inside the hero block.
type HeroAction struct {
	Text    string `json:"text"`
	Link    string `json:"link"`
	Variant string `json:"variant,omitempty"`
}

// HeroConfig is the hero block rendered by template:hero layouts.
type HeroConfig struct {
	Title   string       `json:"title"`
	Tagline string       `json:"tagline"`
	Image   string       `json:"image,omitempty"`
	Actions []HeroAction `json:"actions,omitempty"`
}

// HeadTag is a per-page <tag attrs /> rendered in the page <Head>.
type HeadTag struct {
	Tag   string
	Attrs map[string]string
}

// NavOverride overrides the auto-computed previous/next page link.
type NavOverride struct {
	Disabled bool
	Text     string
	Link     string
}

// SidebarNavConfig carries per-page navigation metadata that overrides the
// sidebar tree entry for this page (label, order, hidden, badge, etc.).
type SidebarNavConfig struct {
	Label       string
	Order       int
	Hidden      bool
	Badge       *Badge
	Collapsible bool
	DefaultOpen bool
	Icon        string
}

// Config holds configuration for scanning documentation.
type Config struct {
	ContentDir string          // relative to project root
	Root       string          // absolute project root
	MDConfig   markdown.Config // markdown rendering config
	// GitLastUpdated fills Page.Updated from the file's last git commit when
	// frontmatter does not set it.
	GitLastUpdated bool
}

// Scan walks the content directory and parses all .md/.mdx files.
func Scan(cfg Config) ([]Page, error) {
	absDir := filepath.Join(cfg.Root, cfg.ContentDir)
	if _, err := os.Stat(absDir); os.IsNotExist(err) {
		return nil, nil
	}

	// Collect the file list first so parsing can run concurrently while staying
	// deterministic (results are written back by index).
	absPaths := []string{}
	relPaths := []string{}
	if err := pluginutil.WalkMD(absDir, func(absPath, relPath string) error {
		absPaths = append(absPaths, absPath)
		relPaths = append(relPaths, relPath)
		return nil
	}); err != nil {
		return nil, err
	}

	pages := make([]Page, len(absPaths))
	errs := make([]error, len(absPaths))
	pool := make(chan struct{}, runtime.GOMAXPROCS(0))
	var wg sync.WaitGroup
	for i := range absPaths {
		wg.Add(1)
		pool <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-pool }()
			p, err := scanPage(cfg, absPaths[i], relPaths[i])
			if err != nil {
				errs[i] = err
				return
			}
			pages[i] = p
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return pages, nil
}

// scanPage parses a single content file into a Page (frontmatter, body, and
// optional git metadata). Safe to run concurrently.
func scanPage(cfg Config, absPath, relPath string) (Page, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return Page{}, fmt.Errorf("reading %s: %w", absPath, err)
	}

	pagePath := strings.TrimSuffix(relPath, filepath.Ext(relPath))

	var htmlContent string
	var fm *Frontmatter
	if strings.HasSuffix(absPath, ".mdx") {
		htmlContent, fm = ParseMDX(string(data), cfg.MDConfig)
	} else {
		htmlContent, fm = ParseMD(string(data), cfg.MDConfig)
	}

	if fm.Title == "" {
		fm.Title = PathToTitle(relPath)
	}

	dirPart := fm.Sidebar
	if dirPart == "" {
		if idx := strings.LastIndex(pagePath, "/"); idx > 0 {
			dirPart = pagePath[:idx]
		}
	}

	updated := fm.Updated
	var authors []string
	if cfg.GitLastUpdated {
		if updated == "" {
			updated = gitinfo.LastCommit(cfg.Root, absPath)
		}
		authors = gitinfo.LastAuthors(cfg.Root, absPath, 5)
	}

	return Page{
		Path:          pagePath,
		Title:         fm.Title,
		Order:         fm.Order,
		Content:       htmlContent,
		Dir:           dirPart,
		Sidebar:       fm.Sidebar,
		SourcePath:    absPath,
		Keywords:      fm.Keywords,
		CustomSidebar: fm.CustomSidebar,
		Description:   fm.Description,
		Toc:           fm.Toc,
		Hero:          fm.Hero,
		Template:      fm.Template,
		Head:          fm.Head,
		Prev:          fm.Prev,
		Next:          fm.Next,
		SidebarCfg:    fm.SidebarConfig,
		Badge:         fm.Badge,
		Draft:         fm.Draft,
		EditURL:       fm.EditURL,
		Tags:          fm.Tags,
		Categories:    fm.Categories,
		Date:          fm.Date,
		Updated:       updated,
		Authors:       authors,
	}, nil
}

// ParseMD parses a .md file, extracting frontmatter and rendering markdown.
func ParseMD(src string, cfg markdown.Config) (html string, fm *Frontmatter) {
	raw, segments := markdown.ParseMDXSegments(src, cfg)
	html = markdown.RenderSegmentsToHTML(segments)
	fm = decodeFrontmatter(raw)
	return
}

// ParseMDX parses an .mdx file, extracting frontmatter and rendering with JSX
// support. The result is identical to ParseMD since both delegate to the same
// markdown renderer.
func ParseMDX(src string, cfg markdown.Config) (html string, fm *Frontmatter) {
	return ParseMD(src, cfg)
}

// ParseKeywords splits a frontmatter `keywords:` value (comma or space
// separated) into a slice of trimmed keywords.
func ParseKeywords(raw string) []string {
	return frontmatter.StringSlice(raw)
}

// ExtractFrontmatter parses YAML frontmatter from markdown content. The returned
// map uses the shared mini-YAML parser (internal/frontmatter) so nested keys,
// typed values, and flow collections are supported.
func ExtractFrontmatter(src string) map[string]any {
	fm, _ := frontmatter.Parse(src)
	return fm
}

// StripFrontmatter removes YAML frontmatter from markdown content.
func StripFrontmatter(src string) string {
	lines := strings.Split(src, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return src
	}
	closeIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closeIdx = i
			break
		}
	}
	if closeIdx < 0 || closeIdx+1 >= len(lines) {
		return src
	}
	return strings.Join(lines[closeIdx+1:], "\n")
}

// SearchEntry represents a search index entry.
type SearchEntry struct {
	Title      string   `json:"title"`
	Path       string   `json:"path"`
	Content    string   `json:"content"`
	Tags       []string `json:"tags,omitempty"`
	Categories []string `json:"categories,omitempty"`
}

// BuildSearchIndex generates a search index from documentation pages.
func BuildSearchIndex(pages []Page) []SearchEntry {
	var entries []SearchEntry
	for _, p := range pages {
		plainText := StripHTMLTags(p.Content)
		if len(plainText) > 500 {
			plainText = plainText[:500]
		}
		entries = append(entries, SearchEntry{
			Title:      p.Title,
			Path:       "/docs/" + p.Path + "/",
			Content:    appendSearchTerms(plainText, p.Tags, p.Categories),
			Tags:       p.Tags,
			Categories: p.Categories,
		})
	}
	return entries
}

// appendSearchTerms appends non-empty tags and categories to plainText for
// fallback JSON search matching. The WASM index also receives them via the
// docfind Document keywords field (see docs_search.go).
func appendSearchTerms(plainText string, tags, categories []string) string {
	extra := dedupeNonEmpty(tags, categories)
	if len(extra) == 0 {
		return plainText
	}
	return strings.Join(append([]string{plainText}, extra...), " ")
}

// BuildSearchDocuments converts docs pages into docfind index documents.
// Tags and categories are merged into the WASM index keywords so searches
// against the tags/categories surface the page; the JSON fallback index gets
// the same terms appended to content (see BuildSearchIndex).
func BuildSearchDocuments(pages []Page) []docfind.Document {
	documents := make([]docfind.Document, 0, len(pages))
	for _, p := range pages {
		keywords := dedupeNonEmpty(p.Keywords, p.Tags, p.Categories)
		body := html.UnescapeString(StripHTMLTags(p.Content))
		if extra := dedupeNonEmpty(p.Tags, p.Categories); len(extra) > 0 {
			body = strings.Join(append([]string{body}, extra...), " ")
		}
		documents = append(documents, docfind.Document{
			Title:    p.Title,
			Category: p.Dir,
			Href:     PageURL(p.Path),
			Body:     body,
			Keywords: keywords,
		})
	}
	return documents
}

func dedupeNonEmpty(slices ...[]string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, s := range slices {
		for _, v := range s {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			if _, dup := seen[v]; dup {
				continue
			}
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}

// StripHTMLTags removes HTML tags from a string, keeping the document's line
// structure: block-level tags (p, li, headings, table cells, …) become line
// breaks, and intra-line whitespace is collapsed. This keeps extracted text
// readable (paragraphs stay separate) and prevents adjacent blocks from running
// together, e.g. "<td>File</td><td>When loaded</td>" → "File\nWhen loaded".
func StripHTMLTags(s string) string {
	var out strings.Builder
	var tag strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			tag.Reset()
			continue
		}
		if r == '>' {
			inTag = false
			if isBlockTag(tag.String()) {
				out.WriteByte('\n')
			}
			continue
		}
		if inTag {
			tag.WriteRune(r)
		} else {
			out.WriteRune(r)
		}
	}
	// Collapse intra-line whitespace and drop blank lines, preserving breaks.
	var b strings.Builder
	for _, line := range strings.Split(out.String(), "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	return b.String()
}

// isBlockTag reports whether an HTML tag name is block-level (so its removal
// should leave a line break).
func isBlockTag(tag string) bool {
	tag = strings.ToLower(strings.TrimSpace(tag))
	tag = strings.TrimPrefix(tag, "/")
	if i := strings.IndexAny(tag, " \t\n\r"); i >= 0 {
		tag = tag[:i]
	}
	switch tag {
	case "p", "br", "div", "li", "ul", "ol", "tr", "td", "th", "table",
		"h1", "h2", "h3", "h4", "h5", "h6", "pre", "blockquote", "section",
		"article", "header", "footer", "nav", "aside", "hr", "dt", "dd":
		return true
	}
	return false
}

// Frontmatter is the fully decoded YAML frontmatter for a documentation page.
type Frontmatter struct {
	Title         string
	Order         int
	Sidebar       string
	CustomSidebar []SidebarItem
	SidebarConfig *SidebarNavConfig
	Badge         *Badge
	Keywords      []string
	Description   string
	Toc           TocConfig
	Hero          *HeroConfig
	Template      string
	Head          []HeadTag
	Prev          *NavOverride
	Next          *NavOverride
	Draft         bool
	EditURL       string
	Tags          []string
	Categories    []string
	Date          string
	Updated       string
}

func decodeFrontmatter(fm map[string]any) *Frontmatter {
	if len(fm) == 0 {
		return &Frontmatter{Order: 999, Template: "doc", Toc: TocConfig{MinLevel: 2, MaxLevel: 3}}
	}

	f := &Frontmatter{
		Order:    999,
		Toc:      TocConfig{MinLevel: 2, MaxLevel: 3},
		Template: "doc",
	}
	f.Title = frontmatter.String(fm["title"])
	f.Order = frontmatter.Int(fm["order"], 999)
	f.Description = frontmatter.String(fm["description"])
	f.Template = frontmatter.String(fm["template"])
	if f.Template == "" {
		f.Template = "doc"
	}
	f.EditURL = frontmatter.String(fm["editUrl"])
	f.Draft = frontmatter.Bool(fm["draft"])
	f.Keywords = frontmatter.StringSlice(fm["keywords"])
	f.Tags = frontmatter.StringSlice(fm["tags"])
	f.Categories = frontmatter.StringSlice(fm["categories"])
	f.Date = frontmatter.String(fm["date"])
	f.Updated = frontmatter.String(fm["lastUpdated"])
	if f.Updated == "" {
		f.Updated = frontmatter.String(fm["updated"])
	}
	f.Badge = decodeBadge(fm["badge"])

	// sidebar (legacy section / custom array / new object form)
	decodeSidebarValue(fm["sidebar"], f)

	// toc
	decodeTOC(fm["toc"], &f.Toc)

	// hero
	decodeHero(fm["hero"], &f.Hero)

	// head
	decodeHead(fm["head"], &f.Head)

	// prev / next
	f.Prev = decodeNavOverride(fm["prev"])
	f.Next = decodeNavOverride(fm["next"])

	return f
}

func decodeSidebarValue(v any, f *Frontmatter) {
	switch t := v.(type) {
	case string:
		if strings.HasPrefix(t, "[") {
			var custom []SidebarItem
			if err := json.Unmarshal([]byte(t), &custom); err == nil {
				f.CustomSidebar = custom
			}
		} else {
			f.Sidebar = t
		}
	case []any:
		f.CustomSidebar = decodeCustomSidebar(t)
	case map[string]any:
		f.SidebarConfig = decodeSidebarNavConfig(t)
	case nil:
	}
}

func decodeCustomSidebar(list []any) []SidebarItem {
	var out []SidebarItem
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		var it SidebarItem
		it.Title = frontmatter.String(m["title"])
		it.URL = frontmatter.String(m["url"])
		it.Icon = frontmatter.String(m["icon"])
		it.Badge = decodeBadge(m["badge"])
		if children, ok := m["children"].([]any); ok {
			it.Children = decodeCustomSidebar(children)
		}
		if it.Title == "" && it.URL == "" && len(it.Children) == 0 {
			continue
		}
		out = append(out, it)
	}
	return out
}

func decodeTOC(v any, toc *TocConfig) {
	switch t := v.(type) {
	case bool:
		toc.Disabled = !t
	case string:
		if strings.EqualFold(strings.TrimSpace(t), "false") {
			toc.Disabled = true
		}
	case map[string]any:
		if n, ok := t["minLevel"]; ok {
			toc.MinLevel = frontmatter.Int(n, 2)
		}
		if n, ok := t["maxLevel"]; ok {
			toc.MaxLevel = frontmatter.Int(n, 3)
		}
		if s, ok := t["label"]; ok {
			toc.Label = frontmatter.String(s)
		}
	}
}

func decodeHero(v any, hero **HeroConfig) {
	m, ok := v.(map[string]any)
	if !ok || m == nil {
		return
	}
	h := &HeroConfig{}
	h.Title = frontmatter.String(m["title"])
	h.Tagline = frontmatter.String(m["tagline"])
	h.Image = frontmatter.String(m["image"])
	if list, ok := m["actions"].([]any); ok {
		for _, item := range list {
			am, ok := item.(map[string]any)
			if !ok {
				continue
			}
			h.Actions = append(h.Actions, HeroAction{
				Text:    frontmatter.String(am["text"]),
				Link:    frontmatter.String(am["link"]),
				Variant: frontmatter.String(am["variant"]),
			})
		}
	}
	*hero = h
}

func decodeHead(v any, out *[]HeadTag) {
	list, ok := v.([]any)
	if !ok {
		return
	}
	for _, item := range list {
		pair, ok := item.([]any)
		if !ok || len(pair) == 0 {
			continue
		}
		tag := frontmatter.String(pair[0])
		if tag == "" {
			continue
		}
		ht := HeadTag{Tag: tag}
		if len(pair) > 1 {
			if attrs, ok := pair[1].(map[string]any); ok {
				ht.Attrs = make(map[string]string, len(attrs))
				for k, val := range attrs {
					ht.Attrs[k] = frontmatter.String(val)
				}
			}
		}
		*out = append(*out, ht)
	}
}

func decodeNavOverride(v any) *NavOverride {
	switch t := v.(type) {
	case bool:
		return &NavOverride{Disabled: !t}
	case string:
		return &NavOverride{Text: t}
	case map[string]any:
		no := &NavOverride{
			Text: frontmatter.String(t["text"]),
			Link: frontmatter.String(t["link"]),
		}
		return no
	}
	return nil
}

func decodeBadge(v any) *Badge {
	switch t := v.(type) {
	case string:
		t = strings.TrimSpace(t)
		if t != "" {
			return &Badge{Text: t}
		}
	case map[string]any:
		text := frontmatter.String(t["text"])
		if text != "" {
			return &Badge{Text: text, Variant: frontmatter.String(t["variant"])}
		}
	}
	return nil
}

func decodeSidebarNavConfig(m map[string]any) *SidebarNavConfig {
	sc := &SidebarNavConfig{
		Label:       frontmatter.String(m["label"]),
		Order:       frontmatter.Int(m["order"], 0),
		Hidden:      frontmatter.Bool(m["hidden"]),
		Icon:        frontmatter.String(m["icon"]),
		Collapsible: frontmatter.Bool(m["collapsible"]),
		DefaultOpen: frontmatter.Bool(m["defaultOpen"]),
	}
	if badge := decodeBadge(m["badge"]); badge != nil {
		sc.Badge = badge
	}
	return sc
}
