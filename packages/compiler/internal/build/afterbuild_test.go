package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/plugin"
)

// TestAfterBuildContextCarriesDevMode verifies the builder propagates DevMode
// into the AfterBuild hook context (the docs plugin's Pagefind indexer relies on
// it to skip dev builds).
func TestAfterBuildContextCarriesDevMode(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(root, "src", "index.tsx")
	if err := os.WriteFile(entry, []byte("export default function Page() { return <div>hi</div>; }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Entry = entry
	cfg.PagesDir = filepath.Join(root, "src")
	cfg.OutDir = filepath.Join(root, "dist")

	var seen *plugin.BuildResultHookCtx
	saved := plugin.DefaultRegistry
	plugin.DefaultRegistry = plugin.NewRegistry()
	defer func() { plugin.DefaultRegistry = saved }()
	if err := plugin.Register(plugin.NewHookFunc("devmode-test", 999, plugin.PluginHooks{
		AfterBuild: func(ctx *plugin.BuildResultHookCtx) error {
			seen = ctx
			return nil
		},
	})); err != nil {
		t.Fatal(err)
	}

	b := New(root, cfg)
	b.DevMode = true
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if seen == nil {
		t.Fatal("AfterBuild hook was not called")
	}
	if !seen.DevMode {
		t.Error("BuildResultHookCtx.DevMode = false, want true")
	}
}
