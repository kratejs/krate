package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/native/pkg"
)

func exampleOrSkip(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("release builds link executables")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	ex, _ := filepath.Abs(filepath.Join("..", "..", "..", "..", "examples", "native-tasks"))
	for _, p := range []string{
		filepath.Join(ex, "node_modules", "@krate", "app", "dist", "index.js"),
		filepath.Join(ex, "node_modules", "@krate", "core", "dist", "index.js"),
	} {
		if _, err := os.Stat(p); err != nil {
			if os.Getenv("KRATE_REQUIRE_E2E") == "1" {
				t.Fatalf("%s is missing (run pnpm install and pnpm build)", p)
			}
			t.Skipf("%s is missing", p)
		}
	}
	t.Setenv("GOWORK", "off")
	return ex
}

// krate native build --release --arch universal --package compiles both
// architectures, merges them and packages one app.
func TestNativeBuildUniversalCLI(t *testing.T) {
	ex := exampleOrSkip(t)
	out := filepath.Join(t.TempDir(), "dist-native")
	code, log := runKrate(t, "native", "build", ex, "--release", "--arch", "universal", "--package", "--format", "zip", "--out", out)
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, log)
	}
	for _, d := range []string{"darwin-arm64", "darwin-amd64"} {
		if _, err := os.Stat(filepath.Join(out, d, "Native Tasks")); err != nil {
			t.Errorf("no %s build: %v", d, err)
		}
	}
	exe := filepath.Join(out, "packages", "darwin-universal", "Native Tasks.app", "Contents", "MacOS", "Native Tasks")
	archs, err := pkg.FatArchitectures(exe)
	if err != nil || strings.Join(archs, ",") != "amd64,arm64" {
		t.Fatalf("universal executable: %v %v\n%s", archs, err, log)
	}
	if _, err := os.Stat(filepath.Join(out, "packages", "native-tasks-1.0.0-darwin-universal.zip")); err != nil {
		matches, _ := filepath.Glob(filepath.Join(out, "packages", "*.zip"))
		t.Errorf("update zip missing: %v (have %v)", err, matches)
	}
	if _, err := os.Stat(filepath.Join(out, ".universal")); err == nil {
		t.Error("the merge work directory must be removed")
	}
}

// The same build packages as Flatpak and Snap on Linux without any tool: the
// manifests are written and the notes say which tools are missing.
func TestNativePackageFlatpakSnapCLI(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the Linux sandbox formats are written for the host target; this test uses linux")
	}
	ex := exampleOrSkip(t)
	out := filepath.Join(t.TempDir(), "dist-native")
	code, log := runKrate(t, "native", "build", ex, "--release", "--package", "--format", "flatpak,snap", "--out", out)
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, log)
	}
	pk := filepath.Join(out, "packages")
	arch := runtime.GOARCH
	for _, f := range []string{
		"flatpak-" + arch + "/com.example.tasks.yml", "flatpak-" + arch + "/com.example.tasks.metainfo.xml",
		"com.example.tasks.flathub.yml", "snap-" + arch + "/snap/snapcraft.yaml",
	} {
		if _, err := os.Stat(filepath.Join(pk, f)); err != nil {
			t.Errorf("missing %s: %v\n%s", f, err, log)
		}
	}
	b, _ := os.ReadFile(filepath.Join(pk, "flatpak-"+arch, "com.example.tasks.yml"))
	if !strings.Contains(string(b), "--socket=wayland") || strings.Contains(string(b), "--share=network") {
		t.Errorf("finish-args must follow the app's permissions (the example uses kv only):\n%s", b)
	}
}
