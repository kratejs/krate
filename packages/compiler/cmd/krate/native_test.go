package main

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func nativeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		writeTestFile(t, filepath.Join(root, filepath.FromSlash(name)), content)
	}
	return root
}

const nativePage = "export default function P() { return <div/>; }\n"

func TestNativeCheckPasses(t *testing.T) {
	root := nativeProject(t, map[string]string{
		"krate.config.json":   `{"native":{"appId":"com.acme.app"}}`,
		"src/pages/index.tsx": "@host\nexport async function addTask(t: string) { return t; }\n" + nativePage,
		"src/db.host.ts":      "export function listTasks() { return []; }\n",
		"src/native/store.go": "package native\n\nfunc Save() {}\n",
	})
	code, out := runKrate(t, "native", "check", root)
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, want := range []string{"host modules: 3", "src/db.host.ts (file name): listTasks", "src/native/store.go (go file): Save", "src/pages/index.tsx (inline): addTask", "Native check passed"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestNativeCheckReportsDiagnostics(t *testing.T) {
	root := nativeProject(t, map[string]string{
		"krate.config.json":   `{"native":{"appId":"com.acme.app"}}`,
		"src/pages/index.tsx": "export const dynamic = \"force-dynamic\";\n" + nativePage,
		"src/api/users.ts":    "export function GET() {}\n",
		"src/bad.host.ts":     "// @server\nexport function a() {}\n",
		"src/Card.tsx":        "@host\nexport function Card() { return <div/>; }\n",
	})
	code, out := runKrate(t, "native", "check", root)
	if code != 1 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, want := range []string{"KR7170", "KR7171", "KR7174", "KR7175", "hint:", "Native check failed"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// The lean-page checks warn but never fail the check, and the config can
// silence a code.
func TestNativeCheckLeanWarnings(t *testing.T) {
	files := map[string]string{
		"krate.config.json":   `{"native":{"appId":"com.acme.app"}}`,
		"src/pages/index.tsx": nativePage,
		"src/fonts.css":       "@font-face { font-family: Brand; src: url(/brand.woff2); }\n.glass { backdrop-filter: blur(8px); }\n",
	}
	code, out := runKrate(t, "native", "check", nativeProject(t, files))
	if code != 0 {
		t.Fatalf("warnings must not fail the check, exit %d:\n%s", code, out)
	}
	for _, want := range []string{"KR7272", "KR7273", "hint:", "2 lean-page warning(s)", "Native check passed"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	files["krate.config.json"] = `{"native":{"appId":"com.acme.app","lean":{"ignore":["KR7272"]}}}`
	code, out = runKrate(t, "native", "check", nativeProject(t, files))
	if code != 0 || strings.Contains(out, "KR7272") || !strings.Contains(out, "KR7273") {
		t.Fatalf("native.lean.ignore should silence only KR7272 (exit %d):\n%s", code, out)
	}
}

func TestNativeCheckWithoutHostCode(t *testing.T) {
	root := nativeProject(t, map[string]string{"src/pages/index.tsx": nativePage})
	code, out := runKrate(t, "native", "check", root)
	if code != 0 || !strings.Contains(out, "host modules: none") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
}

func TestNativeStubsAndUnknown(t *testing.T) {
	for _, sub := range nativeStubs {
		code, out := runKrate(t, "native", sub)
		if code != 2 || !strings.Contains(out, "not implemented yet") {
			t.Errorf("native %s: exit %d:\n%s", sub, code, out)
		}
	}
	if code, _ := runKrate(t, "native", "frobnicate"); code != 1 {
		t.Errorf("unknown subcommand exit = %d", code)
	}
	if code, out := runKrate(t, "native"); code != 1 || !strings.Contains(out, "Usage: krate native") {
		t.Errorf("bare native: exit %d:\n%s", code, out)
	}
	code, out := runKrate(t, "native", "--help")
	if code != 0 || !strings.Contains(out, "check") {
		t.Errorf("native --help: exit %d:\n%s", code, out)
	}
}

// nativeDeps are the npm packages a native project needs, reduced to stubs.
var nativeDeps = map[string]string{
	"node_modules/@krate/app/package.json":     `{"name":"@krate/app","version":"0.0.0","type":"module","exports":{".":"./index.js","./host":"./host.js"}}`,
	"node_modules/@krate/app/index.js":         "export function createClient() { return {}; }\nexport function setClient() {}\nexport function getClient() { return { call() {}, stream() {}, signal() {} }; }\n",
	"node_modules/@krate/app/host.js":          "export function hostSignal(v) { return { get: () => v, set() {} }; }\n",
	"node_modules/@krate/core/package.json":    `{"name":"@krate/core","version":"0.0.0","type":"module","main":"./dist/index.js"}`,
	"node_modules/@krate/core/dist/index.js":   "export const defineConfig = (c) => c;\n",
	"node_modules/@krate/runtime/package.json": `{"name":"@krate/runtime","version":"0.0.0","type":"module"}`,
}

func TestNativeInitThenBuild(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hello-app")
	code, out := runKrate(t, "native", "init", dir)
	if code != 0 {
		t.Fatalf("init exit %d:\n%s", code, out)
	}
	for _, f := range []string{"krate.config.ts", "package.json", "src/lib/greet.host.ts", "src/pages/index.tsx"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); err != nil {
			t.Errorf("init did not create %s", f)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		t.Error("a TypeScript-only init must not create go.mod")
	}
	if code, out := runKrate(t, "native", "init", dir); code != 1 || !strings.Contains(out, "KR7205") {
		t.Errorf("second init: exit %d:\n%s", code, out)
	}

	for name, content := range nativeDeps {
		writeTestFile(t, filepath.Join(dir, filepath.FromSlash(name)), content)
	}
	code, out = runKrate(t, "native", "build", dir)
	if code != 0 {
		t.Fatalf("build exit %d:\n%s", code, out)
	}
	for _, want := range []string{"Hello App", "host/host.js", "krate-shell dist-native/app"} {
		if !strings.Contains(out, want) {
			t.Errorf("build output lacks %q:\n%s", want, out)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "dist-native", "app", "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"appId": "com.example.helloapp"`, `"productName": "Hello App"`, `"assets": "dist"`, `"bundle": "host/host.js"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("app.json lacks %s:\n%s", want, data)
		}
	}

	// --out moves the whole output.
	code, out = runKrate(t, "native", "build", dir, "--out", filepath.Join(dir, "out-elsewhere"))
	if code != 0 {
		t.Fatalf("build --out exit %d:\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "out-elsewhere", "app", "app.json")); err != nil {
		t.Errorf("--out ignored: %v", err)
	}
}

func TestNativeInitGoScaffold(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "go-app")
	code, out := runKrate(t, "native", "init", dir, "--go")
	if code != 0 {
		t.Fatalf("init --go exit %d:\n%s", code, out)
	}
	for _, f := range []string{"go.mod", "src/lib/tasks.go", "src/lib/greet.host.ts"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); err != nil {
			t.Errorf("init --go did not create %s", f)
		}
	}
	tasks, _ := os.ReadFile(filepath.Join(dir, "src", "lib", "tasks.go"))
	if !strings.Contains(string(tasks), "krate.NewSignal") {
		t.Errorf("tasks.go has no host signal:\n%s", tasks)
	}
	if !strings.Contains(out, "go get github.com/kratejs/krate/packages/native") {
		t.Errorf("init --go outside the monorepo must say how to add the host module:\n%s", out)
	}
}

func TestNativeBuildFailures(t *testing.T) {
	root := nativeProject(t, map[string]string{
		"krate.config.json":   `{"native":{"appId":"com.acme.app"}}`,
		"src/pages/index.tsx": nativePage,
	})
	if code, out := runKrate(t, "native", "build", root, "--target", "plan9/amd64"); code != 1 || !strings.Contains(out, "KR7204") {
		t.Errorf("bad target: exit %d:\n%s", code, out)
	}
	bad := nativeProject(t, map[string]string{
		"krate.config.json":   `{"native":{"appId":"com.acme.app"}}`,
		"src/pages/index.tsx": nativePage,
		"src/api/users.ts":    "export function GET() {}\n",
	})
	if code, out := runKrate(t, "native", "build", bad); code != 1 || !strings.Contains(out, "KR7175") {
		t.Errorf("api route: exit %d:\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(bad, "dist-native", "app", "app.json")); err == nil {
		t.Error("a failed build wrote app.json")
	}
}

func TestNativeFlagsRelease(t *testing.T) {
	flags, rest, err := parseFlagsChecked([]string{"native", "build", ".", "--release", "--target", "windows/amd64,darwin/arm64", "--target=linux/arm64"})
	if err != nil {
		t.Fatal(err)
	}
	if !flags.Release || strings.Join(flags.Targets, " ") != "windows/amd64 darwin/arm64 linux/arm64" || strings.Join(rest, " ") != "native build ." {
		t.Errorf("flags = %+v rest = %v", flags, rest)
	}
}

func TestNativeFlagsLayout(t *testing.T) {
	flags, _, err := parseFlagsChecked([]string{"native", "build", ".", "--layout", "shell"})
	if err != nil || flags.Layout != "shell" {
		t.Fatalf("flags = %+v err = %v", flags, err)
	}
	if _, _, err := parseFlagsChecked([]string{"native", "build", ".", "--layout=tiny"}); err == nil {
		t.Error("an unknown layout was accepted")
	}
}

func TestNativeWantsExecutable(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not on PATH")
	}
	goApp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(goApp, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goApp, "src", "tasks.go"), []byte("package src\n\nfunc List() []string { return nil }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tsApp := t.TempDir()
	cfg := config.Default()
	cases := []struct {
		name    string
		root    string
		flags   cliFlags
		want    bool
		plugins bool
	}{
		{"go host, default", goApp, cliFlags{}, true, false},
		{"go host, layout shell", goApp, cliFlags{Layout: "shell"}, false, false},
		{"go host, explicit shell", goApp, cliFlags{Shell: "/x/krate-shell", WantShell: true}, false, false},
		{"no go host", tsApp, cliFlags{}, false, false},
		{"plugins, default", tsApp, cliFlags{}, true, true},
		{"plugins, layout shell", tsApp, cliFlags{Layout: "shell"}, false, true},
		{"no go host, layout executable", tsApp, cliFlags{Layout: "executable"}, true, false},
	}
	for _, c := range cases {
		cfg := cfg
		if c.plugins {
			cfg = config.Default()
			cfg.Native = &config.NativeConfig{Plugins: []string{"./native-plugins/x"}}
		}
		if got := nativeWantsExecutable(c.flags, c.root, cfg); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// The flags of the build, package and check paths combine in one command line.
func TestNativeFlagsCombined(t *testing.T) {
	flags, rest, err := parseFlagsChecked([]string{"native", "build", ".", "--release", "--package", "--format", "zip,deb", "--target", "linux/amd64", "--out", "dist-x", "--scriptc", "--scriptc-fail-on", "dynamic"})
	if err != nil {
		t.Fatal(err)
	}
	if !flags.Release || !flags.Pkg.Package || !flags.Scriptc || flags.ScriptcFailOn != "dynamic" || flags.NativeOut != "dist-x" ||
		strings.Join(flags.Pkg.Formats, ",") != "zip,deb" || strings.Join(flags.Targets, ",") != "linux/amd64" || strings.Join(rest, " ") != "native build ." {
		t.Errorf("flags = %+v rest = %v", flags, rest)
	}
}

func TestNativeProfileMemoryFlags(t *testing.T) {
	flags, rest, err := parseFlagsChecked([]string{"native", "profile", ".", "--memory", "--idle", "45s", "--out", "dist-m"})
	if err != nil {
		t.Fatal(err)
	}
	if !flags.Memory || !flags.IdleSet || flags.Idle != 45*time.Second || flags.NativeOut != "dist-m" || strings.Join(rest, " ") != "native profile ." {
		t.Errorf("flags = %+v rest = %v", flags, rest)
	}
	if _, _, err := parseFlagsChecked([]string{"native", "profile", "--idle", "soon"}); err == nil || !strings.Contains(err.Error(), "--idle") {
		t.Errorf("a bad --idle: %v", err)
	}
}

func TestNativeUsageListsEveryCommandAndFlag(t *testing.T) {
	var sb strings.Builder
	printNativeUsage(&sb)
	for _, want := range []string{"check", "init", "dev", "build", "profile", "package", "sign", "notarize", "keygen", "rollout", "patch", "doctor", "plugin",
		"--delta-from", "--rollout", "--channel", "--release", "--target", "--package", "--format", "--scriptc", "--scriptc-fail-on", "--browser", "--port", "--memory", "--idle"} {
		if !strings.Contains(sb.String(), want) {
			t.Errorf("usage does not mention %q", want)
		}
	}
}

func TestNativeCheckPrintsCapabilities(t *testing.T) {
	root := nativeProject(t, map[string]string{
		"krate.config.json": `{"native":{"appId":"com.acme.app"}}`,
		"src/pages/index.tsx": `import { readText } from "@krate/app/fs";
import { set } from "@krate/app/tray";
void readText; void set;
` + nativePage,
	})
	code, out := runKrate(t, "native", "check", root)
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, want := range []string{"capabilities", "permissions: 2 (derived from the code)", "fs:read", "tray", "src/pages/index.tsx:1", "sdk areas linked: os, permission, fs, menu, tray", "left out of release builds:"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	// A permission the allowlist lacks fails the check with KR7210.
	bad := nativeProject(t, map[string]string{
		"krate.config.json": `{"native":{"appId":"com.acme.app","permissions":["kv"]}}`,
		"src/pages/index.tsx": `import { set } from "@krate/app/tray";
void set;
` + nativePage,
	})
	code, out = runKrate(t, "native", "check", bad)
	if code != 1 || !strings.Contains(out, "KR7210") || !strings.Contains(out, "src/pages/index.tsx:1") || !strings.Contains(out, `"tray"`) {
		t.Errorf("allowlist violation: exit %d:\n%s", code, out)
	}
}

func TestCheckPrintsNativeCapabilities(t *testing.T) {
	root := nativeProject(t, map[string]string{
		"krate.config.json": `{"native":{"appId":"com.acme.app"}}`,
		"src/pages/index.tsx": `import { writeText } from "@krate/app/clipboard";
void writeText;
` + nativePage,
		"node_modules/@krate/app/package.json": `{"name":"@krate/app","type":"module","exports":{"./clipboard":"./clipboard.js"}}`,
		"node_modules/@krate/app/clipboard.js": "export const writeText = () => {};\n",
	})
	code, out := runKrate(t, "check", root)
	if code != 0 || !strings.Contains(out, "Native capabilities") || !strings.Contains(out, "clipboard") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	web := nativeProject(t, map[string]string{"src/pages/index.tsx": nativePage})
	if _, out := runKrate(t, "check", web); strings.Contains(out, "Native capabilities") {
		t.Errorf("a project without a native block must not print the section:\n%s", out)
	}
}

func TestNativeBuildDerivesPermissions(t *testing.T) {
	root := nativeProject(t, map[string]string{
		"krate.config.json": `{"native":{"appId":"com.acme.app"}}`,
		"src/pages/index.tsx": `import { writeText } from "@krate/app/clipboard";
void writeText;
` + nativePage,
		"node_modules/@krate/app/package.json": `{"name":"@krate/app","type":"module","exports":{"./clipboard":"./clipboard.js"}}`,
		"node_modules/@krate/app/clipboard.js": "export const writeText = () => {};\n",
	})
	code, out := runKrate(t, "native", "build", root)
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "permissions  clipboard") {
		t.Errorf("output lacks the derived permissions:\n%s", out)
	}
	data, err := os.ReadFile(filepath.Join(root, "dist-native", "app", "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"clipboard"`) || !strings.Contains(string(data), `"strictPermissions": true`) {
		t.Errorf("app.json:\n%s", data)
	}
}

func TestNativePackageEndToEnd(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hello-app")
	if code, out := runKrate(t, "native", "init", dir); code != 0 {
		t.Fatalf("init exit %d:\n%s", code, out)
	}
	for name, content := range nativeDeps {
		writeTestFile(t, filepath.Join(dir, filepath.FromSlash(name)), content)
	}
	cfgPath := filepath.Join(dir, "krate.config.ts")
	cfg, _ := os.ReadFile(cfgPath)
	cfgText := strings.Replace(string(cfg), "native: {", `native: {
    updater: { url: "https://dl.example.com/updates/latest-{target}.json", publicKey: "`+testUpdaterPub+`" },
    deepLinks: ["hello"],`, 1)
	if cfgText == string(cfg) {
		t.Fatalf("config has no native block to extend:\n%s", cfg)
	}
	if err := os.WriteFile(cfgPath, []byte(cfgText), 0o644); err != nil {
		t.Fatal(err)
	}

	// Packaging before building says what to do.
	code, out := runKrate(t, "native", "package", dir, "--target", "linux/amd64")
	if code != 1 || !strings.Contains(out, "KR7220") {
		t.Fatalf("package before build: exit %d:\n%s", code, out)
	}
	shell := filepath.Join(t.TempDir(), "krate-shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\necho shell\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if code, out := runKrate(t, "native", "build", dir, "--shell", shell, "--target", "linux/amd64"); code != 0 {
		t.Fatalf("build exit %d:\n%s", code, out)
	}
	manifest, _ := os.ReadFile(filepath.Join(dir, "dist-native", "app", "app.json"))
	if !strings.Contains(string(manifest), `"updater"`) || !strings.Contains(string(manifest), testUpdaterPub) {
		t.Errorf("app.json lacks the updater block:\n%s", manifest)
	}
	code, out = runKrate(t, "native", "package", dir, "--target", "linux/amd64", "--format", "deb,tar.gz")
	if code != 0 {
		t.Fatalf("package exit %d:\n%s", code, out)
	}
	for _, f := range []string{"hello-app_0.1.0_amd64.deb", "hello-app-0.1.0-linux-amd64.tar.gz"} {
		if _, err := os.Stat(filepath.Join(dir, "dist-native", "packages", f)); err != nil {
			t.Errorf("missing %s:\n%s", f, out)
		}
	}
	if !strings.Contains(out, "KRATE_UPDATER_PRIVATE_KEY") {
		t.Errorf("no note about the unsigned update file:\n%s", out)
	}
	// An unknown format is KR7222.
	if code, out := runKrate(t, "native", "package", dir, "--target", "linux/amd64", "--format", "msix"); code != 1 || !strings.Contains(out, "KR7222") {
		t.Errorf("bad format: exit %d:\n%s", code, out)
	}
	// Signing a Mac app off macOS is a clear error.
	if code, out := runKrate(t, "native", "sign", dir, "--target", "darwin/arm64"); code != 1 || !strings.Contains(out, "nothing to sign") {
		t.Errorf("sign without a package: exit %d:\n%s", code, out)
	}
}

const testUpdaterPub = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

func TestNativeKeygen(t *testing.T) {
	key := filepath.Join(t.TempDir(), "up.key")
	code, out := runKrate(t, "native", "keygen", "--key-out", key)
	if code != 0 || !strings.Contains(out, "publicKey:") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if st, err := os.Stat(key); err != nil || (runtime.GOOS != "windows" && st.Mode().Perm() != 0o600) {
		t.Fatalf("key file: %v %v", st, err)
	}
	if code, out := runKrate(t, "native", "keygen", "--key-out", key); code != 1 || !strings.Contains(out, "KR7227") {
		t.Errorf("overwrite: exit %d:\n%s", code, out)
	}
	if code, _ := runKrate(t, "native", "keygen", "--key-out", key, "--force"); code != 0 {
		t.Errorf("--force: exit %d", code)
	}
}

// pluginProject writes a project that enables the echo test plugin from
// ./native-plugins/echo and whose page imports from it.
func pluginCLIProject(t *testing.T, page, extraConfig string) string {
	t.Helper()
	src, err := filepath.Abs(filepath.Join("..", "..", "internal", "build", "testdata", "native_plugin", "echo"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"krate.config.json":   `{"native":{"appId":"com.acme.app","plugins":["./native-plugins/echo"]` + extraConfig + `}}`,
		"src/pages/index.tsx": page,
	}
	for _, name := range []string{"krate-plugin.json", "go.mod", "echo.go"} {
		b, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatal(err)
		}
		files["native-plugins/echo/"+name] = string(b)
	}
	return nativeProject(t, files)
}

func TestNativeCheckReportsPlugins(t *testing.T) {
	root := pluginCLIProject(t, `import { echo } from "krate/plugin/echo";
void echo;
`+nativePage, "")
	code, out := runKrate(t, "native", "check", root)
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, want := range []string{"permissions: 1 (derived from the code)", "plugin:echo.use  (plugin echo)", "src/pages/index.tsx:1", "plugin echo: ops used: echo"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "plugin:echo.admin") {
		t.Errorf("an unused plugin permission is reported:\n%s", out)
	}

	// The allowlist names a plugin permission that no plugin declares: KR7255.
	bad := pluginCLIProject(t, nativePage, `,"permissions":["plugin:echo.nope"]`)
	code, out = runKrate(t, "native", "check", bad)
	if code != 1 || !strings.Contains(out, "KR7255") || !strings.Contains(out, "plugin:echo.nope") {
		t.Errorf("unknown plugin permission: exit %d:\n%s", code, out)
	}

	// A plugin that is not there: KR7250 with a hint.
	missing := nativeProject(t, map[string]string{
		"krate.config.json":   `{"native":{"appId":"com.acme.app","plugins":["./native-plugins/nope"]}}`,
		"src/pages/index.tsx": nativePage,
	})
	code, out = runKrate(t, "native", "check", missing)
	if code != 1 || !strings.Contains(out, "KR7250") || !strings.Contains(out, "hint:") {
		t.Errorf("missing plugin: exit %d:\n%s", code, out)
	}
}

func TestNativeDoctorNeedsGoForPlugins(t *testing.T) {
	root := pluginCLIProject(t, nativePage, "")
	_, out := runKrate(t, "native", "doctor", root, "--json")
	if !strings.Contains(out, "native plugins (./native-plugins/echo)") {
		t.Errorf("doctor does not say that plugins need Go:\n%s", out)
	}
}

func TestNativePluginInit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-tool")
	code, out := runKrate(t, "native", "plugin", "init", "my-tool", dir)
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, want := range []string{"krate-plugin.json", "mytool.go", "plugins: ['./", "import { hello } from 'krate/plugin/my-tool'"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "krate-plugin.json")); err != nil {
		t.Error(err)
	}
	if code, out := runKrate(t, "native", "plugin", "init", "my-tool", dir); code != 1 || !strings.Contains(out, "already exists") {
		t.Errorf("second init: exit %d:\n%s", code, out)
	}
	if code, out := runKrate(t, "native", "plugin", "init", "Bad Name", filepath.Join(t.TempDir(), "x")); code != 1 || !strings.Contains(out, "not a valid plugin name") {
		t.Errorf("bad name: exit %d:\n%s", code, out)
	}
	if code, _ := runKrate(t, "native", "plugin"); code != 1 {
		t.Errorf("plugin without a command: exit %d", code)
	}
}

func TestNativeTestFlags(t *testing.T) {
	flags, rest, err := parseFlagsChecked([]string{"native", "test", "app", "tasks", "--grep", "adds", "--workers=3", "--list", "--shell", "/x/shell"})
	if err != nil {
		t.Fatal(err)
	}
	if flags.TestGrep != "adds" || flags.TestWorkers != 3 || !flags.TestList || flags.Shell != "/x/shell" {
		t.Errorf("flags = %+v", flags)
	}
	if strings.Join(rest, " ") != "native test app tasks" {
		t.Errorf("remaining = %v", rest)
	}
	for _, bad := range []string{"0", "x", "-1"} {
		if _, _, err := parseFlagsChecked([]string{"native", "test", "--workers", bad}); err == nil {
			t.Errorf("--workers %s was accepted", bad)
		}
	}
}

func TestNativeTestListAndDiagnostics(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, src string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("krate.config.ts", "export default { entry: 'src/index.tsx', native: { appId: 'com.example.t', productName: 'T', version: '1.0.0' } };\n")
	write("src/index.tsx", "export default function P() { return <main>hi</main>; }\n")
	write("tests/a.native.test.ts", "// test\n")
	write("tests/sub/b.native.test.ts", "// test\n")

	code, out := runKrate(t, "native", "test", dir, "--list")
	if code != 0 {
		t.Fatalf("--list exited %d:\n%s", code, out)
	}
	if !strings.Contains(out, "tests/a.native.test.ts") || !strings.Contains(out, "tests/sub/b.native.test.ts") {
		t.Errorf("--list output:\n%s", out)
	}

	code, out = runKrate(t, "native", "test", dir, "--list", "sub")
	if code != 0 || strings.Contains(out, "tests/a.native.test.ts") || !strings.Contains(out, "tests/sub/b.native.test.ts") {
		t.Errorf("--list sub (exit %d):\n%s", code, out)
	}

	code, out = runKrate(t, "native", "test", dir, "--list", "nope")
	if code == 0 || !strings.Contains(out, `no test file matches "nope"`) {
		t.Errorf("unmatched pattern (exit %d):\n%s", code, out)
	}

	code, out = runKrate(t, "native", "test", dir, "--browser")
	if code == 0 || !strings.Contains(out, "--browser is not available") {
		t.Errorf("--browser (exit %d):\n%s", code, out)
	}

	empty := t.TempDir()
	if err := os.WriteFile(filepath.Join(empty, "krate.config.ts"), []byte("export default { native: { appId: 'com.example.t', productName: 'T', version: '1.0.0' } };\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out = runKrate(t, "native", "test", empty, "--list")
	if code == 0 || !strings.Contains(out, "no test files found") || !strings.Contains(out, "docs/native/testing") {
		t.Errorf("no tests (exit %d):\n%s", code, out)
	}

	code, out = runKrate(t, "native", "help")
	if !strings.Contains(out, "krate native test") && !strings.Contains(out, "  test ") {
		t.Errorf("usage does not list test (exit %d):\n%s", code, out)
	}
}

func TestNativeRolloutEditsAFeedAndHaltsIt(t *testing.T) {
	feed := filepath.Join(t.TempDir(), "latest-linux-amd64.json")
	sig := strings.Repeat("A", 86) + "=="
	if err := os.WriteFile(feed, []byte(`{"version":"1.2.0","url":"a.tar.gz","signature":"`+sig+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := runKrate(t, "native", "rollout", "--feed", feed, "--rollout", "20", "--rollout-ramp-hours", "24", "--critical", "--min-version", "1.0.0"); code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	b, _ := os.ReadFile(feed)
	for _, want := range []string{`"percentage": 20`, `"ramp_hours": 24`, `"critical": true`, `"min_version": "1.0.0"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("feed lacks %s:\n%s", want, b)
		}
	}
	if code, out := runKrate(t, "native", "rollout", "--feed", feed, "--halt"); code != 0 {
		t.Fatalf("halt: exit %d:\n%s", code, out)
	}
	b, _ = os.ReadFile(feed)
	if !strings.Contains(string(b), `"percentage": 0`) || !strings.Contains(string(b), sig) {
		t.Errorf("halted feed:\n%s", b)
	}
	if code, _ := runKrate(t, "native", "rollout", "--feed", feed, "--rollout", "250"); code == 0 {
		t.Error("a percentage over 100 was accepted")
	}
	if code, _ := runKrate(t, "native", "rollout", "--feed", feed); code == 0 {
		t.Error("rollout with nothing to change succeeded")
	}
	if code, _ := runKrate(t, "native", "rollout"); code == 0 {
		t.Error("rollout without --feed succeeded")
	}
}

func writeTgz(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	f.Close()
}

func TestNativePatchWritesAFeedEntry(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "up.key")
	if code, out := runKrate(t, "native", "keygen", "--key-out", key); code != 0 {
		t.Fatalf("keygen: %d\n%s", code, out)
	}
	t.Setenv("KRATE_UPDATER_PRIVATE_KEY_FILE", key)
	old, nw, out := filepath.Join(dir, "a-1.0.0.tar.gz"), filepath.Join(dir, "a-1.1.0.tar.gz"), filepath.Join(dir, "p.krpatch")
	body := strings.Repeat("some executable bytes ", 400)
	writeTgz(t, old, map[string]string{"acme": body, "app.json": "{}"})
	writeTgz(t, nw, map[string]string{"acme": body + "more", "app.json": "{}"})
	code, text := runKrate(t, "native", "patch", "--patch-old", old, "--patch-new", nw, "--patch-kind", "tree", "--patch-exe", "acme",
		"--patch-from", "1.0.0", "--patch-to", "1.1.0", "--patch-app-id", "com.acme.app", "--patch-target", "linux-amd64", "--patch-out", out)
	if code != 0 || !strings.Contains(text, `"from": "1.0.0"`) || !strings.Contains(text, `"sha512"`) {
		t.Fatalf("exit %d:\n%s", code, text)
	}
	if st, err := os.Stat(out); err != nil || st.Size() == 0 {
		t.Fatalf("patch file: %v %v", st, err)
	}
	if code, _ := runKrate(t, "native", "patch", "--patch-old", old); code == 0 {
		t.Error("patch with missing flags succeeded")
	}
}
