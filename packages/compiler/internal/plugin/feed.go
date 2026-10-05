package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/docs"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
)

// FeedPluginOptions holds typed configuration for the feed plugin.
type FeedPluginOptions struct {
	BaseURL     string `json:"baseUrl"`
	ContentDir  string `json:"contentDir,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Language    string `json:"language,omitempty"`
	// Type is "rss", "atom", "json", or "all" (default "all").
	Type string `json:"type,omitempty"`
	// Count caps the number of items (default 20).
	Count int `json:"count,omitempty"`
	// PathPrefix is the URL prefix docs are mounted under (default "/docs").
	PathPrefix string `json:"pathPrefix,omitempty"`
}

func init() {
	_ = Register(&HookFunc{name: "feed", order: 102, hooks: PluginHooks{
		AfterBuild: generateFeeds,
	}})
}

// feedItem is the normalized feed entry shared by all encoders.
type feedItem struct {
	Title   string
	URL     string
	Summary string
	Date    string
	Updated string
}

func generateFeeds(ctx *BuildResultHookCtx) error {
	cfg, ok := ctx.Config.(*config.Config)
	if !ok {
		return nil
	}

	opts := parseFeedOptions(cfg)
	if opts == nil {
		return nil // not configured
	}

	baseURL := strings.TrimRight(opts.BaseURL, "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(cfg.SEO.BaseURL, "/")
	}
	if baseURL == "" {
		return fmt.Errorf("feed plugin requires option \"baseUrl\" (e.g. https://example.com) or seo.baseUrl in config")
	}

	contentDir := opts.ContentDir
	if contentDir == "" {
		if docsOpts := parseDocsOptions(cfg); docsOpts != nil {
			contentDir = docsOpts.ContentDir
		}
	}
	if contentDir == "" {
		contentDir = "src/content/docs"
	}

	title := opts.Title
	if title == "" {
		if docsOpts := parseDocsOptions(cfg); docsOpts != nil {
			title = docsOpts.Title
		}
	}
	if title == "" {
		title = cfg.SEO.SiteName
	}
	if title == "" {
		title = "Feed"
	}
	description := opts.Description
	if description == "" {
		description = cfg.SEO.Description
	}
	language := opts.Language
	if language == "" {
		language = "en"
	}
	count := opts.Count
	if count <= 0 {
		count = 20
	}
	prefix := opts.PathPrefix
	if prefix == "" {
		prefix = docs.DocsBasePath
	}
	prefix = "/" + strings.Trim(prefix, "/")

	pages, err := docs.Scan(docs.Config{
		ContentDir:     contentDir,
		Root:           ctx.Root,
		MDConfig:       cfg.Markdown,
		GitLastUpdated: true,
	})
	if err != nil {
		return fmt.Errorf("feed: scanning docs: %w", err)
	}

	items := make([]feedItem, 0, len(pages))
	for _, p := range pages {
		if p.Draft {
			continue
		}
		url := baseURL + docsPageURL(prefix, p.Path)
		date := p.Date
		if date == "" {
			date = p.Updated
		}
		items = append(items, feedItem{
			Title:   p.Title,
			URL:     url,
			Summary: p.Description,
			Date:    toRFC3339(date),
			Updated: toRFC3339(p.Updated),
		})
	}

	// Newest first; undated entries sort last, then by URL for determinism.
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Date != items[j].Date {
			return items[i].Date > items[j].Date
		}
		return items[i].URL < items[j].URL
	})
	if len(items) > count {
		items = items[:count]
	}

	kind := strings.ToLower(strings.TrimSpace(opts.Type))
	if kind == "" {
		kind = "all"
	}
	write := func(name, content string) error {
		if err := os.WriteFile(path.Join(ctx.OutDir, name), []byte(content), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
		return nil
	}
	if kind == "rss" || kind == "all" {
		if err := write("feed.xml", renderRSS(baseURL, title, description, language, items)); err != nil {
			return err
		}
	}
	if kind == "atom" || kind == "all" {
		if err := write("atom.xml", renderAtom(baseURL, title, description, items)); err != nil {
			return err
		}
	}
	if kind == "json" || kind == "all" {
		if err := write("feed.json", renderJSONFeed(baseURL, title, description, items)); err != nil {
			return err
		}
	}
	return nil
}

func parseFeedOptions(cfg *config.Config) *FeedPluginOptions {
	for _, pc := range cfg.Plugins {
		if pc.Name == "feed" {
			opts := &FeedPluginOptions{}
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

// docsPageURL prefixes a normalized docs page path with the configured prefix.
func docsPageURL(prefix, pagePath string) string {
	normalized := docs.NormalizePagePath(pagePath)
	if normalized == "" {
		return prefix + "/"
	}
	return prefix + "/" + normalized + "/"
}

// toRFC3339 normalizes a frontmatter date (RFC3339 or YYYY-MM-DD) to RFC3339.
func toRFC3339(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return s
}

func renderRSS(baseURL, title, description, language string, items []feedItem) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom">` + "\n")
	b.WriteString("  <channel>\n")
	el := func(tag, val string) {
		if val == "" {
			return
		}
		b.WriteString("    <" + tag + ">" + escape.HTML(val) + "</" + tag + ">\n")
	}
	el("title", title)
	el("link", baseURL+"/")
	el("description", description)
	el("language", language)
	b.WriteString(fmt.Sprintf("    <atom:link href=%q rel=\"self\" type=\"application/rss+xml\"/>\n", escape.HTMLAttr(baseURL+"/feed.xml")))
	for _, it := range items {
		b.WriteString("    <item>\n")
		b.WriteString("      <title>" + escape.HTML(it.Title) + "</title>\n")
		b.WriteString("      <link>" + escape.HTML(it.URL) + "</link>\n")
		b.WriteString("      <guid isPermaLink=\"true\">" + escape.HTML(it.URL) + "</guid>\n")
		if it.Summary != "" {
			b.WriteString("      <description>" + escape.HTML(it.Summary) + "</description>\n")
		}
		if it.Date != "" {
			if t, err := time.Parse(time.RFC3339, it.Date); err == nil {
				b.WriteString("      <pubDate>" + t.Format(time.RFC1123Z) + "</pubDate>\n")
			}
		}
		b.WriteString("    </item>\n")
	}
	b.WriteString("  </channel>\n</rss>\n")
	return b.String()
}

func renderAtom(baseURL, title, description string, items []feedItem) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	b.WriteString(`<feed xmlns="http://www.w3.org/2005/Atom">` + "\n")
	b.WriteString("  <title>" + escape.HTML(title) + "</title>\n")
	b.WriteString(fmt.Sprintf("  <link href=%q rel=\"self\"/>\n", escape.HTMLAttr(baseURL+"/atom.xml")))
	b.WriteString(fmt.Sprintf("  <link href=%q/>\n", escape.HTMLAttr(baseURL+"/")))
	b.WriteString("  <id>" + escape.HTML(baseURL+"/") + "</id>\n")
	if description != "" {
		b.WriteString("  <subtitle>" + escape.HTML(description) + "</subtitle>\n")
	}
	updated := time.Now().UTC().Format(time.RFC3339)
	if len(items) > 0 && items[0].Updated != "" {
		updated = items[0].Updated
	}
	b.WriteString("  <updated>" + escape.HTML(updated) + "</updated>\n")
	for _, it := range items {
		b.WriteString("  <entry>\n")
		b.WriteString("    <title>" + escape.HTML(it.Title) + "</title>\n")
		b.WriteString(fmt.Sprintf("    <link href=%q/>\n", escape.HTMLAttr(it.URL)))
		b.WriteString("    <id>" + escape.HTML(it.URL) + "</id>\n")
		entry := it.Updated
		if entry == "" {
			entry = it.Date
		}
		b.WriteString("    <updated>" + escape.HTML(entry) + "</updated>\n")
		if it.Summary != "" {
			b.WriteString("    <summary>" + escape.HTML(it.Summary) + "</summary>\n")
		}
		b.WriteString("  </entry>\n")
	}
	b.WriteString("</feed>\n")
	return b.String()
}

// docsHeadExtras returns JSX head markup for the docs page: <link rel=alternate>
// feed discovery (when the feed plugin is configured) and JSON-LD structured
// data (when seo.baseUrl is set). Emitted inside the page's <Head> block.
func docsHeadExtras(cfg *config.Config, page docs.Page, siteTitle string) string {
	baseURL := strings.TrimRight(cfg.SEO.BaseURL, "/")

	var sb strings.Builder

	// Math: load KaTeX from a CDN and auto-render the `.krate-math` spans the
	// markdown pipeline emits. Only emitted when markdown.math is enabled.
	if cfg.Markdown.Math {
		const katex = "https://cdn.jsdelivr.net/npm/katex@0.16.11/dist"
		sb.WriteString("        <link rel=\"stylesheet\" href=\"" + katex + "/katex.min.css\" />\n")
		sb.WriteString("        <script src=\"" + katex + "/katex.min.js\" defer={true}></script>\n")
		sb.WriteString("        <script src=\"" + katex + "/contrib/auto-render.min.js\" defer={true}></script>\n")
		sb.WriteString("        <script>{`document.addEventListener('DOMContentLoaded',function(){if(window.renderMathInElement){renderMathInElement(document.body,{delimiters:[{left:'$$',right:'$$',display:true},{left:'$',right:'$',display:false}],ignoredTags:['pre','code','script','style'],throwOnError:false});}});`}</script>\n")
	}

	if fo := parseFeedOptions(cfg); fo != nil {
		feedBase := baseURL
		if feedBase == "" {
			feedBase = strings.TrimRight(fo.BaseURL, "/")
		}
		if feedBase != "" {
			kind := strings.ToLower(fo.Type)
			if kind == "" {
				kind = "all"
			}
			add := func(href, typ, title string) {
				sb.WriteString("        <link rel=\"alternate\" href=")
				sb.WriteString(jsxAttrExpr(feedBase + href))
				sb.WriteString(" type=")
				sb.WriteString(jsxAttrExpr(typ))
				sb.WriteString(" title=")
				sb.WriteString(jsxAttrExpr(title))
				sb.WriteString(" />\n")
			}
			if kind == "rss" || kind == "all" {
				add("/feed.xml", "application/rss+xml", "RSS")
			}
			if kind == "atom" || kind == "all" {
				add("/atom.xml", "application/atom+xml", "Atom")
			}
			if kind == "json" || kind == "all" {
				add("/feed.json", "application/feed+json", "JSON Feed")
			}
		}
	}

	if baseURL != "" && page.Title != "" {
		typ := "TechArticle"
		switch strings.ToLower(page.Template) {
		case "blog", "post":
			typ = "BlogPosting"
		}
		ld := map[string]any{
			"@context": "https://schema.org",
			"@type":    typ,
			"headline": page.Title,
			"url":      baseURL + docsPageURL("/docs", page.Path),
		}
		if page.Description != "" {
			ld["description"] = page.Description
		}
		if page.Date != "" {
			ld["datePublished"] = toRFC3339(page.Date)
		}
		if page.Updated != "" {
			ld["dateModified"] = toRFC3339(page.Updated)
		}
		if len(page.Keywords) > 0 {
			ld["keywords"] = strings.Join(page.Keywords, ", ")
		}
		if siteTitle != "" {
			ld["publisher"] = map[string]any{"@type": "Organization", "name": siteTitle}
		}
		if data, err := json.Marshal(ld); err == nil {
			sb.WriteString("        <script type=\"application/ld+json\">{`")
			sb.WriteString(escapeTemplateLit(string(data)))
			sb.WriteString("`}</script>\n")
		}
	}

	return sb.String()
}

type jsonFeed struct {
	Version     string         `json:"version"`
	Title       string         `json:"title"`
	HomePageURL string         `json:"home_page_url"`
	FeedURL     string         `json:"feed_url"`
	Description string         `json:"description,omitempty"`
	Items       []jsonFeedItem `json:"items"`
}

type jsonFeedItem struct {
	ID            string `json:"id"`
	URL           string `json:"url"`
	Title         string `json:"title,omitempty"`
	Summary       string `json:"summary,omitempty"`
	DatePublished string `json:"date_published,omitempty"`
	DateModified  string `json:"date_modified,omitempty"`
}

func renderJSONFeed(baseURL, title, description string, items []feedItem) string {
	feed := jsonFeed{
		Version:     "https://jsonfeed.org/version/1.1",
		Title:       title,
		HomePageURL: baseURL + "/",
		FeedURL:     baseURL + "/feed.json",
		Description: description,
	}
	for _, it := range items {
		feed.Items = append(feed.Items, jsonFeedItem{
			ID:            it.URL,
			URL:           it.URL,
			Title:         it.Title,
			Summary:       it.Summary,
			DatePublished: it.Date,
			DateModified:  it.Updated,
		})
	}
	data, _ := json.MarshalIndent(feed, "", "  ")
	return string(data) + "\n"
}
