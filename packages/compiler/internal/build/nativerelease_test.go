package build

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
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
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return &releaseApp{url: url, base: strings.SplitN(url, "/?", 2)[0], c: c, cmd: cmd}
}
