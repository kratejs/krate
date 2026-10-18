package build

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestGlobalHeadHTML(t *testing.T) {
	b := &Builder{Cfg: &config.Config{
		Head: config.HeadConfig{
			Meta:    []config.HeadMeta{{Name: "robots", Content: "index,follow"}},
			Links:   []config.HeadLink{{Rel: "preconnect", Href: "https://cdn.example"}},
			Scripts: []config.HeadScript{{Src: "https://cdn.example/a.js", Defer: true}},
			Raw:     []string{"<!-- raw -->"},
		},
	}}
	got := b.globalHeadHTML("")
	for _, want := range []string{
		`<meta name="robots" content="index,follow">`,
		`<link rel="preconnect" href="https://cdn.example">`,
		`<script src="https://cdn.example/a.js" defer></script>`,
		`<!-- raw -->`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestGlobalHeadEmpty(t *testing.T) {
	b := &Builder{Cfg: &config.Config{}}
	if got := b.globalHeadHTML(""); got != "" {
		t.Fatalf("expected empty head, got %q", got)
	}
}

func TestGlobalHeadDedup(t *testing.T) {
	b := &Builder{Cfg: &config.Config{Head: config.HeadConfig{
		Meta:    []config.HeadMeta{{Name: "robots", Content: "index,follow"}},
		Links:   []config.HeadLink{{Rel: "canonical", Href: "https://x/y"}},
		Scripts: []config.HeadScript{{Src: "/a.js"}},
	}}}
	existing := `<meta name="robots" content="noindex"><link rel="canonical" href="https://x/y"><script src="/a.js"></script>`
	if got := b.globalHeadHTML(existing); got != "" {
		t.Fatalf("expected all tags deduped against the page head, got %q", got)
	}
}

func TestHeadScriptInlineEscaped(t *testing.T) {
	got := renderHeadScript(config.HeadScript{Content: "var x='</script>';"})
	if !strings.Contains(got, `<\/script>`) {
		t.Errorf("expected escaped terminator, got:\n%s", got)
	}
	if strings.Contains(got, `var x='</script>'`) {
		t.Errorf("unescaped inline terminator present:\n%s", got)
	}
}
