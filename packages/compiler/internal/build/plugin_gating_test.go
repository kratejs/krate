package build

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestBuildGatesOnPluginError verifies a failing plugin fails `krate build`
// with a non-zero-exit-able error instead of printing a warning and exiting 0.
// The canonical case: a Go plugin (krate-plugin-demo-go style) whose per-platform
// binary is missing (bin/ is built, not committed) must abort the build, not
// silently drop the plugin's contribution.
func TestBuildGatesOnPluginError(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pagesDir, "index.tsx"), []byte(`export default function Index(){ return <p>plugin gate test</p> }`), 0644); err != nil {
		t.Fatal(err)
	}

	// Plugin package advertising runtime "go" with a binary that does not exist.
	pluginDir := filepath.Join(root, "plugins", "broken-go")
	_ = os.MkdirAll(pluginDir, 0755)
	// Same shape as examples/plugins/krate-plugin-demo-go/index.js. Include the
	// host platform so the test is deterministic on every GOOS/GOARCH (e.g.
	// darwin-arm64) - the binary entry points at a file that does not exist.
	binaryName := "bin/missing"
	if runtime.GOOS == "windows" {
		binaryName = "bin/missing.exe"
	}
	desc := `module.exports = function() {
  return {
    name: "broken-go",
    order: 10,
    module: "",
    runtime: "go",
    hooks: { BeforeBuild: null, AfterParse: null, AfterRender: null, AfterPage: null },
    binaries: { "` + runtime.GOOS + "-" + runtime.GOARCH + `": "` + binaryName + `" },
  };
};
`
	if err := os.WriteFile(filepath.Join(pluginDir, "index.js"), []byte(desc), 0644); err != nil {
		t.Fatal(err)
	}

	// Control: no plugins, trivial page builds cleanly.
	goodCfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	goodCfg.Minify = false
	if err := New(root, goodCfg).BuildAll(); err != nil {
		t.Fatalf("control build should succeed, got: %v", err)
	}

	// Broken Go plugin must make the build fail (exit-1 path), not warn+exit 0.
	badCfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	badCfg.Minify = false
	badCfg.Plugins = []config.PluginConfig{{
		Name:    "broken-go",
		Module:  pluginDir,
		Options: map[string]interface{}{
			// resolveImportForModule reads Options for the config plugin entry
			// only in krate.config.ts; here it is passed through untouched.
		},
	}}
	err = New(root, badCfg).BuildAll()
	if err == nil {
		t.Fatal("build with broken Go plugin succeeded; expected non-nil error (exit 1)")
	}
	if !strings.Contains(err.Error(), "Go plugin binary not found") {
		t.Errorf("expected missing Go plugin binary error, got: %v", err)
	}
}
