//go:build darwin

package login

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The LaunchAgent fallback round-trips in a scratch HOME; no launchctl call is
// made, so nothing is started.
func TestLaunchAgentRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KRATE_APP_ID", "com.acme.login")
	s := macStore{}
	if it, err := s.get(); err != nil || it.Enabled {
		t.Fatalf("fresh: %+v, %v", it, err)
	}
	if err := s.set(true, []string{"--background"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(home, "Library", "LaunchAgents", "com.acme.login.plist"))
	if err != nil || !strings.Contains(string(b), "<string>--background</string>") || !strings.Contains(string(b), "<string>com.acme.login</string>") {
		t.Fatalf("plist %q, %v", b, err)
	}
	if it, _ := s.get(); !it.Enabled || it.Method != "launchagent" {
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
