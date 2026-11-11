package build

import (
	"bufio"
	"bytes"
	"context"
	"debug/pe"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/version"
	"github.com/kratejs/krate/packages/native/wire"
)

// areaModules extends fakeKrateModules with the @krate/app area modules the
// release fixtures import, reduced to the exports the code names.
func areaModules() map[string]string {
	m := map[string]string{}
	for k, v := range fakeKrateModules {
		m[k] = v
	}
	m["node_modules/@krate/app/package.json"] = `{"name":"@krate/app","version":"0.0.0","type":"module","exports":{".":"./index.js","./host":"./host.js","./kv":"./kv.js","./fs":"./fs.js","./tray":"./tray.js","./os":"./os.js","./clipboard":"./clipboard.js"}}`
	m["node_modules/@krate/app/kv.js"] = "export const get = () => {}; export const set = () => {}; export const keys = () => {};\n"
	m["node_modules/@krate/app/fs.js"] = "export const readText = () => {}; export const writeText = () => {};\n"
	m["node_modules/@krate/app/tray.js"] = "export const set = () => {}; export const remove = () => {};\n"
	m["node_modules/@krate/app/os.js"] = "export const secretGet = () => {}; export const info = () => {};\n"
	m["node_modules/@krate/app/clipboard.js"] = "export const readText = () => {}; export const writeText = () => {};\n"
	return m
}

func releaseNativeDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "packages", "native")
}

func goMod(t *testing.T) string {
	return "module example.com/rel\n\ngo 1.26.1\n\nrequire github.com/kratejs/krate/packages/native v0.0.0\n\nreplace github.com/kratejs/krate/packages/native => " + filepath.ToSlash(releaseNativeDir(t)) + "\n"
}

// tsOnlyProject is an app with TypeScript host code only.
func tsOnlyProject(t *testing.T, extraUI string) string {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, areaModules())
	writeTree(t, root, map[string]string{
		"package.json":          `{"name":"rel-ts","type":"module"}`,
		"src/lib/greet.host.ts": "export function greet(name: string): string { return 'hello ' + name; }\n",
		"src/pages/index.tsx": "import { greet } from '../lib/greet.host';\nimport { keys } from '@krate/app/kv';\n" + extraUI +
			"export default function P() { return <button onClick={() => { greet('x'); keys(); }}>go</button>; }\n",
	})
	return root
}

// goOnlyProject is an app with Go host code only.
func goOnlyProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, areaModules())
	writeTree(t, root, map[string]string{
		"package.json":     `{"name":"rel-go","type":"module"}`,
		"go.mod":           goMod(t),
		"src/lib/store.go": "package lib\n\nfunc Names() []string { return []string{\"a\", \"b\"} }\n",
		"src/pages/index.tsx": `import { Names } from '../lib/store.go';
export default function P() { return <button onClick={() => Names()}>go</button>; }
`,
	})
	return root
}

func releaseBuilder(t *testing.T, root string, mut func(n *config.NativeConfig)) *Builder {
	t.Helper()
	// Projects live in a temp dir, outside the monorepo: point the generated
	// module at this checkout's native module.
	t.Setenv("KRATE_NATIVE_DIR", releaseNativeDir(t))
	b := newNativeBuilder(t, root)
	if mut != nil {
		mut(b.Cfg.Native)
	}
	return b
}

func requireRelease(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("release builds link executables")
	}
	if _, err := exec.LookPath("go"); err != nil {
		requireE2E(t, "go is not installed")
	}
	t.Setenv("GOWORK", "off")
}

// fakeGo replaces the go build of a release build with a stub that records its
// arguments and writes an empty output file.
func fakeGo(t *testing.T) *[][]string {
	t.Helper()
	var calls [][]string
	old := NativeGoRunner
	NativeGoRunner = func(dir string, env []string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		for i, a := range args {
			if a == "-o" && i+1 < len(args) {
				if err := os.WriteFile(args[i+1], []byte("stub"), 0o755); err != nil {
					return nil, err
				}
			}
		}
		return nil, nil
	}
	t.Cleanup(func() { NativeGoRunner = old })
	return &calls
}

func argValue(args []string, prefix string) string {
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			return strings.TrimPrefix(a, prefix)
		}
	}
	return ""
}

func TestNativeReleaseDerivesPermissionsAndReport(t *testing.T) {
	calls := fakeGo(t)
	root := tsOnlyProject(t, "import { readText } from '@krate/app/fs';\nvoid readText;\n")
	writeTree(t, root, map[string]string{
		"src/lib/secret.host.ts": "import { secretGet } from '@krate/app/os';\nexport async function token() { return secretGet('t'); }\n",
	})
	b := releaseBuilder(t, root, nil)
	res, err := b.BuildNativeRelease(NativeReleaseOptions{OutDir: filepath.Join(root, "out"), Targets: []string{"linux/amd64"}})
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if !res.Compiled || len(res.Binaries) != 1 || len(*calls) != 1 {
		t.Fatalf("compiled=%v binaries=%v calls=%d", res.Compiled, res.Binaries, len(*calls))
	}
	want := []string{"fs:read", "kv", "os:secrets.read"}
	if strings.Join(res.Manifest.Permissions, ",") != strings.Join(want, ",") || !res.Manifest.StrictPermissions {
		t.Fatalf("manifest permissions = %v strict=%v", res.Manifest.Permissions, res.Manifest.StrictPermissions)
	}
	args := (*calls)[0]
	tags := argValue(args, "-tags=")
	for _, w := range []string{"krate_no_tray", "krate_no_shortcut", "krate_no_window", "krate_no_dialog"} {
		if !strings.Contains(tags, w) {
			t.Errorf("tags %q lack %s", tags, w)
		}
	}
	for _, w := range []string{"krate_no_fs", "krate_no_kv", "krate_no_db", "krate_no_hostjs"} {
		if strings.Contains(tags, w) {
			t.Errorf("tags %q has %s", tags, w)
		}
	}
	ld := argValue(args, "-ldflags=")
	if !strings.Contains(ld, "-s -w") || strings.Contains(ld, "windowsgui") || !strings.Contains(ld, "shell.BuildInfo=Acme 1.0.0") {
		t.Errorf("ldflags = %q", ld)
	}
	if !has(args, "-trimpath") {
		t.Errorf("args %v lack -trimpath", args)
	}

	data, err := os.ReadFile(filepath.Join(root, "out", "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rep NativeReport
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Layout != "executable" || rep.AppID != "com.acme.app" || !rep.HostJS || rep.GoHost {
		t.Fatalf("report header: %+v", rep)
	}
	var names []string
	sites := map[string][]string{}
	for _, p := range rep.Permissions {
		names = append(names, p.Name)
		for _, s := range p.Sites {
			sites[p.Name] = append(sites[p.Name], s.Kind+":"+s.File)
		}
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("report permissions = %v", names)
	}
	if got := sites["os:secrets.read"]; len(got) != 1 || got[0] != "host:src/lib/secret.host.ts" {
		t.Errorf("secrets sites = %v", got)
	}
	if got := sites["fs:read"]; len(got) != 1 || got[0] != "ui:src/pages/index.tsx" {
		t.Errorf("fs sites = %v", got)
	}
	if strings.Join(rep.Areas.Linked, ",") != "db,fs,kv" || !has(rep.Areas.Shaken, "tray") || has(rep.Areas.Shaken, "kv") {
		t.Errorf("areas = %+v", rep.Areas)
	}
	if len(rep.Targets) != 1 || rep.Targets[0].Target != "linux/amd64" || rep.Targets[0].Binary != "linux-amd64/Acme" || rep.Targets[0].Size != 4 {
		t.Errorf("targets = %+v", rep.Targets)
	}

	// The embedded manifest is the derived one.
	app := readNativeManifest(t, filepath.Join(b.nativeOutDir(), "release", "stage", "app"))
	if got := nativeStrings(app["permissions"]); strings.Join(got, ",") != strings.Join(want, ",") || app["strictPermissions"] != true {
		t.Errorf("app.json permissions = %v strict=%v", got, app["strictPermissions"])
	}
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestNativeReleaseAllowlistViolation(t *testing.T) {
	fakeGo(t)
	root := tsOnlyProject(t, "import { readText } from '@krate/app/fs';\nvoid readText;\n")
	b := releaseBuilder(t, root, func(n *config.NativeConfig) { n.Permissions = []string{"kv"} })
	_, err := b.BuildNativeRelease(NativeReleaseOptions{OutDir: filepath.Join(root, "out")})
	if err == nil || !hasDiagCode(err, diag.CodeNativePermission) {
		t.Fatalf("want KR7210, got %v", err)
	}
	msg := BuildErrorSummary(err) + err.Error()
	var found bool
	var me *multiError
	if asMulti(err, &me) {
		for _, e := range me.errs {
			if d, ok := e.(diag.Diagnostic); ok && d.Code == diag.CodeNativePermission {
				found = true
				if d.File != "src/pages/index.tsx" || d.Line != 3 || !strings.Contains(d.Message, `"fs:read"`) || !strings.Contains(d.Message, "readText") || d.Hint == "" {
					t.Errorf("diagnostic = %+v", d)
				}
			}
		}
	}
	if !found {
		t.Fatalf("no KR7210 diagnostic in %q", msg)
	}
	// An allowlist that covers the code builds, and fs covers fs:read.
	b = releaseBuilder(t, root, func(n *config.NativeConfig) { n.Permissions = []string{"kv", "fs"} })
	if _, err := b.BuildNativeRelease(NativeReleaseOptions{OutDir: filepath.Join(root, "out")}); err != nil {
		t.Fatalf("covered allowlist: %v", err)
	}
}

func asMulti(err error, target **multiError) bool {
	if m, ok := err.(*multiError); ok {
		*target = m
		return true
	}
	return false
}

func TestNativeBuildDerivesFromGoHostIsNotAPermission(t *testing.T) {
	root := goOnlyProject(t)
	writeTree(t, root, map[string]string{
		"src/lib/store.go": "package lib\n\nimport \"github.com/kratejs/krate/packages/native/sdk/tray\"\n\nvar _ = tray.Remove\n\nfunc Names() []string { return nil }\n",
	})
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	b := releaseBuilder(t, root, nil)
	res, err := b.BuildNativeApp(NativeAppOptions{Derive: true, SkipGoCompile: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Manifest.Permissions) != 0 || !res.Manifest.StrictPermissions {
		t.Fatalf("permissions = %v", res.Manifest.Permissions)
	}
	c := res.Capabilities
	if strings.Join(c.GoAreas, ",") != "tray" || strings.Join(c.Linked(), ",") != "menu,tray" {
		t.Fatalf("go areas %v linked %v", c.GoAreas, c.Linked())
	}
}

func TestNativeBuildWindowScopeIsIntersected(t *testing.T) {
	root := tsOnlyProject(t, "")
	scope := []string{"kv", "tray"}
	b := releaseBuilder(t, root, func(n *config.NativeConfig) { n.Window.Permissions = &scope })
	res, err := b.BuildNativeApp(NativeAppOptions{Derive: true})
	if err != nil {
		t.Fatal(err)
	}
	if wp := res.Manifest.Window.Permissions; wp == nil || strings.Join(*wp, ",") != "kv" {
		t.Fatalf("window permissions = %v", res.Manifest.Window.Permissions)
	}
	// Without Derive the configured list passes through.
	res, err = releaseBuilder(t, root, func(n *config.NativeConfig) { n.Window.Permissions = &scope; n.Permissions = []string{"kv", "tray"} }).BuildNativeApp(NativeAppOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Manifest.Permissions, ",") != "kv,tray" || res.Manifest.StrictPermissions {
		t.Fatalf("plain build permissions = %v strict=%v", res.Manifest.Permissions, res.Manifest.StrictPermissions)
	}
}

func TestNativeReleaseCompileModes(t *testing.T) {
	goDir := t.TempDir() // an empty PATH entry: no go command
	cases := []struct {
		name     string
		mode     string
		haveGo   bool
		compiled bool
		wantCode diag.Code
		note     string
	}{
		{"auto with go compiles", "auto", true, true, "", ""},
		{"auto without go falls back", "auto", false, false, "", "go command was not found"},
		{"always with go compiles", "always", true, true, "", ""},
		{"always without go fails", "always", false, false, diag.CodeNativeNeedGo, ""},
		{"never with go keeps the layout", "never", true, false, "", ""},
		{"never without go keeps the layout", "never", false, false, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calls := fakeGo(t)
			if !c.haveGo {
				t.Setenv("PATH", goDir)
			}
			root := tsOnlyProject(t, "")
			b := releaseBuilder(t, root, func(n *config.NativeConfig) { n.Release.Compile = c.mode })
			res, err := b.BuildNativeRelease(NativeReleaseOptions{OutDir: filepath.Join(root, "out")})
			if c.wantCode != "" {
				if err == nil || !hasDiagCode(err, c.wantCode) {
					t.Fatalf("want %s, got %v", c.wantCode, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.Compiled != c.compiled {
				t.Fatalf("compiled = %v", res.Compiled)
			}
			if c.compiled {
				if len(*calls) != 1 || res.AppDir != "" {
					t.Fatalf("calls=%d appdir=%q", len(*calls), res.AppDir)
				}
			} else {
				if len(*calls) != 0 {
					t.Fatalf("go build ran: %v", *calls)
				}
				if _, err := os.Stat(filepath.Join(res.AppDir, "app.json")); err != nil {
					t.Fatalf("no app dir: %v", err)
				}
				if _, err := os.Stat(filepath.Join(root, "out", "report.json")); err != nil {
					t.Fatalf("no report: %v", err)
				}
				var rep NativeReport
				data, _ := os.ReadFile(filepath.Join(root, "out", "report.json"))
				_ = json.Unmarshal(data, &rep)
				if rep.Layout != "app-dir" {
					t.Fatalf("layout = %q", rep.Layout)
				}
			}
			if c.note != "" && !strings.Contains(strings.Join(res.Notes, "\n"), c.note) {
				t.Fatalf("notes = %v", res.Notes)
			}
		})
	}
}

func TestNativeGoHostCodeNeedsGo(t *testing.T) {
	root := goOnlyProject(t)
	t.Setenv("PATH", t.TempDir())
	for _, mode := range []string{"auto", "never"} {
		b := releaseBuilder(t, root, func(n *config.NativeConfig) { n.Release.Compile = mode })
		_, err := b.BuildNativeRelease(NativeReleaseOptions{OutDir: filepath.Join(root, "out")})
		if err == nil || !hasDiagCode(err, diag.CodeNativeNeedGo) {
			t.Fatalf("%s: want KR7212, got %v", mode, err)
		}
	}
}

func TestNativeReleasePGOFlag(t *testing.T) {
	calls := fakeGo(t)
	root := tsOnlyProject(t, "")

	b := releaseBuilder(t, root, nil)
	res, err := b.BuildNativeRelease(NativeReleaseOptions{OutDir: filepath.Join(root, "out")})
	if err != nil {
		t.Fatal(err)
	}
	if argValue((*calls)[0], "-pgo=") != "" || res.PGO != "" || len(res.Notes) != 0 {
		t.Fatalf("no profile exists: args %v notes %v", (*calls)[0], res.Notes)
	}

	// The default path, native.pgo in the project root.
	prof := filepath.Join(root, "native.pgo")
	if err := os.WriteFile(prof, []byte("profile"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = releaseBuilder(t, root, nil).BuildNativeRelease(NativeReleaseOptions{OutDir: filepath.Join(root, "out")})
	if err != nil {
		t.Fatal(err)
	}
	if got := argValue((*calls)[1], "-pgo="); got != prof || res.PGO != prof {
		t.Fatalf("pgo arg = %q, result %q", got, res.PGO)
	}

	// A configured path that does not exist is a note, not an error.
	_ = os.Remove(prof)
	res, err = releaseBuilder(t, root, func(n *config.NativeConfig) { n.Release.PGO = "profiles/app.pgo" }).BuildNativeRelease(NativeReleaseOptions{OutDir: filepath.Join(root, "out")})
	if err != nil {
		t.Fatal(err)
	}
	if argValue((*calls)[2], "-pgo=") != "" || !strings.Contains(strings.Join(res.Notes, "\n"), "profiles/app.pgo") {
		t.Fatalf("missing configured profile: args %v notes %v", (*calls)[2], res.Notes)
	}
}

func TestNativeReleaseWindowsFlagsAndTargets(t *testing.T) {
	calls := fakeGo(t)
	root := tsOnlyProject(t, "")
	b := releaseBuilder(t, root, nil)
	res, err := b.BuildNativeRelease(NativeReleaseOptions{OutDir: filepath.Join(root, "out"), Targets: []string{"windows/amd64", "darwin/arm64", "macos/arm64", "linux/arm64"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Binaries) != 3 {
		t.Fatalf("binaries = %+v (macos/arm64 duplicates darwin/arm64)", res.Binaries)
	}
	var paths []string
	for _, b := range res.Binaries {
		paths = append(paths, filepath.ToSlash(mustRel(res.OutDir, b.Path)))
	}
	if strings.Join(paths, ",") != "windows-amd64/Acme.exe,darwin-arm64/Acme,linux-arm64/Acme" {
		t.Fatalf("paths = %v", paths)
	}
	if !strings.Contains(argValue((*calls)[0], "-ldflags="), "-H windowsgui") || strings.Contains(argValue((*calls)[1], "-ldflags="), "windowsgui") {
		t.Errorf("windowsgui only on windows: %v / %v", (*calls)[0], (*calls)[1])
	}
	if _, err := b.BuildNativeRelease(NativeReleaseOptions{Targets: []string{"plan9/amd64"}}); err == nil || !hasDiagCode(err, diag.CodeNativeTarget) {
		t.Fatalf("unknown target: %v", err)
	}
}

func TestNativeModuleVersion(t *testing.T) {
	old := versionValue()
	defer setVersionValue(old)
	t.Setenv("KRATE_NATIVE_VERSION", "")
	setVersionValue("dev")
	if _, ok := NativeModuleVersion(); ok {
		t.Fatal("dev builds have no version")
	}
	setVersionValue("0.5.0-beta.3")
	if v, ok := NativeModuleVersion(); !ok || v != "v0.5.0-beta.3" {
		t.Fatalf("version = %q %v", v, ok)
	}
	t.Setenv("KRATE_NATIVE_VERSION", "0.9.1")
	if v, _ := NativeModuleVersion(); v != "v0.9.1" {
		t.Fatalf("override = %q", v)
	}
}

// Outside the monorepo the generated module requires the module version that
// matches the compiler; a development compiler has none (KR7213).
func TestNativeReleaseModuleOutsideMonorepo(t *testing.T) {
	fakeGo(t)
	old := versionValue()
	defer setVersionValue(old)
	t.Setenv("KRATE_NATIVE_VERSION", "")
	setVersionValue("dev")
	root := tsOnlyProject(t, "")
	// FindMonorepoNative walks up from the project; a temp dir has no checkout.
	b := releaseBuilder(t, root, nil)
	t.Setenv("KRATE_NATIVE_DIR", "")
	_, err := b.BuildNativeRelease(NativeReleaseOptions{OutDir: filepath.Join(root, "out")})
	if err == nil || !hasDiagCode(err, diag.CodeNativeModuleVersion) {
		t.Fatalf("want KR7213, got %v", err)
	}
	setVersionValue("1.2.3")
	b = releaseBuilder(t, root, nil)
	t.Setenv("KRATE_NATIVE_DIR", "")
	if _, err := b.BuildNativeRelease(NativeReleaseOptions{OutDir: filepath.Join(root, "out")}); err != nil {
		t.Fatal(err)
	}
	gomod, err := os.ReadFile(filepath.Join(root, ".krate", "native", "release", "mod", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gomod), "require "+nativeGoModulePath+" v1.2.3") || strings.Contains(string(gomod), "replace "+nativeGoModulePath) {
		t.Fatalf("go.mod:\n%s", gomod)
	}
	// The module file set is the embedded copy of the native module's.
	if !strings.Contains(string(gomod), "modernc.org/quickjs") {
		t.Fatalf("go.mod lacks the native module's requirements:\n%s", gomod)
	}
}

// The embedded copies of the native module's go.mod and go.sum, which a
// generated module outside the monorepo starts from, match the module's.
func TestNativeModCopiesMatch(t *testing.T) {
	for name, have := range map[string]string{"go.mod": nativeModGoMod, "go.sum": nativeModGoSum} {
		want, err := os.ReadFile(filepath.Join(releaseNativeDir(t), name))
		if err != nil {
			t.Fatal(err)
		}
		if string(want) != have {
			t.Errorf("internal/build/nativemod/%s.txt is stale; run: cp packages/native/%s packages/compiler/internal/build/nativemod/%s.txt", name, name, name)
		}
	}
}

func TestReleaseMainGeneration(t *testing.T) {
	ts := releaseMain(nil, false)
	if !strings.Contains(ts, "shell.Main(shell.MustZipFS(appZip), nil)") || strings.Contains(ts, "scheme") {
		t.Errorf("TS-only main:\n%s", ts)
	}
	g := releaseMain([]string{"example.com/rel/.krate/native/src/lib", "example.com/rel/.krate/native/other"}, false)
	for _, w := range []string{"//go:embed app.zip", "runtime.LockOSThread()", "glue0.Register(r)", "glue1.Register(r)", `"github.com/kratejs/krate/packages/native/scheme"`} {
		if !strings.Contains(g, w) {
			t.Errorf("main lacks %q:\n%s", w, g)
		}
	}
}

// --- real executables ---

type releaseApp struct {
	url  string
	base string
	c    *http.Client
	cmd  *exec.Cmd
}

// runReleaseBinary starts an executable with --browser and waits for ready.
func runReleaseBinary(t *testing.T, bin string) *releaseApp {
	t.Helper()
	return runBrowserBinary(t, bin)
}

// runBrowserBinary is runReleaseBinary with more arguments (the app directory
// of a shell that is not a release executable).
func runBrowserBinary(t *testing.T, bin string, extra ...string) *releaseApp {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"--browser"}, extra...)...)
	cmd.Stderr = &testLogWriter{t: t}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %s: %v", bin, err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})
	lines := make(chan string, 8)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	var url string
	for ready := false; !ready; {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatal("the executable closed stdout before ready")
			}
			if strings.HasPrefix(l, "url ") {
				url = strings.TrimPrefix(l, "url ")
			} else if l == "ready" {
				ready = true
			}
		case <-time.After(60 * time.Second):
			t.Fatal("the executable never became ready")
		}
	}
	if url == "" {
		t.Fatal("browser mode printed no url")
	}
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	resp, err := c.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return &releaseApp{url: url, base: strings.SplitN(url, "/?", 2)[0], c: c, cmd: cmd}
}

func (a *releaseApp) get(t *testing.T, path string) (int, string) {
	t.Helper()
	resp, err := a.c.Get(a.base + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// call invokes a host id and returns the JSON of the reply object.
func (a *releaseApp) call(t *testing.T, fn string, args func(*wire.Encoder)) string {
	t.Helper()
	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("calls")
	e.BeginArray()
	e.BeginObject()
	e.Key("id")
	e.Int(1)
	e.Key("fn")
	e.String(fn)
	e.Key("args")
	e.BeginArray()
	if args != nil {
		args(e)
	}
	e.EndArray()
	e.EndObject()
	e.EndArray()
	e.EndObject()
	resp, err := a.c.Post(a.base+"/__krate/call", "application/x-krate-frames", bytes.NewReader(e.Frame()))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	i, j := bytes.IndexByte(b, '{'), bytes.LastIndexByte(b, '}')
	if i < 0 || j < i {
		t.Fatalf("call %s: status %d body %q", fn, resp.StatusCode, b)
	}
	return string(b[i : j+1])
}

// goTool runs a go command and returns its output.
func goTool(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("go", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("go %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestNativeReleaseTSOnlyRuns(t *testing.T) {
	requireRelease(t)
	root := tsOnlyProject(t, "")
	b := releaseBuilder(t, root, nil)
	res, err := b.BuildNativeRelease(NativeReleaseOptions{OutDir: filepath.Join(root, "out")})
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	bin := res.Binaries[0].Path
	t.Logf("TS-only release executable: %s", sizeMB(res.Binaries[0].Size))
	info := goTool(t, "version", "-m", bin)
	if !strings.Contains(info, "modernc.org/quickjs") {
		t.Errorf("an app with TypeScript host code must link QuickJS:\n%s", info)
	}
	if !strings.Contains(info, "-trimpath=true") || !strings.Contains(info, "CGO_ENABLED=0") {
		t.Errorf("build settings:\n%s", info)
	}

	id := b.NativeHost().Registered[0].ID
	app := runReleaseBinary(t, bin)
	if code, body := app.get(t, "/"); code != 200 || !strings.Contains(body, "<button") {
		t.Fatalf("index: %d %q", code, body)
	}
	if got := app.call(t, id, func(e *wire.Encoder) { e.String("world") }); !strings.Contains(got, "hello world") {
		t.Fatalf("host call %s: %s", id, got)
	}
	// Derived permissions: kv only.
	if got := app.call(t, "krate/kv.keys", nil); strings.Contains(got, "permission_denied") {
		t.Fatalf("kv should be granted: %s", got)
	}
	if got := app.call(t, "krate/db.query", func(e *wire.Encoder) { e.String("select 1") }); !strings.Contains(got, "permission_denied") {
		t.Fatalf("db should be denied: %s", got)
	}
	if got := app.call(t, "krate/os.info", nil); !strings.Contains(got, "permission_denied") {
		t.Fatalf("os:info is not needed by this app: %s", got)
	}
	if out, err := exec.Command(bin, "--version").Output(); err != nil || strings.TrimSpace(string(out)) != "Acme 1.0.0" {
		t.Errorf("--version = %q %v", out, err)
	}
}

func sizeMB(n int64) string { return FormatBytes(n) }

func TestNativeReleaseGoOnlyHasNoQuickJSAndShakesTray(t *testing.T) {
	requireRelease(t)
	root := goOnlyProject(t)
	b := releaseBuilder(t, root, nil)
	res, err := b.BuildNativeRelease(NativeReleaseOptions{OutDir: filepath.Join(root, "out")})
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	goOnlySize := res.Binaries[0].Size
	// Symbols are stripped from the shipped executable; a second build keeps
	// them so nm can look inside.
	sym, err := releaseBuilder(t, root, nil).BuildNativeRelease(NativeReleaseOptions{OutDir: filepath.Join(root, "out-sym"), KeepSymbols: true})
	if err != nil {
		t.Fatalf("release with symbols: %v", err)
	}
	bin := sym.Binaries[0].Path
	t.Logf("Go-only release executable (no TS host, nothing from the SDK): %s", sizeMB(goOnlySize))

	info := goTool(t, "version", "-m", bin)
	for _, bad := range []string{"quickjs", "libquickjs"} {
		if strings.Contains(info, bad) {
			t.Errorf("a Go-only app must not link %s:\n%s", bad, info)
		}
	}
	if !strings.Contains(argsTags(t, info), "krate_no_hostjs") {
		t.Errorf("tags in build info: %s", argsTags(t, info))
	}
	nm := goTool(t, "tool", "nm", bin)
	for _, bad := range []string{"modernc.org/quickjs", "sdk/tray.", "sdk/shortcut.", "sdk/notify.", "krate/hostjs.", "jsruntime."} {
		if strings.Contains(nm, bad) {
			t.Errorf("symbol %q is linked into a Go-only app", bad)
		}
	}
	if !strings.Contains(nm, "packages/native/scheme.") {
		t.Errorf("the scheme handler is missing")
	}

	app := runReleaseBinary(t, bin)
	if code, _ := app.get(t, "/"); code != 200 {
		t.Fatalf("index: %d", code)
	}
	got := app.call(t, b.NativeGo().Modules[0].Funcs[0], nil)
	if !strings.Contains(got, `"a"`) || !strings.Contains(got, `"b"`) {
		t.Fatalf("Go host call: %s", got)
	}
	// Nothing needed: even the default permissions are gone.
	if got := app.call(t, "krate/kv.keys", nil); !strings.Contains(got, "not_found") && !strings.Contains(got, "permission_denied") {
		t.Fatalf("kv should be unreachable: %s", got)
	}

	// The same app with a tray import links tray code and is larger.
	trayRoot := goOnlyProject(t)
	writeTree(t, trayRoot, map[string]string{
		"src/pages/index.tsx": `import { Names } from '../lib/store.go';
import { set } from '@krate/app/tray';
export default function P() { return <button onClick={() => { Names(); set({}); }}>go</button>; }
`,
	})
	tres, err := releaseBuilder(t, trayRoot, nil).BuildNativeRelease(NativeReleaseOptions{OutDir: filepath.Join(trayRoot, "out"), KeepSymbols: true})
	if err != nil {
		t.Fatalf("tray release: %v", err)
	}
	t.Logf("Go-only release executable with tray (symbols kept): %s vs %s without tray (symbols kept)", sizeMB(tres.Binaries[0].Size), sizeMB(sym.Binaries[0].Size))
	if tres.Binaries[0].Size <= sym.Binaries[0].Size {
		t.Errorf("linking tray did not grow the executable (%d <= %d)", tres.Binaries[0].Size, sym.Binaries[0].Size)
	}
	if !strings.Contains(goTool(t, "tool", "nm", tres.Binaries[0].Path), "sdk/tray.") {
		t.Errorf("an app that uses tray lacks tray symbols")
	}
	if got := strings.Join(tres.Manifest.Permissions, ","); got != "tray" {
		t.Errorf("permissions = %q", got)
	}
}

func argsTags(t *testing.T, info string) string {
	for _, l := range strings.Split(info, "\n") {
		if strings.Contains(l, "-tags=") {
			return l
		}
	}
	return ""
}

func TestNativeReleaseCrossCompiles(t *testing.T) {
	requireRelease(t)
	root := goOnlyProject(t)
	b := releaseBuilder(t, root, nil)
	targets := []string{"windows/amd64", "darwin/arm64", "linux/arm64"}
	res, err := b.BuildNativeRelease(NativeReleaseOptions{OutDir: filepath.Join(root, "out"), Targets: targets})
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if len(res.Binaries) != 3 {
		t.Fatalf("binaries = %+v", res.Binaries)
	}
	want := map[string]string{
		"windows/amd64": filepath.Join("windows-amd64", "Acme.exe"),
		"darwin/arm64":  filepath.Join("darwin-arm64", "Acme"),
		"linux/arm64":   filepath.Join("linux-arm64", "Acme"),
	}
	for _, bin := range res.Binaries {
		if got := mustRel(res.OutDir, bin.Path); got != want[bin.Target] {
			t.Errorf("%s at %s, want %s", bin.Target, got, want[bin.Target])
		}
		t.Logf("%s: %s", bin.Target, sizeMB(bin.Size))
		head := make([]byte, 4)
		f, err := os.Open(bin.Path)
		if err != nil {
			t.Fatal(err)
		}
		f.Read(head)
		f.Close()
		switch bin.Target {
		case "windows/amd64":
			pf, err := pe.Open(bin.Path)
			if err != nil {
				t.Fatalf("not a PE file: %v", err)
			}
			oh, ok := pf.OptionalHeader.(*pe.OptionalHeader64)
			pf.Close()
			if !ok || oh.Subsystem != 2 {
				t.Errorf("windows executable subsystem = %+v, want 2 (windows GUI)", oh)
			}
		case "darwin/arm64":
			if !(head[0] == 0xcf && head[1] == 0xfa) && !(head[3] == 0xcf && head[2] == 0xfa) {
				t.Errorf("not a 64-bit Mach-O: % x", head)
			}
		case "linux/arm64":
			if string(head) != "\x7fELF" {
				t.Errorf("not an ELF file: % x", head)
			}
		}
	}
	_ = runtime.GOOS
}

func versionValue() string     { return version.Value }
func setVersionValue(v string) { version.Value = v }

type readySignal struct {
	buf   bytes.Buffer
	ready chan struct{}
	done  bool
}

func (r *readySignal) Write(p []byte) (int, error) {
	r.buf.Write(p)
	if !r.done && strings.Contains(r.buf.String(), "ready\n") {
		r.done = true
		close(r.ready)
	}
	return len(p), nil
}

// krate native profile: run the executable, collect a CPU profile when it
// exits, save it as native.pgo; the next release build passes it to go build.
func TestNativeProfileCollectsAndFeedsPGO(t *testing.T) {
	requireRelease(t)
	if runtime.GOOS == "windows" {
		t.Skip("a Windows process is stopped without writing its profile")
	}
	root := tsOnlyProject(t, "")
	b := releaseBuilder(t, root, nil)
	sig := &readySignal{ready: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-sig.ready:
			time.Sleep(500 * time.Millisecond) // let it serve a little
			cancel()
		case <-time.After(120 * time.Second):
			cancel()
		}
	}()
	res, err := b.ProfileNative(NativeProfileOptions{OutDir: filepath.Join(root, "out"), Args: []string{"--browser"}, Stdout: sig, Stderr: &testLogWriter{t: t}, Ctx: ctx})
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	if res.Path != filepath.Join(root, "native.pgo") || res.Size == 0 {
		t.Fatalf("result = %+v", res)
	}
	head, _ := os.ReadFile(res.Path)
	if len(head) < 2 || head[0] != 0x1f || head[1] != 0x8b {
		t.Fatalf("the profile is not a gzip-compressed pprof file: % x", head[:min(len(head), 8)])
	}

	rel, err := releaseBuilder(t, root, nil).BuildNativeRelease(NativeReleaseOptions{OutDir: filepath.Join(root, "out2")})
	if err != nil {
		t.Fatalf("release with pgo: %v", err)
	}
	if rel.PGO != res.Path {
		t.Fatalf("pgo = %q", rel.PGO)
	}
	if info := goTool(t, "version", "-m", rel.Binaries[0].Path); !strings.Contains(info, "-pgo=") {
		t.Errorf("build info lacks the pgo setting:\n%s", info)
	}
}
