package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// A site whose only client code is a @krate/three Canvas must still ship the
// runtime exports the engine reads from the global object (createRoot,
// krateWithOwner, ...), although no page names them.
func TestThreeOnlySiteShipsEngineRuntime(t *testing.T) {
	t.Parallel()
	e2eEnv(t)
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	runtimeSrc := filepath.Join(repo, "packages", "runtime")
	if _, err := os.Stat(filepath.Join(runtimeSrc, "dist", "krate-runtime.js")); err != nil {
		requireE2E(t, "the runtime package is not built: %v", err)
		t.Skip("runtime dist not available")
	}
	threePkg := filepath.Join(repo, "packages", "three")
	threeLib := filepath.Join(threePkg, "node_modules", "three")
	if _, err := os.Stat(filepath.Join(threeLib, "package.json")); err != nil {
		requireE2E(t, "three is not installed for packages/three (run pnpm install): %v", err)
		t.Skip("three not installed")
	}

	root := t.TempDir()
	writeProjectFile(t, root, "package.json", `{"name":"three-only","private":true,"type":"module"}`)
	writeProjectFile(t, root, "src/pages/index.tsx", `import { Canvas } from "@krate/three";

export default function Page() {
  return (
    <Canvas id="scene">
      <ambientLight intensity={1} />
      <mesh><boxGeometry /><meshBasicMaterial color="red" /></mesh>
    </Canvas>
  );
}
`)
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
	cfg.Minify = true
	if _, err := captureBuild(New(root, cfg)); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	var js strings.Builder
	err = filepath.WalkDir(cfg.OutDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(p) != ".js" {
			return err
		}
		data, err := os.ReadFile(p)
		js.Write(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"createRoot:", "krateWithOwner:", "krateSetRef:", "krateUnmount:"} {
		if !strings.Contains(js.String(), name) {
			t.Errorf("the built runtime does not define %s, which the @krate/three engine reads from the global object", strings.TrimSuffix(name, ":"))
		}
	}
}
