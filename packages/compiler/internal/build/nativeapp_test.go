package build

import (
	"bytes"
	"debug/elf"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// repoRoot is the Krate checkout containing this test.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
}

// fakeKrateModules are the npm packages a native project depends on, reduced
// to what a build needs: the host call client, the host API and defineConfig.
var fakeKrateModules = map[string]string{
	"node_modules/@krate/app/package.json": `{"name":"@krate/app","version":"0.0.0","type":"module","exports":{".":"./index.js","./host":"./host.js","./kv":"./kv.js"}}`,
	"node_modules/@krate/app/index.js": `let current;
export function createClient(o = {}) { return { call: (fn, a) => Promise.resolve([fn, a]), stream: (fn, a) => ({ fn, a }), signal: (n) => { const [g] = (o.createSignal || ((v) => [() => v, () => {}]))(undefined); return g; } }; }
export function setClient(c) { current = c; }
export function getClient() { return (current ??= createClient()); }
export function call(fn, args, opts) { return getClient().call(fn, args, opts); }
export function stream(fn, args, opts) { return getClient().stream(fn, args, opts); }
export function signal(name) { return getClient().signal(name); }
`,
	"node_modules/@krate/app/host.js":          "export function hostSignal(v) { return { get: () => v, set: () => {} }; }\n",
	"node_modules/@krate/app/kv.js":            "const m = new Map();\nexport async function get(k) { return m.has(k) ? m.get(k) : null; }\nexport async function set(k, v) { m.set(k, v); }\n",
	"node_modules/@krate/core/package.json":    `{"name":"@krate/core","version":"0.0.0","type":"module","main":"./dist/index.js"}`,
	"node_modules/@krate/core/dist/index.js":   "export const defineConfig = (c) => c;\n",
	"node_modules/@krate/runtime/package.json": `{"name":"@krate/runtime","version":"0.0.0","type":"module","main":"./dist/index.js"}`,
}

// nativeCopyTree copies src to dst, skipping build outputs and dependencies.
func nativeCopyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", "dist", "dist-native", ".krate":
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// nativeExampleCopy copies examples/native-tasks to a temp dir with fake
// dependencies and a go.mod that replaces the host module with this checkout's.
func nativeExampleCopy(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	nativeCopyTree(t, filepath.Join(repoRoot(t), "examples", "native-tasks"), root)
	writeTree(t, root, fakeKrateModules)
	pointGoModAtMonorepo(t, root)
	return root
}

func pointGoModAtMonorepo(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, "go.mod")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	native := filepath.ToSlash(filepath.Join(repoRoot(t), "packages", "native"))
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "replace ") {
			line = "replace github.com/kratejs/krate/packages/native => " + native
		}
		out = append(out, line)
	}
	if err := os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newNativeBuilder(t *testing.T, root string) *Builder {
	t.Helper()
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Native = &config.NativeConfig{AppID: "com.acme.app", ProductName: "Acme", Version: "1.0.0"}
	cfg.Resolve(root)
	return New(root, cfg)
}

func hasGoHostPackage(t *testing.T) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(repoRoot(t), "packages", "native", "gohost"))
	return err == nil
}

func readNativeManifest(t *testing.T, appDir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(appDir, "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("app.json: %v\n%s", err, data)
	}
	return m
}

func nativeStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestNativeAppBuildsExample(t *testing.T) {
	root := nativeExampleCopy(t)
	b := newNativeBuilder(t, root)
	// Cfg from the example's own config is not loaded here; the builder
	// gets the same native block the example declares.
	b.Cfg.Native = &config.NativeConfig{AppID: "com.example.tasks", ProductName: "Native Tasks", Version: "1.0.0", Window: config.NativeWindow{Width: 960, Height: 640, KeepAlive: true, ContextMenu: "custom"}, Permissions: []string{"dialog", "tray", "clipboard:read"},
		FileAssociations: []config.NativeFileAssociation{{Ext: []string{"task"}, Name: "Task list"}}}
	compile := hasGoHostPackage(t)
	res, err := b.BuildNativeApp(NativeAppOptions{SkipGoCompile: !compile})
	if err != nil {
		t.Fatalf("native build: %v", err)
	}
	app := res.AppDir
	if app != filepath.Join(root, "dist-native", "app") {
		t.Fatalf("app dir = %s", app)
	}

	m := readNativeManifest(t, app)
	for k, want := range map[string]any{"appId": "com.example.tasks", "productName": "Native Tasks", "version": "1.0.0", "assets": "dist", "debug": false} {
		if m[k] != want {
			t.Errorf("app.json %s = %v, want %v", k, m[k], want)
		}
	}
	if got := strings.Join(nativeStrings(m["permissions"]), ","); got != "dialog,tray,clipboard:read" {
		t.Errorf("app.json permissions = %q", got)
	}
	if m["singleInstance"] != true {
		t.Errorf("app.json singleInstance = %v; an app with file associations takes the single-instance lock", m["singleInstance"])
	}
	win := m["window"].(map[string]any)
	if win["title"] != "Native Tasks" || win["width"] != float64(960) || win["height"] != float64(640) || win["resizable"] != true || win["path"] != "/" || win["keepAlive"] != true || win["contextMenu"] != "custom" {
		t.Errorf("window = %v", win)
	}
	fb := m["fallbacks"].([]any)
	if len(fb) != 1 || fb[0].(map[string]any)["pattern"] != "/tasks/[id]" || fb[0].(map[string]any)["file"] != "tasks/[id]/index.html" {
		t.Errorf("fallbacks = %v", fb)
	}
	hostjs := m["hostjs"].(map[string]any)
	if hostjs["bundle"] != "host/host.js" || hostjs["map"] != "host/host.js.map" {
		t.Errorf("hostjs = %v", hostjs)
	}
	gh := m["gohost"].(map[string]any)
	wantBin := "gohost/krate-gohost"
	if runtime.GOOS == "windows" {
		wantBin += ".exe"
	}
	if gh["binary"] != wantBin {
		t.Errorf("gohost binary = %v", gh["binary"])
	}
	if got := strings.Join(nativeStrings(gh["funcs"]), ","); got != "src/lib/tasks.Add,src/lib/tasks.Get,src/lib/tasks.List,src/lib/tasks.Toggle" {
		t.Errorf("funcs = %s", got)
	}
	if got := strings.Join(nativeStrings(gh["streams"]), ","); got != "src/lib/tasks.Watch" {
		t.Errorf("streams = %s", got)
	}
	if got := strings.Join(nativeStrings(gh["signals"]), ","); got != "src/lib/tasks.Count" {
		t.Errorf("signals = %s", got)
	}

	for _, p := range []string{"dist/index.html", "dist/tasks/[id]/index.html", "host/host.js", "host/host.js.map"} {
		if _, err := os.Stat(filepath.Join(app, filepath.FromSlash(p))); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(app, "dist", ".krate")); err == nil {
		t.Error("the build manifest directory shipped in dist")
	}

	// The UI bundle holds stubs and the client bootstrap, never host code.
	ui := readTree(t, filepath.Join(app, "dist"))
	var js strings.Builder
	for name, content := range ui {
		if strings.HasSuffix(name, ".js") {
			js.WriteString(content)
		}
	}
	all := js.String()
	for _, want := range []string{"src/lib/tasks.Add", "src/lib/tasks.Count", "src/lib/tasks.Watch", "src/lib/notes.host.greet", "globalThis.createSignal"} {
		if !strings.Contains(all, want) {
			t.Errorf("dist JS lacks %q", want)
		}
	}
	for _, leak := range []string{"from the host", "sync.Mutex", "krate.NewSignal", "next++"} {
		if strings.Contains(all, leak) {
			t.Errorf("host code leaked into dist JS: %q", leak)
		}
	}

	// The Go host program and its glue.
	mainGo, err := os.ReadFile(res.GoHostMain)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`glue0 "example.com/native-tasks/.krate/native/glue/src/lib"`,
		`"github.com/kratejs/krate/packages/native/gohost"`,
		"gohost.Serve(func(r *scheme.Registry) {",
		"glue0.Register(r)",
	} {
		if !strings.Contains(string(mainGo), want) {
			t.Errorf("gohost main.go lacks %q:\n%s", want, mainGo)
		}
	}
	vet := exec.Command("go", "vet", "./.krate/native/glue/...")
	vet.Dir = root
	if out, err := vet.CombinedOutput(); err != nil {
		t.Fatalf("generated glue does not compile: %v\n%s", err, out)
	}

	if !compile {
		t.Log("packages/native/gohost does not exist in this checkout; the Go host binary was not built")
		return
	}
	bin := filepath.Join(app, "gohost", "krate-gohost")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("gohost binary: %v", err)
	}
	if runtime.GOOS == "linux" {
		// Release builds strip the symbol table.
		f, err := elf.Open(bin)
		if err != nil {
			t.Fatal(err)
		}
		if f.Section(".symtab") != nil {
			t.Error("the Go host binary of a release build still has a symbol table")
		}
		f.Close()
	}
	sock := filepath.Join(t.TempDir(), "gohost.sock")
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "KRATE_GOHOST_SOCKET="+sock)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	// The child quits when its stdin closes, so hold a pipe open.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	deadline := time.After(3 * time.Second)
	for running := true; running; {
		select {
		case err := <-done:
			t.Fatalf("gohost exited early: %v\n%s", err, buf.String())
		case <-deadline:
			running = false
		case <-time.After(50 * time.Millisecond):
			if _, err := os.Stat(sock); err == nil {
				running = false
			}
		}
	}
	_ = cmd.Process.Kill()
}

func hasDiagCode(err error, code diag.Code) bool {
	var walk func(error) bool
	walk = func(e error) bool {
		var d diag.Diagnostic
		if errors.As(e, &d) && d.Code == code {
			return true
		}
		if u, ok := e.(interface{ Unwrap() []error }); ok {
			for _, c := range u.Unwrap() {
				if walk(c) {
					return true
				}
			}
		}
		return false
	}
	return walk(err)
}

func TestNativeAppDeveloperToolsAreDevOnlyUnlessDebug(t *testing.T) {
	for _, tc := range []struct {
		name  string
		dev   bool
		debug bool
		want  bool
	}{
		{"release", false, false, false},
		{"release with native.debug", false, true, true},
		{"dev", true, false, true},
	} {
		root := nativeExampleCopy(t)
		b := newNativeBuilder(t, root)
		b.Cfg.Native = &config.NativeConfig{AppID: "com.example.tasks", Debug: tc.debug}
		if _, err := b.BuildNativeApp(NativeAppOptions{SkipGoCompile: true, Dev: tc.dev}); err != nil {
			t.Fatalf("%s: native build: %v", tc.name, err)
		}
		m := readNativeManifest(t, filepath.Join(root, "dist-native", "app"))
		if m["debug"] != tc.want {
			t.Errorf("%s: manifest debug = %v, want %v", tc.name, m["debug"], tc.want)
		}
	}
}
