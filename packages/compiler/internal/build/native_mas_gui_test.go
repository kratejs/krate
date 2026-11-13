package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/native/pkg"
)

// A Mac App Store variant of the example, ad hoc signed with the app sandbox
// entitlement, starts and serves its pages. The app runs inside its container,
// so this proves the shell works sandboxed: its data directories, its sockets
// and its webview all live where the sandbox allows.
func TestNativeMASSandboxGUI(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the Mac App Store sandbox exists on macOS only")
	}
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		t.Skip("set KRATE_NATIVE_GUI=1 to run the sandboxed GUI test")
	}
	ex := e2eRequireDeps(t)
	out := filepath.Join(t.TempDir(), "dist-native")
	_, res := e2eBuildRelease(t, ex, out, "krate_devcontrol")

	cfg, err := config.Load(ex)
	if err != nil {
		t.Fatal(err)
	}
	meta := pkg.MetadataFromConfig(ex, cfg)
	meta.Permissions = res.Manifest.Permissions
	payload, err := pkg.Discover(out, "darwin", runtime.GOARCH, meta.ProductName)
	if err != nil {
		t.Fatal(err)
	}
	pk, err := pkg.Package(pkg.Options{
		Meta: meta, Payload: payload, OutDir: filepath.Join(out, "packages"), Formats: []string{"mas"},
		MAS: pkg.MASOptions{AppIdentity: "-", Getenv: func(string) string { return "" }}, Log: &testLogWriter{t: t},
	})
	if err != nil {
		t.Fatalf("packaging the MAS variant: %v", err)
	}
	var app string
	for _, a := range pk.Artifacts {
		if a.Format == "app" {
			app = a.Path
		}
	}
	if app == "" {
		t.Fatalf("no .app in %+v", pk.Artifacts)
	}
	ents, err := exec.Command("codesign", "-d", "--entitlements", "-", app).CombinedOutput()
	if err != nil || !strings.Contains(string(ents), "com.apple.security.app-sandbox") {
		t.Fatalf("the app is not signed with the sandbox entitlement: %v\n%s", err, ents)
	}

	home, _ := os.UserHomeDir()
	container := filepath.Join(home, "Library", "Containers", meta.AppID)
	_, statErr := os.Stat(container)
	existed := statErr == nil
	exe := filepath.Join(app, "Contents", "MacOS", meta.ProductName)
	s := e2eStartShell(t, exe, "")
	s.waitReady(90 * time.Second)
	e2eGUIFlows(t, s)

	if st, err := os.Stat(container); err != nil || !st.IsDir() {
		t.Errorf("a sandboxed app runs in %s: %v", container, err)
	}
	if _, err := s.send("quit", 10*time.Second); err != nil {
		t.Errorf("quit: %v", err)
	}
	if !existed {
		_ = os.RemoveAll(container)
	}
}
