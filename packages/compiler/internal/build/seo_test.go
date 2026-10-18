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
