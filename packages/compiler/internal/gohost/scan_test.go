package gohost

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRouteMethodsPrefersHandlerAndKeepsRegistrationOrder(t *testing.T) {
	src := func(body string) []byte { return []byte("package api\n" + body) }
	cases := []struct {
		name string
		src  []byte
		want []string
	}{
		{"handler wins over method functions", src("func Handler() {}\nfunc GET() {}\n"), []string{"Handler"}},
		{"methods in registration order, not source order", src("func POST() {}\nfunc DELETE() {}\nfunc GET() {}\nfunc HEAD() {}\nfunc PATCH() {}\nfunc PUT() {}\nfunc OPTIONS() {}\n"),
			[]string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"}},
		{"a method on a receiver is not a handler", src("type T struct{}\nfunc (T) GET() {}\nfunc (T) Handler() {}\n"), nil},
		{"lowercase and unknown names are helpers", src("func get() {}\nfunc Get() {}\nfunc TRACE() {}\n"), nil},
		{"a source that does not parse yields nothing", []byte("package api\nfunc GET( {"), nil},
	}
	for _, tc := range cases {
		if got := RouteMethods(tc.src); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: RouteMethods = %v, want %v", tc.name, got, tc.want)
		}
	}
}

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

func TestPathMatchingIsPerSegment(t *testing.T) {
	routes := []RouteEntry{{Method: "GET", Path: "/api/users/{id}"}, {Method: "POST", Path: "/api/users/{id}/posts"}}
	for _, tc := range []struct {
		method, path string
		want         bool
	}{
		{"GET", "/api/users/7", true},
		{"GET", "/api/users/7/", true},
		{"get", "/api/users/7", true},
		{"GET", "/api/users", false},
		{"GET", "/api/users/7/extra", false},
		{"GET", "/api/users/7/posts", false},
		{"POST", "/api/users/7/posts", true},
		{"POST", "/api/users/7", false},
		{"HEAD", "/api/users/7/posts", false},
		{"HEAD", "/api/users/7", true},
	} {
		if got := RouteMatches(tc.method, tc.path, routes); got != tc.want {
			t.Errorf("RouteMatches(%s, %s) = %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestAllowedMethodsOrderIsStable(t *testing.T) {
	routes := []RouteEntry{
		{Method: "DELETE", Path: "/api/x"}, {Method: "post", Path: "/api/x"}, {Method: "GET", Path: "/api/x"}, {Method: "TRACE", Path: "/api/x"},
	}
	got, ok := AllowedMethods("/api/x", routes)
	want := []string{"GET", "HEAD", "POST", "DELETE", "OPTIONS", "TRACE"}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("AllowedMethods = %v, want %v", got, want)
	}
}

func TestScanRouteFilesSkipsHelpersAndSorts(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"b.go", "a.go", "_helper.go", "notes.md", "users/[id].go", "users/_shared.go", "z_test.go"} {
		p := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("package api\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ScanRouteFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	var rel []string
	for _, f := range got {
		r, _ := filepath.Rel(dir, f)
		rel = append(rel, filepath.ToSlash(r))
	}
	// _test.go files are Go sources like any other here; only the underscore
	// prefix and the extension filter.
	want := []string{"a.go", "b.go", "users/[id].go", "z_test.go"}
	if !reflect.DeepEqual(rel, want) {
		t.Errorf("ScanRouteFiles = %v, want %v", rel, want)
	}
	if _, err := ScanRouteFiles(filepath.Join(dir, "missing")); err == nil {
		t.Error("a missing directory must be an error")
	}
}

func TestGoFilePackage(t *testing.T) {
	for src, want := range map[string]string{
		"package api\n":                        "api",
		"// comment\n\n  package   my_pkg2 \n": "my_pkg2",
		"package main // trailing":             "main",
		"func f() {}":                          "",
	} {
		if got := GoFilePackage([]byte(src)); got != want {
			t.Errorf("GoFilePackage(%q) = %q, want %q", src, got, want)
		}
	}
}

func TestSanitizeNames(t *testing.T) {
	for in, want := range map[string]string{"": "_", "ok_Name1": "ok_Name1", "my-comp.v2": "my_comp_v2", "a b/c": "a_b_c", "[id]": "_id_"} {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"": "", ".": "", "ui": "ui", "ui/my-card": "ui/my_card", "a-b/c.d/e": "a_b/c_d/e"} {
		if got := SanitizeRelDir(in); got != want {
			t.Errorf("SanitizeRelDir(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWriteManifestSortsAndRejectsDuplicates(t *testing.T) {
	root := t.TempDir()
	routes := []Route{
		{Path: "/api/b", Methods: []string{"POST", "GET"}},
		{Path: "/api/a", Methods: []string{"Handler"}},
	}
	if err := WriteManifest(root, routes); err != nil {
		t.Fatal(err)
	}
	got, _, err := LoadManifest(RoutesManifestPath(root))
	if err != nil {
		t.Fatal(err)
	}
	want := []RouteEntry{{Method: "", Path: "/api/a"}, {Method: "GET", Path: "/api/b"}, {Method: "POST", Path: "/api/b"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("manifest = %v, want %v", got, want)
	}
	// users/index.go and users.go both map to /api/users: the build must say so.
	err = WriteManifest(root, []Route{{Path: "/api/users", Methods: []string{"GET"}}, {Path: "/api/users", Methods: []string{"GET"}}})
	if err == nil || !strings.Contains(err.Error(), "duplicate Go API route") {
		t.Errorf("duplicate routes: err = %v", err)
	}
	if _, _, err := LoadManifest(filepath.Join(root, "nope.json")); err == nil {
		t.Error("loading a missing manifest must fail")
	}
}

func TestGoModHelpers(t *testing.T) {
	mod := "module example.com/app\n\ngo 1.22\n\nrequire (\n\tgithub.com/x/y v1.0.0\n)\n\nreplace github.com/x/y => ../y\nreplace github.com/q/r => ./vendor/r\nreplace github.com/s/t => github.com/s/t v1.2.3\n"
	if got := ParseModulePath(mod); got != "example.com/app" {
		t.Errorf("ParseModulePath = %q", got)
	}
	if got := ParseModulePath("// no module line\n"); got != "" {
		t.Errorf("ParseModulePath without a module clause = %q", got)
	}
	version, directives := SplitGoMod(mod)
	if version != "1.22" || strings.Contains(directives, "module ") || strings.Contains(directives, "\ngo ") || !strings.Contains(directives, "require (") {
		t.Errorf("SplitGoMod = %q / %q", version, directives)
	}
	rebased := RebaseReplacePaths(directives)
	for _, want := range []string{"=> ../../../y", "=> ../../vendor/r", "=> github.com/s/t v1.2.3"} {
		if !strings.Contains(rebased, want) {
			t.Errorf("rebased replace lacks %q:\n%s", want, rebased)
		}
	}
	once := EnsureGoextRequire("module m\n")
	if !strings.Contains(once, "require "+GoextModulePath+" v0.0.0") || !strings.Contains(once, "replace "+GoextModulePath+" => ../goext") {
		t.Errorf("EnsureGoextRequire = %q", once)
	}
	if EnsureGoextRequire(once) != once {
		t.Error("EnsureGoextRequire must be idempotent")
	}
}
