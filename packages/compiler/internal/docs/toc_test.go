package docs

import (
	"strings"
	"testing"
)

// The per-heading "#" link must be marked data-pagefind-ignore so Pagefind does
// not index the "#" as part of the heading text.
func TestInjectHeadingAnchorsMarksPagefindIgnore(t *testing.T) {
	out := InjectHeadingAnchors(`<h2 id="install">Install</h2><p>Body</p>`)
	if !strings.Contains(out, `class="heading-anchor" data-pagefind-ignore`) {
		t.Fatalf("anchor missing data-pagefind-ignore: %s", out)
	}
	if !strings.Contains(out, `href="#install"`) {
		t.Fatalf("anchor href missing: %s", out)
	}
	// Headings without an id get no anchor.
	if got := InjectHeadingAnchors(`<h2>No id</h2>`); strings.Contains(got, "heading-anchor") {
		t.Fatalf("anchor injected without id: %s", got)
	}
}

// The docfind/JSON index is built from pre-anchor content, so the indexed body
// must never contain the injected "#" link text.
func TestSearchDocumentsExcludeAnchorHash(t *testing.T) {
	pages := []Page{{Title: "Install", Path: "install", Content: "<h2>Install</h2><p>Run krate.</p>"}}
	got := BuildSearchDocuments(pages)
	if len(got) != 1 {
		t.Fatalf("documents = %d, want 1", len(got))
	}
	if strings.Contains(got[0].Body, "#") {
		t.Fatalf("search body contains '#' anchor text: %q", got[0].Body)
	}
}
