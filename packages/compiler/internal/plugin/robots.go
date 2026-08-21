package plugin

import (
	"fmt"
	"os"
	"path"
	"strings"

	"krate-compiler/internal/config"
)

func init() {
	Register(&HookFunc{name: "robots", order: 101, hooks: PluginHooks{
		AfterBuild: generateRobotsTxt,
	}})
}

func generateRobotsTxt(ctx *BuildResultHookCtx) error {
	cfg, ok := ctx.Config.(*config.Config)
	if !ok {
		return nil
	}

	// Only generate if robots config is present or seo.baseUrl is set (auto-generate default)
	robotsCfg := cfg.Robots
	baseURL := strings.TrimRight(cfg.SEO.BaseURL, "/")

	if robotsCfg.Allow == "" && robotsCfg.Disallow == "" && robotsCfg.Sitemap == "" && baseURL == "" {
		return nil
	}

	var b strings.Builder
	b.WriteString("User-agent: *\n")

	if robotsCfg.Disallow != "" {
		b.WriteString(fmt.Sprintf("Disallow: %s\n", robotsCfg.Disallow))
	} else if robotsCfg.Allow != "" {
		b.WriteString(fmt.Sprintf("Allow: %s\n", robotsCfg.Allow))
	} else {
		b.WriteString("Allow: /\n")
	}

	sitemapURL := robotsCfg.Sitemap
	if sitemapURL == "" && baseURL != "" {
		sitemapURL = baseURL + "/sitemap.xml"
	}
	if sitemapURL != "" {
		b.WriteString(fmt.Sprintf("\nSitemap: %s\n", sitemapURL))
	}

	robotsPath := path.Join(ctx.OutDir, "robots.txt")
	if err := os.WriteFile(robotsPath, []byte(b.String()), 0644); err != nil {
		return fmt.Errorf("writing robots.txt: %w", err)
	}
	return nil
}
