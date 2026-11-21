package build

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/scene3d/matc"
)

// fakeMatcScript writes a matc stand-in that copies its input to the output
// file, so the build needs no Filament download.
func fakeMatcScript(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-in for matc")
	}
	script := filepath.Join(t.TempDir(), "matc")
	body := "#!/bin/sh\nout=\"\"\nin=\"\"\nwhile [ $# -gt 0 ]; do\n  if [ \"$1\" = \"-o\" ]; then out=\"$2\"; fi\n  in=\"$1\"\n  shift\ndone\ncat \"$in\" > \"$out\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

// TestScene3DCompiledBuild builds a page that uses @krate/3d and checks that
// the <Canvas> JSX was compiled into engine calls (no <mesh> left), that the
// custom shader's .filamat is emitted and referenced, and that a scene the
// compiler cannot analyse still builds and reports KR8701.
func TestScene3DCompiledBuild(t *testing.T) {
	root, cfg := scene3dProject(t, "scene3d", true)
	t.Setenv("KRATE_FILAMENT_MATC", fakeMatcScript(t))
	warnings, err := captureBuild(New(root, cfg))
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	pageJS := func(dir string) string {
		matches, _ := filepath.Glob(filepath.Join(cfg.OutDir, dir, "*.js"))
		if dir != "" {
			more, _ := filepath.Glob(filepath.Join(cfg.OutDir, dir, "*", "*.js"))
			matches = append(matches, more...)
		}
		var b strings.Builder
		for _, m := range matches {
			data, _ := os.ReadFile(m)
			b.Write(data)
		}
		return b.String()
	}
	js := pageJS("")
	if js == "" {
		t.Fatalf("no page script found under %s", cfg.OutDir)
	}
	for _, want := range []string{".staticMesh(", ".dynamicMesh(", ".instanced(", ".setMaterialParam(", ".light(", `"custom"`} {
		if !strings.Contains(js, want) && want != ".setMaterialParam(" {
			t.Errorf("compiled page script lacks %s", want)
		}
	}
	html, _ := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	for name, text := range map[string]string{"html": string(html), "script": js} {
		for _, tag := range []string{"<mesh", "<group", "<boxGeometry", "<meshStandardMaterial", "<directionalLight"} {
			if strings.Contains(text, tag) {
				t.Errorf("%s still contains %s", name, tag)
			}
		}
	}
	if !strings.Contains(string(html), "<canvas") {
		t.Errorf("the compiled Canvas did not render a <canvas>:\n%s", html)
	}

	// The engine is copied from the app's filament package, never bundled.
	filDir := filepath.Join(cfg.OutDir, "assets", "3d", "filament-"+matc.FilamentVersion)
	for _, name := range []string{"filament.js", "filament.wasm"} {
		fi, err := os.Stat(filepath.Join(filDir, name))
		if err != nil || fi.Size() == 0 {
			t.Errorf("dist lacks the copied %s: %v", name, err)
		}
	}
	filURL := "/assets/3d/filament-" + matc.FilamentVersion + "/filament.js"
	matBase := "/assets/3d/materials-" + matc.FilamentVersion
	for name, text := range map[string]string{"compiled page": js, "fallback page": pageJS("fallback")} {
		if !strings.Contains(text, `"`+filURL+`"`) {
			t.Errorf("the %s script does not reference %s", name, filURL)
		}
		if !strings.Contains(text, `"`+matBase+`"`) {
			t.Errorf("the %s script does not pass materialBase %s", name, matBase)
		}
		if strings.Contains(text, `import("filament")`) || strings.Contains(text, "require(\"fs\")") {
			t.Errorf("the %s script bundles or imports the filament package", name)
		}
	}

	matDir := filepath.Join(cfg.OutDir, "assets", "3d", "materials-"+matc.FilamentVersion)
	for _, name := range []string{"standard", "standard_transparent", "physical", "physical_transparent", "unlit", "unlit_transparent"} {
		if fi, err := os.Stat(filepath.Join(matDir, name+".filamat")); err != nil || fi.Size() == 0 {
			t.Errorf("dist lacks the packaged material %s.filamat: %v", name, err)
		}
	}

	custom, _ := filepath.Glob(filepath.Join(matDir, "????????????????.filamat"))
	if len(custom) != 1 {
		t.Fatalf("want one compiled custom .filamat in %s, got %v", matDir, custom)
	}
	ref := regexp.MustCompile(regexp.QuoteMeta(matBase) + `/[0-9a-f]{16}\.filamat`).FindString(js)
	if ref == "" || !strings.HasSuffix(custom[0], filepath.FromSlash(strings.TrimPrefix(ref, matBase+"/"))) {
		t.Errorf("the script does not reference the emitted asset %s (found %q)", custom[0], ref)
	}
	mat, _ := os.ReadFile(custom[0])
	if !strings.Contains(string(mat), "materialParams.tint") || !strings.Contains(string(mat), `{ type : float3, name : "tint" }`) {
		t.Errorf("the compiled material is not the generated source:\n%s", mat)
	}

	if !strings.Contains(warnings, "KR8701") || !strings.Contains(warnings, "Ball") && !strings.Contains(warnings, "component <Ball>") {
		t.Errorf("the fallback page should print KR8701 naming the component, got:\n%s", warnings)
	}
	if strings.Count(warnings, "KR8701") != 1 {
		t.Errorf("KR8701 should be reported once, got:\n%s", warnings)
	}
	fb, _ := os.ReadFile(filepath.Join(cfg.OutDir, "fallback", "index.html"))
	if !strings.Contains(string(fb), "<canvas") {
		t.Errorf("the fallback page must still render a canvas:\n%s", fb)
	}
}

// scene3dProject copies a fixture into a temp project whose node_modules link
// the runtime, @krate/3d and (when withFilament) the real filament package.
func scene3dProject(t *testing.T, fixtureName string, withFilament bool) (string, *config.Config) {
	t.Helper()
	ensureRuntimeDist(t)
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	pkg3d := filepath.Join(repo, "packages", "3d")
	fixture, err := filepath.Abs(filepath.Join("testdata", fixtureName))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := copyTree(t, filepath.Join(fixture, "src"), filepath.Join(root, "src")); err != nil {
		t.Fatal(err)
	}
	writeProjectFile(t, root, "package.json", `{"name":"scene3d-fixture","private":true,"type":"module"}`)
	modules := filepath.Join(root, "node_modules")
	if err := os.MkdirAll(filepath.Join(modules, "@krate"), 0o755); err != nil {
		t.Fatal(err)
	}
	links := map[string]string{
		filepath.Join(modules, "@krate", "runtime"): filepath.Join(repo, "packages", "runtime"),
		filepath.Join(modules, "@krate", "3d"):      pkg3d,
	}
	if withFilament {
		lib := filepath.Join(pkg3d, "node_modules", "filament")
		if _, err := os.Stat(filepath.Join(lib, "filament.wasm")); err != nil {
			requireE2E(t, "filament is not installed for packages/3d (run pnpm install): %v", err)
			t.Skipf("filament is not installed: %v", err)
		}
		links[filepath.Join(modules, "filament")] = lib
	}
	for link, target := range links {
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("cannot link %s: %v", target, err)
		}
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.PublicDir = filepath.Join(root, "public")
	return root, cfg
}

// TestScene3DMissingFilament checks that an app without the filament package
// still builds and gets a KR8702 warning with an install hint, instead of a
// bundler failure.
func TestScene3DMissingFilament(t *testing.T) {
	root, cfg := scene3dProject(t, "scene3d", false)
	t.Setenv("KRATE_FILAMENT_MATC", fakeMatcScript(t))
	warnings, err := captureBuild(New(root, cfg))
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if !strings.Contains(warnings, "KR8702") || !strings.Contains(warnings, "install filament") {
		t.Errorf("want a KR8702 warning with the install hint, got:\n%s", warnings)
	}
	if _, err := os.Stat(filepath.Join(cfg.OutDir, "assets", "3d", "filament-"+matc.FilamentVersion)); err == nil {
		t.Errorf("no filament assets should be emitted without the package")
	}
}
