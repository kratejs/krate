//go:build windows

package login

import (
	"os"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// The Run value round-trips in a scratch key under HKCU, not the real Run key.
func TestRunKeyRoundTrip(t *testing.T) {
	old := runKeyPath
	runKeyPath = `Software\Krate\LoginTest`
	t.Cleanup(func() {
		runKeyPath = old
		_ = registry.DeleteKey(registry.CURRENT_USER, `Software\Krate\LoginTest`)
		_ = registry.DeleteKey(registry.CURRENT_USER, `Software\Krate`)
	})
	t.Setenv("KRATE_APP_ID", "com.acme.login")
	var s registryStore
	if it, err := s.get(); err != nil || it.Enabled {
		t.Fatalf("fresh: %+v, %v", it, err)
	}
	if err := s.set(true, []string{"--background", "a b"}); err != nil {
		t.Fatal(err)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	v, _, err := k.GetStringValue("com.acme.login")
	k.Close()
	if err != nil {
		t.Fatal(err)
	}
	if want := windowsCommandLine(mustExe(t), []string{"--background", "a b"}); v != want {
		t.Fatalf("value %q, want %q", v, want)
	}
	if it, _ := s.get(); !it.Enabled || it.Method != "registry" {
		t.Fatalf("after set: %+v", it)
	}
	if err := s.set(false, nil); err != nil {
		t.Fatal(err)
	}
	if it, _ := s.get(); it.Enabled {
		t.Fatalf("after removal: %+v", it)
	}
	if err := s.set(false, nil); err != nil {
		t.Fatalf("removing twice: %v", err)
	}
}

func mustExe(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}
