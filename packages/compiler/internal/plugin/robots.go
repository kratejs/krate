package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// RobotsPluginOptions is the typed option set for the built-in robots plugin
// (`robots({ ... })`). Options win over the top-level `robots` config block; the
// `rules` form supports multiple user-agent groups.
type RobotsPluginOptions struct {
	Allow    string       `json:"allow,omitempty"`
	Disallow string       `json:"disallow,omitempty"`
	Sitemap  string       `json:"sitemap,omitempty"`
	Rules    []RobotsRule `json:"rules,omitempty"`
}

// RobotsRule is one user-agent group in a robots.txt.
type RobotsRule struct {
	UserAgent string   `json:"userAgent,omitempty"`
	Allow     []string `json:"allow,omitempty"`
	Disallow  []string `json:"disallow,omitempty"`
}

func init() {
	_ = Register(&HookFunc{name: "robots", order: 101, hooks: PluginHooks{
		AfterBuild: generateRobotsTxt,
	}})
}

// robotsPluginOptions returns the plugin's options and whether the robots plugin
// is configured. Options are decoded from the plugin entry; a decode failure
// surfaces as a build error rather than a silent skip.
func robotsPluginOptions(cfg *config.Config) (RobotsPluginOptions, bool, error) {
	for _, pc := range cfg.Plugins {
		if pc.Name != "robots" {
			continue
		}
		var opts RobotsPluginOptions
		if pc.Options != nil {
			data, err := json.Marshal(pc.Options)
			if err != nil {
				return opts, true, fmt.Errorf("robots options: %w", err)
			}
			if err := json.Unmarshal(data, &opts); err != nil {
				return opts, true, fmt.Errorf("robots options: %w", err)
			}
		}
		return opts, true, nil
	}
	return RobotsPluginOptions{}, false, nil
}

func generateRobotsTxt(ctx *BuildResultHookCtx) error {
	cfg, ok := ctx.Config.(*config.Config)
	if !ok {
		return nil
	}

	opts, configured, err := robotsPluginOptions(cfg)
	if err != nil {
		return err
	}

	baseURL := strings.TrimRight(cfg.SEO.BaseURL, "/")
	hasTop := cfg.Robots.Allow != "" || cfg.Robots.Disallow != "" || cfg.Robots.Sitemap != ""
	if !configured && !hasTop && baseURL == "" {
		return nil
	}

	// Plugin options win, then the top-level `robots` block.
	allow := firstNonEmpty(opts.Allow, cfg.Robots.Allow)
	disallow := firstNonEmpty(opts.Disallow, cfg.Robots.Disallow)
	sitemapURL := firstNonEmpty(opts.Sitemap, cfg.Robots.Sitemap)
	if sitemapURL == "" && baseURL != "" {
		sitemapURL = baseURL + "/sitemap.xml"
	}

	var b strings.Builder
	if len(opts.Rules) > 0 {
		for _, r := range opts.Rules {
			ua := r.UserAgent
			if ua == "" {
				ua = "*"
			}
			b.WriteString("User-agent: " + ua + "\n")
			if len(r.Allow) == 0 && len(r.Disallow) == 0 {
				b.WriteString("Allow: /\n")
			}
			for _, a := range r.Allow {
				b.WriteString("Allow: " + a + "\n")
			}
			for _, d := range r.Disallow {
				b.WriteString("Disallow: " + d + "\n")
			}
			b.WriteString("\n")
		}
	} else {
		b.WriteString("User-agent: *\n")
		switch {
		case disallow != "":
			b.WriteString("Disallow: " + disallow + "\n")
		case allow != "":
			b.WriteString("Allow: " + allow + "\n")
		default:
			b.WriteString("Allow: /\n")
		}
		if sitemapURL != "" {
			b.WriteString("\n")
		}
	}
	if sitemapURL != "" {
		b.WriteString("Sitemap: " + sitemapURL + "\n")
	}

	if err := os.WriteFile(path.Join(ctx.OutDir, "robots.txt"), []byte(b.String()), 0644); err != nil {
		return fmt.Errorf("writing robots.txt: %w", err)
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
