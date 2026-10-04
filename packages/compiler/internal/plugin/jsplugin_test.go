package plugin

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/astjson"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/environ"
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

// TestJSPluginProcessEnv verifies the resolved .env (environ.Current) is
// visible inside a JS plugin's hook via process.env.
func TestJSPluginProcessEnv(t *testing.T) {
	old := environ.Current
	environ.Current = map[string]string{"KRATE_TEST_ENV": "plugin-env"}
	defer func() { environ.Current = old }()

	root, outDir, cfg := writeTestPlugin(t, `
export default {
  name: "env-plugin",
  order: 10,
  hooks: {
    BeforeBuild(ctx, options, krate) {
      return { files: [{ path: "env.txt", content: process.env.KRATE_TEST_ENV || "missing" }] };
    },
  },
};
`)

	ctx := &BuildHookCtx{Root: root, OutDir: outDir, Pages: nil}
	if err := RunCommunityPlugins("BeforeBuild", []config.PluginConfig{cfg}, root, outDir, ctx); err != nil {
		t.Fatalf("RunCommunityPlugins: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(outDir, "env.txt"))
	if err != nil {
		t.Fatalf("env.txt not written: %v", err)
	}
	if want := "plugin-env"; string(data) != want {
		t.Errorf("env.txt = %q, want %q (process.env not visible in plugin)", data, want)
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

// TestJSPluginKrateCapabilities exercises the richer krate object: metadata
// fields (projectRoot/outDir/pagesDir/dev/pages/version/config) and the
// capability methods (resolveFile/readFile/emitFile/writeFileToRoot/
// injectHead/injectCSS).
func TestJSPluginKrateCapabilities(t *testing.T) {
	root, outDir, cfg := writeTestPlugin(t, `
export default {
  name: "cap-plugin",
  order: 10,
  hooks: {
    BeforeBuild(ctx, options, krate) {
      const resolved = krate.resolveFile("./data/input.txt");
      krate.emitFile("copied.txt", krate.readFile("./data/input.txt"));
      krate.emitFile("meta.json", JSON.stringify({
        resolved: resolved,
        projectRoot: krate.projectRoot,
        root: krate.root,
        outDir: krate.outDir,
        pagesDir: krate.pagesDir,
        dev: krate.dev,
        devMode: krate.devMode,
        pages: krate.pages,
        version: krate.version,
        configOutDir: krate.config && krate.config.outDir,
      }));
      krate.writeFileToRoot("public/asset.txt", "asset-body");
      return {};
    },
    AfterRender(ctx, options, krate) {
      krate.injectHead('<meta name="cap" content="1">');
      krate.injectCSS(".cap{color:red}");
      return { html: "cap:" + ctx.html };
    },
  },
};
`)
	if err := os.MkdirAll(filepath.Join(root, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data", "input.txt"), []byte("input-body"), 0644); err != nil {
		t.Fatal(err)
	}

	pagesDir := filepath.Join(root, "src", "pages")
	env := CommunityEnv{PagesDir: pagesDir, DevMode: true, Config: &config.Config{OutDir: outDir}}
	ctx := &BuildHookCtx{Root: root, OutDir: outDir, Pages: []string{"index.tsx", "about.tsx"}, DevMode: true}
	if err := RunCommunityPlugins("BeforeBuild", []config.PluginConfig{cfg}, root, outDir, ctx, env); err != nil {
		t.Fatalf("RunCommunityPlugins BeforeBuild: %v", err)
	}

	if data, err := os.ReadFile(filepath.Join(outDir, "copied.txt")); err != nil || string(data) != "input-body" {
		t.Fatalf("copied.txt = %q (err %v), want %q", data, err, "input-body")
	}
	if data, err := os.ReadFile(filepath.Join(root, "public", "asset.txt")); err != nil || string(data) != "asset-body" {
		t.Fatalf("public/asset.txt = %q (err %v), want %q", data, err, "asset-body")
	}

	metaBytes, err := os.ReadFile(filepath.Join(outDir, "meta.json"))
	if err != nil {
		t.Fatalf("meta.json not written: %v", err)
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf("decoding meta.json: %v", err)
	}
	if meta["projectRoot"] != root || meta["root"] != root {
		t.Errorf("projectRoot/root = %v/%v, want %v", meta["projectRoot"], meta["root"], root)
	}
	if meta["outDir"] != outDir {
		t.Errorf("outDir = %v, want %v", meta["outDir"], outDir)
	}
	if meta["pagesDir"] != pagesDir {
		t.Errorf("pagesDir = %v, want %v", meta["pagesDir"], pagesDir)
	}
	if meta["dev"] != true || meta["devMode"] != true {
		t.Errorf("dev/devMode = %v/%v, want true", meta["dev"], meta["devMode"])
	}
	if meta["configOutDir"] != outDir {
		t.Errorf("config.outDir = %v, want %v", meta["configOutDir"], outDir)
	}
	if v, _ := meta["version"].(string); v == "" || v == "1.0.0" {
		t.Errorf("version = %v, want the real compiler version", meta["version"])
	}
	if pages, ok := meta["pages"].([]interface{}); !ok || len(pages) != 2 {
		t.Errorf("pages = %v, want 2 entries", meta["pages"])
	}
	if meta["resolved"] != filepath.Join(root, "data", "input.txt") {
		t.Errorf("resolveFile = %v, want %v", meta["resolved"], filepath.Join(root, "data", "input.txt"))
	}

	rctx := &RenderHookCtx{Page: "index.tsx", HTML: "<p>x</p>", HeadHTML: "<title>t</title>"}
	if err := RunCommunityPlugins("AfterRender", []config.PluginConfig{cfg}, root, outDir, rctx, env); err != nil {
		t.Fatalf("RunCommunityPlugins AfterRender: %v", err)
	}
	if !strings.Contains(rctx.HeadHTML, `name="cap"`) {
		t.Errorf("injectHead did not reach HeadHTML: %q", rctx.HeadHTML)
	}
	if !strings.Contains(rctx.RawCSS, ".cap{color:red}") {
		t.Errorf("injectCSS did not reach RawCSS: %q", rctx.RawCSS)
	}
	if rctx.HTML != "cap:<p>x</p>" {
		t.Errorf("HTML = %q, want %q", rctx.HTML, "cap:<p>x</p>")
	}
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

// TestBuildGoHookArgsInjectsKrateMetadata verifies Go plugin hook args carry the
// shared build metadata (projectRoot/outDir/pagesDir/version/devMode/pages/
// config) that the SDK exposes as KrateInfo.
func TestBuildGoHookArgsInjectsKrateMetadata(t *testing.T) {
	env := CommunityEnv{
		PagesDir: "/proj/src/pages",
		DevMode:  true,
		Config:   &config.Config{OutDir: "/proj/dist"},
	}

	rctx := &RenderHookCtx{Page: "p.tsx", HTML: "<p>", HeadHTML: "", RawCSS: ""}
	args, err := buildGoHookArgs("AfterRender", "/proj", "/proj/dist", env, rctx)
	if err != nil {
		t.Fatalf("buildGoHookArgs AfterRender: %v", err)
	}
	render := decodeArgs(t, args)
	if render["projectRoot"] != "/proj" || render["outDir"] != "/proj/dist" {
		t.Errorf("root/outDir = %v/%v", render["projectRoot"], render["outDir"])
	}
	if render["pagesDir"] != "/proj/src/pages" || render["devMode"] != true {
		t.Errorf("pagesDir/devMode = %v/%v", render["pagesDir"], render["devMode"])
	}
	if v, _ := render["version"].(string); v == "" {
		t.Errorf("version missing: %v", render["version"])
	}
	if pages, ok := render["pages"].([]interface{}); !ok || len(pages) != 1 || pages[0] != "p.tsx" {
		t.Errorf("pages = %v", render["pages"])
	}
	if render["config"] == nil {
		t.Error("config missing from Go hook args")
	}

	// AfterParse carries the program document plus the same metadata.
	pctx := &ParseHookCtx{Page: "p.tsx"}
	args, err = buildGoHookArgs("AfterParse", "/proj", "/proj/dist", env, pctx)
	if err != nil {
		t.Fatalf("buildGoHookArgs AfterParse: %v", err)
	}
	parse := decodeArgs(t, args)
	if parse["projectRoot"] != "/proj" || parse["page"] != "p.tsx" {
		t.Errorf("AfterParse args = %v", parse)
	}
}

func decodeArgs(t *testing.T, args interface{}) map[string]interface{} {
	t.Helper()
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// captureOutput redirects stdout and stderr around fn and returns what each
// received. Plugin console/log output is written synchronously, so the pipes
// cannot deadlock on the small payloads these tests produce.
func captureOutput(t *testing.T, fn func()) (string, string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wOut, wErr
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()

	fn()
	wOut.Close()
	wErr.Close()
	out, _ := io.ReadAll(rOut)
	errOut, _ := io.ReadAll(rErr)
	return string(out), string(errOut)
}

// TestJSPluginKrateLogWarn verifies krate.log is verbose-only while krate.warn
// always prints, and both carry the [plugin:<name>] prefix.
func TestJSPluginKrateLogWarn(t *testing.T) {
	root, outDir, cfg := writeTestPlugin(t, `
export default {
  name: "log-plugin",
  order: 10,
  hooks: {
    BeforeBuild(ctx, options, krate) {
      krate.log("hello-log");
      krate.warn("hello-warn");
      console.log("hello-console");
      return {};
    },
  },
};
`)
	ctx := &BuildHookCtx{Root: root, OutDir: outDir, Pages: nil, DevMode: false}

	defer SetVerbose(false)

	// Verbose off: krate.log and console.log are suppressed; warn always shows.
	SetVerbose(false)
	stdout, stderr := captureOutput(t, func() {
		if err := RunCommunityPlugins("BeforeBuild", []config.PluginConfig{cfg}, root, outDir, ctx); err != nil {
			t.Fatalf("RunCommunityPlugins: %v", err)
		}
	})
	if strings.Contains(stdout+stderr, "hello-log") {
		t.Errorf("krate.log printed without --verbose:\nstdout=%q\nstderr=%q", stdout, stderr)
	}
	// console.log is an always-on polyfill (not gated by --verbose), and is now
	// prefixed so it is attributable to the plugin.
	if !strings.Contains(stdout, "hello-console") || !strings.Contains(stdout, "[plugin:test-plugin]") {
		t.Errorf("console.log should always print, prefixed:\nstdout=%q", stdout)
	}
	if !strings.Contains(stderr, "hello-warn") {
		t.Errorf("krate.warn did not print:\nstderr=%q", stderr)
	}
	if !strings.Contains(stderr, "[plugin:test-plugin]") {
		t.Errorf("krate.warn missing plugin prefix:\nstderr=%q", stderr)
	}

	// Verbose on: krate.log and console.log now appear, still prefixed.
	SetVerbose(true)
	stdout, _ = captureOutput(t, func() {
		if err := RunCommunityPlugins("BeforeBuild", []config.PluginConfig{cfg}, root, outDir, ctx); err != nil {
			t.Fatalf("RunCommunityPlugins (verbose): %v", err)
		}
	})
	if !strings.Contains(stdout, "hello-log") {
		t.Errorf("krate.log missing with --verbose:\nstdout=%q", stdout)
	}
	if !strings.Contains(stdout, "[plugin:test-plugin]") {
		t.Errorf("krate.log missing plugin prefix:\nstdout=%q", stdout)
	}
	if !strings.Contains(stdout, "hello-console") || !strings.Contains(stdout, "[plugin:test-plugin]") {
		t.Errorf("console.log missing with --verbose:\nstdout=%q", stdout)
	}
}

// TestHookTrace verifies the verbose hook trace emits plugin/hook/timing lines
// only when verbose mode is enabled, and marks failures.
func TestHookTrace(t *testing.T) {
	defer SetVerbose(false)

	SetVerbose(false)
	stdout, stderr := captureOutput(t, func() {
		traceHook("demo", "BeforeBuild", 5*time.Millisecond, nil)
	})
	if stderr != "" || stdout != "" {
		t.Errorf("trace printed while quiet: stdout=%q stderr=%q", stdout, stderr)
	}

	SetVerbose(true)
	_, stderr = captureOutput(t, func() {
		traceHook("demo", "BeforeBuild", 12*time.Millisecond, nil)
		traceHook("demo", "AfterRender", 3*time.Millisecond, errTraceTest)
	})
	if !strings.Contains(stderr, "[plugin:demo]") || !strings.Contains(stderr, "BeforeBuild") {
		t.Errorf("missing trace line:\nstderr=%q", stderr)
	}
	if !strings.Contains(stderr, "error") {
		t.Errorf("failed hook not marked in trace:\nstderr=%q", stderr)
	}
}

var errTraceTest = fmt.Errorf("boom")
