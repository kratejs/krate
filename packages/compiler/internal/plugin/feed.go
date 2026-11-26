package plugin

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/docs"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
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
		MDConfig:       siteMarkdown(cfg, cfg.Markdown),
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
		url := CanonicalURLMode(baseURL, cfg.BaseURLPath(), feedPagePath(cfg, prefix, p.Path), cfg.TrailingSlashMode())
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

	siteBase := feedSiteBase(baseURL, cfg.BaseURLPath())
	kind := strings.ToLower(strings.TrimSpace(opts.Type))
	if kind == "" {
		kind = "all"
	}
	write := func(name, content string) error {
		if err := fsutil.WriteFileAtomic(path.Join(ctx.OutDir, name), []byte(content), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
		return nil
	}
	if kind == "rss" || kind == "all" {
		if err := write("feed.xml", renderRSS(siteBase, title, description, language, items)); err != nil {
			return err
		}
	}
	if kind == "atom" || kind == "all" {
		if err := write("atom.xml", renderAtom(siteBase, title, description, items)); err != nil {
			return err
		}
	}
	if kind == "json" || kind == "all" {
		if err := write("feed.json", renderJSONFeed(siteBase, title, description, items)); err != nil {
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

func renderRSS(siteBase, title, description, language string, items []feedItem) string {
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
	el("link", siteBase+"/")
	el("description", description)
	el("language", language)
	b.WriteString(fmt.Sprintf("    <atom:link href=%q rel=\"self\" type=\"application/rss+xml\"/>\n", escape.HTMLAttr(siteBase+"/feed.xml")))
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

func renderAtom(siteBase, title, description string, items []feedItem) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	b.WriteString(`<feed xmlns="http://www.w3.org/2005/Atom">` + "\n")
	b.WriteString("  <title>" + escape.HTML(title) + "</title>\n")
	b.WriteString(fmt.Sprintf("  <link href=%q rel=\"self\"/>\n", escape.HTMLAttr(siteBase+"/atom.xml")))
	b.WriteString(fmt.Sprintf("  <link href=%q/>\n", escape.HTMLAttr(siteBase+"/")))
	b.WriteString("  <id>" + escape.HTML(siteBase+"/") + "</id>\n")
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
// data (when seo.baseUrl is set), plus the mermaid and KaTeX loaders when the
// site uses them. Emitted inside the page's <Head> block.
func docsHeadExtras(cfg *config.Config, page docs.Page, siteTitle string, use docsAssetUse) string {
	baseURL := strings.TrimRight(cfg.SEO.BaseURL, "/")

	var sb strings.Builder

	if use.math && cfg.Markdown.Math {
		sb.WriteString(mathHeadMarkup(page))
	}
	if use.mermaid && cfg.Markdown.Mermaid {
		sb.WriteString("        <script type=\"module\">{`")
		sb.WriteString(escapeTemplateLit(mermaidLoaderScript))
		sb.WriteString("`}</script>\n")
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
				sb.WriteString(jsxAttrExpr(feedSiteBase(feedBase, cfg.BaseURLPath()) + href))
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
			"url":      docsJSONLDURL(cfg, baseURL, page),
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

func renderJSONFeed(siteBase, title, description string, items []feedItem) string {
	feed := jsonFeed{
		Version:     "https://jsonfeed.org/version/1.1",
		Title:       title,
		HomePageURL: siteBase + "/",
		FeedURL:     siteBase + "/feed.json",
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

// feedSiteBase is the absolute URL of the site root without a trailing slash:
// origin plus base path, not doubled when the origin already ends with it.
func feedSiteBase(origin, basePath string) string {
	return strings.TrimRight(CanonicalURL(origin, basePath, "/"), "/")
}

// docsJSONLDURL is the absolute URL of a docs page for structured data. It uses
// the page's own locale- and version-aware URL (set by the docs plugin) so a
// translated page does not point at the English one.
func docsJSONLDURL(cfg *config.Config, baseURL string, page docs.Page) string {
	u := page.URL
	if u == "" {
		u = docsPageURL("/docs", page.Path)
	}
	return CanonicalURLMode(baseURL, cfg.BaseURLPath(), u, cfg.TrailingSlashMode())
}

// feedPagePath maps a content file path (which includes locale and version
// directories) to the URL path the docs plugin serves it at: the locale
// prefixes the whole URL (/fr/docs/guide/) and a non-current version sits under
// the docs prefix (/docs/v1/guide/).
func feedPagePath(cfg *config.Config, prefix, pagePath string) string {
	locales, defLocale := resolveLocales(cfg.I18n)
	versions, curVersion := resolveVersions(cfg.Versions)
	parts := strings.Split(pagePath, "/")
	locale, version := "", ""
	if len(parts) > 1 && parts[0] != defLocale {
		for _, l := range locales {
			if l != "" && l == parts[0] {
				locale, parts = l, parts[1:]
				break
			}
		}
	}
	if len(parts) > 1 {
		for _, v := range versions {
			if v != "" && v != curVersion && v == parts[0] {
				version, parts = v, parts[1:]
				break
			}
		}
	}
	u := prefix
	if version != "" {
		u += "/" + version
	}
	u = docsPageURL(u, strings.Join(parts, "/"))
	if locale != "" {
		u = "/" + locale + u
	}
	return u
}

const (
	// mermaidVersion and katexVersion are pinned so a CDN release cannot change
	// rendering under a built site.
	mermaidVersion = "11.17.2"
	katexVersion   = "0.16.11"

	mermaidModuleURL = "https://cdn.jsdelivr.net/npm/mermaid@" + mermaidVersion + "/dist/mermaid.esm.min.mjs"
	katexBaseURL     = "https://cdn.jsdelivr.net/npm/katex@" + katexVersion + "/dist"
)

// docsAssetUse records which CDN-backed markdown features some page of the
// site uses. The loaders are emitted on every page of a site that uses a
// feature (client navigation does not re-run head scripts, so a page without
// a diagram must still be able to render the next one) but they fetch nothing
// until a matching element is on screen.
type docsAssetUse struct {
	mermaid, math bool
}

// docsPageUse reports what one rendered page needs.
func docsPageUse(html string) docsAssetUse {
	return docsAssetUse{
		mermaid: strings.Contains(html, `class="mermaid"`),
		math:    strings.Contains(html, "krate-math"),
	}
}

// docsSiteUse reports what any of the pages needs.
func docsSiteUse(pages []docs.Page) docsAssetUse {
	var u docsAssetUse
	for _, pg := range pages {
		pu := docsPageUse(pg.Content)
		u.mermaid = u.mermaid || pu.mermaid
		u.math = u.math || pu.math
	}
	return u
}

// mermaidLoaderScript imports mermaid only when a .mermaid element exists, on
// load, on client navigation and on theme change.
const mermaidLoaderScript = "var __krateMermaidP;" +
	"function __krateTheme(){return document.documentElement.getAttribute('data-theme')==='dark'?'dark':'neutral';}" +
	"var __krateThemeSeen=__krateTheme();" +
	"function __krateMermaidRun(){" +
	"if(!document.querySelector('.mermaid')){return;}" +
	"document.querySelectorAll('.mermaid').forEach(function(el){if(!el.getAttribute('data-src')){el.setAttribute('data-src',el.textContent);}});" +
	"document.querySelectorAll('.mermaid').forEach(function(el){if(el.getAttribute('data-processed')){el.removeAttribute('data-processed');var s=el.getAttribute('data-src');if(s!=null){el.textContent=s;}}});" +
	"if(!__krateMermaidP){__krateMermaidP=import('" + mermaidModuleURL + "').then(function(m){return m.default;});}" +
	"__krateMermaidP.then(function(mermaid){" +
	"mermaid.initialize({startOnLoad:false,theme:__krateTheme()});" +
	"var __p=mermaid.run({querySelector:'.mermaid'});if(__p&&__p.catch){__p.catch(function(){});}" +
	"}).catch(function(){__krateMermaidP=null;});}" +
	"if(document.readyState!=='loading'){__krateMermaidRun();}else{document.addEventListener('DOMContentLoaded',__krateMermaidRun);}" +
	"window.addEventListener('krate:navigate',function(){setTimeout(__krateMermaidRun,60);});" +
	"new MutationObserver(function(){var t=__krateTheme();if(t===__krateThemeSeen){return;}__krateThemeSeen=t;__krateMermaidRun();}).observe(document.documentElement,{attributes:true,attributeFilter:['data-theme']});"

// katexLoaderScript loads KaTeX CSS and scripts only when a .krate-math span
// exists, then renders on load and on client navigation.
const katexLoaderScript = "var __krateKatexP;" +
	"function __krateAdd(tag,attrs){return new Promise(function(res,rej){var e=document.createElement(tag);for(var k in attrs){e[k]=attrs[k];}e.onload=res;e.onerror=rej;document.head.appendChild(e);});}" +
	"function __krateMathRun(){" +
	"if(!document.querySelector('.krate-math')){return;}" +
	"if(!__krateKatexP){var b='" + katexBaseURL + "';" +
	"var css=document.querySelector('link[data-krate-katex]')?Promise.resolve():__krateAdd('link',{rel:'stylesheet',href:b+'/katex.min.css'});" +
	"__krateKatexP=Promise.all([css,__krateAdd('script',{src:b+'/katex.min.js'})]).then(function(){return __krateAdd('script',{src:b+'/contrib/auto-render.min.js'});});}" +
	"__krateKatexP.then(function(){if(window.renderMathInElement){renderMathInElement(document.body,{delimiters:[{left:'$$',right:'$$',display:true},{left:'$',right:'$',display:false}],ignoredTags:['pre','code','script','style'],throwOnError:false});}}).catch(function(){__krateKatexP=null;});}" +
	"if(document.readyState!=='loading'){__krateMathRun();}else{document.addEventListener('DOMContentLoaded',__krateMathRun);}" +
	"window.addEventListener('krate:navigate',function(){setTimeout(__krateMathRun,60);});"

// mathHeadMarkup returns the KaTeX loader. A page that has math also links the
// stylesheet directly so first paint is styled; other pages fetch nothing.
func mathHeadMarkup(page docs.Page) string {
	var sb strings.Builder
	if docsPageUse(page.Content).math {
		sb.WriteString("        <link rel=\"stylesheet\" href=\"" + katexBaseURL + "/katex.min.css\" data-krate-katex=\"\" />\n")
	}
	sb.WriteString("        <script>{`" + escapeTemplateLit(katexLoaderScript) + "`}</script>\n")
	return sb.String()
}
