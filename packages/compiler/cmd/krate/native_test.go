package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
