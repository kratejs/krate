package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMatchRedirect(t *testing.T) {
	tests := []struct {
		path   string
		source string
		want   bool
	}{
		// Exact matches
		{"/old", "/old", true},
		{"/old", "/old/", false},
		{"/old/page", "/old", false},
		{"/", "/", true},
		{"", "/old", false},

		// Wildcard suffix
		{"/old/page", "/old/*", true},
		{"/old", "/old/*", true},
		{"/old/deep/nested", "/old/*", true},
		{"/other/page", "/old/*", false},
		{"/oldpage", "/old/*", false}, // no slash boundary
		{"/old/", "/old/*", true},
		{"/old//double", "/old/*", true},

		// Different paths
		{"/a", "/b", false},
		{"/a/b", "/a/c", false},
	}

	for _, tt := range tests {
		t.Run(tt.path+"_"+tt.source, func(t *testing.T) {
			got := matchRedirect(tt.path, tt.source)
			if got != tt.want {
				t.Errorf("matchRedirect(%q, %q) = %v, want %v", tt.path, tt.source, got, tt.want)
			}
		})
	}
}

func TestRewriteDestination(t *testing.T) {
	tests := []struct {
		path        string
		source      string
		destination string
		want        string
	}{
		// Non-wildcard - simple passthrough
		{"/old", "/old", "/new", "/new"},
		{"/old/page", "/old", "/new", "/new"},

		// Wildcard with :splat replacement
		{"/legacy/docs/intro", "/legacy/*", "/docs/:splat", "/docs//docs/intro"},
		{"/legacy/page", "/legacy/*", "/new/:splat", "/new//page"},
		{"/legacy/", "/legacy/*", "/v2/:splat", "/v2//"},

		// Wildcard append (no :splat)
		{"/old/page", "/old/*", "/new", "/new/page"},
		{"/old/deep/path", "/old/*", "/v2", "/v2/deep/path"},
		{"/old/", "/old/*", "/v2", "/v2/"},
	}

	for _, tt := range tests {
		got := rewriteDestination(tt.path, tt.source, tt.destination)
		if got != tt.want {
			t.Errorf("rewriteDestination(%q, %q, %q) = %q, want %q", tt.path, tt.source, tt.destination, got, tt.want)
		}
	}
}

func TestMatchRoute(t *testing.T) {
	tests := []struct {
		urlPath    string
		pattern    string
		wantParams map[string]string
		wantMatch  bool
	}{
		// Exact matches
		{"/", "/", nil, true},
		{"/about", "/about", nil, true},
		{"/about/page", "/about", nil, false}, // different segment count
		{"/about", "/about/page", nil, false},

		// Dynamic segments
		{"/video/abc123", "/video/[id]", map[string]string{"id": "abc123"}, true},
		{"/video/xyz-789-test", "/video/[id]", map[string]string{"id": "xyz-789-test"}, true},
		{"/user/john/posts/42", "/user/[username]/posts/[postId]", map[string]string{"username": "john", "postId": "42"}, true},

		// Multiple params
		{"/blog/2024/hello-world", "/blog/[year]/[slug]", map[string]string{"year": "2024", "slug": "hello-world"}, true},

		// Segment count mismatch
		{"/video", "/video/[id]", nil, false},
		{"/video/a/b", "/video/[id]", nil, false},

		// No params
		{"/api/health", "/api/health", nil, true},
		{"/api/users", "/api/health", nil, false},

		// Empty path
		{"/", "/", nil, true},
	}

	for _, tt := range tests {
		gotParams, gotMatch := matchRoute(tt.urlPath, tt.pattern)
		if gotMatch != tt.wantMatch {
			t.Errorf("matchRoute(%q, %q) match = %v, want %v", tt.urlPath, tt.pattern, gotMatch, tt.wantMatch)
			continue
		}
		if !gotMatch {
			continue
		}
		if tt.wantParams == nil {
			continue
		}
		if len(gotParams) != len(tt.wantParams) {
			t.Errorf("matchRoute(%q, %q) params count = %d, want %d", tt.urlPath, tt.pattern, len(gotParams), len(tt.wantParams))
			continue
		}
		for k, v := range tt.wantParams {
			if gotParams[k] != v {
				t.Errorf("matchRoute(%q, %q) params[%q] = %q, want %q", tt.urlPath, tt.pattern, k, gotParams[k], v)
			}
		}
	}
}

func TestMatchDynamicRoute(t *testing.T) {
	tests := []struct {
		urlPath   string
		pattern   string
		wantMatch bool
		wantID    string
	}{
		{"/video/abc123", "video/[id]", true, "abc123"},
		{"/video/hello-world", "video/[id]", true, "hello-world"},
		{"/user/john/posts/42", "user/[username]/posts/[postId]", true, ""},
		{"/video", "video/[id]", false, ""},
		{"/video/a/b", "video/[id]", false, ""},
		{"/other/abc", "video/[id]", false, ""},
	}

	for _, tt := range tests {
		params, ok := matchDynamicRoute(tt.urlPath, tt.pattern)
		if ok != tt.wantMatch {
			t.Errorf("matchDynamicRoute(%q, %q) = %v, want %v", tt.urlPath, tt.pattern, ok, tt.wantMatch)
			continue
		}
		if ok && tt.wantID != "" {
			if params["id"] != tt.wantID {
				t.Errorf("matchDynamicRoute(%q, %q) params[id] = %q, want %q", tt.urlPath, tt.pattern, params["id"], tt.wantID)
			}
		}
	}
}

func TestFindDynamicRoutes(t *testing.T) {
	dir := t.TempDir()
	// Create static page directory
	_ = os.MkdirAll(filepath.Join(dir, "about"), 0755)
	_ = os.WriteFile(filepath.Join(dir, "about", "index.html"), []byte("<html>about</html>"), 0644)

	// Create dynamic route directory
	dynDir := filepath.Join(dir, "video", "[id]")
	_ = os.MkdirAll(dynDir, 0755)
	_ = os.WriteFile(filepath.Join(dynDir, "index.html"), []byte("<html>video</html>"), 0644)

	// Create another dynamic route
	dynDir2 := filepath.Join(dir, "user", "[username]", "posts", "[postId]")
	_ = os.MkdirAll(dynDir2, 0755)
	_ = os.WriteFile(filepath.Join(dynDir2, "index.html"), []byte("<html>post</html>"), 0644)

	routes := findDynamicRoutes(dir)
	// Only dirs with index.html: video/[id] and user/[username]/posts/[postId]
	// (user/[username] has no index.html so it's skipped)
	if len(routes) != 2 {
		t.Fatalf("expected 2 dynamic route directories, got %d", len(routes))
	}

	routeMap := make(map[string]string)
	for _, r := range routes {
		routeMap[r.pattern] = r.dir
	}

	if _, ok := routeMap["video/[id]"]; !ok {
		t.Error("missing dynamic route video/[id]")
	}
	if _, ok := routeMap["user/[username]/posts/[postId]"]; !ok {
		t.Error("missing dynamic route user/[username]/posts/[postId]")
	}
}

func TestStaticRouteExists(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "items", "alpha"), 0755)
	_ = os.WriteFile(filepath.Join(dir, "items", "alpha", "index.html"), []byte("<html>static alpha</html>"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>home</html>"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "favicon.ico"), []byte("x"), 0644)

	cases := []struct {
		path string
		want bool
	}{
		{"/items/alpha", true},
		{"/items/alpha/", true},
		{"/items/beta", false},
		{"/", true},
		{"/favicon.ico", true},
		{"/missing.html", false},
	}
	for _, tc := range cases {
		if got := staticRouteExists(dir, tc.path); got != tc.want {
			t.Errorf("staticRouteExists(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestShouldServeStatic verifies the ssrPageHandler routing decision: a static
// file wins only for SSG pages / unmatched routes. A manifest-registered
// ssr/isr/streaming page must ALWAYS go through the sidecar even when a
// pre-generated (baked) file exists - regression for pre-generated ISR variants
// and runtime-component regions being frozen at their build timestamp.
func TestShouldServeStatic(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "cached", "known"), 0755)
	_ = os.WriteFile(filepath.Join(dir, "cached", "known", "index.html"), []byte("<html>baked</html>"), 0644)
	_ = os.MkdirAll(filepath.Join(dir, "items", "alpha"), 0755)
	_ = os.WriteFile(filepath.Join(dir, "items", "alpha", "index.html"), []byte("<html>alpha</html>"), 0644)

	mkPage := func(mode string) *ManifestPage {
		return &ManifestPage{Mode: mode, Route: "/cached/[id]"}
	}

	cases := []struct {
		name string
		path string
		page *ManifestPage
		want bool
	}{
		// SSG page with a concrete static file -> static wins (beats sibling
		// dynamic [param] template).
		{"ssg static file present", "/items/alpha", &ManifestPage{Mode: "ssg"}, true},
		// SSG page with NO static file -> not static (falls to dynamic template).
		{"ssg no static file", "/items/beta", &ManifestPage{Mode: "ssg"}, false},
		// Pre-generated ISR variant: static file EXISTS but page is ISR -> must
		// NOT be served statically (goes to the sidecar for revalidation).
		{"isr pre-generated variant", "/cached/known", mkPage("isr"), false},
		{"ssr page with baked shell", "/cached/known", mkPage("ssr"), false},
		{"streaming page with baked shell", "/cached/known", mkPage("streaming"), false},
		// Unmatched route with a static file -> static.
		{"unknown route static", "/cached/known", nil, true},
	}
	for _, tc := range cases {
		if got := shouldServeStatic(dir, tc.path, tc.page); got != tc.want {
			t.Errorf("%s: shouldServeStatic(%q) = %v, want %v", tc.name, tc.path, got, tc.want)
		}
	}
}

func TestStaticBeatsDynamicRoute(t *testing.T) {
	dir := t.TempDir()
	static := filepath.Join(dir, "items", "alpha")
	_ = os.MkdirAll(static, 0755)
	_ = os.WriteFile(filepath.Join(static, "index.html"), []byte("<html>STATIC ALPHA</html>"), 0644)
	dyn := filepath.Join(dir, "items", "[id]")
	_ = os.MkdirAll(dyn, 0755)
	_ = os.WriteFile(filepath.Join(dyn, "index.html"), []byte("<html>DYNAMIC TEMPLATE</html>"), 0644)

	routes := findDynamicRoutes(dir)
	var pattern string
	for _, r := range routes {
		if r.pattern == "items/[id]" {
			pattern = r.pattern
		}
	}
	if pattern == "" {
		t.Fatal("expected dynamic route items/[id]")
	}

	// Mirror the handler: the dynamic branch runs only when no static file
	// exists. For /items/alpha the static gate must be closed so the
	// handlerMux skips the items/[id] template.
	if staticRouteExists(dir, "/items/alpha") != true {
		t.Error("static /items/alpha must exist so the handler skips the dynamic template")
	}
	// The pattern still matches the URL - precedence comes from the gate above.
	if _, ok := matchDynamicRoute("/items/alpha", pattern); !ok {
		t.Error("items/[id] pattern should still match /items/alpha; the static gate is the precedence mechanism")
	}
}

func TestFindDynamicRoutesEmpty(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "about"), 0755)
	_ = os.WriteFile(filepath.Join(dir, "about", "index.html"), []byte("<html>about</html>"), 0644)

	routes := findDynamicRoutes(dir)
	if len(routes) != 0 {
		t.Errorf("expected 0 dynamic routes, got %d", len(routes))
	}
}

func TestApplyDynamicRouteParams(t *testing.T) {
	tmpl := "<html><head><title>Video: " + dynamicParamSentinel("id") + "</title>" +
		"<meta name=description content=\"Watch " + dynamicParamSentinel("id") + "\"></head>" +
		"<body><p>Video ID: <strong>" + dynamicParamSentinel("id") + "</strong></p></body></html>"

	got := applyDynamicRouteParams(tmpl, map[string]string{"id": "abc123"})

	if strings.Contains(got, dynamicParamSentinel("id")) {
		t.Errorf("sentinel not substituted: %s", got)
	}
	// Substituted in text, title, and attribute positions.
	if !strings.Contains(got, "<title>Video: abc123</title>") {
		t.Errorf("title not substituted: %s", got)
	}
	if !strings.Contains(got, "content=\"Watch abc123\"") {
		t.Errorf("attribute not substituted: %s", got)
	}
	if !strings.Contains(got, "<strong>abc123</strong>") {
		t.Errorf("body text not substituted: %s", got)
	}
	// Params script injected before </head>.
	if !strings.Contains(got, `__KRATE_PARAMS__={"id":"abc123"}</script></head>`) {
		t.Errorf("params script not injected: %s", got)
	}
}

func TestApplyDynamicRouteParamsEscapes(t *testing.T) {
	tmpl := "<p>" + dynamicParamSentinel("id") + "</p>"
	got := applyDynamicRouteParams(tmpl, map[string]string{"id": `<script>&"`})
	if strings.Contains(got, "<script>&\"") {
		t.Errorf("param value not HTML-escaped: %s", got)
	}
	if !strings.Contains(got, "&lt;script&gt;&amp;") {
		t.Errorf("expected escaped param value in output: %s", got)
	}
}
