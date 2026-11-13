package shell

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/internal/gohosttest"
	"github.com/kratejs/krate/packages/native/plugin"
)

func pluginApp(t *testing.T, manifest string) string {
	t.Helper()
	root, cleanup, err := gohosttest.TempDir("shellplug-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	dir := filepath.Join(root, "app")
	if err := writeFile(filepath.Join(dir, "dist", "index.html"), "<!doctype html><title>p</title>plugin app"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(dir, ManifestFile), manifest); err != nil {
		t.Fatal(err)
	}
	return dir
}

const pluginManifest = `{"appId":"dev.krate.plug","productName":"P","version":"1.0.0","assets":"dist",
 "window":{"title":"P","width":300,"height":200,"path":"/"},
 "plugins":["gt"],"permissions":["plugin:gt.read","window"]}`

func TestPluginNamespaceViolationFailsLoad(t *testing.T) {
	dir := pluginApp(t, strings.Replace(pluginManifest, `"gt"`, `"gt"`, 1))
	p := newGUIPlugin()
	p.Init = func(c *plugin.Context) error { return c.Func("krate/fs.readFile", nil) }
	err := Run(dir, RunOptions{Browser: true, Out: &strings.Builder{}, Logf: t.Logf, Plugins: []*plugin.Plugin{p}})
	if err == nil || !strings.Contains(err.Error(), "namespace") {
		t.Fatalf("Run: %v, want a namespace error", err)
	}
}
