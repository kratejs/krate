package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadFunctionFormConfig verifies defineConfig's function form is invoked
// with the build context (command/mode/isDev).
func TestLoadFunctionFormConfig(t *testing.T) {
	root := t.TempDir()
	src := `export default ({ command, mode, isDev }) => ({
  outDir: isDev ? "dist-dev" : "dist-prod",
  plugins: [{ name: "docs", options: { title: command + ":" + mode } }],
});`
	if err := os.WriteFile(filepath.Join(root, "krate.config.ts"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWith(root, LoadOptions{Command: "build", Mode: "production"})
	if err != nil {
		t.Fatalf("LoadWith(production): %v", err)
	}
	if filepath.Base(cfg.OutDir) != "dist-prod" {
		t.Errorf("OutDir = %q, want dist-prod", cfg.OutDir)
	}
	if len(cfg.Plugins) != 1 || cfg.Plugins[0].Options["title"] != "build:production" {
		t.Errorf("function context not passed: %+v", cfg.Plugins)
	}

	cfg, err = LoadWith(root, LoadOptions{Command: "build", Mode: "development"})
	if err != nil {
		t.Fatalf("LoadWith(development): %v", err)
	}
	if filepath.Base(cfg.OutDir) != "dist-dev" {
		t.Errorf("OutDir = %q, want dist-dev for development mode", cfg.OutDir)
	}
}

func TestExtendsMergesBaseConfig(t *testing.T) {
	root := t.TempDir()
	base := `{"outDir":"base-dist","seo":{"baseUrl":"https://base.example","siteName":"Base"},"plugins":[{"name":"sitemap"}],"redirects":[{"source":"/a","destination":"/b"}]}`
	if err := os.WriteFile(filepath.Join(root, "base.json"), []byte(base), 0644); err != nil {
		t.Fatal(err)
	}
	child := `{"extends":"./base.json","outDir":"child-dist","seo":{"siteName":"Child"},"plugins":[{"name":"robots"}],"redirects":[{"source":"/c","destination":"/d"}]}`
	if err := os.WriteFile(filepath.Join(root, "krate.config.json"), []byte(child), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWith(root, LoadOptions{})
	if err != nil {
		t.Fatalf("LoadWith: %v", err)
	}
	if filepath.Base(cfg.OutDir) != "child-dist" {
		t.Errorf("OutDir = %q, want child-dist (child wins)", cfg.OutDir)
	}
	if cfg.SEO.BaseURL != "https://base.example" {
		t.Errorf("site.url = %q, want inherited base URL", cfg.SEO.BaseURL)
	}
	if cfg.SEO.SiteName != "Child" {
		t.Errorf("siteName = %q, want Child", cfg.SEO.SiteName)
	}
	if len(cfg.Plugins) != 2 {
		t.Errorf("plugins = %d, want 2 (concatenated)", len(cfg.Plugins))
	}
	if len(cfg.Redirects) != 2 {
		t.Errorf("redirects = %d, want 2 (concatenated)", len(cfg.Redirects))
	}
}

func TestLoadTSConfigPathsWithAliases(t *testing.T) {
	dir := t.TempDir()
	tsconfig := `{
		"compilerOptions": {
			"baseUrl": "./src",
			"paths": {
				"@/*": ["./*"],
				"@components/*": ["./components/*"],
				"@utils": ["./utils/index.ts"]
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(tsconfig), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{}
	cfg.LoadTSConfigPaths(dir)

	if len(cfg.PathAliases) != 3 {
		t.Fatalf("expected 3 path aliases, got %d", len(cfg.PathAliases))
	}

	// Check aliases exist (map order not guaranteed)
	aliasMap := make(map[string]PathAlias)
	for _, a := range cfg.PathAliases {
		aliasMap[a.Prefix] = a
	}

	// @/*
	alias, ok := aliasMap["@/*"]
	if !ok {
		t.Fatal("missing alias @/*")
	}
	if len(alias.Targets) != 1 || alias.Targets[0] != "./*" {
		t.Errorf("@/* targets = %v, want [\"./*\"]", alias.Targets)
	}

	// @components/*
	alias, ok = aliasMap["@components/*"]
	if !ok {
		t.Fatal("missing alias @components/*")
	}
	if len(alias.Targets) != 1 || alias.Targets[0] != "./components/*" {
		t.Errorf("@components/* targets = %v, want [\"./components/*\"]", alias.Targets)
	}

	// @utils (no wildcard)
	alias, ok = aliasMap["@utils"]
	if !ok {
		t.Fatal("missing alias @utils")
	}
	if len(alias.Targets) != 1 || alias.Targets[0] != "./utils/index.ts" {
		t.Errorf("@utils targets = %v, want [\"./utils/index.ts\"]", alias.Targets)
	}

	// Check TSBaseDir resolved correctly
	expectedBase := filepath.Join(dir, "src")
	if cfg.TSBaseDir != expectedBase {
		t.Errorf("TSBaseDir = %q, want %q", cfg.TSBaseDir, expectedBase)
	}
}
