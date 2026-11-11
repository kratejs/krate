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
	"github.com/kratejs/krate/packages/compiler/internal/native/scaffold"
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
	b.Cfg.Native = &config.NativeConfig{AppID: "com.example.tasks", ProductName: "Native Tasks", Version: "1.0.0", Window: config.NativeWindow{Width: 960, Height: 640, KeepAlive: true}, Permissions: []string{"dialog", "tray", "clipboard:read"}}
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
	win := m["window"].(map[string]any)
	if win["title"] != "Native Tasks" || win["width"] != float64(960) || win["height"] != float64(640) || win["resizable"] != true || win["path"] != "/" || win["keepAlive"] != true {
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
	if gh["binary"] != "gohost/krate-gohost" {
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

// A dynamic page without generateStaticParams gets one shell page and a
// fallback entry; the shell carries the script that fills params from the URL.
func TestNativeAppDynamicRoutes(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, fakeKrateModules)
	writeTree(t, root, map[string]string{
		"package.json":                 `{"name":"t","type":"module"}`,
		"src/pages/index.tsx":          "export default function P() { return <main>home</main>; }\n",
		"src/pages/a/[id].tsx":         "export default function P({ params }: { params: { id: string } }) { return <main>A {params.id}</main>; }\n",
		"src/pages/b/[slug].tsx":       "export async function generateStaticParams() { return [{ slug: 'one' }, { slug: 'two' }]; }\nexport default function P({ params }: { params: { slug: string } }) { return <main>B {params.slug}</main>; }\n",
		"src/pages/c/[id].tsx":         "export const dynamicParams = false;\nexport async function generateStaticParams() { return [{ id: '9' }]; }\nexport default function P({ params }: { params: { id: string } }) { return <main>C {params.id}</main>; }\n",
		"src/pages/docs/[...path].tsx": "export default function P() { return <main>docs</main>; }\n",
	})
	res, err := newNativeBuilder(t, root).BuildNativeApp(NativeAppOptions{})
	if err != nil {
		t.Fatalf("native build: %v", err)
	}
	var patterns []string
	for _, f := range res.Manifest.Fallbacks {
		patterns = append(patterns, f.Pattern+"="+f.File)
	}
	want := "/a/[id]=a/[id]/index.html,/b/[slug]=b/[slug]/index.html,/docs/[...path]=docs/[...path]/index.html"
	if got := strings.Join(patterns, ","); got != want {
		t.Errorf("fallbacks = %s\nwant       %s", got, want)
	}
	dist := filepath.Join(res.AppDir, "dist")
	for _, p := range []string{"b/one/index.html", "b/two/index.html", "c/9/index.html", "a/[id]/index.html"} {
		if _, err := os.Stat(filepath.Join(dist, filepath.FromSlash(p))); err != nil {
			t.Errorf("missing prerendered %s", p)
		}
	}
	if _, err := os.Stat(filepath.Join(dist, "c", "[id]", "index.html")); err == nil {
		t.Error("a route with dynamicParams = false must not get a shell page")
	}
	shell, _ := os.ReadFile(filepath.Join(dist, "a", "[id]", "index.html"))
	if !strings.Contains(string(shell), "__KRATE_PARAM_id__") || !strings.Contains(string(shell), "window.__KRATE_PARAMS__=p") {
		t.Errorf("shell page lacks the sentinel or the param script:\n%s", shell)
	}
	concrete, _ := os.ReadFile(filepath.Join(dist, "b", "one", "index.html"))
	if strings.Contains(string(concrete), "__KRATE_PARAM_") || !strings.Contains(string(concrete), "one") {
		t.Errorf("prerendered page has no real param:\n%s", concrete)
	}
}

// The param script is the only difference between the web and native HTML of
// a dynamic page.
func TestNativeDynamicPageDiffersOnlyByParamScript(t *testing.T) {
	files := map[string]string{
		"src/pages/a/[id].tsx": "export default function P({ params }: { params: { id: string } }) { return <main>A {params.id}</main>; }\n",
	}
	webRoot, _, err := buildNativeFixture(t, files, false)
	if err != nil {
		t.Fatal(err)
	}
	natRoot, _, err := buildNativeFixture(t, files, true)
	if err != nil {
		t.Fatal(err)
	}
	web, _ := os.ReadFile(filepath.Join(webRoot, "dist", "a", "[id]", "index.html"))
	nat, _ := os.ReadFile(filepath.Join(natRoot, "dist", "a", "[id]", "index.html"))
	stripped := strings.Replace(string(nat), bootScriptOpen+nativeParamJS+"</script>", "", 1)
	if stripped != string(web) {
		t.Errorf("native page differs from the web page beyond the param script:\nweb:    %s\nnative: %s", web, nat)
	}
}

// Host calls never run while prerendering, and a page that only reads a host
// signal still gets client code to fill it in.
func TestNativeAppPrerendersHostCallsAsEmptySlots(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, fakeKrateModules)
	writeTree(t, root, map[string]string{
		"package.json":        `{"name":"t","type":"module"}`,
		"go.mod":              "module example.com/t\n\ngo 1.26.1\n\nrequire github.com/kratejs/krate/packages/native v0.0.0\n\nreplace github.com/kratejs/krate/packages/native => " + filepath.ToSlash(filepath.Join(repoRoot(t), "packages", "native")) + "\n",
		"src/lib/store.go":    "package lib\n\nimport \"github.com/kratejs/krate/packages/native/krate\"\n\nvar Count = krate.NewSignal(3)\n\nfunc Names() []string { return []string{\"a\"} }\n",
		"src/pages/index.tsx": "import { Count } from '../lib/store.go';\nexport default function P() { return <main><p>Open {Count()}</p></main>; }\n",
		"src/pages/top.tsx": `import { createSignal, createEffect } from '@krate/runtime';
import { Names } from '../lib/store.go';
export default function T() {
  const pending = Names();
  const [names, setNames] = createSignal<string[]>([]);
  createEffect(() => { Names().then((x) => setNames(x)); });
  return <main><p>{String(pending)}</p><ul>{names().map((n) => <li key={n}>{n}</li>)}</ul></main>;
}
`,
	})
	res, err := newNativeBuilder(t, root).BuildNativeApp(NativeAppOptions{SkipGoCompile: true})
	if err != nil {
		t.Fatalf("native build: %v", err)
	}
	dist := filepath.Join(res.AppDir, "dist")
	for _, p := range []string{"index.html", "top/index.html"} {
		html, err := os.ReadFile(filepath.Join(dist, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		s := string(html)
		if strings.Contains(s, "undefined") || strings.Contains(s, "[object Promise]") || strings.Contains(s, "src/lib/store") {
			t.Errorf("%s bakes a host value into the HTML:\n%s", p, s)
		}
		if !strings.Contains(s, "<script src=") {
			t.Errorf("%s ships no client code, so the host values would never appear:\n%s", p, s)
		}
	}
}

func TestNativeAppCheckTargetFailuresAbort(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, fakeKrateModules)
	writeTree(t, root, map[string]string{
		"package.json":        `{"name":"t","type":"module"}`,
		"src/pages/index.tsx": "export default function P() { return <main/>; }\n",
		"src/api/users.ts":    "export function GET() { return new Response('x'); }\n",
	})
	_, err := newNativeBuilder(t, root).BuildNativeApp(NativeAppOptions{})
	if err == nil {
		t.Fatal("expected the API route to fail the native build")
	}
	if !hasDiagCode(err, diag.CodeNativeAPIRoute) {
		t.Errorf("error lacks %s: %v", diag.CodeNativeAPIRoute, err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "dist-native", "app", "app.json")); statErr == nil {
		t.Error("a failed build left an app.json")
	}
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

func goHostProject(goMod string) map[string]string {
	return map[string]string{
		"package.json":     `{"name":"t","type":"module"}`,
		"go.mod":           goMod,
		"src/lib/store.go": "package lib\n\nfunc Names() []string { return nil }\n",
		"src/pages/index.tsx": `import { Names } from '../lib/store.go';
export default function P() { return <button onClick={() => Names()}>go</button>; }
`,
	}
}

func TestNativeAppGoModuleMustRequireNativeHost(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, fakeKrateModules)
	writeTree(t, root, goHostProject("module example.com/t\n\ngo 1.26.1\n"))
	_, err := newNativeBuilder(t, root).BuildNativeApp(NativeAppOptions{})
	if err == nil || !hasDiagCode(err, diag.CodeNativeGoModule) {
		t.Fatalf("want KR7202, got %v", err)
	}

	noMod := t.TempDir()
	writeTree(t, noMod, fakeKrateModules)
	files := goHostProject("")
	delete(files, "go.mod")
	writeTree(t, noMod, files)
	_, err = newNativeBuilder(t, noMod).BuildNativeApp(NativeAppOptions{})
	if err == nil || !hasDiagCode(err, diag.CodeNativeGoModule) {
		t.Fatalf("want KR7202 without go.mod, got %v", err)
	}
}

func TestNativeAppRequiresKrateApp(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"package.json":         `{"name":"t","type":"module"}`,
		"src/pages/index.tsx":  "import { greet } from '../lib/g.host';\nexport default function P() { return <button onClick={() => greet()}>x</button>; }\n",
		"src/lib/g.host.ts":    "export async function greet() { return 'hi'; }\n",
		"node_modules/.keep":   "",
		"src/lib/unrelated.ts": "export const x = 1;\n",
	})
	_, err := newNativeBuilder(t, root).BuildNativeApp(NativeAppOptions{})
	if err == nil || !hasDiagCode(err, diag.CodeNativeAppMissing) {
		t.Fatalf("want KR7200, got %v", err)
	}
}

func TestNativeAppRejectsNamespaceGoImport(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, fakeKrateModules)
	files := goHostProject("module example.com/t\n\ngo 1.26.1\n\nrequire github.com/kratejs/krate/packages/native v0.0.0\n")
	files["src/pages/index.tsx"] = "import * as store from '../lib/store.go';\nexport default function P() { return <button onClick={() => store.Names()}>go</button>; }\n"
	writeTree(t, root, files)
	_, err := newNativeBuilder(t, root).BuildNativeApp(NativeAppOptions{})
	if err == nil || !hasDiagCode(err, diag.CodeNativeGoNamespace) {
		t.Fatalf("want KR7201, got %v", err)
	}
}

func TestNativeAppUnknownTarget(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"src/pages/index.tsx": "export default function P() { return <main/>; }\n"})
	for _, target := range []string{"plan9", "linux/mips", "beos/amd64"} {
		_, err := newNativeBuilder(t, root).BuildNativeApp(NativeAppOptions{Target: target})
		if err == nil || !hasDiagCode(err, diag.CodeNativeTarget) {
			t.Errorf("target %q: want KR7204, got %v", target, err)
		}
	}
	for target, want := range map[string]string{"macos/arm64": "darwin/arm64", "linux/amd64": "linux/amd64", "windows/arm64": "windows/arm64"} {
		goos, goarch, d := parseNativeTarget(target)
		if d != nil || goos+"/"+goarch != want {
			t.Errorf("parseNativeTarget(%q) = %s/%s, %v", target, goos, goarch, d)
		}
	}
}

func TestNativeInitScaffoldsBuildableApps(t *testing.T) {
	for _, withGo := range []bool{false, true} {
		name := "ts"
		if withGo {
			name = "go"
		}
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "my-app")
			opts := scaffold.Options{Go: withGo}
			if withGo {
				opts.NativeReplace = filepath.ToSlash(filepath.Join(repoRoot(t), "packages", "native"))
			}
			files, d := scaffold.Init(root, opts)
			if d != nil {
				t.Fatal(d)
			}
			if len(files) == 0 {
				t.Fatal("no files written")
			}
			if _, d := scaffold.Init(root, opts); d == nil || d.Code != diag.CodeNativeInit {
				t.Errorf("a second init must refuse to overwrite, got %v", d)
			}
			writeTree(t, root, fakeKrateModules)
			cfg := config.Default()
			cfg.PagesDir = filepath.Join(root, "src", "pages")
			cfg.OutDir = filepath.Join(root, "dist")
			cfg.Native = &config.NativeConfig{AppID: "com.example.myapp", ProductName: "My App"}
			cfg.Resolve(root)
			res, err := New(root, cfg).BuildNativeApp(NativeAppOptions{SkipGoCompile: true})
			if err != nil {
				t.Fatalf("scaffolded app does not build: %v", err)
			}
			if res.Manifest.HostJS == nil {
				t.Error("the scaffold has a .host.ts file, so app.json needs hostjs")
			}
			if (res.Manifest.GoHost != nil) != withGo {
				t.Errorf("gohost = %v, want present=%v", res.Manifest.GoHost, withGo)
			}
		})
	}
}

func TestNativeShellCopy(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"src/pages/index.tsx": "export default function P() { return <main/>; }\n",
		"fake-shell":          "binary",
	})
	res, err := newNativeBuilder(t, root).BuildNativeApp(NativeAppOptions{Shell: filepath.Join(root, "fake-shell"), WantShell: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.ShellPath == "" {
		t.Fatalf("no shell copied; notes: %v", res.Notes)
	}
	if data, _ := os.ReadFile(res.ShellPath); string(data) != "binary" {
		t.Errorf("shell content = %q", data)
	}
}

func TestGoModRequires(t *testing.T) {
	const path = "github.com/kratejs/krate/packages/native"
	for gomod, want := range map[string]bool{
		"module m\n": false,
		"module m\nrequire " + path + " v0.1.0\n":                                 true,
		"module m\nrequire (\n\t" + path + " v0.1.0\n)\n":                         true,
		"module m\nrequire (\n\tother v1\n\t" + path + " v0.1.0 // indirect\n)\n": true,
		"module m\nreplace " + path + " => ../native\n":                           false,
		"// require " + path + " v1\nmodule m\n":                                  false,
		"module m\nrequire " + path + "/sub v1.0.0\n":                             false,
	} {
		if got := goModRequires(gomod, path); got != want {
			t.Errorf("goModRequires(%q) = %v, want %v", gomod, got, want)
		}
	}
}

func TestBootStubRewritesOnlyGeneratedImports(t *testing.T) {
	gen := "import { call as __call, stream as __stream, signal as __signal } from \"@krate/app\";\nx\n"
	if got := bootStub(gen, "./app.js"); !strings.Contains(got, `import { __krateHostCall as __call, __krateHostStream as __stream, __krateHostSignal as __signal } from "./app.js"`) {
		t.Errorf("generated import not rewritten:\n%s", got)
	}
	if got := bootStub("import { call, signal } from \"@krate/app\";\n", "./app.js"); !strings.Contains(got, "__krateHostCall as call, __krateHostSignal as signal") {
		t.Errorf("unaliased names keep their local name:\n%s", got)
	}
	own := "import { call } from \"@krate/app\";\nimport { app } from \"@krate/app\";\n"
	got := bootStub(own, "./app.js")
	if !strings.Contains(got, `import { app } from "@krate/app"`) || strings.Count(got, "./app.js") != 1 {
		t.Errorf("the project's own import must stay:\n%s", got)
	}
}

// An async function declaration used as a handler keeps its async keyword.
func TestAsyncFunctionHandlerStaysAsync(t *testing.T) {
	root, _, err := buildNativeFixture(t, map[string]string{
		"src/pages/index.tsx": `import { createSignal } from "@krate/runtime";
export default function P() {
  const [n, setN] = createSignal(0);
  async function bump() { await Promise.resolve(); setN(n() + 1); }
  return <button onClick={bump}>{n()}</button>;
}
`,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	var js string
	for name, c := range readTree(t, filepath.Join(root, "dist")) {
		if strings.HasSuffix(name, ".js") {
			js += c
		}
	}
	if !strings.Contains(js, "async function") || strings.Contains(js, `"onClick",function`) {
		t.Errorf("handler lost its async keyword:\n%s", js)
	}
}

// nodeCheckSyntax fails the test when node cannot parse the script.
func nodeCheckSyntax(t *testing.T, name, js string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		requireE2E(t, "node is not installed")
	}
	file := filepath.Join(t.TempDir(), "check.js")
	if err := os.WriteFile(file, []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, "--check", file).CombinedOutput(); err != nil {
		t.Errorf("%s is not valid JavaScript: %v\n%s", name, err, out)
	}
}

func clientJS(t *testing.T, root string) string {
	t.Helper()
	var js strings.Builder
	for name, c := range readTree(t, filepath.Join(root, "dist")) {
		if strings.HasSuffix(name, ".js") && !strings.HasPrefix(name, "chunks/") {
			js.WriteString(c)
			js.WriteString("\n")
		}
	}
	return js.String()
}

// A capitalized arrow constant (all Go host functions look like one) used by a
// handler is declared once; `var X` next to `function X` does not parse.
func TestCapitalizedArrowConstIsDeclaredOnce(t *testing.T) {
	root, _, err := buildNativeFixture(t, map[string]string{
		"src/lib/h.ts": "export const Helper = (a: number) => a * 2;\n",
		"src/pages/index.tsx": `import { createSignal } from "@krate/runtime";
import { Helper } from "../lib/h";
export default function P() {
  const [n, setN] = createSignal(0);
  return <button onClick={() => setN(Helper(n() + 1))}>{n()}</button>;
}
`,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	js := clientJS(t, root)
	if strings.Contains(js, "function Helper") && strings.Contains(js, "var Helper") {
		t.Errorf("Helper is declared twice:\n%s", js)
	}
	nodeCheckSyntax(t, "page script", js)
}

// An arrow function as an operand of another operator is parenthesized.
func TestArrowOperandIsParenthesized(t *testing.T) {
	root, _, err := buildNativeFixture(t, map[string]string{
		"src/lib/opts.ts": "export const opts: { fetch?: (i: number) => number } = {};\n",
		"src/pages/index.tsx": `import { createSignal } from "@krate/runtime";
import { opts } from "../lib/opts";
export default function P() {
  const [n, setN] = createSignal(0);
  const run = () => { const f = opts.fetch ?? ((i: number) => i + 1); setN(f(n())); };
  return <button onClick={run}>{n()}</button>;
}
`,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	nodeCheckSyntax(t, "page script", clientJS(t, root))
}

func TestNativeTargetDisablesViewTransitionsByDefault(t *testing.T) {
	root := t.TempDir()
	b := newNativeBuilder(t, root)
	if head := b.runtimeCommonHead(); !strings.Contains(head, "@view-transition") {
		t.Fatalf("a web build keeps view transitions on: %s", head)
	}
	b.NativeTarget = true
	head := b.runtimeCommonHead()
	if strings.Contains(head, "@view-transition") || !strings.Contains(head, `"viewTransitions":false`) {
		t.Errorf("a native build must not emit cross-document view transitions: %s", head)
	}
	b.Cfg.ViewTransitions = "auto"
	if head := b.runtimeCommonHead(); !strings.Contains(head, "@view-transition") {
		t.Errorf("an explicit viewTransitions: \"auto\" must still be honored: %s", head)
	}
}
