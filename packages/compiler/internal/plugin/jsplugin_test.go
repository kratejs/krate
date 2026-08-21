package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"krate-compiler/internal/config"
)

// writeTestPlugin writes a JS plugin module into a temp project and returns
// the project root, output dir, and a config pointing at it.
func writeTestPlugin(t *testing.T, code string) (root, outDir string, cfg config.PluginConfig) {
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

func TestJSPluginAfterRenderModifiesContext(t *testing.T) {
	root, outDir, cfg := writeTestPlugin(t, `
export default {
  name: "test-plugin",
  order: 10,
  hooks: {
    AfterRender(ctx, options, krate) {
      return {
        html: "<b>" + ctx.page + "</b>" + ctx.html,
        headHTML: "<meta name=\"generator\" content=\"test\">",
        rawCSS: ".injected{}",
      };
    },
  },
};
`)

	ctx := &RenderHookCtx{Page: "index.tsx", HTML: "<p>body</p>", HeadHTML: "<title>t</title>", HasJS: true, RawCSS: ".a{}"}
	if err := RunCommunityPlugins("AfterRender", []config.PluginConfig{cfg}, root, outDir, ctx); err != nil {
		t.Fatalf("RunCommunityPlugins: %v", err)
	}

	if want := "<b>index.tsx</b><p>body</p>"; ctx.HTML != want {
		t.Errorf("HTML = %q, want %q", ctx.HTML, want)
	}
	if want := "<title>t</title><meta name=\"generator\" content=\"test\">"; ctx.HeadHTML != want {
		t.Errorf("HeadHTML = %q, want %q", ctx.HeadHTML, want)
	}
	if want := ".a{}\n.injected{}"; ctx.RawCSS != want {
		t.Errorf("RawCSS = %q, want %q", ctx.RawCSS, want)
	}
}

func TestJSPluginFactoryReceivesOptions(t *testing.T) {
	root, outDir, cfg := writeTestPlugin(t, `
export default function(options) {
  return {
    name: "factory-plugin",
    order: 10,
    hooks: {
      BeforeBuild(ctx, opts, krate) {
        return { files: [{ path: "factory.txt", content: opts.greeting }] };
      },
    },
  };
};
`)

	ctx := &BuildHookCtx{Root: root, OutDir: outDir, Pages: nil}
	if err := RunCommunityPlugins("BeforeBuild", []config.PluginConfig{cfg}, root, outDir, ctx); err != nil {
		t.Fatalf("RunCommunityPlugins: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(outDir, "factory.txt"))
	if err != nil {
		t.Fatalf("factory plugin did not write factory.txt: %v", err)
	}
	if string(data) != "hi" {
		t.Errorf("factory.txt = %q, want %q", data, "hi")
	}
}

// TestJSPluginNamedHooksExport verifies the config-factory module shape: the
// module exports `hooks` as a named export and a default factory that returns a
// serializable descriptor. The compiler reads hooks from the named export when
// the factory result carries no hooks (metadata only).
func TestJSPluginNamedHooksExport(t *testing.T) {
	root, outDir, cfg := writeTestPlugin(t, `
export const hooks = {
  BeforeBuild(ctx, options, krate) {
    return { files: [{ path: "named-hooks.txt", content: options.greeting + " / " + (krate.outDir || "") }] };
  },
};
export default function(options) {
  return {
    name: "named-hooks-plugin",
    order: 10,
    module: (typeof import.meta !== 'undefined' && import.meta.url) ? import.meta.url : '',
    options: options || {},
  };
};
`)

	ctx := &BuildHookCtx{Root: root, OutDir: outDir, Pages: nil}
	if err := RunCommunityPlugins("BeforeBuild", []config.PluginConfig{cfg}, root, outDir, ctx); err != nil {
		t.Fatalf("RunCommunityPlugins: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(outDir, "named-hooks.txt"))
	if err != nil {
		t.Fatalf("plugin did not write named-hooks.txt: %v", err)
	}
	if want := "hi / " + outDir; string(data) != want {
		t.Errorf("named-hooks.txt = %q, want %q", data, want)
	}
}

func TestJSPluginAsyncHook(t *testing.T) {
	root, outDir, cfg := writeTestPlugin(t, `
export default {
  name: "async-plugin",
  order: 10,
  hooks: {
    AfterRender(ctx, options, krate) {
      return Promise.resolve({ html: "async:" + ctx.html });
    },
  },
};
`)

	ctx := &RenderHookCtx{Page: "p.tsx", HTML: "<p>body</p>", HeadHTML: "", RawCSS: ""}
	if err := RunCommunityPlugins("AfterRender", []config.PluginConfig{cfg}, root, outDir, ctx); err != nil {
		t.Fatalf("RunCommunityPlugins: %v", err)
	}
	if want := "async:<p>body</p>"; ctx.HTML != want {
		t.Errorf("HTML = %q, want %q", ctx.HTML, want)
	}
}

func TestJSPluginMissingHookIsSkipped(t *testing.T) {
	root, outDir, cfg := writeTestPlugin(t, `
export default {
  name: "partial-plugin",
  order: 10,
  hooks: { AfterRender(ctx, options, krate) { return { html: "x" }; } },
};
`)

	// Plugin only implements AfterRender — running AfterPage should be a no-op.
	ctx := &PageHookCtx{Page: "p.tsx", OutName: "p", HTML: "orig", HeadHTML: ""}
	if err := RunCommunityPlugins("AfterPage", []config.PluginConfig{cfg}, root, outDir, ctx); err != nil {
		t.Fatalf("RunCommunityPlugins: %v", err)
	}
	if ctx.HTML != "orig" {
		t.Errorf("HTML modified despite no AfterPage hook: %q", ctx.HTML)
	}
}

func TestJSPluginGeneratesRoutes(t *testing.T) {
	root, outDir, cfg := writeTestPlugin(t, `
export default {
  name: "routes-plugin",
  order: 10,
  hooks: {
    GenerateRoutes(ctx, options, krate) {
      return { routes: [{ path: "generated/hello", title: "Hello", content: "<h1>hi</h1>" }] };
    },
  },
};
`)

	ctx := &BuildHookCtx{Root: root, OutDir: outDir, Pages: nil}
	if err := RunCommunityPlugins("GenerateRoutes", []config.PluginConfig{cfg}, root, outDir, ctx); err != nil {
		t.Fatalf("RunCommunityPlugins: %v", err)
	}
	routeFile := filepath.Join(outDir, "generated", "hello", "index.html")
	data, err := os.ReadFile(routeFile)
	if err != nil {
		t.Fatalf("generated route not written: %v", err)
	}
	if !contains(string(data), "<h1>hi</h1>") {
		t.Errorf("generated route missing content: %q", data)
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
