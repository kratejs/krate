package build

import (
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// e2eBuildNativeDev builds a project the way `krate native dev` does: the dev
// overlay is in the pages and the manifest enables the developer tools.
func e2eBuildNativeDev(t *testing.T, root, out string) string {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	b := New(root, cfg)
	b.DevMode = true
	res, err := b.BuildNativeApp(NativeAppOptions{OutDir: out, Dev: true})
	if err != nil {
		t.Fatalf("native dev build: %v", err)
	}
	return res.AppDir
}

// e2eNoPageErrors fails the test when the page of a dev build reported an error
// to the terminal while it ran.
func e2eNoPageErrors(t *testing.T, s *e2eShell) {
	t.Helper()
	time.Sleep(300 * time.Millisecond) // reports are posted asynchronously
	var bad []string
	for _, l := range strings.Split(s.errs.String(), "\n") {
		if strings.Contains(l, "[webview]") {
			bad = append(bad, l)
		}
	}
	if len(bad) > 0 {
		t.Errorf("the page reported errors:\n%s", strings.Join(bad, "\n"))
	}
}
