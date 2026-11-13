package shell

import (
	"strings"
	"testing"
)

func TestStrictPermissionsAndWindowScope(t *testing.T) {
	m, err := ParseManifest([]byte(`{"appId":"com.acme.app","strictPermissions":true}`), "app.json")
	if err != nil {
		t.Fatal(err)
	}
	if m.HasPermission("kv") || m.HasPermission("window") {
		t.Fatal("strict with no list must grant nothing")
	}
	m, err = ParseManifest([]byte(`{"appId":"com.acme.app","permissions":["kv","window"],"window":{"permissions":["kv"]}}`), "app.json")
	if err != nil || m.Window.Permissions == nil || len(*m.Window.Permissions) != 1 {
		t.Fatalf("window scope: %v %+v", err, m.Window)
	}
	_, err = ParseManifest([]byte(`{"appId":"com.acme.app","permissions":["kv"],"window":{"permissions":["fs"]}}`), "app.json")
	if err == nil || !strings.Contains(err.Error(), "window.permissions") {
		t.Fatalf("a window scope outside the app set: %v", err)
	}
}
