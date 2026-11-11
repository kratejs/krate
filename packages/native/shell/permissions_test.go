package shell

import (
	"strings"
	"testing"
)

func TestManifestPermissions(t *testing.T) {
	m, err := ParseManifest([]byte(`{"appId":"com.acme.app","permissions":["window","clipboard:read"]}`), "app.json")
	if err != nil {
		t.Fatal(err)
	}
	if !m.HasPermission("window") || !m.HasPermission("clipboard:read") {
		t.Fatal("granted permissions missing")
	}
	if m.HasPermission("clipboard") || m.HasPermission("dialog") || m.HasPermission("kv") {
		t.Fatal("a non-empty list grants exactly what it names")
	}
	m, _ = ParseManifest([]byte(`{"appId":"com.acme.app"}`), "app.json")
	for _, p := range []string{"window", "app", "menu", "dialog", "notify", "clipboard", "kv", "db", "os:info"} {
		if !m.HasPermission(p) {
			t.Errorf("default set lacks %s", p)
		}
	}
	for _, p := range []string{"clipboard:read", "tray", "shortcut", "fs", "os:secrets"} {
		if m.HasPermission(p) {
			t.Errorf("default set allows %s", p)
		}
	}
	if _, err := ParseManifest([]byte(`{"appId":"com.acme.app","permissions":["windwo"]}`), "app.json"); err == nil || !strings.Contains(err.Error(), "windwo") {
		t.Fatalf("unknown permission: %v", err)
	}
}
