package build

import "testing"

// TestRegexRedirectSources: next.config style regex parameters match (issue:
// /store/:slug((?!compare$)[^/]+) was copied verbatim and never matched).
func TestRegexRedirectSources(t *testing.T) {
	cases := []struct {
		source, path string
		want         map[string]string
	}{
		{`/store/:slug((?!compare$)[^/]+)`, "/store/some-template", map[string]string{"slug": "some-template"}},
		{`/store/:slug((?!compare$)[^/]+)`, "/store/some-template/", map[string]string{"slug": "some-template"}},
		{`/store/:slug((?!compare$)[^/]+)`, "/store/compare", nil},
		{`/store/:slug((?!compare$)[^/]+)`, "/store/a/b", nil},
		{`/store/:slug((?!compare$)[^/]+)`, "/store", nil},
		{`/p/:id(\d+)`, "/p/42", map[string]string{"id": "42"}},
		{`/p/:id(\d+)`, "/p/x42", nil},
		{`/p/:id(\d+)?`, "/p", map[string]string{"id": ""}},
		{`/p/:id(\d+)?`, "/p/7", map[string]string{"id": "7"}},
		{`/docs/:path((?:a|b)/.*)`, "/docs/a/x/y", map[string]string{"path": "a/x/y"}},
		{`/v/:n(\d+)*`, "/v/1/2/3", map[string]string{"n": "1/2/3"}},
		{`/v/:n(\d+)+`, "/v", nil},
		{`/(en|de)/about`, "/de/about", map[string]string{"0": "de"}},
		{`/bad/:x((unclosed)`, "/bad/x", nil},
	}
	for _, c := range cases {
		params, ok := matchRedirectParams(c.path, c.source)
		if ok != (c.want != nil) {
			t.Errorf("%s ~ %s: matched=%v, want %v", c.source, c.path, ok, c.want != nil)
			continue
		}
		for k, v := range c.want {
			if params[k] != v {
				t.Errorf("%s ~ %s: param %s = %q, want %q", c.source, c.path, k, params[k], v)
			}
		}
	}
	if got := rewriteDestination("/store/some-template", `/store/:slug((?!compare$)[^/]+)`, "/templates/:slug"); got != "/templates/some-template" {
		t.Errorf("destination = %q", got)
	}
}
