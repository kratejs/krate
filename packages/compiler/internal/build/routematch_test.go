package build

import (
	"reflect"
	"testing"
)

func TestMatchRoutePatternCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		url, pattern string
		ok           bool
		params       map[string]string
	}{
		{"/", "", true, map[string]string{}},
		{"/about", "about", true, map[string]string{}},
		{"/about/", "about", true, map[string]string{}},
		{"/about/x", "about", false, nil},
		{"/blog/hi", "blog/[slug]", true, map[string]string{"slug": "hi"}},
		{"/blog", "blog/[slug]", false, nil},
		{"/blog//", "blog/[slug]", false, nil},
		{"/blog/a/b", "blog/[slug]", false, nil},
		{"/docs/a/b/c", "docs/[...rest]", true, map[string]string{"rest": "a/b/c"}},
		{"/docs", "docs/[...rest]", false, nil},
		{"/docs/a//b", "docs/[...rest]", false, nil},
		{"/shop", "shop/[[...f]]", true, map[string]string{"f": ""}},
		{"/shop/a/b", "shop/[[...f]]", true, map[string]string{"f": "a/b"}},
		{"/", "[[...f]]", true, map[string]string{"f": ""}},
		{"/x/y", "[...a]/z", false, nil},
		{"/x", "other", false, nil},
	}
	for _, tc := range tests {
		params, ok := matchRoutePattern(tc.url, tc.pattern)
		if ok != tc.ok || (ok && !reflect.DeepEqual(params, tc.params)) {
			t.Errorf("matchRoutePattern(%q, %q) = %v, %v; want %v, %v", tc.url, tc.pattern, params, ok, tc.params, tc.ok)
		}
		if !ok && params != nil {
			t.Errorf("matchRoutePattern(%q, %q) returned params on a miss", tc.url, tc.pattern)
		}
	}
}

func TestMatchRoutePrefixCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		url, pattern string
		ok           bool
		params       map[string]string
	}{
		{"/anything/here", "", true, map[string]string{}},
		{"/blog/hi/comments", "blog/[slug]", true, map[string]string{"slug": "hi"}},
		{"/blog", "blog/[slug]", false, nil},
		{"/docs/a/b", "docs/[...rest]", true, map[string]string{"rest": "a/b"}},
		{"/docs", "docs/[[...rest]]", true, map[string]string{"rest": ""}},
		{"/other/x", "blog", false, nil},
	}
	for _, tc := range tests {
		params, ok := matchRoutePrefix(tc.url, tc.pattern)
		if ok != tc.ok || (ok && !reflect.DeepEqual(params, tc.params)) {
			t.Errorf("matchRoutePrefix(%q, %q) = %v, %v; want %v, %v", tc.url, tc.pattern, params, ok, tc.params, tc.ok)
		}
	}
}

func TestMatchRouteMissDoesNotAllocate(t *testing.T) {
	matchRoutePattern("/a/b", "x/[id]") // warm the pattern cache
	n := testing.AllocsPerRun(100, func() {
		matchRoutePattern("/a/b", "x/[id]")
	})
	if n != 0 {
		t.Errorf("a miss allocated %v times", n)
	}
}

func TestFindPageForRouteReloadAndParams(t *testing.T) {
	t.Parallel()
	s := &SSRServer{manifest: &ServerManifest{Pages: []ManifestPage{
		{Route: "[...all]", Source: "all.tsx"},
		{Route: "blog/[slug]", Source: "post.tsx"},
		{Route: "blog/hello", Source: "post.tsx"},
		{Route: "blog/seg", Source: "seg.tsx", Segment: true},
	}}}
	p, params := s.FindPageForRoute("/blog/hello")
	if p == nil || p.Route != "blog/hello" || params["slug"] != "hello" {
		t.Fatalf("pre-generated variant: %+v %v", p, params)
	}
	if p, _ := s.FindPageForRoute("/blog/seg"); p == nil || p.Route != "blog/[slug]" {
		t.Fatalf("segment pages must not match: %+v", p)
	}
	// A replaced manifest (dev reload) is picked up.
	s.manifest = &ServerManifest{Pages: []ManifestPage{{Route: "only", Source: "o.tsx"}}}
	if p, _ := s.FindPageForRoute("/blog/hello"); p != nil {
		t.Fatalf("stale index used: %+v", p)
	}
	if p, _ := s.FindPageForRoute("/only"); p == nil {
		t.Fatal("new manifest not matched")
	}
}

func TestCompareRouteSpecificityOrder(t *testing.T) {
	// Each list is most specific first.
	for _, order := range [][]string{
		{"about", "[slug]", "[...rest]", "[[...opt]]"},
		{"blog/new", "blog/[id]", "blog/[...rest]"},
		{"[a]/b", "[a]/[b]"},
		{"shop", "shop/[[...filter]]"},
		{"", "[[...filter]]"},
		{"a/b", "a/b/[[...x]]"},
	} {
		for i := 0; i < len(order); i++ {
			for j := i + 1; j < len(order); j++ {
				if compareRouteSpecificity(order[i], order[j]) >= 0 || compareRouteSpecificity(order[j], order[i]) <= 0 {
					t.Errorf("%q must rank before %q", order[i], order[j])
				}
			}
		}
	}
}
