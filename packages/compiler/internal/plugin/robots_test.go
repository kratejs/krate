package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func runRobots(t *testing.T, cfg *config.Config) string {
	t.Helper()
	out := t.TempDir()
	ctx := &BuildResultHookCtx{Root: out, OutDir: out, Config: cfg}
	if err := generateRobotsTxt(ctx); err != nil {
		t.Fatalf("generateRobotsTxt: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(out, "robots.txt"))
	if err != nil {
		t.Fatalf("robots.txt not written: %v", err)
	}
	return string(data)
}

func TestRobotsPluginOptionsAreHonored(t *testing.T) {
	cfg := &config.Config{
		SEO: config.SEOConfig{BaseURL: "https://example.com"},
		Plugins: []config.PluginConfig{{
			Name:    "robots",
			Options: map[string]interface{}{"disallow": "/admin", "sitemap": "https://example.com/sitemap.xml"},
		}},
	}
	got := runRobots(t, cfg)
	if !strings.Contains(got, "Disallow: /admin") {
		t.Errorf("plugin options ignored:\n%s", got)
	}
	if !strings.Contains(got, "Sitemap: https://example.com/sitemap.xml") {
		t.Errorf("sitemap option ignored:\n%s", got)
	}
}

func TestRobotsMultiGroupRules(t *testing.T) {
	cfg := &config.Config{
		Plugins: []config.PluginConfig{{
			Name: "robots",
			Options: map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"userAgent": "*", "disallow": []interface{}{"/private"}},
					map[string]interface{}{"userAgent": "GPTBot", "disallow": []interface{}{"/"}},
				},
			},
		}},
	}
	got := runRobots(t, cfg)
	for _, want := range []string{"User-agent: *", "Disallow: /private", "User-agent: GPTBot"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRobotsTopLevelFallback(t *testing.T) {
	cfg := &config.Config{
		Robots:  config.RobotsConfig{Allow: "/"},
		Plugins: []config.PluginConfig{{Name: "robots"}},
	}
	got := runRobots(t, cfg)
	if !strings.Contains(got, "Allow: /") {
		t.Errorf("top-level robots not used when plugin has no options:\n%s", got)
	}
}

func TestConfigWarningsUnknownPluginOptions(t *testing.T) {
	cfg := &config.Config{Plugins: []config.PluginConfig{{
		Name: "docs",
		Options: map[string]interface{}{
			"titel":  "typo",
			"search": map[string]interface{}{"engine": "docfind", "bogus": true},
		},
	}}}
	w := strings.Join(ConfigWarnings(cfg), "\n")
	if !strings.Contains(w, `unknown docs option "titel"`) {
		t.Errorf("missing top-level typo warning: %s", w)
	}
	if !strings.Contains(w, `unknown docs option "search.bogus"`) {
		t.Errorf("missing nested typo warning: %s", w)
	}
}

func TestConfigWarningsCleanConfig(t *testing.T) {
	cfg := &config.Config{Plugins: []config.PluginConfig{{
		Name: "docs",
		Options: map[string]interface{}{
			"title":  "Docs",
			"search": map[string]interface{}{"engine": "pagefind", "maxResults": 8},
			"links":  []interface{}{map[string]interface{}{"icon": "lucide:github", "url": "https://x"}},
		},
	}}}
	if w := ConfigWarnings(cfg); len(w) != 0 {
		t.Errorf("expected no warnings, got: %v", w)
	}
}
