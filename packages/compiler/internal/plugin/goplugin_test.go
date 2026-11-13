package plugin

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/astjson"
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

// GoClosePlugins closes every running Go plugin subprocess (test helper).
func GoClosePlugins() {
	CloseGoPlugins()
}
