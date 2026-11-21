package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// buildThreeCompiled builds the testdata/threecompiled fixture: the page as
// written (the compile-time optimizer on) and a copy with <Canvas
// compile={false}> (every scene on the runtime reconciler). It returns the
// output directory with pages "/" and "/off/".
func buildThreeCompiled(t *testing.T) string {
	t.Helper()
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	runtimeSrc := filepath.Join(repo, "packages", "runtime")
	if _, err := os.Stat(filepath.Join(runtimeSrc, "dist", "krate-runtime.js")); err != nil {
		requireE2E(t, "the runtime package is not built: %v", err)
	}
	threePkg := filepath.Join(repo, "packages", "three")
	threeLib := filepath.Join(threePkg, "node_modules", "three")
	if _, err := os.Stat(filepath.Join(threeLib, "package.json")); err != nil {
		requireE2E(t, "three is not installed for packages/three (run pnpm install): %v", err)
	}
	fixture, err := filepath.Abs(filepath.Join("testdata", "threecompiled"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := copyTree(t, filepath.Join(fixture, "src"), filepath.Join(root, "src")); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(root, "src", "pages", "index.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	off := strings.Replace(string(page), "<Canvas id=\"scene\"", "<Canvas compile={false} id=\"scene\"", 1)
	if off == string(page) {
		t.Fatal("the fixture has no Canvas to switch off")
	}
	writeProjectFile(t, root, "src/pages/off.tsx", off)
	writeProjectFile(t, root, "package.json", `{"name":"three-compiled-fixture","private":true,"type":"module"}`)
	modules := filepath.Join(root, "node_modules")
	if err := os.MkdirAll(filepath.Join(modules, "@krate"), 0755); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{
		filepath.Join(modules, "@krate", "runtime"): runtimeSrc,
		filepath.Join(modules, "@krate", "three"):   threePkg,
		filepath.Join(modules, "three"):             threeLib,
	} {
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("cannot link %s: %v", target, err)
		}
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.PublicDir = filepath.Join(root, "public")
	cfg.Minify = false
	if _, err := captureBuild(New(root, cfg)); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	return cfg.OutDir
}

// pageScripts returns the page scripts of the compiled page ("") or of the
// compile={false} page ("off"), without shared chunks.
func pageScripts(t *testing.T, out, page string) string {
	t.Helper()
	var all strings.Builder
	err := filepath.Walk(out, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".js") {
			return err
		}
		rel, _ := filepath.Rel(out, filepath.ToSlash(p))
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "chunks/") || strings.HasPrefix(rel, "off/") != (page == "off") {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		all.Write(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return all.String()
}

// TestThreeCompiledBuild checks what the compile-time optimizer emits for the
// fixture scene, and that compile={false} leaves the scene on the reconciler.
func TestThreeCompiledBuild(t *testing.T) {
	t.Parallel()
	out := buildThreeCompiled(t)
	on := pageScripts(t, out, "")
	off := pageScripts(t, out, "off")
	for _, want := range []string{
		"h(K3CompiledScene,",    // one compiled element for the static part
		"__t.instances(",        // the grid boxes and the two spheres: InstancedMesh
		"__t.dynamicInstances(", // the ring of reactive meshes
		"__t.fx(",               // reactive transforms are direct effects
		"new Float32Array([",    // instance matrices computed at compile time
		"compileAsync",          // the runtime warms the shaders up
		"matrixWorldAutoUpdate",
		"matrixAutoUpdate",
	} {
		if !strings.Contains(on, want) {
			t.Errorf("compiled page has no %q", want)
		}
	}
	// Meshes with a ref (and the lights and Probe beside them) stay on the
	// reconciler path; compiled ones do not appear as h('mesh', ...) elements.
	if !strings.Contains(on, "name:'byref'") {
		t.Error("the mesh with a ref was compiled")
	}
	for _, gone := range []string{"krateList(()=>(GRID)", "krateList(()=>(RING)", "h('mesh',{name:'inner'", "h('mesh',{name:'arm'", "h('group',{name:'spinner'"} {
		if strings.Contains(on, gone) {
			t.Errorf("compiled page still builds %q through the reconciler", gone)
		}
	}
	// compile={false}: nothing is compiled, the whole scene is h() elements.
	for _, gone := range []string{"h(K3CompiledScene", "__t.instances(", "__t.dynamicInstances("} {
		if strings.Contains(off, gone) {
			t.Errorf("compile={false} page still has %q", gone)
		}
	}
	for _, want := range []string{"krateList(()=>(GRID)", "krateList(()=>(RING)", "h('mesh',{name:'inner'"} {
		if !strings.Contains(off, want) {
			t.Errorf("compile={false} page has no %q", want)
		}
	}
}

// TestBrowserThreeCompiled drives the optimized scene and its compile={false}
// twin in a browser with WebGL: pixels, events, reactive writes, instancing and
// draw calls, useFrame, and the same picture from both paths.
func TestBrowserThreeCompiled(t *testing.T) {
	t.Parallel()
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run browser checks")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	e2eEnv(t)
	script, err := filepath.Abs(filepath.Join("testdata", "browser_three_compiled.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	out := buildThreeCompiled(t)
	res, err := exec.Command("node", script, out, browser).CombinedOutput()
	if err != nil {
		if strings.Contains(string(res), "Executable doesn't exist") || strings.Contains(string(res), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, res)
		}
		t.Fatalf("browser checks failed: %v\n%s", err, res)
	}
	t.Logf("%s", res)
}
