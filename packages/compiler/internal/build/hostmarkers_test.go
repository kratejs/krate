package build

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func TestWebBuildRejectsHostMarkers(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"src/pages/index.tsx": "@host\nexport async function addTask(t: string) { return t; }\n" + devPage("home"),
	})
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	err := New(root, cfg).BuildAll()
	if err == nil {
		t.Fatal("a web build with @host must fail")
	}
	var found bool
	for _, d := range DiagnosticsFromError(err) {
		if d.Code == diag.CodeHostInWebBuild {
			found = true
		}
	}
	if !found {
		t.Fatalf("no KR7173 in %v", errors.Unwrap(err))
	}

	b := New(root, cfg)
	b.NativeTarget = true
	if err := b.checkHostMarkers(); err != nil {
		t.Errorf("a native build allows host markers: %v", err)
	}
}
