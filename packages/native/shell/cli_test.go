package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An executable with an app next to it is that app, distributed: it does not
// take --dev (developer tools and a control channel that runs script in its
// pages) unless the app was built with native.debug, and it does not run
// another app directory. Either would let whoever starts the signed executable
// act as the app: its keychain items, its granted files, its camera and
// microphone consent. A bare krate-shell (no app next to it) is unchanged.
func TestDistributedShellRefusesDevAndForeignApps(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "dist", "MyApp")
	own := DefaultAppDir(exe)
	foreign := filepath.Join(root, "evil")
	for _, d := range []string{own, foreign} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, ManifestFile), []byte(`{"appId":"com.acme.app"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := checkLaunch(exe, "", true); err == nil || !strings.Contains(err.Error(), "--dev") {
		t.Errorf("--dev in a distributed app: %v", err)
	}
	if err := checkLaunch(exe, foreign, false); err == nil || !strings.Contains(err.Error(), "another app") {
		t.Errorf("a foreign app directory: %v", err)
	}
	if err := checkLaunch(exe, own, false); err != nil {
		t.Errorf("its own app directory: %v", err)
	}
	if err := checkLaunch(exe, "", false); err != nil {
		t.Errorf("a plain start: %v", err)
	}
	// native.debug keeps --dev, never a foreign app.
	if err := os.WriteFile(filepath.Join(own, ManifestFile), []byte(`{"appId":"com.acme.app","debug":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkLaunch(exe, "", true); err != nil {
		t.Errorf("--dev with native.debug: %v", err)
	}
	if err := checkLaunch(exe, foreign, true); err == nil {
		t.Error("a foreign app directory with native.debug")
	}
	// A bare shell runs any app, in dev too.
	bare := filepath.Join(root, "tools", "krate-shell")
	if err := checkLaunch(bare, foreign, true); err != nil {
		t.Errorf("a bare shell: %v", err)
	}
}
