package build

import (
	"strings"
	"testing"
)

func TestExpandStaticParams(t *testing.T) {
	sets := func(m ...map[string]string) []map[string]string { return m }
	p := func(kv ...string) map[string]string {
		out := map[string]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			out[kv[i]] = kv[i+1]
		}
		return out
	}

	pages, errs := expandStaticParams("/x/blog/[slug].tsx", "blog/[slug].tsx",
		sets(p("slug", "a"), p("slug", "b"), p("slug", "a"), p("slug", "c", "extra", "ok")))
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	var got []string
	for _, pg := range pages {
		got = append(got, pg.OutPath)
	}
	if strings.Join(got, ",") != "blog/a,blog/b,blog/c" {
		t.Errorf("paths = %v, want the unique entries in order", got)
	}

	// Catch-all and optional catch-all values are several segments.
	pages, errs = expandStaticParams("/x/docs/[...path].tsx", "docs/[...path].tsx", sets(p("path", "a/b/c")))
	if len(errs) != 0 || len(pages) != 1 || pages[0].OutPath != "docs/a/b/c" {
		t.Errorf("catch-all: pages=%+v errs=%v", pages, errs)
	}
	pages, errs = expandStaticParams("/x/docs/[[...path]].tsx", "docs/[[...path]].tsx", sets(p("path", "")))
	if len(errs) != 0 || len(pages) != 1 || pages[0].OutPath != "docs" {
		t.Errorf("optional catch-all: pages=%+v errs=%v", pages, errs)
	}

	bad := []struct {
		name, route string
		sets        []map[string]string
		want        string
	}{
		{"missing param", "blog/[slug].tsx", sets(p("slug", "a"), p("id", "x")), `entry #2 {"id":"x"} has no value for [slug]`},
		{"empty value", "blog/[slug].tsx", sets(p("slug", "")), "empty value for [slug]"},
		{"traversal", "blog/[slug].tsx", sets(p("slug", "..")), "not a plain path segment"},
		{"slash in single segment", "blog/[slug].tsx", sets(p("slug", "a/b")), `containing "/"`},
		{"traversal in catch-all", "docs/[...p].tsx", sets(p("p", "a/../../b")), "not a plain path segment"},
		{"backslash", "blog/[slug].tsx", sets(p("slug", `a\b`)), "backslash"},
		{"conflicting entries", "blog/[slug].tsx", sets(p("slug", "a"), p("slug", "a", "lang", "fr")), "same page as entry #1"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			pages, errs := expandStaticParams("/x/"+tc.route, tc.route, tc.sets)
			if len(errs) == 0 {
				t.Fatalf("no error; pages = %+v", pages)
			}
			if !strings.Contains(errs[0], tc.want) || !strings.Contains(errs[0], "KR7142") {
				t.Errorf("error = %q, want it to contain %q and KR7142", errs[0], tc.want)
			}
			for _, pg := range pages {
				if strings.Contains(pg.OutPath, "[") || strings.Contains(pg.OutPath, "..") {
					t.Errorf("unsafe output path %q was planned", pg.OutPath)
				}
			}
		})
	}
}
