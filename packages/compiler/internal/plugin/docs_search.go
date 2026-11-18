package plugin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/docfind"
	"github.com/kratejs/krate/packages/compiler/internal/docs"
	"github.com/kratejs/krate/packages/compiler/internal/plugin/searchassets"
)

// DocsSearchOptions configures the docs site search bar.
type DocsSearchOptions struct {
	// Enabled turns the search bar on/off. Default: true.
	Enabled bool `json:"enabled"`
	// Engine selects the index backend. "pagefind" (default, recommended) runs
	// the Pagefind indexer after the production build; "docfind" embeds a WASM
	// search index built in-process from the docs; "json" uses the classic
	// search-index.json fallback. Dev builds always use docfind, and the client
	// falls back to docfind/JSON automatically when the selected index is
	// unavailable.
	Engine string `json:"engine"`
	// MaxResults caps the number of results shown. Default: 8.
	MaxResults int `json:"maxResults"`
	// Placeholder is the search input placeholder (theme-rendered).
	Placeholder string `json:"placeholder,omitempty"`
	// Hotkey is the keyboard shortcut hint shown by the theme (e.g. "/" or "mod+k").
	Hotkey string `json:"hotkey,omitempty"`
	// Pagefind configures the "pagefind" engine. Ignored for other engines.
	Pagefind *PagefindOptions `json:"pagefind"`
	// Load controls when pages fetch the search module. "lazy" (default) leaves
	// it to the theme's search box to load it on first use; "eager" adds a
	// deferred script to every docs page, for themes that expect
	// window.__krateSearch to exist on load.
	Load string `json:"load,omitempty"`
}

// PagefindOptions configures the Pagefind indexer used when Engine is
// "pagefind". Pagefind is opt-in, runs only on production builds (via
// `npx pagefind`), and indexes the built HTML.
type PagefindOptions struct {
	// ExcludeSelectors lists extra CSS selectors Pagefind should not index.
	// Default: [] (the docs content wrapper already carries
	// data-pagefind-body, so chrome is excluded).
	ExcludeSelectors []string `json:"excludeSelectors"`
	// IncludeCharacters prevents Pagefind from stripping these characters when
	// indexing (useful for docs that talk about `<head>` etc). Default: "".
	IncludeCharacters string `json:"includeCharacters"`
	// ForceLanguage creates a single index as the given ISO 639-1 code,
	// ignoring per-page detection. Default: "".
	ForceLanguage string `json:"forceLanguage"`
	// OutputSubdir is the bundle directory under the output root.
	// Default: "pagefind".
	OutputSubdir string `json:"outputSubdir"`
	// Verbose prints extra indexing logs. Default: false.
	Verbose bool `json:"verbose"`
}

// searchDir is the output sub-directory for search assets.
const searchDir = "docs/search"

// searchConfig returns the effective search options with defaults applied.
// The default engine is "pagefind" (recommended); dev builds downgrade it to
// docfind in engineForBuild.
func searchConfig(opts *DocsPluginOptions) (enabled bool, engine string, maxResults int) {
	enabled = true
	engine = "pagefind"
	maxResults = 8
	if opts.Search != nil {
		if !opts.Search.Enabled {
			enabled = false
		}
		switch opts.Search.Engine {
		case "docfind", "json", "pagefind":
			engine = opts.Search.Engine
		}
		if opts.Search.MaxResults > 0 {
			maxResults = opts.Search.MaxResults
		}
	}
	return
}

// headingAnchorExcludeSelector is the class krate injects on the per-heading
// "#" link (see docs.InjectHeadingAnchors). The link sits inside the heading, so
// Pagefind would otherwise index the "#" as part of the title.
const headingAnchorExcludeSelector = ".heading-anchor"

// pagefindOptions returns the effective Pagefind options with defaults applied.
func pagefindOptions(opts *DocsPluginOptions) PagefindOptions {
	po := PagefindOptions{OutputSubdir: "pagefind"}
	if opts != nil && opts.Search != nil && opts.Search.Pagefind != nil {
		po = *opts.Search.Pagefind
		if po.OutputSubdir == "" {
			po.OutputSubdir = "pagefind"
		}
	}
	po.ExcludeSelectors = withHeadingAnchorExclude(po.ExcludeSelectors)
	return po
}

// withHeadingAnchorExclude ensures the heading-anchor selector is excluded while
// preserving any user-supplied selectors.
func withHeadingAnchorExclude(selectors []string) []string {
	for _, s := range selectors {
		if strings.TrimSpace(s) == headingAnchorExcludeSelector {
			return selectors
		}
	}
	return append(selectors, headingAnchorExcludeSelector)
}

// engineForBuild returns the effective engine for a build, taking dev mode into
// account. Pagefind indexes built HTML in a post-build pass that dev's
// incremental rebuilds don't re-run, so dev always uses docfind (falling back to
// JSON in the client if it can't load).
func engineForBuild(engine string, dev bool) string {
	if dev && engine == "pagefind" {
		return "docfind"
	}
	return engine
}

// buildSearchAssets writes the search UI and, when the effective engine is
// "docfind", builds the in-process WASM index and writes the searchable module +
// JS glue. The emitted search.js is told which engine to try first via an
// injected ENGINE constant; the client degrades to docfind then JSON if it can't
// load. Pagefind is handled separately in the AfterBuild hook (it indexes built
// HTML), so when the effective engine is "pagefind" only the JSON fallback is
// written here.
//
// It returns an error only when the WASM index build fails; the caller should
// treat that as a warning and let the client fall back to the JSON index.
//
// The first return value is the site-relative URL of the content-hashed,
// minified search module ("" when it was not written).
func (p *DocsPlugin) buildSearchAssets(ctx *BuildHookCtx, pages []docs.Page, engine string, maxResults int, pf PagefindOptions) (string, error) {
	basePath, locales := "", []string{} // never nil: it is emitted as a JS array
	if cfg, ok := ctx.Config.(*config.Config); ok {
		basePath = cfg.BaseURLPath()
		if cfg.I18n.Enabled() {
			_, def := resolveLocales(cfg.I18n)
			for _, l := range cfg.I18n.Locales {
				if l != def {
					locales = append(locales, l)
				}
			}
		}
	}
	outDir := filepath.Join(ctx.OutDir, searchDir)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", fmt.Errorf("creating search dir: %w", err)
	}

	// Dev builds always use docfind: Pagefind's post-build index is not
	// regenerated by dev's incremental rebuilds.
	engine = engineForBuild(engine, ctx.DevMode)

	searchJS := searchassets.SearchJS
	if maxResults > 0 {
		searchJS = bytes.ReplaceAll(searchJS, []byte("var MAX_RESULTS = 8;"),
			[]byte(fmt.Sprintf("var MAX_RESULTS = %d;", maxResults)))
	}
	searchJS = bytes.ReplaceAll(searchJS, []byte(`var ENGINE = "docfind";`),
		[]byte(fmt.Sprintf("var ENGINE = %q;", engine)))
	searchJS = bytes.ReplaceAll(searchJS, []byte(`var SEARCH_BASE = "/docs/search/";`),
		[]byte(fmt.Sprintf("var SEARCH_BASE = %q;", basePath+"/docs/search/")))
	searchJS = bytes.ReplaceAll(searchJS, []byte(`var INDEX_JSON = "/docs/data/search-index.json";`),
		[]byte(fmt.Sprintf("var INDEX_JSON = %q;", basePath+"/docs/data/search-index.json")))
	localesJSON, _ := json.Marshal(locales)
	searchJS = bytes.ReplaceAll(searchJS, []byte(`var LOCALES = [];`),
		[]byte("var LOCALES = "+string(localesJSON)+";"))
	searchJS = bytes.ReplaceAll(searchJS, []byte(`var BASE_PATH = "";`),
		[]byte(fmt.Sprintf("var BASE_PATH = %q;", basePath)))
	pagefindBase := basePath + "/" + filepath.ToSlash(pf.OutputSubdir) + "/"
	searchJS = bytes.ReplaceAll(searchJS, []byte(`var PAGE_FIND_BASE = "/pagefind/";`),
		[]byte(fmt.Sprintf("var PAGE_FIND_BASE = %q;", pagefindBase)))
	if !ctx.DevMode {
		searchJS = minifySearchJS(searchJS)
	}
	jsName := hashedSearchName(searchJS)
	// Drop modules from earlier builds (dev rebuilds reuse the directory).
	if old, _ := filepath.Glob(filepath.Join(outDir, "search.*.js")); len(old) > 0 {
		for _, f := range old {
			_ = os.Remove(f)
		}
	}
	if err := os.WriteFile(filepath.Join(outDir, jsName), searchJS, 0644); err != nil {
		return "", err
	}
	jsURL := basePath + "/" + searchDir + "/" + jsName
	if err := os.WriteFile(filepath.Join(outDir, "search.css"), searchassets.SearchCSS, 0644); err != nil {
		return jsURL, err
	}

	if engine != "docfind" {
		return jsURL, nil
	}

	documents := docs.BuildSearchDocumentsBase(pages, basePath)
	if len(documents) == 0 {
		return jsURL, nil
	}

	wasmBytes, err := docfind.Build(context.Background(), documents)
	if err != nil {
		return jsURL, fmt.Errorf("docfind: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, docfind.WASMName), wasmBytes, 0644); err != nil {
		return jsURL, err
	}
	return jsURL, os.WriteFile(filepath.Join(outDir, docfind.DocfindJSName), docfind.DocfindJS, 0644)
}

// minifySearchJS minifies the search module, returning it unchanged when
// esbuild reports an error.
func minifySearchJS(js []byte) []byte {
	res := api.Transform(string(js), api.TransformOptions{
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      true,
		Charset:           api.CharsetUTF8,
	})
	if len(res.Errors) > 0 {
		return js
	}
	return res.Code
}

// hashedSearchName names the search module after its content, so it can be
// cached as immutable and a changed engine or option yields a new URL.
func hashedSearchName(js []byte) string {
	sum := sha256.Sum256(js)
	return "search." + hex.EncodeToString(sum[:4]) + ".js"
}

// Search UI lives in the theme, not the plugin. The plugin emits the index and
// search.js (which defines window.__krateSearch); the theme renders its own
// component (see @krate/base-docs-theme DocsSearch).
