package plugin

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/docs"
)

func searchTestPages() []docs.Page {
	return []docs.Page{
		{Title: "Getting Started", Path: "getting-started", Content: "<h1>Getting Started</h1><p>Install krate and build.</p>"},
		{Title: "Configuration", Path: "configuration", Content: "<h1>Configuration</h1><p>Configure krate.</p>"},
	}
}

func readSearchJS(t *testing.T, outDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outDir, searchDir, "search.js"))
	if err != nil {
		t.Fatalf("reading search.js: %v", err)
	}
	return string(data)
}

func TestBuildSearchAssetsEngineSelection(t *testing.T) {
	tests := []struct {
		name        string
		engine      string
		dev         bool
		wantEngine  string
		wantDocfind bool
	}{
		{name: "docfind prod", engine: "docfind", wantEngine: "docfind", wantDocfind: true},
		{name: "json prod", engine: "json", wantEngine: "json", wantDocfind: false},
		{name: "pagefind prod", engine: "pagefind", wantEngine: "pagefind", wantDocfind: false},
		{name: "pagefind dev falls back to docfind", engine: "pagefind", dev: true, wantEngine: "docfind", wantDocfind: true},
		{name: "docfind dev", engine: "docfind", dev: true, wantEngine: "docfind", wantDocfind: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outDir := t.TempDir()
			ctx := &BuildHookCtx{OutDir: outDir, DevMode: tt.dev}
			p := &DocsPlugin{}

			if err := p.buildSearchAssets(ctx, searchTestPages(), tt.engine, 8, PagefindOptions{OutputSubdir: "pagefind"}); err != nil {
				t.Fatalf("buildSearchAssets: %v", err)
			}

			js := readSearchJS(t, outDir)
			if want := "var ENGINE = \"" + tt.wantEngine + "\";"; !strings.Contains(js, want) {
				t.Errorf("search.js missing %q", want)
			}
			if want := "var MAX_RESULTS = 8;"; !strings.Contains(js, want) {
				t.Errorf("search.js missing %q", want)
			}

			wasm := filepath.Join(outDir, searchDir, "docfind_bg.wasm")
			if _, err := os.Stat(wasm); (err == nil) != tt.wantDocfind {
				t.Errorf("docfind_bg.wasm present=%v, want %v", err == nil, tt.wantDocfind)
			}
			// search.css is always written.
			if _, err := os.Stat(filepath.Join(outDir, searchDir, "search.css")); err != nil {
				t.Errorf("search.css missing: %v", err)
			}
		})
	}
}

func TestBuildSearchAssetsMaxResultsInjected(t *testing.T) {
	outDir := t.TempDir()
	ctx := &BuildHookCtx{OutDir: outDir}
	p := &DocsPlugin{}
	if err := p.buildSearchAssets(ctx, searchTestPages(), "json", 12, PagefindOptions{OutputSubdir: "pagefind"}); err != nil {
		t.Fatalf("buildSearchAssets: %v", err)
	}
	if js := readSearchJS(t, outDir); !strings.Contains(js, "var MAX_RESULTS = 12;") {
		t.Errorf("MAX_RESULTS not injected: %s", js)
	}
}

func TestBuildSearchAssetsPagefindBaseInjected(t *testing.T) {
	outDir := t.TempDir()
	ctx := &BuildHookCtx{OutDir: outDir}
	p := &DocsPlugin{}
	if err := p.buildSearchAssets(ctx, searchTestPages(), "pagefind", 8, PagefindOptions{OutputSubdir: "search-bundle"}); err != nil {
		t.Fatalf("buildSearchAssets: %v", err)
	}
	js := readSearchJS(t, outDir)
	if !strings.Contains(js, `var PAGE_FIND_BASE = "/search-bundle/";`) {
		t.Errorf("PAGE_FIND_BASE not injected: %s", js)
	}
}

func TestEngineForBuild(t *testing.T) {
	cases := []struct {
		engine string
		dev    bool
		want   string
	}{
		{"pagefind", true, "docfind"},
		{"pagefind", false, "pagefind"},
		{"docfind", true, "docfind"},
		{"json", true, "json"},
	}
	for _, c := range cases {
		if got := engineForBuild(c.engine, c.dev); got != c.want {
			t.Errorf("engineForBuild(%q, %v) = %q, want %q", c.engine, c.dev, got, c.want)
		}
	}
}

func TestSearchConfigEngineValidation(t *testing.T) {
	// Unknown engines are ignored and fall back to the pagefind default.
	_, engine, _ := searchConfig(&DocsPluginOptions{Search: &DocsSearchOptions{Enabled: true, Engine: "bogus"}})
	if engine != "pagefind" {
		t.Errorf("engine = %q, want pagefind for unknown engine", engine)
	}
	// No search options at all -> pagefind default.
	_, engine, _ = searchConfig(&DocsPluginOptions{})
	if engine != "pagefind" {
		t.Errorf("engine = %q, want pagefind default", engine)
	}
	_, engine, _ = searchConfig(&DocsPluginOptions{Search: &DocsSearchOptions{Enabled: true, Engine: "docfind"}})
	if engine != "docfind" {
		t.Errorf("engine = %q, want docfind", engine)
	}
	enabled, _, max := searchConfig(&DocsPluginOptions{Search: &DocsSearchOptions{Enabled: false, Engine: "pagefind", MaxResults: 3}})
	if enabled {
		t.Error("expected search disabled")
	}
	if max != 3 {
		t.Errorf("maxResults = %d, want 3", max)
	}
}

func TestPagefindOptionsDefaults(t *testing.T) {
	po := pagefindOptions(&DocsPluginOptions{Search: &DocsSearchOptions{Engine: "pagefind"}})
	if po.OutputSubdir != "pagefind" {
		t.Errorf("OutputSubdir = %q, want pagefind", po.OutputSubdir)
	}

	po = pagefindOptions(&DocsPluginOptions{Search: &DocsSearchOptions{
		Engine:   "pagefind",
		Pagefind: &PagefindOptions{ExcludeSelectors: []string{".toc", ".nav"}},
	}})
	if po.OutputSubdir != "pagefind" || len(po.ExcludeSelectors) != 2 {
		t.Errorf("unexpected pagefind options: %+v", po)
	}
}

func TestPagefindArgs(t *testing.T) {
	args := pagefindArgs("/site/dist", PagefindOptions{OutputSubdir: "pagefind"})
	joined := strings.Join(args, " ")
	for _, want := range []string{"--yes", "pagefind", "--site /site/dist", "--output-subdir pagefind"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q missing %q", joined, want)
		}
	}

	args = pagefindArgs("/site/dist", PagefindOptions{
		OutputSubdir:      "search-bundle",
		ExcludeSelectors:  []string{".docs-navbar", ".toc"},
		IncludeCharacters: "<>",
		ForceLanguage:     "en",
		Verbose:           true,
	})
	joined = strings.Join(args, " ")
	for _, want := range []string{
		"--site /site/dist",
		"--output-subdir search-bundle",
		"--exclude-selectors .docs-navbar, .toc",
		"--include-characters <>",
		"--force-language en",
		"--verbose",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q missing %q", joined, want)
		}
	}
}

func TestLastLine(t *testing.T) {
	if got := lastLine("a\nb\n\n  c  \n"); got != "c" {
		t.Errorf("lastLine = %q, want c", got)
	}
	if got := lastLine(""); got != "" {
		t.Errorf("lastLine(empty) = %q, want empty", got)
	}
}

func TestAfterBuildSkipsWhenNotPagefind(t *testing.T) {
	// A non-pagefind engine (and dev mode) must not invoke the indexer.
	cfg := &config.Config{Plugins: []config.PluginConfig{{
		Name:    "docs",
		Options: map[string]interface{}{"search": map[string]interface{}{"engine": "docfind"}},
	}}}
	ctx := &BuildResultHookCtx{Root: t.TempDir(), OutDir: t.TempDir(), Config: cfg}
	if err := (&DocsPlugin{}).afterBuild(ctx); err != nil {
		t.Fatalf("afterBuild should be a no-op for docfind: %v", err)
	}
}

func TestAfterBuildSkipsInDev(t *testing.T) {
	cfg := &config.Config{Plugins: []config.PluginConfig{{
		Name:    "docs",
		Options: map[string]interface{}{"search": map[string]interface{}{"engine": "pagefind"}},
	}}}
	ctx := &BuildResultHookCtx{Root: t.TempDir(), OutDir: t.TempDir(), Config: cfg, DevMode: true}
	if err := (&DocsPlugin{}).afterBuild(ctx); err != nil {
		t.Fatalf("afterBuild should be a no-op in dev: %v", err)
	}
}

// TestRunPagefindEndToEnd exercises the real Pagefind CLI over a tiny static
// site. It is opt-in because it requires Node/network to fetch the Pagefind
// binary: set KRATE_PAGEFIND_E2E=1 to run it.
func TestRunPagefindEndToEnd(t *testing.T) {
	if os.Getenv("KRATE_PAGEFIND_E2E") != "1" {
		t.Skip("set KRATE_PAGEFIND_E2E=1 to exercise the Pagefind CLI")
	}
	if _, err := exec.LookPath("npx"); err != nil {
		t.Skipf("npx not available: %v", err)
	}

	root := t.TempDir()
	outDir := filepath.Join(root, "dist")
	if err := os.MkdirAll(filepath.Join(outDir, "guide"), 0755); err != nil {
		t.Fatal(err)
	}
	page := `<!DOCTYPE html><html lang="en"><head><title>Guide</title></head><body>` +
		`<div data-pagefind-body><h1>Guide</h1><p>Everything about signals and reactivity.</p></div>` +
		`</body></html>`
	if err := os.WriteFile(filepath.Join(outDir, "guide", "index.html"), []byte(page), 0644); err != nil {
		t.Fatal(err)
	}

	if err := runPagefind(root, outDir, PagefindOptions{OutputSubdir: "pagefind"}); err != nil {
		t.Fatalf("runPagefind: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "pagefind", "pagefind.js")); err != nil {
		t.Errorf("pagefind bundle missing: %v", err)
	}
}
