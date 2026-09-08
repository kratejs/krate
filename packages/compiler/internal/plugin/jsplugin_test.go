package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/astjson"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
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

// TestJSPluginTypeScriptEntry verifies a plugin directory whose entry point is
// index.ts (not index.js) resolves and runs inside the JS runtime.
func TestJSPluginTypeScriptEntry(t *testing.T) {
	root := t.TempDir()
	outDir := filepath.Join(root, "dist")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatal(err)
	}
	pluginDir := filepath.Join(root, "plugins", "test-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "index.ts"), []byte(`
export default {
  name: "test-plugin",
  order: 10,
  hooks: {
    BeforeBuild(ctx, options, krate) {
      return { files: [{ path: "ts.txt", content: "ts entry works" }] };
    },
  },
};
`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := config.PluginConfig{Name: "test-plugin", Module: "plugins/test-plugin"}

	ctx := &BuildHookCtx{Root: root, OutDir: outDir, Pages: []string{"index.tsx"}}
	if err := RunCommunityPlugins("BeforeBuild", []config.PluginConfig{cfg}, root, outDir, ctx); err != nil {
		t.Fatalf("RunCommunityPlugins with .ts entry: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outDir, "ts.txt"))
	if err != nil {
		t.Fatalf("plugin with .ts entry did not write ts.txt: %v", err)
	}
	if want := "ts entry works"; string(data) != want {
		t.Errorf("ts.txt = %q, want %q", data, want)
	}
}

func TestJSPluginAfterMarkdownParseModifiesHTML(t *testing.T) {
	root, outDir, cfg := writeTestPlugin(t, `
export default {
  name: "test-plugin",
  order: 10,
  hooks: {
    AfterMarkdownParse(ctx, options, krate) {
      return { html: "<section>" + ctx.html + "</section>" };
    },
  },
};
`)

	ctx := &MarkdownHookCtx{Page: "docs/readme.md", HTML: "<p>parsed</p>", Route: "docs/readme"}
	if err := RunCommunityPlugins("AfterMarkdownParse", []config.PluginConfig{cfg}, root, outDir, ctx); err != nil {
		t.Fatalf("RunCommunityPlugins: %v", err)
	}

	if want := "<section><p>parsed</p></section>"; ctx.HTML != want {
		t.Errorf("HTML = %q, want %q", ctx.HTML, want)
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

func TestJSPluginAfterParseEditsAST(t *testing.T) {
	src := `export default function App() { return <div>hello</div>; }`
	toks := lexer.New(src).Tokenize()
	prog := parser.New(toks).ParseProgram()
	if prog == nil || len(prog.Body) == 0 {
		t.Fatal("setup: failed to parse program")
	}

	root, outDir, cfg := writeTestPlugin(t, `
export default {
  name: "ast-plugin",
  order: 10,
  hooks: {
    AfterParse(ctx, options, krate) {
      const walk = (node) => {
        if (!node || typeof node !== 'object') return null;
        if (node.kind === 'JSXText' && node.value === 'hello') return node;
        for (const k in node) {
          const v = node[k];
          if (Array.isArray(v)) { for (const item of v) { const r = walk(item); if (r) return r; } }
          else { const r = walk(v); if (r) return r; }
        }
        return null;
      };
      const el = walk(ctx.program);
      if (el) el.value = 'hello plugin';
      return { ast: ctx.program };
    },
  },
};
`)

	ctx := &ParseHookCtx{Page: "index.tsx", Program: prog}
	if err := RunCommunityPlugins("AfterParse", []config.PluginConfig{cfg}, root, outDir, ctx); err != nil {
		t.Fatalf("RunCommunityPlugins: %v", err)
	}
	if ctx.Program == nil {
		t.Fatal("AfterParse plugin dropped the program")
	}

	exportStmt, ok := ctx.Program.Body[0].(*ast.ExportStmt)
	if !ok {
		t.Fatalf("Body[0] = %T, want *ast.ExportStmt", ctx.Program.Body[0])
	}
	fn, ok := exportStmt.Declaration.(*ast.FnDecl)
	if !ok {
		t.Fatalf("declaration = %T, want *ast.FnDecl", exportStmt.Declaration)
	}
	ret, ok := fn.Body[0].(*ast.ReturnStmt)
	if !ok {
		t.Fatalf("Body[0] = %T, want *ast.ReturnStmt", fn.Body[0])
	}
	jsx, ok := ret.Value.(*ast.JSXElement)
	if !ok {
		t.Fatalf("return value = %T, want *ast.JSXElement", ret.Value)
	}
	text, ok := jsx.Children[0].(*ast.JSXText)
	if !ok {
		t.Fatalf("child = %T, want *ast.JSXText", jsx.Children[0])
	}
	if want := "hello plugin"; text.Value != want {
		t.Errorf("JSXText.Value = %q, want %q (plugin AST edit did not flow into Go program)", text.Value, want)
	}

	// The edited program must still encode to a valid AST document.
	doc, err := astjson.EncodeProgram(ctx.Program)
	if err != nil {
		t.Fatalf("re-encoding edited program: %v", err)
	}
	if !contains(string(doc), "hello plugin") {
		t.Errorf("re-encoded doc does not contain the plugin edit")
	}
}

func TestJSPluginHeadInjections(t *testing.T) {
	root, outDir, cfg := writeTestPlugin(t, `
export default {
  name: "inject-plugin",
  order: 10,
  hooks: {
    AfterRender(ctx, options, krate) {
      return {
        metaTags: ['name="description" content="injected"'],
        scripts: ['/assets/plugin.js'],
      };
    },
  },
};
`)

	ctx := &RenderHookCtx{Page: "p.tsx", HTML: "<p>body</p>", HeadHTML: "<title>t</title>", RawCSS: ""}
	if err := RunCommunityPlugins("AfterRender", []config.PluginConfig{cfg}, root, outDir, ctx); err != nil {
		t.Fatalf("RunCommunityPlugins: %v", err)
	}
	if want := "<title>t</title>\n<meta name=\"description\" content=\"injected\">\n<script src=\"/assets/plugin.js\"></script>"; ctx.HeadHTML != want {
		t.Errorf("HeadHTML = %q, want %q", ctx.HeadHTML, want)
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
