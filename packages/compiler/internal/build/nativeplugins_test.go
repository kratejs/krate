package build

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/native/plugins"
	krateversion "github.com/kratejs/krate/packages/compiler/internal/version"
	"github.com/kratejs/krate/packages/native/wire"
)

func examplePluginDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "examples", "native-plugin", "native-plugins", "system-info")
}

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

const sysinfoPage = `import { Info, Load } from "krate/plugin/system-info";
export default function Home() { return <main><p>{Load()}</p><button onClick={() => Info()}>go</button></main>; }
`

func TestNativePluginDevBuild(t *testing.T) {
	requireRelease(t)
	b := pluginProject(t, sysinfoPage, examplePluginDir(t))
	out := filepath.Join(b.Root, "out")
	res, err := b.BuildNativeApp(NativeAppOptions{OutDir: out, Dev: true})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	m := res.Manifest
	if strings.Join(m.Plugins, ",") != "system-info" {
		t.Errorf("manifest plugins = %v", m.Plugins)
	}
	// The app lists no permissions, so a dev build keeps the SDK defaults and
	// adds the plugin permissions its code needs.
	perms := strings.Join(m.Permissions, ",")
	for _, want := range []string{"kv", "plugin:system-info.monitor", "plugin:system-info.read"} {
		if !strings.Contains(perms, want) {
			t.Errorf("permissions %s lack %s", perms, want)
		}
	}
	dts, err := os.ReadFile(filepath.Join(b.Root, ".krate", "native", "plugins", "system-info.d.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"export function Info(", "export function CPULoad(", "AsyncIterable<number>", "ReadonlyHostSignal<number>"} {
		if !strings.Contains(string(dts), want) {
			t.Errorf("the plugin's .d.ts lacks %q:\n%s", want, dts)
		}
	}
	ambient, _ := os.ReadFile(filepath.Join(b.Root, ".krate", "native", "plugins", "modules.d.ts"))
	if !strings.Contains(string(ambient), `declare module "krate/plugin/system-info"`) {
		t.Errorf("modules.d.ts:\n%s", ambient)
	}
	if js := readFile(t, filepath.Join(res.AppDir, "dist")); !strings.Contains(js, "plugin/system-info.Info") {
		t.Errorf("the page bundle does not call plugin/system-info.Info")
	}

	// The shell built for the plugins runs the app and serves the plugin's ops.
	shell, err := b.PluginShell(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("PluginShell: %v", err)
	}
	again, err := b.PluginShell(runtime.GOOS, runtime.GOARCH)
	if err != nil || again != shell {
		t.Fatalf("a second PluginShell call: %v %v", again, err)
	}
	app := runBrowserBinary(t, shell, res.AppDir)
	got := app.call(t, "plugin/system-info.Info", nil)
	if !strings.Contains(got, `"ok"`) || !strings.Contains(got, runtime.GOOS) {
		t.Errorf("Info: %s", got)
	}
	if got := app.call(t, "plugin/system-info.Memory", nil); !strings.Contains(got, `"total"`) {
		t.Errorf("Memory: %s", got)
	}
}

func readFile(t *testing.T, dir string) string {
	t.Helper()
	var sb strings.Builder
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (strings.HasSuffix(p, ".js") || strings.HasSuffix(p, ".html")) {
			b, _ := os.ReadFile(p)
			sb.Write(b)
		}
		return nil
	})
	return sb.String()
}

const echoPageSome = `import { Echo, Free } from "krate/plugin/echo";
export default function Home() { return <main><button onClick={() => Echo("hi")}>go</button><i>{String(Free)}</i></main>; }
`

func TestNativePluginReleaseShakesOps(t *testing.T) {
	requireRelease(t)
	b := pluginProject(t, echoPageSome, echoPluginDir(t))
	out := filepath.Join(b.Root, "out")
	bin, res := func() (string, *NativeReleaseResult) {
		res, err := b.BuildNativeRelease(NativeReleaseOptions{OutDir: out})
		if err != nil {
			t.Fatalf("release: %v", err)
		}
		return res.Binaries[0].Path, res
	}()
	// Only Echo and Free are used: the use permission is derived, admin is not,
	// and Shout and Reset are not linked.
	if got := strings.Join(res.Manifest.Permissions, ","); got != "plugin:echo.use" {
		t.Errorf("permissions = %q, want plugin:echo.use", got)
	}
	if len(res.Report.Plugins) != 1 {
		t.Fatalf("report plugins = %+v", res.Report.Plugins)
	}
	rp := res.Report.Plugins[0]
	if sortedJoin(rp.Shaken) != "Reset,Shout" || sortedJoin(rp.Ops) != "Echo,Free,Hand" {
		t.Errorf("report plugin = %+v", rp)
	}
	data, _ := os.ReadFile(filepath.Join(out, "report.json"))
	var rep struct{ Plugins []map[string]any }
	if json.Unmarshal(data, &rep) != nil || len(rep.Plugins) != 1 {
		t.Errorf("report.json: %s", data)
	}
	app := runReleaseBinary(t, bin)
	echoArg := func(e *wire.Encoder) { e.String("hi") }
	if got := app.call(t, "plugin/echo.Echo", echoArg); !strings.Contains(got, `"ok":"hi"`) {
		t.Errorf("Echo: %s", got)
	}
	if got := app.call(t, "plugin/echo.Free", nil); !strings.Contains(got, `"ok":"free"`) {
		t.Errorf("Free: %s", got)
	}
	// The manual op registered from Init, after the start hook ran.
	if got := app.call(t, "plugin/echo.Hand", nil); !strings.Contains(got, `"ok":"started"`) && !strings.Contains(got, `permission_denied`) {
		t.Errorf("Hand: %s", got)
	}
	// Shaken ops are not linked at all.
	for _, op := range []string{"Shout", "Reset"} {
		if got := app.call(t, "plugin/echo."+op, echoArg); !strings.Contains(got, `"not_found"`) {
			t.Errorf("%s was linked into the release executable: %s", op, got)
		}
	}
}

func TestNativePluginReleasePermissionsFollowUse(t *testing.T) {
	requireRelease(t)
	page := `import * as echo from "krate/plugin/echo";
export default function Home() { return <main onClick={() => echo.Reset()}>{String(echo.Echo)}</main>; }
`
	b := pluginProject(t, page, echoPluginDir(t))
	res, err := b.BuildNativeRelease(NativeReleaseOptions{OutDir: filepath.Join(b.Root, "out")})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(res.Manifest.Permissions, ","); got != "plugin:echo.admin,plugin:echo.use" {
		t.Errorf("permissions = %q", got)
	}
	if rp := res.Report.Plugins[0]; sortedJoin(rp.Shaken) != "Free,Shout" {
		t.Errorf("shaken = %v", rp.Shaken)
	}
	app := runReleaseBinary(t, res.Binaries[0].Path)
	if got := app.call(t, "plugin/echo.Shout", func(e *wire.Encoder) { e.String("x") }); !strings.Contains(got, "not_found") {
		t.Errorf("Shout: %s", got)
	}
	if got := app.call(t, "plugin/echo.Reset", nil); !strings.Contains(got, `"ok"`) {
		t.Errorf("Reset: %s", got)
	}
}

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

func TestNativePluginNeedsGo(t *testing.T) {
	b := pluginProject(t, echoPageSome, echoPluginDir(t))
	t.Setenv("PATH", t.TempDir())
	_, err := b.BuildNativeApp(NativeAppOptions{OutDir: filepath.Join(b.Root, "out")})
	if !hasDiagCode(err, diag.CodeNativeNeedGo) {
		t.Errorf("without go on PATH: %v", err)
	}
}

func TestNativePluginReleaseNeverCompileFails(t *testing.T) {
	b := pluginProject(t, echoPageSome, echoPluginDir(t))
	b.Cfg.Native.Release.Compile = "never"
	_, err := b.BuildNativeRelease(NativeReleaseOptions{OutDir: filepath.Join(b.Root, "out")})
	if !hasDiagCode(err, diag.CodeNativeNeedGo) {
		t.Errorf("compile never with plugins: %v", err)
	}
}

func TestNativePluginShellCacheKey(t *testing.T) {
	requireRelease(t)
	calls := fakeGo(t)
	b := pluginProject(t, echoPageSome, echoPluginDir(t))
	if _, err := b.BuildNativeApp(NativeAppOptions{OutDir: filepath.Join(b.Root, "out"), Dev: true}); err != nil {
		t.Fatal(err)
	}
	p1, err := b.PluginShell("linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.PluginShell("linux", "amd64"); err != nil || len(*calls) != 1 {
		t.Fatalf("the second call rebuilt the shell (%d go builds): %v", len(*calls), err)
	}
	p2, _ := b.PluginShell("linux", "arm64")
	if p1 == p2 {
		t.Error("the shell of another platform shares a cache entry")
	}
	if n := len(*calls); n != 2 {
		t.Errorf("go builds = %d, want 2", n)
	}
	// Another Krate version is another shell.
	old := versionForTest(t, "9.9.9")
	defer old()
	p3, _ := b.PluginShell("linux", "amd64")
	if p3 == p1 {
		t.Error("a new Krate version reuses the old shell")
	}
}

func sortedJoin(in []string) string {
	s := append([]string(nil), in...)
	sort.Strings(s)
	return strings.Join(s, ",")
}

func versionForTest(t *testing.T, v string) func() {
	t.Helper()
	old := krateversion.Value
	krateversion.Value = v
	return func() { krateversion.Value = old }
}

func TestNativePluginGlueIsNamespaced(t *testing.T) {
	requireRelease(t)
	b := pluginProject(t, echoPageSome, echoPluginDir(t))
	if _, err := b.BuildNativeApp(NativeAppOptions{OutDir: filepath.Join(b.Root, "out"), Dev: true}); err != nil {
		t.Fatal(err)
	}
	shell := filepath.Join(b.Root, ".krate", "native", "pluginshell")
	glue, err := os.ReadFile(filepath.Join(shell, "plugins", "echo", "glue.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"Echo", "Shout", "Reset", "Free"} {
		if !strings.Contains(string(glue), `r.Func("plugin/echo.`+id+`"`) {
			t.Errorf("glue does not register plugin/echo.%s", id)
		}
	}
	if strings.Contains(string(glue), `"krate/`) || strings.Count(string(glue), "r.Func(") != 4 || strings.Contains(string(glue), `"plugin/echo.Hand"`) {
		t.Errorf("glue registers something else:\n%s", glue)
	}
	plRaw, _ := os.ReadFile(filepath.Join(shell, "plugins", "echo", "plugin.go"))
	pl := strings.Join(strings.Fields(string(plRaw)), " ")
	for _, want := range []string{`Name: "echo",`, `{Name: "use", Doc: "call the echo ops"}`, `{Name: "Hand", Permission: "use", Manual: true}`, "Init: userpkg.Init,", "Register: Register,"} {
		if !strings.Contains(pl, want) {
			t.Errorf("plugin.go lacks %q:\n%s", want, pl)
		}
	}
	reg, _ := os.ReadFile(filepath.Join(shell, "registry", "registry.go"))
	if !strings.Contains(string(reg), "krate.local/pluginshell/plugins/echo") || !strings.Contains(string(reg), "p0.Plugin") {
		t.Errorf("registry.go:\n%s", reg)
	}
	gomod, _ := os.ReadFile(filepath.Join(shell, "go.mod"))
	for _, want := range []string{"module krate.local/pluginshell", "example.com/krate-plugin-echo v0.0.0", "replace example.com/krate-plugin-echo =>", "replace github.com/kratejs/krate/packages/native =>"} {
		if !strings.Contains(string(gomod), want) {
			t.Errorf("go.mod lacks %q:\n%s", want, gomod)
		}
	}
	// The TypeScript module types the manual op as untyped.
	dts, _ := os.ReadFile(filepath.Join(b.Root, ".krate", "native", "plugins", "echo.d.ts"))
	if !strings.Contains(string(dts), "export function Hand(...args: unknown[]): Promise<unknown>;") || !strings.Contains(string(dts), "export function Echo(") {
		t.Errorf("echo.d.ts:\n%s", dts)
	}
}

func TestFormatNativeCapabilitiesNamesPlugins(t *testing.T) {
	requireRelease(t)
	b := pluginProject(t, echoPageSome, echoPluginDir(t))
	res, ds, err := b.AnalyzeNativeCapabilities()
	if err != nil || len(ds) > 0 {
		t.Fatalf("AnalyzeNativeCapabilities: %v %v", err, ds)
	}
	out := FormatNativeCapabilities(res, nil)
	for _, want := range []string{"plugin:echo.use  (plugin echo)", "plugin echo: ops used: Echo, Free", "src/pages/index.tsx"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "plugin:echo.admin") {
		t.Errorf("an unused permission is listed:\n%s", out)
	}
}

func TestNativePluginScaffoldBuilds(t *testing.T) {
	requireRelease(t)
	dir := filepath.Join(t.TempDir(), "native-plugins", "my-tool")
	native := filepath.Join(repoRoot(t), "packages", "native")
	if _, d := plugins.Scaffold(dir, plugins.ScaffoldOptions{Name: "my-tool", NativeReplace: native}); d != nil {
		t.Fatal(d)
	}
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOWORK", "off")
	for _, args := range [][]string{{"vet", "./..."}, {"test", "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %v in the scaffolded plugin: %v\n%s", args, err, out)
		}
	}
	page := `import { Hello, Ticks } from "krate/plugin/my-tool";
export default function P() { return <b onClick={() => { Hello("x"); Ticks(100); }}>x</b>; }
`
	b := pluginProject(t, page, dir)
	res, err := b.BuildNativeRelease(NativeReleaseOptions{OutDir: filepath.Join(b.Root, "out")})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(res.Manifest.Permissions, ","); got != "plugin:my-tool.use" {
		t.Errorf("permissions = %q", got)
	}
	app := runReleaseBinary(t, res.Binaries[0].Path)
	if got := app.call(t, "plugin/my-tool.Hello", func(e *wire.Encoder) { e.String("Ada") }); !strings.Contains(got, "Hello, Ada, from the my-tool plugin") {
		t.Errorf("Hello: %s", got)
	}
}

// An app with Go host code and plugins is still one executable: the main
// package lives in the app's module and requires the plugin shell module.
func TestNativePluginReleaseWithGoHostCode(t *testing.T) {
	requireRelease(t)
	root := goOnlyProject(t)
	writeTree(t, root, map[string]string{"src/pages/index.tsx": `import { Names } from '../lib/store.go';
import { Echo } from "krate/plugin/echo";
export default function P() { return <button onClick={() => { Names(); Echo("x"); }}>go</button>; }
`})
	b := releaseBuilder(t, root, func(n *config.NativeConfig) { n.Plugins = []string{echoPluginDir(t)} })
	res, err := b.BuildNativeRelease(NativeReleaseOptions{OutDir: filepath.Join(root, "out")})
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if got := strings.Join(res.Manifest.Permissions, ","); got != "plugin:echo.use" {
		t.Errorf("permissions = %q", got)
	}
	app := runReleaseBinary(t, res.Binaries[0].Path)
	if got := app.call(t, "src/lib/store.Names", nil); !strings.Contains(got, `"ok"`) {
		t.Errorf("Go host call: %s", got)
	}
	if got := app.call(t, "plugin/echo.Echo", func(e *wire.Encoder) { e.String("both") }); !strings.Contains(got, `"ok":"both"`) {
		t.Errorf("plugin call: %s", got)
	}
}

// The layout "shell" with plugins: the app directory plus a shell built for it.
func TestNativePluginShellLayout(t *testing.T) {
	requireRelease(t)
	b := pluginProject(t, echoPageSome, echoPluginDir(t))
	res, err := b.BuildNativeApp(NativeAppOptions{OutDir: filepath.Join(b.Root, "out"), Derive: true, Distribute: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.DistExe == "" || res.ShellPath == "" {
		t.Fatalf("no distributable: %+v", res)
	}
	app := runBrowserBinary(t, res.DistExe)
	if got := app.call(t, "plugin/echo.Echo", func(e *wire.Encoder) { e.String("shell") }); !strings.Contains(got, `"ok":"shell"`) {
		t.Errorf("Echo through the shell layout: %s", got)
	}
	// A derived build (krate native build) shakes unused ops like a release.
	if got := app.call(t, "plugin/echo.Shout", func(e *wire.Encoder) { e.String("x") }); !strings.Contains(got, "not_found") {
		t.Errorf("an unused op is linked into the shell layout's shell: %s", got)
	}
}

func TestDevRestartsOnPluginGoChange(t *testing.T) {
	root := t.TempDir()
	local := filepath.Join(root, "native-plugins", "hello")
	remote := filepath.Join(root, "node_modules", "@acme", "p")
	b := &Builder{Root: root, nativePlugins: &nativePluginState{list: []*plugins.Plugin{
		{Manifest: plugins.Manifest{Name: "hello"}, Dir: local, Local: true},
		{Manifest: plugins.Manifest{Name: "p"}, Dir: remote, Local: false},
	}}}
	nd := &nativeDev{b: b}
	for _, c := range []struct {
		path string
		want bool
	}{
		{filepath.Join(local, "hello.go"), true},
		{filepath.Join(local, "util", "x.go"), true},
		{filepath.Join(local, "go.mod"), true},
		{filepath.Join(local, "krate-plugin.json"), true},
		{filepath.Join(local, "hello_test.go"), false},
		{filepath.Join(local, "README.md"), false},
		{filepath.Join(root, "src", "lib", "tasks.go"), false},
		{filepath.Join(remote, "p.go"), false},
	} {
		if got := nd.changedPlugin([]string{c.path}) != ""; got != c.want {
			t.Errorf("changedPlugin(%s) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestNativePluginUnboundOp(t *testing.T) {
	requireRelease(t)
	dir := filepath.Join(t.TempDir(), "echo")
	for _, name := range []string{"go.mod", "echo.go"} {
		b, err := os.ReadFile(filepath.Join(echoPluginDir(t), name))
		if err != nil {
			t.Fatal(err)
		}
		writeTree(t, dir, map[string]string{name: string(b)})
	}
	m, _ := os.ReadFile(filepath.Join(echoPluginDir(t), "krate-plugin.json"))
	writeTree(t, dir, map[string]string{"krate-plugin.json": strings.Replace(string(m), `{ "name": "Free" }`, `{ "name": "Free" }, { "name": "Missing", "permission": "use" }`, 1)})
	b := pluginProject(t, echoPageSome, dir)
	_, err := b.BuildNativeApp(NativeAppOptions{OutDir: filepath.Join(b.Root, "out"), Dev: true})
	if !hasDiagCode(err, diag.CodePluginOp) {
		t.Fatalf("a listed op that does not exist: %v", err)
	}
	for _, d := range DiagnosticsFromError(err) {
		if d.Code == diag.CodePluginOp && !strings.Contains(d.Message, "Missing") {
			t.Errorf("message %q does not name the op", d.Message)
		}
	}
}

// pluginCallJS calls plugin/echo.Echo from the page, as the window the page is
// in, and resolves to the reply object.
const pluginCallJS = `(async () => {
  const te = new TextEncoder(), td = new TextDecoder();
  const j = te.encode(JSON.stringify({calls: [{id: 1, fn: "plugin/echo.Echo", args: ["hi"]}]}));
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
