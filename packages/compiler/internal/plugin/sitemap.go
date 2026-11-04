package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/gitinfo"
)

// SitemapPluginOptions holds typed configuration for the sitemap plugin.
type SitemapPluginOptions struct {
	BaseURL    string `json:"baseUrl"`
	ChangeFreq string `json:"changeFreq,omitempty"` // always|hourly|daily|weekly|monthly|yearly|never
	Priority   string `json:"priority,omitempty"`   // 0.0 - 1.0 (default "0.5")
	// Exclude lists route patterns (path.Match globs such as "/admin/*" or
	// "/private/") left out of the sitemap, for SSR or private pages.
	Exclude []string `json:"exclude,omitempty"`
}

func init() {
	_ = Register(&HookFunc{name: "sitemap", order: 100, hooks: PluginHooks{
		AfterBuild: generateSitemap,
	}})
}

func generateSitemap(ctx *BuildResultHookCtx) error {
	opts := &SitemapPluginOptions{}
	configured := false

	if cfg, ok := ctx.Config.(*config.Config); ok {
		for _, pc := range cfg.Plugins {
			if pc.Name == "sitemap" {
				configured = true
				if pc.Options != nil {
					data, _ := json.Marshal(pc.Options)
					_ = json.Unmarshal(data, opts)
				}
				break
			}
		}
	}

	// Fallback to seo.baseUrl if sitemap baseUrl not set
	baseURL := strings.TrimRight(opts.BaseURL, "/")
	if baseURL == "" {
		if cfg, ok := ctx.Config.(*config.Config); ok && cfg.SEO.BaseURL != "" {
			baseURL = strings.TrimRight(cfg.SEO.BaseURL, "/")
		}
	}
	if baseURL == "" {
		// Not configured - skip silently. Only error when the user explicitly
		// opted into the sitemap plugin but forgot a base URL.
		if !configured {
			return nil
		}
		return fmt.Errorf("sitemap plugin requires option \"baseUrl\" (e.g. https://example.com) or seo.baseUrl in config")
	}

	changeFreq := opts.ChangeFreq
	if changeFreq == "" {
		changeFreq = "weekly"
	}
	priority := opts.Priority
	if priority == "" {
		priority = "0.5"
	}

	now := time.Now().UTC().Format("2006-01-02")

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")

	basePath := ""
	if cfg, ok := ctx.Config.(*config.Config); ok {
		basePath = cfg.BaseURLPath()
	}
	seen := map[string]bool{}
	for _, p := range ctx.Pages {
		// Intercepting-route fragments are slot content, not pages.
		if strings.HasPrefix(p.OutName, "_krate/") {
			continue
		}
		if !sitemapIncludes(p, opts.Exclude) {
			continue
		}
		loc := CanonicalURL(baseURL, basePath, p.OutName)
		if seen[loc] {
			continue
		}
		seen[loc] = true
		// Prefer the source file's last git commit date, falling back to the
		// build date when git metadata is unavailable.
		lastmod := now
		if p.Page != "" {
			if lc := gitinfo.LastCommit(ctx.Root, p.Page); lc != "" {
				if t, err := time.Parse(time.RFC3339, lc); err == nil {
					lastmod = t.UTC().Format("2006-01-02")
				}
			}
		}
		b.WriteString("  <url>\n")
		b.WriteString(fmt.Sprintf("    <loc>%s</loc>\n", xmlEscape(loc)))
		b.WriteString(fmt.Sprintf("    <lastmod>%s</lastmod>\n", lastmod))
		b.WriteString(fmt.Sprintf("    <changefreq>%s</changefreq>\n", changeFreq))
		b.WriteString(fmt.Sprintf("    <priority>%s</priority>\n", priority))
		b.WriteString("  </url>\n")
	}

	b.WriteString("</urlset>\n")

	sitemapPath := path.Join(ctx.OutDir, "sitemap.xml")
	if err := os.WriteFile(sitemapPath, []byte(b.String()), 0644); err != nil {
		return fmt.Errorf("writing sitemap: %w", err)
	}
	return nil
}

// sitemapIncludes reports whether a page belongs in the sitemap: error pages,
// dynamic-route fallback templates, noindex pages and excluded routes do not.
func sitemapIncludes(p PageResult, exclude []string) bool {
	if p.IsErrorPage || strings.ContainsAny(p.OutName, "[]") {
		return false
	}
	if headNoindex(p.HeadHTML) {
		return false
	}
	route := CanonicalRoutePath(p.OutName)
	for _, pat := range exclude {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}
		if !strings.HasPrefix(pat, "/") {
			pat = "/" + pat
		}
		if ok, _ := path.Match(pat, route); ok {
			return false
		}
		if ok, _ := path.Match(strings.TrimRight(pat, "/"), strings.TrimRight(route, "/")); ok {
			return false
		}
		// A trailing /* also covers deeper routes.
		if strings.HasSuffix(pat, "/*") && strings.HasPrefix(route, strings.TrimSuffix(pat, "*")) {
			return false
		}
	}
	return true
}

var robotsMetaRe = regexp.MustCompile(`(?is)<meta\s+[^>]*name\s*=\s*["']robots["'][^>]*>`)

// headNoindex reports whether the head carries a robots meta with noindex.
func headNoindex(head string) bool {
	for _, m := range robotsMetaRe.FindAllString(head, -1) {
		if strings.Contains(strings.ToLower(m), "noindex") {
			return true
		}
	}
	return false
}

var xmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")

func xmlEscape(s string) string { return xmlEscaper.Replace(s) }
