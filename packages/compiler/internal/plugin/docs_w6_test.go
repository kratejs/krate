package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/docs"
	"github.com/kratejs/krate/packages/compiler/internal/markdown"
)

func TestSearchIndexUsesRealURLsForAllLocales(t *testing.T) {
	pages := []docs.Page{
		{Path: "index", Title: "Home", Content: "<p>home</p>", URL: "/docs/"},
		{Path: "guides/index", Title: "Guides", Content: "<p>g</p>", URL: "/docs/guides/"},
		{Path: "start", Title: "Start", Content: "<p>s</p>", URL: "/docs/start/"},
		{Path: "start", Title: "Demarrer", Content: "<p>fr</p>", URL: "/fr/docs/start/", Locale: "fr"},
		{Path: "start", Title: "はじめに", Content: "<p>日本語</p>", URL: "/ja/docs/start/", Locale: "ja"},
	}
	var got []string
	for _, e := range docs.BuildSearchIndexBase(pages, "/sub") {
		got = append(got, e.Path)
	}
	want := []string{"/sub/docs/", "/sub/docs/guides/", "/sub/docs/start/", "/sub/fr/docs/start/", "/sub/ja/docs/start/"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("index paths = %v, want %v", got, want)
	}
	for _, d := range docs.BuildSearchDocumentsBase(pages, "") {
		if strings.HasSuffix(d.Href, "/index/") {
			t.Errorf("docfind href %q points at a non-existent index page", d.Href)
		}
	}
}

func TestWriteAssetsIndexesEveryLocale(t *testing.T) {
	out := t.TempDir()
	ctx := &BuildHookCtx{OutDir: out, Config: &config.Config{BasePath: "/sub"}}
	pages := []docs.Page{
		{Path: "start", Title: "Start", Content: "<p>s</p>", URL: "/docs/start/"},
		{Path: "start", Title: "JA", Content: "<p>日本語</p>", URL: "/ja/docs/start/", Locale: "ja"},
	}
	(&DocsPlugin{}).writeAssets(ctx, nil, pages, &DocsPluginOptions{})
	data, err := os.ReadFile(filepath.Join(out, "docs", "data", "search-index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []docs.SearchEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[1].Path != "/sub/ja/docs/start/" || !strings.Contains(entries[1].Content, "日本語") {
		t.Errorf("json index must include translated pages with their own URL: %+v", entries)
	}
}

func TestLocaleSwitchFallbackAndHreflang(t *testing.T) {
	roots := map[string]string{"en": "/docs/", "fr": "/fr/docs/"}
	links := localeSwitchLinksWithLabels([]string{"en", "fr", "ja"}, "en",
		map[string]string{"en": "/docs/start/"}, nil, roots)
	if len(links) != 2 {
		t.Fatalf("a locale with neither a translation nor a landing page must be hidden: %+v", links)
	}
	if links[0].Fallback || links[0].URL != "/docs/start/" {
		t.Errorf("en link = %+v", links[0])
	}
	if !links[1].Fallback || links[1].URL != "/fr/docs/" {
		t.Errorf("fr link should fall back to the locale home with a marker: %+v", links[1])
	}

	cfg := &config.Config{BasePath: "/sub", SEO: config.SEOConfig{BaseURL: "https://example.com"}}
	page := docs.Page{Path: "start", Title: "Start", URL: "/docs/start/"}
	tsx := (&DocsPlugin{}).generateTSX(&BuildHookCtx{}, cfg, page, "", true, nil, nil, nil, "", "", "", "",
		docsLocaleInfo{Locale: "en", DefaultLocale: "en", Locales: links}, &DocsPluginOptions{}, nil, markdown.Config{}, "/docs")
	if !strings.Contains(tsx, `hreflang={"en"}`) || !strings.Contains(tsx, "https://example.com/sub/docs/start/") {
		t.Errorf("real translation missing from hreflang:\n%s", tsx)
	}
	if strings.Contains(tsx, `hreflang={"fr"}`) {
		t.Errorf("untranslated locale must not be advertised as an alternate:\n%s", tsx)
	}
}

func TestDocsLinksCarryBasePath(t *testing.T) {
	cfg := &config.Config{BasePath: "/sub"}
	page := docs.Page{Path: "start", Title: "Start", URL: "/docs/start/"}
	sections := []docs.SidebarItem{{Title: "Start", URL: "/docs/start/"}, {Title: "G", URL: "/docs/g/", Children: []docs.SidebarItem{{Title: "C", URL: "/docs/g/c/"}}}}
	crumbs := []docs.Breadcrumb{{Label: "Docs", URL: "/docs/"}}
	tsx := (&DocsPlugin{}).generateTSX(&BuildHookCtx{}, cfg, page, "", true, sections, nil, crumbs, "P", "/docs/p/", "N", "/docs/n/",
		docsLocaleInfo{}, &DocsPluginOptions{}, nil, markdown.Config{}, "/docs")
	for _, want := range []string{
		`src="/sub/docs/search/search.js"`, `"url":"/sub/docs/start/"`, `"url":"/sub/docs/g/c/"`,
		`/sub/docs/p/`, `/sub/docs/n/`, `routeBase: "/sub/docs"`,
	} {
		if !strings.Contains(tsx, want) {
			t.Errorf("missing %q in generated page:\n%s", want, tsx)
		}
	}
	if strings.Contains(tsx, `"/docs/`) {
		t.Errorf("an unprefixed /docs/ URL remains:\n%s", tsx)
	}
}

func TestSearchJSIsBaseAndLocaleAware(t *testing.T) {
	out := t.TempDir()
	cfg := &config.Config{BasePath: "/sub", I18n: config.I18nConfig{Locales: []string{"en", "fr"}, DefaultLocale: "en"}}
	ctx := &BuildHookCtx{OutDir: out, Config: cfg}
	if err := (&DocsPlugin{}).buildSearchAssets(ctx, nil, "json", 8, PagefindOptions{OutputSubdir: "pagefind"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "docs", "search", "search.js"))
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{`var SEARCH_BASE = "/sub/docs/search/";`, `var INDEX_JSON = "/sub/docs/data/search-index.json";`,
		`var LOCALES = ["fr"];`, `var BASE_PATH = "/sub";`, `var PAGE_FIND_BASE = "/sub/pagefind/";`} {
		if !strings.Contains(js, want) {
			t.Errorf("search.js missing %q", want)
		}
	}
}

func TestFeedAndJSONLDUseLocalizedURLs(t *testing.T) {
	cfg := &config.Config{
		BasePath: "/sub",
		SEO:      config.SEOConfig{BaseURL: "https://example.com"},
		I18n:     config.I18nConfig{Locales: []string{"en", "fr"}, DefaultLocale: "en"},
	}
	if got := CanonicalURL("https://example.com", cfg.BaseURLPath(), feedPagePath(cfg, "/docs", "fr/getting-started")); got != "https://example.com/sub/fr/docs/getting-started/" {
		t.Errorf("fr feed URL = %q", got)
	}
	if got := feedPagePath(cfg, "/docs", "getting-started"); got != "/docs/getting-started/" {
		t.Errorf("default-locale feed path = %q", got)
	}
	if got := feedPagePath(cfg, "/docs", "guides/index"); got != "/docs/guides/" {
		t.Errorf("index feed path = %q", got)
	}
	fr := docs.Page{Path: "getting-started", Title: "T", URL: "/fr/docs/getting-started/"}
	if got := docsJSONLDURL(cfg, "https://example.com", fr); got != "https://example.com/sub/fr/docs/getting-started/" {
		t.Errorf("JSON-LD url = %q", got)
	}
	head := docsHeadExtras(cfg, fr, "Docs")
	if !strings.Contains(head, `/sub/fr/docs/getting-started/`) {
		t.Errorf("JSON-LD in head points at the wrong page:\n%s", head)
	}
}
