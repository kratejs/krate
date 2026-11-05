package build

import "testing"

// A generateStaticParams variant of a request-time page has a plain route
// ("/blog/hello") but must still render with the params of its [slug] source.
func TestFindPageForRouteVariantKeepsParams(t *testing.T) {
	s := &SSRServer{manifest: &ServerManifest{Pages: []ManifestPage{
		{Route: "/blog/[slug]", Source: "src/pages/blog/[slug].tsx", Mode: "ssr"},
		{Route: "/blog/hello", Source: "src/pages/blog/[slug].tsx", Mode: "ssr"},
		{Route: "/about", Source: "src/pages/about.tsx", Mode: "ssr"},
	}}}
	page, params := s.FindPageForRoute("/blog/hello")
	if page == nil || page.Route != "/blog/hello" {
		t.Fatalf("page = %+v", page)
	}
	if params["slug"] != "hello" {
		t.Errorf("variant params = %v, want slug=hello", params)
	}
	page, params = s.FindPageForRoute("/blog/other")
	if page == nil || page.Route != "/blog/[slug]" || params["slug"] != "other" {
		t.Errorf("pattern page = %+v params = %v", page, params)
	}
	if _, params = s.FindPageForRoute("/about"); len(params) != 0 {
		t.Errorf("static page params = %v, want none", params)
	}
}
