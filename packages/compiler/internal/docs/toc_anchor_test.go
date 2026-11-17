package docs

import (
	"strings"
	"testing"
)

func TestInjectHeadingAnchorsAccessible(t *testing.T) {
	got := InjectHeadingAnchors(`<h2 id="a-b">Go <code>x</code> &amp; "quotes"</h2><p>t</p>`)
	want := `<h2 id="a-b">Go <code>x</code> &amp; "quotes"<a href="#a-b" class="heading-anchor" data-pagefind-ignore aria-label="Link to section: Go x &amp; &quot;quotes&quot;"><span aria-hidden="true">#</span></a></h2><p>t</p>`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	// The heading text comes first, so the "#" never leads the heading name.
	if strings.Index(got, "Go ") > strings.Index(got, "heading-anchor") {
		t.Errorf("anchor must follow the heading text: %s", got)
	}
}

func TestInjectHeadingAnchorsSkipsHeadingsWithoutID(t *testing.T) {
	in := `<h3>plain</h3>`
	if got := InjectHeadingAnchors(in); got != in {
		t.Errorf("got %s", got)
	}
}
