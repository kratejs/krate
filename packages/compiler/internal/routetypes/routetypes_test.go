package routetypes

import (
	"strings"
	"testing"
)

func TestParamsExtraction(t *testing.T) {
	cases := []struct {
		pattern string
		want    []string
	}{
		{"/", nil},
		{"/about", nil},
		{"/video/[id]", []string{"id"}},
		{"/user/[username]/posts/[postId]", []string{"username", "postId"}},
		{"/docs/[...slug]", []string{"slug"}},
	}
	for _, c := range cases {
		got := Params(c.pattern)
		if len(got) != len(c.want) {
			t.Errorf("Params(%q) = %v, want %v", c.pattern, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("Params(%q) = %v, want %v", c.pattern, got, c.want)
				break
			}
		}
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"":           "/",
		"about":      "/about",
		"/about/":    "/about",
		"/":          "/",
		"/a/b/":      "/a/b",
		"video/[id]": "/video/[id]",
		".":          "/",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGenerateStaticAndDynamic(t *testing.T) {
	out := Generate([]Route{
		{Pattern: ".", Source: "src/pages/index.tsx"},      // root -> "/"
		{Pattern: "about", Source: "src/pages/about.tsx"},  // -> "/about"
		{Pattern: "about/", Source: "src/pages/about.tsx"}, // duplicate after normalize
		{Pattern: "video/[id]", Source: "src/pages/video/[id].tsx", Mode: "ssr"},
		{Pattern: "user/[username]/posts/[postId]", Source: "src/pages/user/x.tsx"},
	})

	// Static literal union.
	if !strings.Contains(out, `| "/"`) {
		t.Errorf("expected root route literal, got:\n%s", out)
	}
	if !strings.Contains(out, `| "/about"`) {
		t.Errorf("expected static route literal, got:\n%s", out)
	}
	// Duplicate collapsed (one union literal; manifest entry also names it).
	if strings.Count(out, `| "/about"`) != 1 {
		t.Errorf("expected /about deduped, got:\n%s", out)
	}
	// Dynamic template literals.
	if !strings.Contains(out, "`/video/${string}`") {
		t.Errorf("expected dynamic video route, got:\n%s", out)
	}
	if !strings.Contains(out, "`/user/${string}/posts/${string}`") {
		t.Errorf("expected multi-param dynamic route, got:\n%s", out)
	}
	// RouteParams map.
	if !strings.Contains(out, `"/video/[id]": { id: string };`) {
		t.Errorf("expected RouteParams entry, got:\n%s", out)
	}
	if !strings.Contains(out, `"/user/[username]/posts/[postId]": { username: string; postId: string };`) {
		t.Errorf("expected multi-param RouteParams entry, got:\n%s", out)
	}
	// Manifest entry with mode.
	if !strings.Contains(out, `route: "/video/[id]", source: "src/pages/video/[id].tsx", params: ["id"], mode: "ssr"`) {
		t.Errorf("expected manifest entry, got:\n%s", out)
	}
}

func TestGenerateNoDynamic(t *testing.T) {
	out := Generate([]Route{{Pattern: "/about"}})
	if !strings.Contains(out, "export type DynamicRoute =\n  never;") {
		t.Errorf("expected never for DynamicRoute, got:\n%s", out)
	}
	if !strings.Contains(out, "// no dynamic routes") {
		t.Errorf("expected no-dynamic comment, got:\n%s", out)
	}
}

func TestGenerateEmpty(t *testing.T) {
	out := Generate(nil)
	if !strings.Contains(out, "export type StaticRoute =\n  never;") {
		t.Errorf("expected never StaticRoute, got:\n%s", out)
	}
}

func TestGenerateDeterministic(t *testing.T) {
	routes := []Route{{Pattern: "z"}, {Pattern: "a"}, {Pattern: "m"}}
	a := Generate(routes)
	b := Generate([]Route{{Pattern: "m"}, {Pattern: "z"}, {Pattern: "a"}})
	if a != b {
		t.Error("Generate output must be deterministic regardless of input order")
	}
	if !(strings.Index(a, `"/a"`) < strings.Index(a, `"/m"`) && strings.Index(a, `"/m"`) < strings.Index(a, `"/z"`)) {
		t.Errorf("expected sorted routes, got:\n%s", a)
	}
}

func TestEscapeString(t *testing.T) {
	out := Generate([]Route{{Pattern: `/weird"quote`}})
	if !strings.Contains(out, `"/weird\"quote"`) {
		t.Errorf("expected escaped quote, got:\n%s", out)
	}
}

func TestBridge(t *testing.T) {
	out := Bridge("./.krate/types/routes.js", "./.krate/types/content.js")
	for _, want := range []string{
		`import type { ContentTypes } from "./.krate/types/content.js";`,
		`import type { Route, RouteParams } from "./.krate/types/routes.js";`,
		"namespace Krate",
		"interface TypedRoutes",
		"href: Route;",
		"params: RouteParams;",
		"content: ContentTypes;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Bridge missing %q, got:\n%s", want, out)
		}
	}
}

func TestBridgeNoContent(t *testing.T) {
	out := Bridge("./routes.js", "")
	if strings.Contains(out, "ContentTypes") || strings.Contains(out, "content:") {
		t.Errorf("Bridge with no content path must omit content, got:\n%s", out)
	}
}
