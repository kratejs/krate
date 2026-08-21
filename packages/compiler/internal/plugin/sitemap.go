package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"krate-compiler/internal/config"
)

// SitemapPluginOptions holds typed configuration for the sitemap plugin.
type SitemapPluginOptions struct {
	BaseURL    string `json:"baseUrl"`
	ChangeFreq string `json:"changeFreq,omitempty"` // always|hourly|daily|weekly|monthly|yearly|never
	Priority   string `json:"priority,omitempty"`   // 0.0 - 1.0 (default "0.5")
}

func init() {
	Register(&HookFunc{name: "sitemap", order: 100, hooks: PluginHooks{
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
					json.Unmarshal(data, opts)
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
		// Not configured — skip silently. Only error when the user explicitly
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

	for _, p := range ctx.Pages {
		loc := baseURL + "/"
		if p.OutName != "." {
			loc = baseURL + "/" + path.Clean(p.OutName)
		}
		b.WriteString("  <url>\n")
		b.WriteString(fmt.Sprintf("    <loc>%s</loc>\n", loc))
		b.WriteString(fmt.Sprintf("    <lastmod>%s</lastmod>\n", now))
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
