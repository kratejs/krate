package docs

import "testing"

func TestTOCItemInlineHTML(t *testing.T) {
	in := `<h2 id="a">Use <code class="x">useState</code> and <em>care</em> <a href="/x">link</a><a href="#a" class="heading-anchor" aria-label="Link to section: x"><span aria-hidden="true">#</span></a></h2>` +
		`<h3 id="b">Plain &amp; simple</h3>` +
		`<h3 id="c"><script>alert(1)</script><strong onclick="x()">Bold <kbd>K</kbd></strong> a&lt;b</h3>`
	items := ExtractTOC(in)
	if len(items) != 3 {
		t.Fatalf("items = %+v", items)
	}
	if want := `Use <code>useState</code> and <em>care</em> link`; items[0].HTML != want {
		t.Errorf("html[0] = %q, want %q", items[0].HTML, want)
	}
	if items[1].HTML != "" || items[1].Title != "Plain & simple" {
		t.Errorf("plain item = %+v", items[1])
	}
	if want := `alert(1)<strong>Bold <kbd>K</kbd></strong> a&lt;b`; items[2].HTML != want {
		t.Errorf("html[2] = %q, want %q", items[2].HTML, want)
	}
}
