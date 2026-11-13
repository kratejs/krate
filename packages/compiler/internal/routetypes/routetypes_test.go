package routetypes

import (
	"strings"
	"testing"
)

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
	if !strings.Contains(out, `readonly route: "/video/[id]"; readonly source: "src/pages/video/[id].tsx"; readonly params: readonly ["id"]; readonly mode: "ssr"`) {
		t.Errorf("expected manifest entry, got:\n%s", out)
	}
}
