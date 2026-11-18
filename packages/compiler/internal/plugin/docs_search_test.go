package plugin

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/plugin/searchassets"
)

var hashedSearchRe = regexp.MustCompile(`^/docs/search/search\.[0-9a-f]{8}\.js$`)

func TestSearchModuleIsMinifiedAndHashed(t *testing.T) {
	out := t.TempDir()
	ctx := &BuildHookCtx{OutDir: out, Config: &config.Config{}}
	p := &DocsPlugin{}

	url, err := p.buildSearchAssets(ctx, nil, "json", 8, PagefindOptions{OutputSubdir: "pagefind"})
	if err != nil {
		t.Fatal(err)
	}
	if !hashedSearchRe.MatchString(url) {
		t.Fatalf("want a content-hashed URL, got %q", url)
	}
	dir := filepath.Join(out, "docs", "search")
	if _, err := os.Stat(filepath.Join(dir, "search.js")); err == nil {
		t.Error("the unhashed search.js must not be written")
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.Base(url)))
	if err != nil {
		t.Fatalf("hashed module not written: %v", err)
	}
	if len(data) >= len(searchassets.SearchJS)*3/4 {
		t.Errorf("module not minified: %d bytes from %d", len(data), len(searchassets.SearchJS))
	}
	if strings.Contains(string(data), "krate docs search UI") {
		t.Error("comments survived minification")
	}
	if !strings.Contains(string(data), "=[],") && !strings.Contains(string(data), "=[];") {
		t.Error("LOCALES must be an array when i18n is off, not null: the client reads its length")
	}
	if !strings.Contains(string(data), `"json"`) {
		t.Error("the engine option was not applied")
	}

	// A different option changes the content, so it changes the URL.
	other, err := p.buildSearchAssets(ctx, nil, "json", 5, PagefindOptions{OutputSubdir: "pagefind"})
	if err != nil {
		t.Fatal(err)
	}
	if other == url {
		t.Error("changing maxResults must change the hashed name")
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.Base(url))); err == nil {
		t.Error("the stale module from the earlier build was not removed")
	}
}

func TestSearchHeadTag(t *testing.T) {
	opts := &DocsPluginOptions{searchScript: "/docs/search/search.0123abcd.js"}
	lazy := searchHeadTag(opts)
	if !strings.Contains(lazy, `name="krate-search-src"`) || strings.Contains(lazy, "<script") {
		t.Errorf("default must advertise the module without loading it: %s", lazy)
	}
	opts.Search = &DocsSearchOptions{Load: "eager"}
	eager := searchHeadTag(opts)
	if !strings.Contains(eager, "<script") || !strings.Contains(eager, "defer") || !strings.Contains(eager, "search.0123abcd.js") {
		t.Errorf("eager must keep a deferred script: %s", eager)
	}
	if got := searchHeadTag(&DocsPluginOptions{}); got != "" {
		t.Errorf("no module, no tag: %q", got)
	}
}
