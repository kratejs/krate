package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func echoPluginDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.Abs(filepath.Join("testdata", "native_plugin", "echo"))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// pluginProject writes a project with fake npm packages whose page is page and
// that enables the plugins in dirs.
func pluginProject(t *testing.T, page string, dirs ...string) *Builder {
	t.Helper()
	root := t.TempDir()
	t.Setenv("KRATE_NATIVE_DIR", filepath.Join(repoRoot(t), "packages", "native"))
	t.Setenv("GOWORK", "off")
	writeTree(t, root, fakeKrateModules)
	writeTree(t, root, map[string]string{"src/pages/index.tsx": page})
	b := newNativeBuilder(t, root)
	b.Cfg.Native.Plugins = dirs
	return b
}

const echoPageSome = `import { echo, free } from "krate/plugin/echo";
export default function Home() { return <main><button onClick={() => echo("hi")}>go</button><i>{String(free)}</i></main>; }
`

func TestNativePluginAllowlist(t *testing.T) {
	requireRelease(t)
	// native.permissions that does not allow what the code needs is KR7210,
	// and a plugin permission no plugin declares is KR7255.
	b := pluginProject(t, echoPageSome, echoPluginDir(t))
	b.Cfg.Native.Permissions = []string{"kv"}
	_, err := b.BuildNativeRelease(NativeReleaseOptions{OutDir: filepath.Join(b.Root, "out")})
	if err == nil || !strings.Contains(BuildErrorSummary(err)+err.Error(), "native permission error") {
		t.Errorf("allowlist without the plugin permission: %v", err)
	}
	b = pluginProject(t, echoPageSome, echoPluginDir(t))
	b.Cfg.Native.Permissions = []string{"plugin:echo.nope"}
	_, err = b.BuildNativeApp(NativeAppOptions{OutDir: filepath.Join(b.Root, "out")})
	if !hasDiagCode(err, diag.CodePluginPermission) {
		t.Errorf("unknown plugin permission: %v", err)
	}
}

// pluginCallJS calls plugin/echo.echo from the page, as the window the page is
// in, and resolves to the reply object.
const pluginCallJS = `(async () => {
  const te = new TextEncoder(), td = new TextDecoder();
  const j = te.encode(JSON.stringify({calls: [{id: 1, fn: "plugin/echo.echo", args: ["hi"]}]}));
  const b = new Uint8Array(4 + j.length + 2);
  new DataView(b.buffer).setUint32(0, j.length);
  b.set(j, 4);
  const r = await fetch("/__krate/call", {method: "POST", headers: {"x-krate-window": globalThis.__KRATE_WINDOW__ || "main"}, body: b});
  const x = new Uint8Array(await r.arrayBuffer());
  const n = new DataView(x.buffer).getUint32(0);
  return JSON.parse(td.decode(x.slice(4, 4 + n)));
})()`

// The plugin executable in a real window: a call from a window that has the
// permission succeeds, and the same call from a window scoped to nothing is
// denied with the plugin permission named.
func TestNativePluginGUI(t *testing.T) {
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		t.Skip("set KRATE_NATIVE_GUI=1 to run the native plugin GUI end-to-end test")
	}
	requireRelease(t)
	for _, scoped := range []bool{false, true} {
		name := "granted"
		if scoped {
			name = "scoped-to-nothing"
		}
		t.Run(name, func(t *testing.T) {
			b := pluginProject(t, echoPageSome, echoPluginDir(t))
			if scoped {
				none := []string{}
				b.Cfg.Native.Window.Permissions = &none
			}
			res, err := b.BuildNativeRelease(NativeReleaseOptions{OutDir: filepath.Join(b.Root, "out"), ExtraTags: []string{"krate_devcontrol"}})
			if err != nil {
				t.Fatal(err)
			}
			s := e2eStartShell(t, res.Binaries[0].Path, "")
			s.waitReady(60 * time.Second)
			var reply struct {
				OK  string `json:"ok"`
				Err *struct {
					Code string         `json:"code"`
					Data map[string]any `json:"data"`
				} `json:"err"`
			}
			if err := s.query(pluginCallJS, &reply); err != nil {
				t.Fatalf("query: %v", err)
			}
			if !scoped && reply.OK != "hi" {
				t.Errorf("granted window: %+v", reply)
			}
			if scoped && (reply.Err == nil || reply.Err.Code != "permission_denied" || reply.Err.Data["permission"] != "plugin:echo.use") {
				t.Errorf("window scoped to nothing: %+v", reply)
			}
			if _, err := s.send("quit", 10*time.Second); err != nil {
				t.Errorf("quit: %v", err)
			}
		})
	}
}
