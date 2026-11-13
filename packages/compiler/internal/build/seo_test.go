package build

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

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
