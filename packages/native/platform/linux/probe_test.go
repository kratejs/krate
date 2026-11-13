//go:build linux

package linux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDisplayInfo(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")
	if d := displayInfo(); d.Available || !strings.Contains(d.Detail, "neither") {
		t.Errorf("no session: %+v", d)
	}
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	if d := displayInfo(); d.Available {
		t.Errorf("wayland socket missing but available: %+v", d)
	}
	if err := os.WriteFile(filepath.Join(dir, "wayland-test"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if d := displayInfo(); !d.Available || !strings.HasPrefix(d.Detail, "wayland") {
		t.Errorf("wayland: %+v", d)
	}
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", ":987654")
	if d := displayInfo(); d.Available {
		t.Errorf("x11 socket missing but available: %+v", d)
	}
	t.Setenv("DISPLAY", "remote.example:0")
	if d := displayInfo(); !d.Available {
		t.Errorf("remote x11 display: %+v", d)
	}
}

func TestProbeShape(t *testing.T) {
	info := Probe()
	if info.OS != "linux" || info.Webview.Name != "WebKitGTK 4.1" || info.Webview.MinVersion != "2.40" {
		t.Fatalf("probe = %+v", info)
	}
	if info.Webview.OK && (!info.Webview.Present || info.Webview.Version == "") {
		t.Errorf("ok without a version: %+v", info.Webview)
	}
	if !info.Webview.OK && info.Webview.Error == "" {
		t.Errorf("not ok without an error: %+v", info.Webview)
	}
}
