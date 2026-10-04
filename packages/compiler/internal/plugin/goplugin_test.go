package plugin

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/astjson"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
	pluginsdk "github.com/kratejs/krate/packages/compiler/pluginsdk"
)

// buildFixturePlugin compiles the testdata fixture plugin into a temp
// directory and returns the executable path.
func buildFixturePlugin(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "fixtureplugin.exe")
	cmd := exec.Command("go", "build", "-o", exe, "./internal/plugin/testdata/fixtureplugin")
	cmd.Dir = root
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building fixture plugin: %v\n%s", err, out)
	}
	return exe
}

// startFixturePlugin launches the fixture plugin as a subprocess and returns
// the dispensed client plus a kill function.
func startFixturePlugin(t *testing.T, exe string) (*pluginsdk.RPCClient, func()) {
	t.Helper()
	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig:  pluginsdk.HandshakeConfig,
		Plugins:          map[string]goplugin.Plugin{pluginsdk.PluginName: &pluginsdk.Bridge{}},
		Cmd:              exec.Command(exe),
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolNetRPC},
		Logger:           hclog.New(&hclog.LoggerOptions{Level: hclog.Error, Output: io.Discard}),
		StartTimeout:     30 * time.Second,
	})
	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		t.Fatalf("connecting to plugin: %v", err)
	}
	raw, err := rpcClient.Dispense(pluginsdk.PluginName)
	if err != nil {
		client.Kill()
		t.Fatalf("dispensing plugin: %v", err)
	}
	impl, ok := raw.(*pluginsdk.RPCClient)
	if !ok {
		client.Kill()
		t.Fatalf("dispensed unexpected type %T", raw)
	}
	return impl, client.Kill
}

func dispatchJSON(t *testing.T, impl *pluginsdk.RPCClient, hook string, args interface{}) pluginsdk.Result {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	out, err := impl.Dispatch("build", hook, raw)
	if err != nil {
		t.Fatalf("%s: %v", hook, err)
	}
	var res pluginsdk.Result
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("decoding %s result: %v", hook, err)
	}
	return res
}

func TestGoPluginEndToEnd(t *testing.T) {
	exe := buildFixturePlugin(t)
	impl, kill := startFixturePlugin(t, exe)
	defer kill()

	// BeforeBuild writes a file via the embedded Result.
	root := t.TempDir()
	out := t.TempDir()
	res := dispatchJSON(t, impl, "BeforeBuild", pluginsdk.BuildArgs{
		Root:    root,
		OutDir:  out,
		Pages:   []string{"index.tsx"},
		DevMode: false,
	})
	if len(res.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(res.Files))
	}
	if want := "go plugin running from " + root; res.Files[0].Content != want {
		t.Errorf("file content = %q, want %q", res.Files[0].Content, want)
	}

	// AfterParse edits the AST document and returns it under "ast".
	src := `export default function App() { return <b>go-plugin-token</b>; }`
	progDoc, err := astjson.EncodeProgram(parser.New(lexer.New(src).Tokenize()).ParseProgram())
	if err != nil {
		t.Fatal(err)
	}
	res = dispatchJSON(t, impl, "AfterParse", map[string]interface{}{
		"page":    "index.tsx",
		"program": (json.RawMessage)(progDoc),
	})
	if len(res.Ast) == 0 {
		t.Fatal("AfterParse result did not include the edited program document")
	}
	edited, err := astjson.DecodeProgram(res.Ast)
	if err != nil {
		t.Fatalf("decoding edited program: %v", err)
	}
	if !strings.Contains(dumpPage(t, edited), "GO-PLUGIN-EDITED") {
		t.Errorf("edited program does not contain the plugin edit:\n%s", dumpPage(t, edited))
	}

	// AfterRender mutates headHTML and injects a meta tag.
	res = dispatchJSON(t, impl, "AfterRender", map[string]interface{}{
		"page":     "index.tsx",
		"html":     "<p>body</p>",
		"headHTML": "<title>t</title>",
		"hasJS":    false,
		"rawCSS":   "",
	})
	if res.HeadHTML == nil || !strings.Contains(*res.HeadHTML, `content="gofix"`) {
		t.Errorf("headHTML did not receive the render edit: %v", res.HeadHTML)
	}
	if len(res.MetaTags) != 1 || !strings.Contains(res.MetaTags[0], "fixture") {
		t.Errorf("metaTags = %v", res.MetaTags)
	}

	// AfterBuild collects the page summary.
	res = dispatchJSON(t, impl, "AfterBuild", pluginsdk.BuildResultArgs{
		Root:   root,
		OutDir: out,
		Pages: []pluginsdk.PageResult{
			{Page: "index.tsx", OutName: "index", HTML: "<p>html</p>"},
		},
	})
	if len(res.Files) != 1 || !strings.HasPrefix(res.Files[0].Content, "pages:") {
		t.Errorf("AfterBuild files = %v", res.Files)
	}

	// Shut the plugin down and confirm the process exits.
	kill()
}

func dumpPage(t *testing.T, prog *ast.Program) string {
	t.Helper()
	b, err := astjson.EncodeProgram(prog)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestGoPluginOfficialDescriptorWithImportMeta reproduces the official Go
// plugin example's descriptor shape (examples/plugins/krate-plugin-demo-go):
// a CommonJS `module.exports = function() {...}` descriptor that also
// references `import.meta.url` for its `module:` self-path. esbuild treats such
// a file as ESM (due to import.meta) and leaves the `module.exports`
// assignment unwrapped, so discovery used to throw "module is not defined".
// The loader must shim CommonJS for manifest discovery so the official example
// loads and its per-platform binary is resolved.
func TestGoPluginOfficialDescriptorWithImportMeta(t *testing.T) {
	pkgDir := t.TempDir()
	desc := `module.exports = function() {
  return {
    name: "demo-go",
    order: 10,
    module: (typeof import.meta !== "undefined" && import.meta.url) ? import.meta.url : "",
    runtime: "go",
    hooks: { BeforeBuild: null, AfterParse: null, AfterRender: null, ServeRequest: null, ServeResponse: null },
    binaries: { "` + platformKey() + `": "bin/plugin" },
  };
};
`
	if err := os.WriteFile(filepath.Join(pkgDir, "index.js"), []byte(desc), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := discoverGoManifest(pkgDir)
	if err != nil {
		t.Fatalf("discoverGoManifest with official-style descriptor: %v", err)
	}
	if got.Runtime != "go" {
		t.Errorf("runtime = %q, want go", got.Runtime)
	}
	if got.Binaries[platformKey()] != "bin/plugin" {
		t.Errorf("binaries = %v, want host platform -> bin/plugin", got.Binaries)
	}
}

// TestGoPluginDiscoveryAndRouting exercises the full host path: a module whose
// JS descriptor advertises runtime "go" with a per-platform binary. Krate must
// discover the manifest, resolve the binary for the host platform, spawn it via
// go-plugin, run the hook, and apply the returned result.
func TestGoPluginDiscoveryAndRouting(t *testing.T) {
	exe := buildFixturePlugin(t)

	// Build an npm-style package with a JS descriptor and a bin/ directory.
	pkgDir := t.TempDir()
	binDir := filepath.Join(pkgDir, "bin")
	_ = os.MkdirAll(binDir, 0755)
	binName := "fixtureplugin"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	if err := copyFile(exe, filepath.Join(binDir, binName)); err != nil {
		t.Fatal(err)
	}
	desc := `module.exports = function(options) {
  return {
    name: 'fixture-component',
    runtime: 'go',
    hooks: { BeforeBuild: null, AfterParse: null, AfterRender: null, AfterBuild: null },
    binaries: { ['` + platformKey() + `']: 'bin/` + binName + `' },
  };
};
`
	if err := os.WriteFile(filepath.Join(pkgDir, "index.js"), []byte(desc), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.PluginConfig{
		Name:    "fixture-component",
		Module:  pkgDir,
		Options: map[string]interface{}{},
	}

	root := t.TempDir()
	out := t.TempDir()

	// BeforeBuild: the Go plugin writes fixture.txt.
	ctx := &BuildHookCtx{Root: root, OutDir: out, Pages: []string{"index.tsx"}}
	defer GoClosePlugins()
	if err := runCommunityHook("BeforeBuild", cfg, root, out, CommunityEnv{}, ctx); err != nil {
		t.Fatalf("runCommunityHook BeforeBuild: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(out, "fixture.txt"))
	if err != nil {
		t.Fatalf("fixture.txt not written: %v", err)
	}
	if want := "go plugin running from " + root; string(data) != want {
		t.Errorf("fixture.txt = %q, want %q", data, want)
	}

	// AfterParse: edits the program document.
	src := `export default function App() { return <b>go-plugin-token</b>; }`
	progDoc, err := astjson.EncodeProgram(parser.New(lexer.New(src).Tokenize()).ParseProgram())
	if err != nil {
		t.Fatal(err)
	}
	parseCtx := &ParseHookCtx{Page: "index.tsx", Program: decodeProg(t, progDoc)}
	if err := runCommunityHook("AfterParse", cfg, root, out, CommunityEnv{}, parseCtx); err != nil {
		t.Fatalf("runCommunityHook AfterParse: %v", err)
	}
	if parseCtx.Program == nil {
		t.Fatal("AfterParse dropped the program")
	}
	if !strings.Contains(dumpPage(t, parseCtx.Program), "GO-PLUGIN-EDITED") {
		t.Errorf("AfterParse edit did not flow into the program:\n%s", dumpPage(t, parseCtx.Program))
	}

	// AfterRender: injects head content + meta tag.
	renderCtx := &RenderHookCtx{Page: "index.tsx", HTML: "<p>body</p>", HeadHTML: "<title>t</title>", RawCSS: ""}
	if err := runCommunityHook("AfterRender", cfg, root, out, CommunityEnv{}, renderCtx); err != nil {
		t.Fatalf("runCommunityHook AfterRender: %v", err)
	}
	if !strings.Contains(renderCtx.HeadHTML, `content="gofix"`) {
		t.Errorf("AfterRender head edit missing: %q", renderCtx.HeadHTML)
	}
	if !strings.Contains(renderCtx.HeadHTML, `name="fixture" content="true"`) {
		t.Errorf("AfterRender meta tag missing: %q", renderCtx.HeadHTML)
	}
}

// GoClosePlugins closes every running Go plugin subprocess (test helper).
func GoClosePlugins() {
	CloseGoPlugins()
}

func decodeProg(t *testing.T, doc []byte) *ast.Program {
	t.Helper()
	b, err := astjson.DecodeProgram(doc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0755)
}
