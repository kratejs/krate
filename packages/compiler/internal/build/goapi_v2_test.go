package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestSplitGoMod(t *testing.T) {
	content := "module example.com/app\n\ngo 1.23\n\nrequire (\n\tgithub.com/x/y v1.2.3\n)\n\nreplace github.com/a/b => ../b\n"
	ver, directives := splitGoMod(content)
	if ver != "1.23" {
		t.Fatalf("version = %q, want 1.23", ver)
	}
	if strings.Contains(directives, "module ") || strings.Contains(directives, "go 1.23") {
		t.Fatalf("module/go clauses leaked into directives:\n%s", directives)
	}
	if !strings.Contains(directives, "github.com/x/y v1.2.3") {
		t.Fatalf("require directive lost:\n%s", directives)
	}
}

func TestRebaseGoModReplacePaths(t *testing.T) {
	in := "require github.com/x/y v1.2.3\nreplace github.com/a/b => ../b\nreplace example.com/c => ./vendor/c\nreplace example.com/abs => /abs/path"
	out := rebaseGoModReplacePaths(in)
	if !strings.Contains(out, "replace github.com/a/b => ../../../b") {
		t.Fatalf("../b not rebased:\n%s", out)
	}
	if !strings.Contains(out, "replace example.com/c => ../../vendor/c") {
		t.Fatalf("./vendor/c not rebased:\n%s", out)
	}
	if !strings.Contains(out, "replace example.com/abs => /abs/path") {
		t.Fatalf("absolute replace path must be untouched:\n%s", out)
	}
}

func TestWriteGoAPIModuleRootMerge(t *testing.T) {
	root := t.TempDir()
	apiSrc := filepath.Join(root, "src", "api")
	if err := os.MkdirAll(apiSrc, 0o755); err != nil {
		t.Fatal(err)
	}
	modDir := filepath.Join(root, ".krate", "goapi")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFileRel(t, root, "go.mod", "module example.com/app\n\ngo 1.23\n\nrequire github.com/x/y v1.2.3\n\nreplace github.com/a/b => ../b\n")
	writeFileRel(t, root, "go.sum", "github.com/x/y v1.2.3 h1:abc\n")

	cfg := config.GoAPICfg{}
	modulePath, err := writeGoAPIModule(modDir, apiSrc, root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if modulePath != "krate-goapi" {
		t.Fatalf("module path = %q, want krate-goapi", modulePath)
	}
	data, err := os.ReadFile(filepath.Join(modDir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"module krate-goapi", "go 1.23", "github.com/x/y v1.2.3", "=> ../../../b"} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated go.mod missing %q:\n%s", want, got)
		}
	}
	if _, err := os.Stat(filepath.Join(modDir, "go.sum")); err != nil {
		t.Fatalf("go.sum not copied: %v", err)
	}
}

func TestGoAPISiblingPackageAndCache(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	root := t.TempDir()
	writeFileRel(t, root, "src/api/lib/util.go", "package lib\n\nfunc Message() string { return \"hi from lib\" }\n")
	writeFileRel(t, root, "src/api/hello.go",
		"package api\n\nimport (\n\t\"net/http\"\n\n\t\"krate-goapi/routes/lib\"\n\t\"krate-goapi/runtime\"\n)\n\n"+
			"func GET(w http.ResponseWriter, r *http.Request) {\n\truntime.WriteJSON(w, 200, map[string]string{\"msg\": lib.Message()})\n}\n")

	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	b := New(root, cfg)

	if err := b.BuildAllGoAPI(); err != nil {
		t.Fatalf("BuildAllGoAPI: %v", err)
	}
	if _, err := os.Stat(goAPIServerBinPath(root)); err != nil {
		t.Fatalf("sidecar binary missing: %v", err)
	}
	// Second build with unchanged inputs must reuse the cached binary.
	if err := b.BuildAllGoAPI(); err != nil {
		t.Fatalf("second BuildAllGoAPI: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".krate", "cache", "goapi", "key")); err != nil {
		t.Fatalf("binary cache key not written: %v", err)
	}

	manifest, err := os.ReadFile(filepath.Join(root, ".krate", "goapi-routes.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), "/api/hello") {
		t.Fatalf("route missing from manifest: %s", manifest)
	}
}
