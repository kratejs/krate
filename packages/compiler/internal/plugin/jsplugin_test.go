package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// writeTestPlugin writes a JS plugin module into a temp project and returns
// the project root, output dir, and a config pointing at it.
func writeTestPlugin(t testing.TB, code string) (root, outDir string, cfg config.PluginConfig) {
	t.Helper()
	root = t.TempDir()
	outDir = filepath.Join(root, "dist")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatal(err)
	}
	pluginDir := filepath.Join(root, "plugins", "test-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "index.js"), []byte(code), 0644); err != nil {
		t.Fatal(err)
	}
	cfg = config.PluginConfig{
		Name:    "test-plugin",
		Module:  "plugins/test-plugin",
		Options: map[string]interface{}{"greeting": "hi"},
	}
	return root, outDir, cfg
}

func TestJSPluginBeforeBuildWritesFile(t *testing.T) {
	root, outDir, cfg := writeTestPlugin(t, `
export default {
  name: "test-plugin",
  order: 10,
  hooks: {
    BeforeBuild(ctx, options, krate) {
      return { files: [{ path: "note.txt", content: options.greeting + " from " + krate.root }] };
    },
  },
};
`)

	ctx := &BuildHookCtx{Root: root, OutDir: outDir, Pages: []string{"index.tsx"}}
	if err := RunCommunityPlugins("BeforeBuild", []config.PluginConfig{cfg}, root, outDir, ctx); err != nil {
		t.Fatalf("RunCommunityPlugins: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outDir, "note.txt"))
	if err != nil {
		t.Fatalf("plugin did not write note.txt: %v", err)
	}
	if want := "hi from " + root; string(data) != want {
		t.Errorf("note.txt = %q, want %q", data, want)
	}
}

func TestJSPluginRouteTitleIsEscaped(t *testing.T) {
	root, outDir, cfg := writeTestPlugin(t, `
export default {
  name: "routes-plugin",
  order: 10,
  hooks: {
    GenerateRoutes(ctx, options, krate) {
      return { routes: [{ path: "generated/hello", title: "Q&A </title><script>x()</script>", content: "<h1>hi</h1>" }] };
    },
  },
};
`)
	ctx := &BuildHookCtx{Root: root, OutDir: outDir, Pages: nil}
	if err := RunCommunityPlugins("GenerateRoutes", []config.PluginConfig{cfg}, root, outDir, ctx); err != nil {
		t.Fatalf("RunCommunityPlugins: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(outDir, "generated", "hello", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(data), "<title>Q&amp;A &lt;/title&gt;&lt;script&gt;x()&lt;/script&gt;</title>") {
		t.Errorf("route title was not escaped: %q", data)
	}
	if contains(string(data), "<script>x()") {
		t.Errorf("route title injected markup: %q", data)
	}
}

func TestJSPluginPathTraversalRejected(t *testing.T) {
	root, outDir, cfg := writeTestPlugin(t, `
export default {
  name: "evil-plugin",
  order: 10,
  hooks: {
    BeforeBuild(ctx, options, krate) {
      return { files: [{ path: "../evil.txt", content: "escape" }] };
    },
  },
};
`)

	ctx := &BuildHookCtx{Root: root, OutDir: outDir, Pages: nil}
	err := RunCommunityPlugins("BeforeBuild", []config.PluginConfig{cfg}, root, outDir, ctx)
	if err == nil {
		t.Fatal("expected path traversal error, got nil")
	}
	if _, statErr := os.Stat(filepath.Join(root, "evil.txt")); statErr == nil {
		t.Fatal("plugin escaped output directory")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (func() bool {
		for i := 0; i+len(substr) <= len(s); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}

// TestJSPluginKrateReadFileTraversalRejected verifies capability host functions
// reject paths that escape the project root.
func TestJSPluginKrateReadFileTraversalRejected(t *testing.T) {
	root, outDir, cfg := writeTestPlugin(t, `
export default {
  name: "escape-plugin",
  order: 10,
  hooks: {
    BeforeBuild(ctx, options, krate) {
      krate.emitFile("leak.txt", krate.readFile("../secret.txt"));
      return {};
    },
  },
};
`)

	ctx := &BuildHookCtx{Root: root, OutDir: outDir, Pages: nil}
	err := RunCommunityPlugins("BeforeBuild", []config.PluginConfig{cfg}, root, outDir, ctx)
	if err == nil {
		t.Fatal("expected traversal error, got nil")
	}
	if _, statErr := os.Stat(filepath.Join(outDir, "leak.txt")); statErr == nil {
		t.Fatal("plugin read a path outside the project root")
	}
}
