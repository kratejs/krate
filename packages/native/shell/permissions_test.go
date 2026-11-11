package shell

import "testing"

func TestManifestPermissions(t *testing.T) {
	m, err := ParseManifest([]byte(`{"appId":"com.acme.app","permissions":["window","clipboard:read"]}`), "app.json")
	if err != nil {
		t.Fatal(err)
	}
	if !m.HasPermission("window") || !m.HasPermission("clipboard:read") {
		t.Fatal("granted permissions missing")
	}
	if m.HasPermission("clipboard") || m.HasPermission("dialog") {
		t.Fatal("permission granted that the manifest does not list")
	}
	m, _ = ParseManifest([]byte(`{"appId":"com.acme.app"}`), "app.json")
	if m.HasPermission("window") {
		t.Fatal("default is not deny")
	}
}
