package shell

import (
	"net/http"
	"net/http/cookiejar"
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

func TestPluginBrowserMode(t *testing.T) {
	dir := pluginApp(t, pluginManifest)
	var started bool
	p := newGUIPlugin()
	inner := p.Init
	p.Init = func(c *plugin.Context) error {
		c.OnStart(func() error { started = c.App() == nil; return nil })
		return inner(c)
	}
	var ctl *Control
	done := make(chan error, 1)
	ready := make(chan *Control, 1)
	go func() {
		done <- Run(dir, RunOptions{Browser: true, Out: &strings.Builder{}, Logf: t.Logf, Plugins: []*plugin.Plugin{p},
			OnReady: func(c *Control) { ready <- c }})
	}()
	select {
	case ctl = <-ready:
	case err := <-done:
		t.Fatalf("Run: %v", err)
	}
	defer func() {
		ctl.Quit()
		<-done
	}()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	if resp, body := get(t, c, ctl.URL(), nil); resp.StatusCode != 200 || !strings.Contains(body, "plugin app") {
		t.Fatalf("page: %d %q", resp.StatusCode, body)
	}
	base := ctl.URL()[:strings.Index(ctl.URL(), "/?")]
	if got := callVia(t, c, base, "plugin/gt.hello", nil); !strings.Contains(got, "hello from gt") {
		t.Errorf("granted plugin op: %s", got)
	}
	if !started {
		t.Error("the start hook did not run in browser mode (without a platform)")
	}
}

func TestPluginManifestNeedsLinkedPlugin(t *testing.T) {
	dir := pluginApp(t, pluginManifest)
	err := Run(dir, RunOptions{Browser: true, Out: &strings.Builder{}, Logf: t.Logf})
	if err == nil || !strings.Contains(err.Error(), "plugin") {
		t.Fatalf("Run without the plugin linked: %v", err)
	}
	if !strings.Contains(err.Error(), "does not link") {
		t.Errorf("the error does not explain the shell lacks the plugin: %v", err)
	}
	// Linked but wrong name.
	other := newGUIPlugin()
	other.Meta.Name = "other"
	err = Run(dir, RunOptions{Browser: true, Out: &strings.Builder{}, Logf: t.Logf, Plugins: []*plugin.Plugin{other}})
	if err == nil || !strings.Contains(err.Error(), `"gt"`) {
		t.Errorf("Run with another plugin: %v", err)
	}
}

func TestPluginNamespaceViolationFailsLoad(t *testing.T) {
	dir := pluginApp(t, strings.Replace(pluginManifest, `"gt"`, `"gt"`, 1))
	p := newGUIPlugin()
	p.Init = func(c *plugin.Context) error { return c.Func("krate/fs.readFile", nil) }
	err := Run(dir, RunOptions{Browser: true, Out: &strings.Builder{}, Logf: t.Logf, Plugins: []*plugin.Plugin{p}})
	if err == nil || !strings.Contains(err.Error(), "namespace") {
		t.Fatalf("Run: %v, want a namespace error", err)
	}
}

func TestPluginPermissionInManifestNeedsPlugin(t *testing.T) {
	// A shell that lacks the plugin cannot parse its permissions; it says so.
	dir := pluginApp(t, strings.Replace(pluginManifest, "plugin:gt.read", "plugin:zz.read", 1))
	err := Run(dir, RunOptions{Browser: true, Out: &strings.Builder{}, Logf: t.Logf})
	if err == nil || !strings.Contains(err.Error(), "does not link") {
		t.Fatalf("Run: %v", err)
	}
}
