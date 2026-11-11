package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
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
