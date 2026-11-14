package build

import (
	"testing"
)

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
