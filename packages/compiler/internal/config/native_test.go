package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func nativeCfg(t *testing.T, body string) *Config {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "krate.config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return cfg
}

func TestNativeLoadsFullBlock(t *testing.T) {
	cfg := nativeCfg(t, `{
		"hostFunctions": ["addTask", "src/db.ts"],
		"hostDirs": ["src/host"],
		"native": {
			"appId": "com.acme.app", "productName": "Acme", "version": "1.0.0",
			"platforms": ["macos"], "icons": "./assets/icon.png",
			"window": {"width": 900, "height": 600, "resizable": false, "titleBar": "hidden", "keepAlive": true, "contextMenu": "custom"},
			"permissions": ["dialog", "shell:openExternal"],
			"deepLinks": ["acme"],
			"host": {"runtime": "node", "version": "22", "bundle": false, "islands": {"sharp": "node"}},
			"experimental": {"scriptc": true},
			"release": {"compile": "always", "pgo": "./native.pgo"},
			"bundle": {"macos": {"category": "x"}, "windows": {"publisher": "P", "installer": "nsis"}, "linux": {"formats": ["deb", "appimage"]}},
			"updater": {"url": "https://acme.com/updates/{target}.json", "publicKey": "k"}
		}
	}`)
	if len(cfg.HostFunctions) != 2 || cfg.HostDirs[0] != "src/host" {
		t.Errorf("host lists: %v %v", cfg.HostFunctions, cfg.HostDirs)
	}
	n := cfg.EffectiveNative()
	if n.AppID != "com.acme.app" || n.Window.Width != 900 || *n.Window.Resizable || n.Window.TitleBar != "hidden" || !n.Window.KeepAlive || n.Window.ContextMenu != "custom" {
		t.Errorf("native = %+v", n)
	}
	if n.Host.Runtime != "node" || *n.Host.Bundle || n.Host.Islands["sharp"] != "node" || n.Release.Compile != "always" {
		t.Errorf("host/release = %+v %+v", n.Host, n.Release)
	}
	if n.Bundle.Windows.Installer != "nsis" || len(n.Bundle.Linux.Formats) != 2 || n.Updater == nil {
		t.Errorf("bundle/updater = %+v", n.Bundle)
	}
	if len(Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", Warnings)
	}
}

// native_permissions.json must be the SDK's own list. The check reads the
// original from the sibling packages/native module and is skipped when only
// the compiler is checked out.
func TestNativePermissionsMatchSDK(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("..", "..", "..", "native", "sdk", "permission", "names.json"))
	if err != nil {
		t.Skip("packages/native is not checked out: ", err)
	}
	var a, b nativePermissionDoc
	if err := json.Unmarshal(want, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(nativePermissionsJSON, &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("native_permissions.json is stale; copy packages/native/sdk/permission/names.json over it\nsdk:      %v\ncompiler: %v", a, b)
	}
}
