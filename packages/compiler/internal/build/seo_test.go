package build

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// The extended SEO fields (site.titleTemplate/twitter/themeColor/locale) are
// emitted into the head.
func TestGenerateSEOTagsExtended(t *testing.T) {
	seo := config.SEOConfig{
		BaseURL:       "https://example.com",
		SiteName:      "Site",
		Image:         "https://example.com/og.png",
		TitleTemplate: "%s | Site",
		Twitter:       "sitehandle",
		ThemeColor:    "#0b1020",
		Locale:        "en-US",
	}
	got := generateSEOTags("<title>Guide</title>", "docs/guide", seo)
	for _, want := range []string{
		`og:title" content="Guide | Site"`,
		`twitter:site" content="@sitehandle"`,
		`theme-color" content="#0b1020"`,
		`og:locale" content="en_US"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// A title that already carries a site/section suffix must not have the template
// appended again (docs pages use "<page> - <site docs>").
func TestGenerateSEOTagsTitleTemplateSkipsSuffixedTitles(t *testing.T) {
	seo := config.SEOConfig{BaseURL: "https://example.com", SiteName: "Site", TitleTemplate: "%s | Site"}
	got := generateSEOTags("<title>Guide - Site Docs</title>", "docs/guide", seo)
	if strings.Contains(got, "Site Docs | Site") {
		t.Errorf("template duplicated the suffix:\n%s", got)
	}
	bare := generateSEOTags("<title>Guide</title>", "docs/guide", seo)
	if !strings.Contains(bare, `og:title" content="Guide | Site"`) {
		t.Errorf("template not applied to a bare title:\n%s", bare)
	}
}

// A configured description becomes the page's meta description even without a
// base URL, so `krate check` (seo/description) is satisfied by site.description.
func TestGenerateSEOTagsEmitsDefaultMetaDescription(t *testing.T) {
	got := generateSEOTags("<title>T</title>", "", config.SEOConfig{Description: "A site"})
	if !strings.Contains(got, `<meta name="description" content="A site">`) {
		t.Fatalf("missing default meta description: %q", got)
	}
	if got := generateSEOTags(`<meta name="description" content="Own">`, "", config.SEOConfig{Description: "A site"}); got != "" {
		t.Fatalf("page description must win: %q", got)
	}
}

func TestSEOCanonicalMatchesServedForm(t *testing.T) {
	seo := config.SEOConfig{BaseURL: "https://example.com", SiteName: "Site", Image: "/og.png"}
	got := generateSEOTags("<title>Guide</title>", "/docs/guide", seo, seoCtx{BasePath: "/sub"})
	for _, want := range []string{
		`rel="canonical" href="https://example.com/sub/docs/guide/"`,
		`og:url" content="https://example.com/sub/docs/guide/"`,
		`og:image" content="https://example.com/sub/og.png"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	root := generateSEOTags("<title>Home</title>", "/", seo)
	if !strings.Contains(root, `rel="canonical" href="https://example.com/"`) {
		t.Errorf("root canonical:\n%s", root)
	}
}

func TestSEOErrorPageHasNoCanonical(t *testing.T) {
	seo := config.SEOConfig{BaseURL: "https://example.com", SiteName: "Site"}
	got := generateSEOTags("<title>Not found</title>", "/", seo, seoCtx{ErrorPage: true})
	if strings.Contains(got, "canonical") || strings.Contains(got, "og:url") {
		t.Errorf("error page must not advertise a canonical URL:\n%s", got)
	}
}

func TestSEODoesNotDoubleEscapeTitleAndDescription(t *testing.T) {
	seo := config.SEOConfig{BaseURL: "https://example.com", SiteName: "Site"}
	head := `<title>Home &amp; Q&amp;A</title><meta name="description" content="Fish &amp; chips">`
	got := generateSEOTags(head, "/", seo)
	for _, want := range []string{
		`og:title" content="Home &amp; Q&amp;A"`,
		`twitter:title" content="Home &amp; Q&amp;A"`,
		`twitter:description" content="Fish &amp; chips"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "&amp;amp;") {
		t.Errorf("double-escaped entity:\n%s", got)
	}
}

func TestDedupeHeadPageWinsOverLayout(t *testing.T) {
	page := "<title>Page</title>\n<meta name=\"description\" content=\"page desc\">\n<meta property=\"og:image\" content=\"/page.png\">\n"
	layout := "<title>Layout</title>\n<meta name=\"description\" content=\"layout desc\">\n<meta property=\"og:image\" content=\"/layout.png\">\n<meta name=\"author\" content=\"me\">\n<script>var a=1</script>\n<script>var a=1</script>\n"
	got := dedupeHead(page + layout)
	for _, want := range []string{"<title>Page</title>", "page desc", "/page.png", `name="author"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, bad := range []string{"Layout", "layout desc", "/layout.png"} {
		if strings.Contains(got, bad) {
			t.Errorf("duplicate %q survived:\n%s", bad, got)
		}
	}
	if strings.Count(got, "<title") != 1 || strings.Count(got, "<script>") != 2 {
		t.Errorf("titles must dedupe but scripts must not:\n%s", got)
	}
}

func TestTitleTemplateShapesDocumentTitle(t *testing.T) {
	seo := config.SEOConfig{SiteName: "Site", TitleTemplate: "%s | Site"}
	got := templateHeadTitle("<title>Home &amp; Co</title>\n<meta name=\"x\" content=\"y\">", seo)
	if !strings.Contains(got, "<title>Home &amp; Co | Site</title>") {
		t.Errorf("document title not templated:\n%s", got)
	}
	again := templateHeadTitle(got, seo)
	if again != got {
		t.Errorf("template applied twice:\n%s", again)
	}
	if out := templateHeadTitle("<title>Guide - Site Docs</title>", seo); strings.Contains(out, "| Site") {
		t.Errorf("suffixed title was templated again: %s", out)
	}
}
