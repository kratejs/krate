package gohost

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseRouteMapsFilesToPaths(t *testing.T) {
	dir := filepath.Join("proj", "src", "api")
	handler := []byte("package api\nfunc GET() {}\n")
	cases := []struct{ file, path string }{
		{"hello.go", "/api/hello"},
		{"index.go", "/api"},
		{"users/index.go", "/api/users"},
		{"users/[id].go", "/api/users/{id}"},
		{"users/[id]/posts/[postId].go", "/api/users/{id}/posts/{postId}"},
		{"a/b/c.go", "/api/a/b/c"},
	}
	for _, tc := range cases {
		r, ok, err := ParseRoute(dir, filepath.Join(dir, filepath.FromSlash(tc.file)), handler)
		if err != nil || !ok {
			t.Fatalf("%s: ok=%v err=%v", tc.file, ok, err)
		}
		if r.Path != tc.path {
			t.Errorf("%s: path = %q, want %q", tc.file, r.Path, tc.path)
		}
		if !reflect.DeepEqual(r.Methods, []string{"GET"}) {
			t.Errorf("%s: methods = %v", tc.file, r.Methods)
		}
	}
	// A helper file defines no handler and is not a route.
	if _, ok, err := ParseRoute(dir, filepath.Join(dir, "util.go"), []byte("package api\nfunc helper() {}\n")); ok || err != nil {
		t.Errorf("helper file: ok=%v err=%v", ok, err)
	}
}
