package build

import (
	"os"
	"path/filepath"
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
		{"/oldpage", "/old/*", false},    // no slash boundary
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
		// Non-wildcard — simple passthrough
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
		{"/about/page", "/about", nil, false},   // different segment count
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
	os.MkdirAll(filepath.Join(dir, "about"), 0755)
	os.WriteFile(filepath.Join(dir, "about", "index.html"), []byte("<html>about</html>"), 0644)

	// Create dynamic route directory
	dynDir := filepath.Join(dir, "video", "[id]")
	os.MkdirAll(dynDir, 0755)
	os.WriteFile(filepath.Join(dynDir, "index.html"), []byte("<html>video</html>"), 0644)

	// Create another dynamic route
	dynDir2 := filepath.Join(dir, "user", "[username]", "posts", "[postId]")
	os.MkdirAll(dynDir2, 0755)
	os.WriteFile(filepath.Join(dynDir2, "index.html"), []byte("<html>post</html>"), 0644)

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

func TestFindDynamicRoutesEmpty(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "about"), 0755)
	os.WriteFile(filepath.Join(dir, "about", "index.html"), []byte("<html>about</html>"), 0644)

	routes := findDynamicRoutes(dir)
	if len(routes) != 0 {
		t.Errorf("expected 0 dynamic routes, got %d", len(routes))
	}
}
