package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.Entry != "src/index.tsx" {
		t.Errorf("expected Entry 'src/index.tsx', got %q", cfg.Entry)
	}
	if cfg.OutDir != "dist" {
		t.Errorf("expected OutDir 'dist', got %q", cfg.OutDir)
	}
	if cfg.PagesDir != "src/pages" {
		t.Errorf("expected PagesDir 'src/pages', got %q", cfg.PagesDir)
	}
	if cfg.PublicDir != "public" {
		t.Errorf("expected PublicDir 'public', got %q", cfg.PublicDir)
	}
	if !cfg.Minify {
		t.Error("expected Minify to be true by default")
	}
}

func TestShouldMinify(t *testing.T) {
	tests := []struct {
		name     string
		minify   bool
		minHTML  bool
		minCSS   bool
		minJS    bool
		wantHTML bool
		wantCSS  bool
		wantJS   bool
	}{
		{"all true", true, false, false, false, true, true, true},
		{"all false", false, false, false, false, false, false, false},
		{"individual overrides", false, true, true, true, true, true, true},
		{"minify overrides individual", true, false, false, false, true, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Minify: tt.minify, MinifyHTML: tt.minHTML, MinifyCSS: tt.minCSS, MinifyJS: tt.minJS}
			if got := cfg.ShouldMinifyHTML(); got != tt.wantHTML {
				t.Errorf("ShouldMinifyHTML() = %v, want %v", got, tt.wantHTML)
			}
			if got := cfg.ShouldMinifyCSS(); got != tt.wantCSS {
				t.Errorf("ShouldMinifyCSS() = %v, want %v", got, tt.wantCSS)
			}
			if got := cfg.ShouldMinifyJS(); got != tt.wantJS {
				t.Errorf("ShouldMinifyJS() = %v, want %v", got, tt.wantJS)
			}
		})
	}
}

func TestShouldPrecompress(t *testing.T) {
	cases := []struct {
		brotli, gzip, want bool
	}{
		{false, false, false},
		{true, false, true},
		{false, true, true},
		{true, true, true},
	}
	for _, c := range cases {
		cfg := &Config{Compress: CompressConfig{Brotli: c.brotli, Gzip: c.gzip}}
		if got := cfg.ShouldPrecompress(); got != c.want {
			t.Errorf("ShouldPrecompress(brotli=%v,gzip=%v) = %v, want %v", c.brotli, c.gzip, got, c.want)
		}
	}
}

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

func TestImagesConfig(t *testing.T) {
	cfg := Default()
	if err := json.Unmarshal([]byte(`{"images":{"quality":70,"breakpoints":[300,600],"formats":["webp"],"remotePatterns":[{"hostname":"images.example.com"}]}}`), cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Images.Quality != 70 || len(cfg.Images.Breakpoints) != 2 || len(cfg.Images.Formats) != 1 {
		t.Fatalf("images not parsed: %+v", cfg.Images)
	}
	if !cfg.Images.RemoteAllowed("images.example.com", "/x.png") {
		t.Error("allowlisted remote host should be allowed")
	}
	if cfg.Images.RemoteAllowed("evil.com", "/x.png") {
		t.Error("unlisted remote host should be rejected")
	}
	if !cfg.Images.PlaceholderEnabled() {
		t.Error("placeholder should default to on")
	}
}

func TestImageRemotePatterns(t *testing.T) {
	c := ImagesConfig{RemotePatterns: []ImageRemotePattern{
		{Hostname: "*.example.com"},
		{Hostname: "cdn.example.org", Pathname: "/images/**"},
		{Hostname: "**.nested.example"},
	}}
	cases := []struct {
		host, path string
		want       bool
	}{
		{"images.example.com", "/x.png", true},
		{"a.b.example.com", "/x.png", false}, // "*.example.com" is one label
		{"cdn.example.org", "/images/a/b.png", true},
		{"cdn.example.org", "/other.png", false},
		{"a.b.nested.example", "/x.png", true}, // "**.nested.example" any depth
	}
	for _, tc := range cases {
		if got := c.RemoteAllowed(tc.host, tc.path); got != tc.want {
			t.Errorf("RemoteAllowed(%q, %q) = %v, want %v", tc.host, tc.path, got, tc.want)
		}
	}
}

func TestHeadConfigForms(t *testing.T) {
	cfg := Default()
	if err := json.Unmarshal([]byte(`{"head":["<script>a</script>","<!-- b -->"]}`), cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Head.Raw) != 2 {
		t.Errorf("head array form -> Raw = %v", cfg.Head.Raw)
	}

	cfg2 := Default()
	if err := json.Unmarshal([]byte(`{"head":{"scripts":[{"src":"a.js","defer":true}],"meta":[{"name":"x","content":"y"}]}}`), cfg2); err != nil {
		t.Fatal(err)
	}
	if len(cfg2.Head.Scripts) != 1 || cfg2.Head.Scripts[0].Src != "a.js" || !cfg2.Head.Scripts[0].Defer {
		t.Errorf("head object form not parsed: %+v", cfg2.Head)
	}
	if len(cfg2.Head.Meta) != 1 || cfg2.Head.Meta[0].Name != "x" {
		t.Errorf("head meta not parsed: %+v", cfg2.Head.Meta)
	}
}

func TestDeprecationWarnings(t *testing.T) {
	w, err := (&Config{
		Site: SiteConfig{URL: "https://new.example"},
		SEO:  SEOConfig{BaseURL: "https://old.example"},
	}).Validate()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(w, "\n"), "both `site` and `seo`") {
		t.Errorf("missing site/seo deprecation warning: %v", w)
	}

	w, err = (&Config{GoAPI: GoAPICfg{Module: "example.com/api"}}).Validate()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(w, "\n"), "goApi` is deprecated") {
		t.Errorf("missing goApi deprecation warning: %v", w)
	}
}

// knownTopLevelKeys/knownNestedKeys are derived from Config so a new field is
// registered automatically. This guards against drift.
func TestKnownKeysDerivedFromConfig(t *testing.T) {
	typ := reflect.TypeOf(Config{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		name, ok := schemaFieldName(f)
		if !ok {
			continue
		}
		if !knownTopLevelKeys[name] {
			t.Errorf("Config field %q is not in knownTopLevelKeys", name)
		}
	}
	for parent, child := range map[string]string{
		"images": "quality",
		"head":   "scripts",
		"deploy": "target",
		"go":     "componentDirs",
		"compat": "mode",
	} {
		if !knownNestedKeys[parent][child] {
			t.Errorf("knownNestedKeys[%q] missing %q", parent, child)
		}
	}
	if _, ok := knownNestedKeys["content"]; ok {
		t.Error("content should accept arbitrary keys (map type)")
	}
}

// A key without a dedicated switch case is applied by the JSON round-trip.
func TestApplyConfigPropDefaultJSON(t *testing.T) {
	cfg := Default()
	if err := applyConfigProp(cfg, "extends", "./base.config.ts"); err != nil {
		t.Fatalf("applyConfigProp: %v", err)
	}
	if len(cfg.Extends) != 1 || cfg.Extends[0] != "./base.config.ts" {
		t.Fatalf("default JSON path did not apply extends: %v", cfg.Extends)
	}
}

func TestSchemaJSONCoversKeys(t *testing.T) {
	data, err := SchemaJSON()
	if err != nil {
		t.Fatalf("SchemaJSON: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("schema is not JSON: %v", err)
	}
	if m["$schema"] == nil {
		t.Error("missing $schema")
	}
	props, _ := m["properties"].(map[string]any)
	for _, key := range []string{"site", "seo", "head", "security", "headers", "images", "extends", "compress", "devServer", "plugins", "content"} {
		if props[key] == nil {
			t.Errorf("schema missing top-level key %q", key)
		}
	}
}

func TestExtendsPackagePreset(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "node_modules", "@acme", "preset")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(`{"krate":"./krate.config.json"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "krate.config.json"), []byte(`{"outDir":"preset-dist"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "krate.config.json"), []byte(`{"extends":"@acme/preset"}`), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWith(root, LoadOptions{})
	if err != nil {
		t.Fatalf("LoadWith: %v", err)
	}
	if filepath.Base(cfg.OutDir) != "preset-dist" {
		t.Errorf("OutDir = %q, want preset-dist", cfg.OutDir)
	}
}

func TestExtendsDedupesPlugins(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "base.json"), []byte(`{"plugins":[{"name":"docs","options":{"title":"Base"}}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	child := `{"extends":"./base.json","plugins":[{"name":"docs","options":{"title":"Child"}},{"name":"robots"}]}`
	if err := os.WriteFile(filepath.Join(root, "krate.config.json"), []byte(child), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWith(root, LoadOptions{})
	if err != nil {
		t.Fatalf("LoadWith: %v", err)
	}
	if len(cfg.Plugins) != 2 {
		t.Fatalf("plugins = %d, want 2 (deduped docs + robots)", len(cfg.Plugins))
	}
	for _, p := range cfg.Plugins {
		if p.Name == "docs" {
			if title, _ := p.Options["title"].(string); title != "Child" {
				t.Errorf("docs title = %q, want Child (extending config wins)", title)
			}
		}
	}
}

func TestExtendsResolvesModulesRelativeToBase(t *testing.T) {
	root := t.TempDir()
	presets := filepath.Join(root, "presets")
	if err := os.MkdirAll(presets, 0755); err != nil {
		t.Fatal(err)
	}
	base := `{"plugins":[{"name":"demo","module":"./plugin.ts"}],"markdown":{"plugins":["./md.ts"]}}`
	if err := os.WriteFile(filepath.Join(presets, "base.config.json"), []byte(base), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "krate.config.json"), []byte(`{"extends":"./presets/base.config.json"}`), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWith(root, LoadOptions{})
	if err != nil {
		t.Fatalf("LoadWith: %v", err)
	}
	wantPlugin := filepath.Join(presets, "plugin.ts")
	found := false
	for _, p := range cfg.Plugins {
		if p.Name == "demo" {
			found = true
			if p.Module != wantPlugin {
				t.Errorf("base plugin module = %q, want %q", p.Module, wantPlugin)
			}
		}
	}
	if !found {
		t.Error("base plugin missing after merge")
	}
	if len(cfg.Markdown.Plugins) != 1 || cfg.Markdown.Plugins[0] != filepath.Join(presets, "md.ts") {
		t.Errorf("base markdown plugins = %v, want resolved against base dir", cfg.Markdown.Plugins)
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

func TestExtendsCycleDetected(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "a.json"), []byte(`{"extends":"./b.json"}`), 0644)
	_ = os.WriteFile(filepath.Join(root, "b.json"), []byte(`{"extends":"./a.json"}`), 0644)
	_ = os.WriteFile(filepath.Join(root, "krate.config.json"), []byte(`{"extends":"./a.json"}`), 0644)
	if _, err := LoadWith(root, LoadOptions{}); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected a cycle error, got %v", err)
	}
}

func TestMarkdownPluginsBecomeCommunityPlugins(t *testing.T) {
	root := t.TempDir()
	cfg := Default()
	cfg.Markdown.Plugins = []string{"./md/transform.ts"}
	cfg.Resolve(root)
	cfg.Resolve(root) // idempotent across repeated Resolve calls

	count := 0
	for _, p := range cfg.Plugins {
		if p.Name == "markdown-transform-1" {
			count++
			if want := filepath.Join(root, "md", "transform.ts"); p.Module != want {
				t.Errorf("module = %q, want %q", p.Module, want)
			}
		}
	}
	if count != 1 {
		t.Fatalf("markdown plugin registered %d times, want 1", count)
	}
}

func TestLoadJSONConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "krate.config.json"), []byte(`{"outDir":"build","site":{"url":"https://x.example"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWith(root, LoadOptions{Command: "build", Mode: "production"})
	if err != nil {
		t.Fatalf("LoadWith json: %v", err)
	}
	if filepath.Base(cfg.OutDir) != "build" {
		t.Errorf("OutDir = %q, want build", cfg.OutDir)
	}
	if cfg.SEO.BaseURL != "https://x.example" {
		t.Errorf("site.url not folded: %q", cfg.SEO.BaseURL)
	}
}

func TestSiteFoldsIntoSEO(t *testing.T) {
	c := &Config{
		SEO:  SEOConfig{BaseURL: "https://old.example", Description: "old"},
		Site: SiteConfig{URL: "https://new.example", Name: "New", TitleTemplate: "%s | New", Twitter: "krate", ThemeColor: "#fff", Locale: "en-US"},
	}
	got := c.EffectiveSEO()
	if got.BaseURL != "https://new.example" || got.SiteName != "New" ||
		got.TitleTemplate != "%s | New" || got.Twitter != "krate" ||
		got.ThemeColor != "#fff" || got.Locale != "en-US" {
		t.Fatalf("site not folded over seo: %+v", got)
	}
	if got.Description != "old" {
		t.Fatalf("Description = %q, want %q (site omitted it)", got.Description, "old")
	}
}

func TestResolve(t *testing.T) {
	root := "/project"
	cfg := &Config{
		Entry:     "src/index.tsx",
		OutDir:    "dist",
		PagesDir:  "src/pages",
		PublicDir: "public",
	}
	cfg.Resolve(root)

	if cfg.Entry != filepath.Join(root, "src/index.tsx") {
		t.Errorf("Entry not resolved: %s", cfg.Entry)
	}
	if cfg.OutDir != filepath.Join(root, "dist") {
		t.Errorf("OutDir not resolved: %s", cfg.OutDir)
	}
	if cfg.PagesDir != filepath.Join(root, "src/pages") {
		t.Errorf("PagesDir not resolved: %s", cfg.PagesDir)
	}
	if cfg.PublicDir != filepath.Join(root, "public") {
		t.Errorf("PublicDir not resolved: %s", cfg.PublicDir)
	}
}

func TestResolveAbsolutePath(t *testing.T) {
	// Only test with a truly absolute path for the current OS
	// On Windows: C:\absolute\path; On Unix: /absolute/path
	if runtime.GOOS == "windows" {
		cfg := &Config{Entry: `C:\absolute\path\index.tsx`}
		cfg.Resolve("/project")
		if cfg.Entry != `C:\absolute\path\index.tsx` {
			t.Errorf("absolute path should not be modified, got %s", cfg.Entry)
		}
	} else {
		cfg := &Config{Entry: "/absolute/path/index.tsx"}
		cfg.Resolve("/project")
		if cfg.Entry != "/absolute/path/index.tsx" {
			t.Errorf("absolute path should not be modified, got %s", cfg.Entry)
		}
	}
}

func TestLoadTSConfigCompilerOptions(t *testing.T) {
	dir := t.TempDir()
	tsconfig := `{"compilerOptions": {"experimentalDecorators": true, "useDefineForClassFields": false}}`
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(tsconfig), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{}
	cfg.LoadTSConfigPaths(dir)
	o := cfg.TSCompilerOptions
	if !o.ExperimentalDecorators || o.UseDefineForClassFields == nil || *o.UseDefineForClassFields {
		t.Errorf("TSCompilerOptions = %+v", o)
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

func TestLoadTSConfigPathsWithoutBaseUrl(t *testing.T) {
	dir := t.TempDir()
	tsconfig := `{
		"compilerOptions": {
			"paths": {
				"@/*": ["./*"]
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(tsconfig), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{}
	cfg.LoadTSConfigPaths(dir)

	if len(cfg.PathAliases) != 1 {
		t.Fatalf("expected 1 path alias, got %d", len(cfg.PathAliases))
	}
	// Without baseUrl, TSBaseDir should be root
	if cfg.TSBaseDir != dir {
		t.Errorf("TSBaseDir = %q, want %q", cfg.TSBaseDir, dir)
	}
}

func TestLoadTSConfigPathsNoFile(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{}
	cfg.LoadTSConfigPaths(dir)
	if len(cfg.PathAliases) != 0 {
		t.Errorf("expected 0 path aliases when no tsconfig.json, got %d", len(cfg.PathAliases))
	}
}

func TestLoadTSConfigPathsInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte("{invalid"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{}
	cfg.LoadTSConfigPaths(dir)
	if len(cfg.PathAliases) != 0 {
		t.Errorf("expected 0 path aliases for invalid JSON, got %d", len(cfg.PathAliases))
	}
}

func TestLoadTSConfigPathsEmptyPaths(t *testing.T) {
	dir := t.TempDir()
	tsconfig := `{
		"compilerOptions": {
			"baseUrl": ".",
			"strict": true
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(tsconfig), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{}
	cfg.LoadTSConfigPaths(dir)
	if len(cfg.PathAliases) != 0 {
		t.Errorf("expected 0 path aliases when no paths key, got %d", len(cfg.PathAliases))
	}
	if cfg.TSBaseDir != dir {
		t.Errorf("TSBaseDir = %q, want %q", cfg.TSBaseDir, dir)
	}
}

func TestLoadTSConfigPathsArrayValues(t *testing.T) {
	dir := t.TempDir()
	tsconfig := `{
		"compilerOptions": {
			"paths": {
				"@app": ["./src/app/index.ts", "./src/app/main.ts"]
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(tsconfig), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{}
	cfg.LoadTSConfigPaths(dir)
	if len(cfg.PathAliases) != 1 {
		t.Fatalf("expected 1 alias, got %d", len(cfg.PathAliases))
	}
	if len(cfg.PathAliases[0].Targets) != 2 {
		t.Errorf("expected 2 targets, got %d", len(cfg.PathAliases[0].Targets))
	}
}

func TestParseTSConfigRedirects(t *testing.T) {
	src := `export default {
		redirects: [
			{ source: "/old", destination: "/new", permanent: true },
			{ source: "/temp", destination: "/other", permanent: false },
		],
	}`
	cfg := &Config{}
	if err := parseTSConfig(src, cfg); err != nil {
		t.Fatalf("parseTSConfig: %v", err)
	}
	if len(cfg.Redirects) != 2 {
		t.Fatalf("expected 2 redirects, got %d", len(cfg.Redirects))
	}
	if cfg.Redirects[0].Source != "/old" || cfg.Redirects[0].Destination != "/new" || !cfg.Redirects[0].Permanent {
		t.Errorf("redirect[0] = %+v", cfg.Redirects[0])
	}
	if cfg.Redirects[1].Source != "/temp" || cfg.Redirects[1].Destination != "/other" || cfg.Redirects[1].Permanent {
		t.Errorf("redirect[1] = %+v", cfg.Redirects[1])
	}
}

func TestParseTSConfigRewrites(t *testing.T) {
	src := `export default {
		rewrites: [
			{ source: "/docs/:path*", destination: "/documentation/:path*" },
		],
	}`
	cfg := &Config{}
	if err := parseTSConfig(src, cfg); err != nil {
		t.Fatalf("parseTSConfig: %v", err)
	}
	if len(cfg.Rewrites) != 1 {
		t.Fatalf("expected 1 rewrite, got %d", len(cfg.Rewrites))
	}
	if cfg.Rewrites[0].Source != "/docs/:path*" || cfg.Rewrites[0].Destination != "/documentation/:path*" {
		t.Errorf("rewrite[0] = %+v", cfg.Rewrites[0])
	}
}

func TestParseTSConfigContent(t *testing.T) {
	src := `export default {
		content: {
			blog: {
				dir: "src/content/blog",
				schema: { title: "string", order: { type: "number", required: true } },
			},
		},
	}`
	cfg := &Config{}
	if err := parseTSConfig(src, cfg); err != nil {
		t.Fatalf("parseTSConfig: %v", err)
	}
	if cfg.Content == nil {
		t.Fatal("expected content config")
	}
	blog, ok := cfg.Content["blog"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected blog collection, got %T", cfg.Content["blog"])
	}
	if blog["dir"] != "src/content/blog" {
		t.Errorf("dir = %v", blog["dir"])
	}
	if _, ok := blog["schema"].(map[string]interface{}); !ok {
		t.Errorf("schema should be an object, got %T", blog["schema"])
	}
}

func TestParseTSConfigSSR(t *testing.T) {
	src := `export default {
		ssr: {
			rendererPort: 4100,
			timeout: 3000,
			maxCacheSize: 64,
			middlewareRuntime: "node",
			apiRuntime: "node",
			ssrRuntime: "node",
			streaming: true,
		},
	}`
	cfg := &Config{}
	if err := parseTSConfig(src, cfg); err != nil {
		t.Fatalf("parseTSConfig: %v", err)
	}
	if cfg.SSR.RendererPort != 4100 {
		t.Errorf("RendererPort = %d, want 4100", cfg.SSR.RendererPort)
	}
	if cfg.SSR.Timeout != 3000 {
		t.Errorf("Timeout = %d, want 3000", cfg.SSR.Timeout)
	}
	if cfg.SSR.MaxCacheSize != 64 {
		t.Errorf("MaxCacheSize = %d, want 64", cfg.SSR.MaxCacheSize)
	}
	if cfg.SSR.MiddlewareRuntime != "node" {
		t.Errorf("MiddlewareRuntime = %q, want node", cfg.SSR.MiddlewareRuntime)
	}
	if cfg.SSR.APIRuntime != "node" {
		t.Errorf("APIRuntime = %q, want node", cfg.SSR.APIRuntime)
	}
	if cfg.SSR.SSRRuntime != "node" {
		t.Errorf("SSRRuntime = %q, want node", cfg.SSR.SSRRuntime)
	}
	if !cfg.SSR.Streaming {
		t.Error("Streaming = false, want true")
	}
}

func TestParseTSConfigSPA(t *testing.T) {
	cfg := Default()
	if !cfg.SPAEnabled() {
		t.Fatal("SPA should be enabled by default")
	}
	if err := parseTSConfig(`export default { spa: false }`, cfg); err != nil {
		t.Fatalf("parseTSConfig: %v", err)
	}
	if cfg.SPAEnabled() {
		t.Fatal("spa: false should disable the router")
	}
}

func TestParseTSConfigSSRStreamingFalse(t *testing.T) {
	src := `export default {
		ssr: { streaming: false },
	}`
	cfg := &Config{}
	if err := parseTSConfig(src, cfg); err != nil {
		t.Fatalf("parseTSConfig: %v", err)
	}
	if cfg.SSR.Streaming {
		t.Error("Streaming = true, want false (default)")
	}
}

func TestParseTSConfigCompat(t *testing.T) {
	src := `export default {
		compat: { mode: "hybrid", userAgent: true },
	}`
	cfg := &Config{}
	if err := parseTSConfig(src, cfg); err != nil {
		t.Fatalf("parseTSConfig: %v", err)
	}
	if cfg.Compat.Mode != "hybrid" {
		t.Errorf("Compat.Mode = %q, want hybrid", cfg.Compat.Mode)
	}
	if !cfg.Compat.UserAgent {
		t.Error("Compat.UserAgent = false, want true")
	}
	if got := cfg.Compat.ModeOrDefault(); got != "hybrid" {
		t.Errorf("ModeOrDefault = %q, want hybrid", got)
	}
}

func TestCompatValidate(t *testing.T) {
	if _, err := (&Config{Compat: CompatConfig{Mode: "nope"}}).Validate(); err == nil {
		t.Error("expected error for invalid compat.mode")
	}
	warnings, err := (&Config{Compat: CompatConfig{Mode: "modern", UserAgent: true}}).Validate()
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "compat.userAgent") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a compat.userAgent warning, got %v", warnings)
	}
	if got := (CompatConfig{}).ModeOrDefault(); got != "modern" {
		t.Errorf("empty ModeOrDefault = %q, want modern", got)
	}
}

func TestParseTSConfigGo(t *testing.T) {
	src := `export default {
		go: {
			module: "myapp",
			tidy: true,
			componentDirs: ["src/components", "src/lib"],
			apiEnabled: false,
			deps: [{ path: "github.com/x/y", version: "v1.2.3" }],
			replaces: [{ from: "github.com/x/y", to: "../y" }],
			exclude: [{ path: "github.com/a/b", version: "v9.9.9" }],
		},
	}`
	cfg := &Config{}
	if err := parseTSConfig(src, cfg); err != nil {
		t.Fatalf("parseTSConfig: %v", err)
	}
	if cfg.Go.Module != "myapp" {
		t.Errorf("Go.Module = %q", cfg.Go.Module)
	}
	if !cfg.Go.TidyOrDefault() {
		t.Error("Go.Tidy should be true")
	}
	if got := cfg.Go.ComponentDirList(); len(got) != 2 || got[0] != "src/components" {
		t.Errorf("ComponentDirList = %v", got)
	}
	if cfg.Go.APIEnabledOrDefault() {
		t.Error("apiEnabled false should disable API")
	}
	if len(cfg.Go.Deps) != 1 || cfg.Go.Deps[0].Path != "github.com/x/y" {
		t.Errorf("Deps = %v", cfg.Go.Deps)
	}
	if len(cfg.Go.Replaces) != 1 || cfg.Go.Replaces[0].To != "../y" {
		t.Errorf("Replaces = %v", cfg.Go.Replaces)
	}
	if len(cfg.Go.Exclude) != 1 || cfg.Go.Exclude[0].Path != "github.com/a/b" {
		t.Errorf("Exclude = %v", cfg.Go.Exclude)
	}
}

func TestEffectiveGoFoldsLegacyGoAPI(t *testing.T) {
	enabled := true
	cfg := &Config{GoAPI: GoAPICfg{
		Enabled:  &enabled,
		Module:   "legacy",
		Deps:     []GoAPIDep{{Path: "github.com/x/y", Version: "v1.0.0"}},
		Replaces: []GoAPIReplace{{From: "github.com/x/y", To: "../y"}},
	}}
	g := cfg.EffectiveGo()
	if g.Module != "legacy" {
		t.Errorf("EffectiveGo.Module = %q, want legacy", g.Module)
	}
	if len(g.Deps) != 1 || g.Deps[0].Path != "github.com/x/y" {
		t.Errorf("EffectiveGo.Deps = %v", g.Deps)
	}
	if len(g.Replaces) != 1 || g.Replaces[0].From != "github.com/x/y" {
		t.Errorf("EffectiveGo.Replaces = %v", g.Replaces)
	}
	// A `go` block overrides the legacy values for the fields it sets.
	cfg.Go.Module = "unified"
	if got := cfg.EffectiveGo().Module; got != "unified" {
		t.Errorf("go.module should win, got %q", got)
	}
}

func TestUnknownKeyWarningsGoItems(t *testing.T) {
	raw := []byte(`{"go":{"deps":[{"path":"x","versoin":"v1"}],"replaces":[{"from":"a","too":"b"}]}}`)
	warnings := UnknownKeyWarnings(raw)
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "go.deps.versoin") {
		t.Errorf("expected a go.deps item-key warning, got: %v", warnings)
	}
	if !strings.Contains(joined, "go.replaces.too") {
		t.Errorf("expected a go.replaces item-key warning, got: %v", warnings)
	}
}

func TestUnknownKeyWarningsAllowsSchema(t *testing.T) {
	raw := []byte(`{"$schema":"./.krate/config.schema.json","outDir":"dist"}`)
	if w := UnknownKeyWarnings(raw); len(w) != 0 {
		t.Errorf("$schema should not warn, got %v", w)
	}
}

func TestUnknownKeyWarningsCompress(t *testing.T) {
	if w := UnknownKeyWarnings([]byte(`{"compress":{"brotli":true,"gzip":true}}`)); len(w) != 0 {
		t.Errorf("compress keys should be known, got warnings: %v", w)
	}
	if w := UnknownKeyWarnings([]byte(`{"compress":{"brotil":true}}`)); len(w) == 0 {
		t.Error("expected a warning for the misspelled compress.brotil")
	}
}

func TestParseTSConfigCompress(t *testing.T) {
	cfg := Default()
	if err := parseTSConfig(`export default { compress: { brotli: true, gzip: false } }`, cfg); err != nil {
		t.Fatalf("parseTSConfig: %v", err)
	}
	if !cfg.Compress.Brotli || cfg.Compress.Gzip || !cfg.ShouldPrecompress() {
		t.Fatalf("compress not parsed: %+v", cfg.Compress)
	}
}

func TestParseTSConfigDefaults(t *testing.T) {
	src := `export default { entry: "src/app.tsx", outDir: "build" }`
	cfg := Default()
	if err := parseTSConfig(src, cfg); err != nil {
		t.Fatalf("parseTSConfig: %v", err)
	}
	if cfg.Entry != "src/app.tsx" {
		t.Errorf("Entry = %q, want 'src/app.tsx'", cfg.Entry)
	}
	if cfg.OutDir != "build" {
		t.Errorf("OutDir = %q, want 'build'", cfg.OutDir)
	}
}

func TestPluginConfigParseOptions(t *testing.T) {
	type SitemapOpts struct {
		BaseURL string `json:"baseUrl"`
	}
	pc := PluginConfig{
		Name:    "sitemap",
		Options: map[string]interface{}{"baseUrl": "https://example.com"},
	}
	var opts SitemapOpts
	if err := pc.ParseOptions(&opts); err != nil {
		t.Fatalf("ParseOptions: %v", err)
	}
	if opts.BaseURL != "https://example.com" {
		t.Errorf("BaseURL = %q, want 'https://example.com'", opts.BaseURL)
	}
}

func TestPluginConfigParseOptionsEmpty(t *testing.T) {
	type Opts struct {
		Foo string `json:"foo"`
	}
	pc := PluginConfig{Name: "test"}
	var opts Opts
	if err := pc.ParseOptions(&opts); err != nil {
		t.Fatalf("ParseOptions with empty options: %v", err)
	}
}

func TestPluginConfigParseOptionsInvalid(t *testing.T) {
	pc := PluginConfig{
		Name:    "test",
		Options: map[string]interface{}{"foo": "bar"},
	}
	var dest int // wrong type
	err := pc.ParseOptions(&dest)
	if err == nil {
		t.Error("expected error for type mismatch, got nil")
	}
}

func TestPathAliasStruct(t *testing.T) {
	pa := PathAlias{
		Prefix:  "@/*",
		Targets: []string{"./*"},
	}
	data, err := json.Marshal(pa)
	if err != nil {
		t.Fatal(err)
	}
	var decoded PathAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Prefix != "@/*" || len(decoded.Targets) != 1 || decoded.Targets[0] != "./*" {
		t.Errorf("round-trip failed: %+v", decoded)
	}
}

func TestPathAliasesObjectForm(t *testing.T) {
	var pas PathAliases
	if err := json.Unmarshal([]byte(`{ "@/*": ["./src/*"], "@/utils": "./src/utils" }`), &pas); err != nil {
		t.Fatal(err)
	}
	if len(pas) != 2 {
		t.Fatalf("expected 2 aliases, got %d", len(pas))
	}
	got := map[string]PathAlias{}
	for _, a := range pas {
		got[a.Prefix] = a
	}
	if a, ok := got["@/*"]; !ok || len(a.Targets) != 1 || a.Targets[0] != "./src/*" {
		t.Errorf("unexpected @/* alias: %+v", got["@/*"])
	}
	if a, ok := got["@/utils"]; !ok || len(a.Targets) != 1 || a.Targets[0] != "./src/utils" {
		t.Errorf("string value not wrapped in array: %+v", got["@/utils"])
	}
}

func TestPathAliasesArrayForm(t *testing.T) {
	var pas PathAliases
	if err := json.Unmarshal([]byte(`[{"prefix":"@/*","targets":["./src/*"]},{"prefix":"@x","targets":["./x"]}]`), &pas); err != nil {
		t.Fatal(err)
	}
	if len(pas) != 2 || pas[0].Prefix != "@/*" || pas[1].Targets[0] != "./x" {
		t.Errorf("array form decode failed: %+v", pas)
	}
}

func TestPathAliasesInvalidValue(t *testing.T) {
	var pas PathAliases
	if err := json.Unmarshal([]byte(`{ "@/*": 42 }`), &pas); err == nil {
		t.Error("expected error for non-string non-array value")
	}
}

func TestPathAliasesInsideConfig(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{ "pathAliases": { "@/*": "./src/*" } }`), &cfg); err != nil {
		t.Fatalf("config-level decode failed: %v", err)
	}
	if len(cfg.PathAliases) != 1 || cfg.PathAliases[0].Prefix != "@/*" {
		t.Errorf("config pathAliases decode failed: %+v", cfg.PathAliases)
	}
}

func TestRedirectStruct(t *testing.T) {
	r := Redirect{
		Source:      "/old",
		Destination: "/new",
		Permanent:   true,
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Redirect
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Source != "/old" || decoded.Destination != "/new" || !decoded.Permanent {
		t.Errorf("round-trip failed: %+v", decoded)
	}
}

func TestRewriteStruct(t *testing.T) {
	rw := Rewrite{
		Source:      "/legacy/*",
		Destination: "/v2/:splat",
	}
	data, err := json.Marshal(rw)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Rewrite
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Source != "/legacy/*" || decoded.Destination != "/v2/:splat" {
		t.Errorf("round-trip failed: %+v", decoded)
	}
}

func TestConfigValidationError(t *testing.T) {
	err := &ConfigValidationError{Message: "outDir must not be empty"}
	if err.Error() != "config validation failed: outDir must not be empty" {
		t.Errorf("unexpected error string: %s", err.Error())
	}
}

func TestFindValidationError(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   int
	}{
		{"present", "something\nKRATE_CONFIG_VALIDATION_ERROR: bad config\n", 10},
		{"absent", "some other error\n", -1},
		{"at start", "KRATE_CONFIG_VALIDATION_ERROR: x", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := strings.Index(tt.stderr, validatePrefix); got != tt.want {
				t.Errorf("Index(stderr, validatePrefix) = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestEndOfLine(t *testing.T) {
	tests := []struct {
		s    string
		from int
		want int
	}{
		{"abc\nxyz", 0, 3},
		{"abc\rxyz", 0, 3},
		{"abc", 0, 3},
		{"abc\n", 4, 4},
	}
	for _, tt := range tests {
		if got := endOfLine(tt.s, tt.from); got != tt.want {
			t.Errorf("endOfLine(%q, %d) = %d, want %d", tt.s, tt.from, got, tt.want)
		}
	}
}

// TestConfigEvalBootstrapResolvesPluginModules verifies the embedded config
// bootstrap converts file:// plugin module URLs (as returned by plugin
// factories via import.meta.url) into filesystem paths, surfaces validate()
// errors, and re-exports the config object.
func TestConfigEvalBootstrapResolvesPluginModules(t *testing.T) {
	content := configEvalBootstrap("./krate.config.ts", LoadOptions{Command: "build", Mode: "production"})
	for _, want := range []string{
		`__krateFileURLToPath`,
		`p.module.indexOf('file://') === 0`,
		`p.module = __krateFileURLToPath(p.module);`,
		`theme.module = __krateFileURLToPath(theme.module);`,
		`globalThis.__krateEvalResult = config;`,
		validatePrefix,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("bootstrap missing %q:\n%s", want, content)
		}
	}
}

func TestConfigUsesModules(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want bool
	}{
		{"import statement", "import { defineConfig } from '@krate/config';\nexport default {}", true},
		{"require call", "const cfg = require('@krate/config');\nexport default {}", true},
		{"from line", "export default {};\nfrom 'x'", false},
		{"plain literal", "export default { entry: 'src/app.tsx' }", false},
		{"window text not import", "export default { script: 'import {}' }", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := configUsesModules(tt.src); got != tt.want {
				t.Errorf("configUsesModules() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigNotExecutableError(t *testing.T) {
	err := configNotExecutableError("krate.config.ts", fmt.Errorf("execution failed:\nCannot find module '@krate/config'"))
	msg := err.Error()
	if !strings.Contains(msg, "npm install") {
		t.Errorf("error should mention running npm install, got:\n%s", msg)
	}
	if !strings.Contains(msg, "krate.config.ts") {
		t.Errorf("error should include config path, got:\n%s", msg)
	}
	if !strings.Contains(msg, "Cannot find module") {
		t.Errorf("error should preserve underlying cause, got:\n%s", msg)
	}
}
