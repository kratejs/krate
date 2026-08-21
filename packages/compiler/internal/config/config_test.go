package config

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestParseTSConfigSSR(t *testing.T) {
	src := `export default {
		ssr: {
			rendererPort: 4100,
			timeout: 3000,
			maxCacheSize: 64,
			middlewareRuntime: "node",
			apiRuntime: "node",
			serverComponentRuntime: "quickjs",
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
	if cfg.SSR.ServerComponentRuntime != "quickjs" {
		t.Errorf("ServerComponentRuntime = %q, want quickjs", cfg.SSR.ServerComponentRuntime)
	}
	if cfg.SSR.SSRRuntime != "node" {
		t.Errorf("SSRRuntime = %q, want node", cfg.SSR.SSRRuntime)
	}
	if !cfg.SSR.Streaming {
		t.Error("Streaming = false, want true")
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

// TestWriteBootstrapResolvesPluginModules verifies the generated config
// bootstrap converts file:// plugin module URLs (as returned by plugin
// factories via import.meta.url) into filesystem paths before serialization.
func TestWriteBootstrapResolvesPluginModules(t *testing.T) {
	bootstrapPath := filepath.Join(t.TempDir(), "bootstrap.mjs")
	content := configBootstrapContent(filepath.Join("C:", "proj", "krate.config.ts"))
	if err := os.WriteFile(bootstrapPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(bootstrapPath)

	data, err := os.ReadFile(bootstrapPath)
	if err != nil {
		t.Fatal(err)
	}
	content = string(data)

	for _, want := range []string{
		`fileURLToPath`,
		`p.module.startsWith('file://')`,
		`p.module = fileURLToPath(p.module);`,
		`JSON.stringify(config)`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("bootstrap missing %q:\n%s", want, content)
		}
	}
}
