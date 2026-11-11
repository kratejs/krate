package main

import (
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
