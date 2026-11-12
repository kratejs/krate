//go:build linux

package login

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/krate"
)

func TestAutostartRoundTrip(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("KRATE_APP_ID", "com.acme.login")
	t.Setenv("KRATE_APP_NAME", "Acme")
	var s autostartStore

	if it, err := s.get(); err != nil || it.Enabled {
		t.Fatalf("fresh: %+v, %v", it, err)
	}
	if err := s.set(true, []string{"--background"}); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(cfg, "autostart", "com.acme.login.desktop")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	if !strings.Contains(string(b), "Name=Acme\n") || !strings.Contains(string(b), desktopQuote(exe)+" --background\n") {
		t.Fatalf("entry:\n%s", b)
	}
	if it, err := s.get(); err != nil || !it.Enabled || it.Method != "autostart" {
		t.Fatalf("after set: %+v, %v", it, err)
	}
	// A user who disabled the entry in their desktop settings reads as off.
	if err := os.WriteFile(p, append(b, "Hidden=true\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	if it, _ := s.get(); it.Enabled {
		t.Fatal("a hidden entry must read as disabled")
	}
	if err := s.set(false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("entry still there: %v", err)
	}
	if err := s.set(false, nil); err != nil {
		t.Fatalf("removing twice must succeed: %v", err)
	}
}

func TestAutostartNeedsAppID(t *testing.T) {
	t.Setenv("KRATE_APP_ID", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := (autostartStore{}).get(); !errors.Is(err, krate.ErrUnsupported) {
		t.Fatalf("got %v", err)
	}
}
