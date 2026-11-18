package markdown

import (
	"strings"
	"testing"
)

func mustContain(t *testing.T, name, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%s: missing %q in:\n%s", name, w, got)
		}
	}
}

func mustNotContain(t *testing.T, name, got string, bads ...string) {
	t.Helper()
	for _, b := range bads {
		if strings.Contains(got, b) {
			t.Errorf("%s: unexpected %q in:\n%s", name, b, got)
		}
	}
}

func TestSlugifyMatchesGitHub(t *testing.T) {
	for in, want := range map[string]string{
		"Go (components & API routes)": "go-components--api-routes",
		"SEO & robots":                 "seo--robots",
		"Redirects & Rewrites":         "redirects--rewrites",
		"Plain heading":                "plain-heading",
		"h - hyperscript":              "h---hyperscript",
		"Components &amp; helpers":     "components--helpers",
		"  Padded  ":                   "padded",
		"Café über":                    "café-über",
	} {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHeadingIDsKeepDoubleHyphensAndDedupe(t *testing.T) {
	out := RenderToHTML("## Go (components & API routes)\n\n## Go (components & API routes)\n\n## SEO & robots\n", Config{HeadingAnchors: true})
	mustContain(t, "ids", out, `id="go-components--api-routes"`, `id="go-components--api-routes-1"`, `id="seo--robots"`)
}
