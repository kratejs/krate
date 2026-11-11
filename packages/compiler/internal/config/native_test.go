package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
			"window": {"width": 900, "height": 600, "resizable": false, "titleBar": "hidden", "keepAlive": true},
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
	if n.AppID != "com.acme.app" || n.Window.Width != 900 || *n.Window.Resizable || n.Window.TitleBar != "hidden" || !n.Window.KeepAlive {
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

func TestNativeLoadsFromTSConfig(t *testing.T) {
	dir := t.TempDir()
	src := `export default { hostDirs: ["src/host"], native: { appId: "com.acme.app", window: { width: 800 } } };`
	if err := os.WriteFile(filepath.Join(dir, "krate.config.ts"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n := cfg.EffectiveNative(); n == nil || n.AppID != "com.acme.app" || n.Window.Width != 800 || len(cfg.HostDirs) != 1 {
		t.Errorf("native = %+v dirs=%v", n, cfg.HostDirs)
	}
}

func TestNativeDefaults(t *testing.T) {
	c := Default()
	if c.EffectiveNative() != nil {
		t.Fatal("no native block should give nil")
	}
	c.Native = &NativeConfig{AppID: "com.acme.app"}
	n := c.EffectiveNative()
	if n.Window.Width != 1280 || n.Window.Height != 800 || !*n.Window.Resizable || n.Window.TitleBar != "native" {
		t.Errorf("window = %+v", n.Window)
	}
	if n.Host.Runtime != "quickjs" || n.Release.Compile != "auto" || !*n.Host.Bundle {
		t.Errorf("host/release = %+v %+v", n.Host, n.Release)
	}
	if strings.Join(n.Platforms, ",") != "windows,macos,linux" {
		t.Errorf("platforms = %v", n.Platforms)
	}
	if c.Native.Window.Width != 0 {
		t.Error("EffectiveNative must not mutate the config")
	}
}

func TestNativeMemoryAndFeatures(t *testing.T) {
	cfg := nativeCfg(t, `{"native": {"appId": "com.acme.app", "memory": "minimal",
		"features": {"media": true, "webgl": false}, "host": {"memoryMB": 48}}}`)
	n := cfg.EffectiveNative()
	if n.Memory != "minimal" || n.Host.MemoryMB != 48 {
		t.Errorf("memory = %q, host.memoryMB = %d", n.Memory, n.Host.MemoryMB)
	}
	f := n.Features
	if f.Media == nil || !*f.Media || f.WebGL == nil || *f.WebGL || f.WebAudio != nil {
		t.Errorf("features = %+v", f)
	}
	c := Default()
	c.Native = &NativeConfig{AppID: "com.acme.app"}
	if got := c.EffectiveNative().Memory; got != "balanced" {
		t.Errorf("default memory = %q, want balanced", got)
	}
	if c.Native.Memory != "" {
		t.Error("EffectiveNative must not mutate the config")
	}
}

func TestValidateNative(t *testing.T) {
	cases := []struct {
		name string
		mut  func(n *NativeConfig)
		want string // error substring; "" for valid
	}{
		{"ok", func(n *NativeConfig) {}, ""},
		{"appId single segment", func(n *NativeConfig) { n.AppID = "acme" }, "reverse-DNS"},
		{"appId bad chars", func(n *NativeConfig) { n.AppID = "com.acme.my app" }, "reverse-DNS"},
		{"productName newline", func(n *NativeConfig) { n.ProductName = "Acme\nDepends: evil" }, "control character"},
		{"publisher newline", func(n *NativeConfig) { n.Bundle.Windows.Publisher = "A\r\nB" }, "control character"},
		{"productName ok", func(n *NativeConfig) { n.ProductName = "Acme Notes" }, ""},
		{"version", func(n *NativeConfig) { n.Version = "1.0" }, "semantic version"},
		{"platform", func(n *NativeConfig) { n.Platforms = []string{"beos"} }, "unknown platform"},
		{"titleBar", func(n *NativeConfig) { n.Window.TitleBar = "glass" }, "titleBar"},
		{"negative size", func(n *NativeConfig) { n.Window.Width = -1 }, "width and height"},
		{"permission", func(n *NativeConfig) { n.Permissions = []string{"camera"} }, "unknown permission"},
		{"deep link", func(n *NativeConfig) { n.DeepLinks = []string{"acme://"} }, "URL scheme"},
		{"plugins ok", func(n *NativeConfig) {
			n.Plugins = []string{"./native-plugins/hello", "@acme/krate-sqlite", "go:github.com/acme/p@v1.0.0", "krate-plugin-x", "../shared/p", "/abs/p"}
		}, ""},
		{"plugin empty", func(n *NativeConfig) { n.Plugins = []string{""} }, "not a plugin reference"},
		{"plugin spaces", func(n *NativeConfig) { n.Plugins = []string{"./a b"} }, "not a plugin reference"},
		{"plugin bare go prefix", func(n *NativeConfig) { n.Plugins = []string{"go:"} }, "not a plugin reference"},
		{"plugin uppercase package", func(n *NativeConfig) { n.Plugins = []string{"Acme"} }, "not a plugin reference"},
		{"plugin duplicate", func(n *NativeConfig) { n.Plugins = []string{"./a", "./a"} }, "twice"},
		{"plugin permission shape", func(n *NativeConfig) { n.Permissions = []string{"plugin:system-info.read", "kv"} }, ""},
		{"plugin permission bad shape", func(n *NativeConfig) { n.Permissions = []string{"plugin:Bad"} }, "unknown permission"},
		{"plugin window permission", func(n *NativeConfig) {
			p := []string{"plugin:system-info.read"}
			n.Permissions = []string{"plugin:system-info.read"}
			n.Window.Permissions = &p
		}, ""},
		{"runtime enum", func(n *NativeConfig) { n.Host.Runtime = "python" }, "host.runtime"},
		{"scriptc needs opt in", func(n *NativeConfig) { n.Host.Runtime = "scriptc" }, "experimental"},
		{"scriptc opted in", func(n *NativeConfig) { n.Host.Runtime = "scriptc"; n.Experimental.Scriptc = true }, ""},
		{"island runtime", func(n *NativeConfig) { n.Host.Islands = map[string]string{"sharp": "scriptc"} }, "host.islands"},
		{"island ok", func(n *NativeConfig) { n.Host.Islands = map[string]string{"sharp": "quickjs"} }, ""},
		{"viewTransitions", func(n *NativeConfig) { n.ViewTransitions = "on" }, "viewTransitions"},
		{"viewTransitions auto", func(n *NativeConfig) { n.ViewTransitions = "auto" }, ""},
		{"compile", func(n *NativeConfig) { n.Release.Compile = "sometimes" }, "release.compile"},
		{"installer", func(n *NativeConfig) { n.Bundle.Windows.Installer = "exe" }, "installer"},
		{"linux format", func(n *NativeConfig) { n.Bundle.Linux.Formats = []string{"snap"} }, "format"},
		{"updater needs key", func(n *NativeConfig) { n.Updater = &NativeUpdater{URL: "https://a.b/u.json"} }, "url and publicKey"},
		{"window permission unknown", func(n *NativeConfig) { w := []string{"camera"}; n.Window.Permissions = &w }, "window.permissions"},
		{"window permission outside the allowlist", func(n *NativeConfig) {
			n.Permissions = []string{"kv"}
			w := []string{"kv", "fs"}
			n.Window.Permissions = &w
		}, "does not allow"},
		{"window permission inside the allowlist", func(n *NativeConfig) {
			n.Permissions = []string{"kv", "fs"}
			w := []string{"fs:read"}
			n.Window.Permissions = &w
		}, ""},
		{"empty window scope", func(n *NativeConfig) { w := []string{}; n.Window.Permissions = &w }, ""},
		{"memory profile", func(n *NativeConfig) { n.Memory = "minimal" }, ""},
		{"memory unknown", func(n *NativeConfig) { n.Memory = "tiny" }, "native.memory"},
		{"host memoryMB negative", func(n *NativeConfig) { n.Host.MemoryMB = -1 }, "host.memoryMB"},
		{"updater https", func(n *NativeConfig) { n.Updater = &NativeUpdater{URL: "http://a.b/u.json", PublicKey: "k"} }, "https"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Default()
			c.Native = &NativeConfig{AppID: "com.acme.app"}
			tc.mut(c.Native)
			_, err := c.Validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), "hint:") {
				t.Errorf("error has no hint: %v", err)
			}
		})
	}
}

func TestValidateNativeMissingAppIDWarns(t *testing.T) {
	c := Default()
	c.Native = &NativeConfig{}
	w, err := c.Validate()
	if err != nil || len(w) != 1 || !strings.Contains(w[0], "native.appId") {
		t.Errorf("warnings=%v err=%v", w, err)
	}
}

func TestNativeUnknownKeys(t *testing.T) {
	raw := []byte(`{"entry":"x","hostFunctions":["a"],"hostDirs":["b"],"native":{"appId":"com.a.b","wndow":{},"window":{"widht":1},"host":{"runtim":"node"},"bundle":{"macos":{"catgory":"x"}},"updater":{"urll":"x"}}}`)
	got := strings.Join(UnknownKeyWarnings(raw), "\n")
	for _, k := range []string{"native.wndow", "native.window.widht", "native.host.runtim", "native.bundle.macos.catgory", "native.updater.urll"} {
		if !strings.Contains(got, k) {
			t.Errorf("missing warning for %s in:\n%s", k, got)
		}
	}
	if strings.Contains(got, "hostFunctions") || strings.Contains(got, "hostDirs") || strings.Contains(got, "native.appId") {
		t.Errorf("known keys warned:\n%s", got)
	}
}

func TestHostListsMergeOnExtends(t *testing.T) {
	if !additiveArrayKeys["hostFunctions"] || !additiveArrayKeys["hostDirs"] {
		t.Error("host lists should concatenate across extends")
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

func TestNativePermissionsAcceptSDKNames(t *testing.T) {
	for _, p := range []string{"window", "clipboard:read", "fs:read", "os:secrets.read", "shortcut", "shell:openExternal", "db"} {
		c := Default()
		c.Native = &NativeConfig{AppID: "com.acme.app", Permissions: []string{p}}
		if _, err := c.Validate(); err != nil {
			t.Errorf("%s rejected: %v", p, err)
		}
	}
	c := Default()
	c.Native = &NativeConfig{AppID: "com.acme.app", Permissions: []string{"camera"}}
	if _, err := c.Validate(); err == nil || !strings.Contains(err.Error(), "known permissions: app, clipboard") {
		t.Errorf("camera: %v", err)
	}
}

func TestNativeReleaseDefaultsAndWindowScope(t *testing.T) {
	c := Default()
	c.Native = &NativeConfig{AppID: "com.acme.app"}
	if n := c.EffectiveNative(); n.Release.PGO != "native.pgo" || n.Release.Compile != "auto" {
		t.Errorf("release defaults = %+v", n.Release)
	}
	cfg := nativeCfg(t, `{"native":{"appId":"com.acme.app","permissions":["kv","window"],"window":{"permissions":["kv"]},"release":{"pgo":"p/app.pgo"}}}`)
	n := cfg.EffectiveNative()
	if n.Window.Permissions == nil || len(*n.Window.Permissions) != 1 || n.Release.PGO != "p/app.pgo" {
		t.Errorf("native = %+v", n)
	}
	cfg = nativeCfg(t, `{"native":{"appId":"com.acme.app","window":{"permissions":[]}}}`)
	if wp := cfg.EffectiveNative().Window.Permissions; wp == nil || len(*wp) != 0 {
		t.Errorf("an empty list must stay a scope that grants nothing: %v", wp)
	}
	if got := strings.Join(UnknownKeyWarnings([]byte(`{"native":{"appId":"com.a.b","window":{"permissions":[]}}}`)), "\n"); got != "" {
		t.Errorf("window.permissions warned: %s", got)
	}
}

func TestNativePluginsKey(t *testing.T) {
	cfg := nativeCfg(t, `{"native":{"appId":"com.a.b","plugins":["./native-plugins/hello","@acme/p"]}}`)
	if got := cfg.EffectiveNative().Plugins; !reflect.DeepEqual(got, []string{"./native-plugins/hello", "@acme/p"}) {
		t.Errorf("plugins = %v", got)
	}
	dir := t.TempDir()
	src := `export default { native: { appId: "com.acme.app", plugins: ["./native-plugins/hello"] } };`
	if err := os.WriteFile(filepath.Join(dir, "krate.config.ts"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	ts, err := Load(dir)
	if err != nil || len(ts.Native.Plugins) != 1 {
		t.Fatalf("krate.config.ts: %v %+v", err, ts.Native)
	}
	// plugins is a known key; a misspelling warns.
	raw := []byte(`{"native":{"appId":"com.a.b","plugins":[],"plugns":[]}}`)
	got := strings.Join(UnknownKeyWarnings(raw), "\n")
	if !strings.Contains(got, "native.plugns") || strings.Contains(got, "native.plugins") {
		t.Errorf("warnings:\n%s", got)
	}
}

func TestNativePluginSpecKind(t *testing.T) {
	for spec, want := range map[string]string{
		"./a": "path", "../a": "path", "/a/b": "path", ".": "path",
		"go:example.com/m": "go", "go:example.com/m@v1.2.3": "go",
		"@acme/krate-x": "npm", "krate-x": "npm", "@acme/krate-x/sub": "npm",
		"": "", " ./a": "", "go:": "", "go:@v1": "", "Has Space": "", "a\\b": "",
	} {
		if got := NativePluginSpecKind(spec); got != want {
			t.Errorf("NativePluginSpecKind(%q) = %q, want %q", spec, got, want)
		}
	}
}
