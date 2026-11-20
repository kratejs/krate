package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestBrowserThreeIsland builds a server-rendered page whose "use client"
// viewer uses @krate/three and @krate/three/drei (as `krate migrate nextjs`
// writes it from @react-three/fiber and drei). The viewer is a Preact island,
// so the build resolves @krate/three to its island build: the scene runs on
// Preact next to a React package component, and no real-React chunk is
// emitted. In a browser with WebGL the model loads, picking, orbiting,
// zooming and pausing work, and a scene error reaches the error boundary.
func TestBrowserThreeIsland(t *testing.T) {
	t.Parallel()
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run browser checks")
	}
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "packages", "runtime", "dist", "krate-runtime.js")); err != nil {
		requireE2E(t, "the runtime package is not built: %v", err)
	}
	threePkg := filepath.Join(repo, "packages", "three")
	threeLib := filepath.Join(threePkg, "node_modules", "three")
	if _, err := os.Stat(filepath.Join(threeLib, "package.json")); err != nil {
		requireE2E(t, "three is not installed for packages/three (run pnpm install): %v", err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "browser_three_island.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := filepath.Abs(filepath.Join("testdata", "three-island"))
	if err != nil {
		t.Fatal(err)
	}
	glb, err := os.ReadFile(filepath.Join("testdata", "three", "public", "triangle.glb"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	e2eEnv(t)
	if err := copyTree(t, filepath.Join(fixture, "src"), filepath.Join(root, "src")); err != nil {
		t.Fatal(err)
	}
	writeProjectFile(t, root, "public/triangle.glb", string(glb))
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "@krate"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	for link, target := range map[string]string{
		filepath.Join(root, "node_modules", "@krate", "three"): threePkg,
		filepath.Join(root, "node_modules", "three"):           threeLib,
	} {
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("cannot link %s: %v", target, err)
		}
	}
	if out, err := captureBuild(New(root, cfg)); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}

	// The island-kind decision: @krate/three is not a reconciler library, so
	// nothing is bundled with React (no isr-* chunk, no react-dom), and the
	// scene shares the island chunks.
	var reactChunks []string
	reactDOM := regexp.MustCompile(`react-dom|__SECRET_INTERNALS|__CLIENT_INTERNALS_DO_NOT_USE`)
	_ = filepath.Walk(cfg.OutDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".js") {
			return nil
		}
		name := filepath.Base(path)
		if strings.HasPrefix(name, "preact-") {
			return nil // preact/compat carries React's internals names as a shim
		}
		data, _ := os.ReadFile(path)
		if strings.HasPrefix(name, "isr-") || reactDOM.Match(data) {
			reactChunks = append(reactChunks, filepath.Base(path))
		}
		return nil
	})
	if len(reactChunks) > 0 {
		t.Fatalf("the @krate/three island shipped React: %v", reactChunks)
	}

	if !canStopInProcessServer {
		t.Skip("cannot stop an in-process server on this platform")
	}
	base, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	out, err := exec.Command("node", script, browser, base).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser checks failed: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}
