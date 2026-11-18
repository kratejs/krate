package plugin

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/docs"
)

func mdCfg(mermaid, math bool) *config.Config {
	cfg := &config.Config{}
	cfg.Markdown.Mermaid = mermaid
	cfg.Markdown.Math = math
	return cfg
}

func TestDocsSiteUse(t *testing.T) {
	plain := docs.Page{Content: "<p>hello</p>"}
	diagram := docs.Page{Content: `<pre class="mermaid">graph TD</pre>`}
	maths := docs.Page{Content: `<p><span class="krate-math krate-math-inline">$x$</span></p>`}

	if u := docsSiteUse([]docs.Page{plain}); u.mermaid || u.math {
		t.Fatalf("plain site reported use: %+v", u)
	}
	if u := docsSiteUse([]docs.Page{plain, diagram}); !u.mermaid || u.math {
		t.Fatalf("diagram site: %+v", u)
	}
	if u := docsSiteUse([]docs.Page{plain, maths}); u.mermaid || !u.math {
		t.Fatalf("math site: %+v", u)
	}
}

func TestMermaidLoaderGating(t *testing.T) {
	cfg := mdCfg(true, false)

	// No page uses mermaid: nothing is emitted, even with the option on.
	if got := docsHeadExtras(cfg, docs.Page{}, "", docsAssetUse{}); strings.Contains(got, "mermaid") {
		t.Fatalf("loader emitted for a site without diagrams:\n%s", got)
	}
	// Option off: nothing, even if the content has a diagram.
	if got := docsHeadExtras(mdCfg(false, false), docs.Page{}, "", docsAssetUse{mermaid: true}); strings.Contains(got, "mermaid") {
		t.Fatalf("loader emitted with markdown.mermaid off:\n%s", got)
	}

	got := docsHeadExtras(cfg, docs.Page{Content: "<p>no diagram</p>"}, "", docsAssetUse{mermaid: true})
	if !strings.Contains(got, "document.querySelector('.mermaid')") {
		t.Fatalf("loader must check for .mermaid before importing:\n%s", got)
	}
	if !strings.Contains(got, "krate:navigate") {
		t.Fatalf("loader must re-check on client navigation")
	}
	if !strings.Contains(got, "mermaid@"+mermaidVersion+"/") || strings.Contains(got, "mermaid@11/") {
		t.Fatalf("mermaid version must be pinned to %s", mermaidVersion)
	}
	// The CDN module is only reachable through a dynamic import().
	if strings.Contains(got, "import mermaid") || !strings.Contains(got, "import('https://cdn.jsdelivr.net/npm/mermaid@") {
		t.Fatalf("mermaid must be loaded with a dynamic import")
	}
}

func TestMathLoaderGating(t *testing.T) {
	cfg := mdCfg(false, true)
	if got := docsHeadExtras(cfg, docs.Page{}, "", docsAssetUse{}); strings.Contains(got, "katex") {
		t.Fatalf("KaTeX emitted for a site without math:\n%s", got)
	}

	plain := docsHeadExtras(cfg, docs.Page{Content: "<p>plain</p>"}, "", docsAssetUse{math: true})
	if strings.Contains(plain, "<link") {
		t.Fatalf("page without math must not link the KaTeX stylesheet:\n%s", plain)
	}
	if !strings.Contains(plain, "document.querySelector('.krate-math')") || !strings.Contains(plain, "krate:navigate") {
		t.Fatalf("loader must be on-demand and navigation aware:\n%s", plain)
	}
	if strings.Contains(plain, `src="https://cdn.jsdelivr.net`) {
		t.Fatalf("page without math must not carry static KaTeX scripts")
	}

	with := docsHeadExtras(cfg, docs.Page{Content: `<span class="krate-math">$x$</span>`}, "", docsAssetUse{math: true})
	if !strings.Contains(with, "katex@"+katexVersion+"/dist/katex.min.css") {
		t.Fatalf("page with math must link the stylesheet:\n%s", with)
	}
}
