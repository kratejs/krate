package sdkos

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/internal/sdktest"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/wire"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestComputeDirs(t *testing.T) {
	tests := []struct {
		name string
		in   dirInput
		want Dirs
	}{
		{"linux defaults", dirInput{GOOS: "linux", Getenv: env(nil), Home: "/home/u", TempDir: "/tmp", UID: 1000, AppID: "com.acme.app"}, Dirs{
			Data: "/home/u/.local/share/com.acme.app", Cache: "/home/u/.cache/com.acme.app", Config: "/home/u/.config/com.acme.app",
			Log: "/home/u/.local/state/com.acme.app/log", Temp: "/tmp/com.acme.app-1000"}},
		{"linux xdg", dirInput{GOOS: "linux", Home: "/home/u", TempDir: "/tmp", UID: 1, AppID: "a",
			Getenv: env(map[string]string{"XDG_DATA_HOME": "/d", "XDG_CACHE_HOME": "/c", "XDG_CONFIG_HOME": "/cfg", "XDG_STATE_HOME": "/s", "XDG_RUNTIME_DIR": "/run/user/1"})}, Dirs{
			Data: "/d/a", Cache: "/c/a", Config: "/cfg/a", Log: "/s/a/log", Temp: "/run/user/1/a"}},
		{"linux relative xdg is ignored", dirInput{GOOS: "linux", Home: "/h", TempDir: "/tmp", UID: 1, AppID: "a",
			Getenv: env(map[string]string{"XDG_DATA_HOME": "relative/dir"})}, Dirs{
			Data: "/h/.local/share/a", Cache: "/h/.cache/a", Config: "/h/.config/a", Log: "/h/.local/state/a/log", Temp: "/tmp/a-1"}},
		{"macos", dirInput{GOOS: "darwin", Getenv: env(nil), Home: "/Users/u", TempDir: "/var/folders/x/T", AppID: "com.acme.app"}, Dirs{
			Data: "/Users/u/Library/Application Support/com.acme.app", Cache: "/Users/u/Library/Caches/com.acme.app",
			Config: "/Users/u/Library/Application Support/com.acme.app/config", Log: "/Users/u/Library/Logs/com.acme.app",
			Temp: "/var/folders/x/T/com.acme.app"}},
		{"windows", dirInput{GOOS: "windows", TempDir: `C:\Temp`, AppID: "com.acme.app",
			Getenv: env(map[string]string{"APPDATA": `C:\Users\u\AppData\Roaming`, "LOCALAPPDATA": `C:\Users\u\AppData\Local`})}, Dirs{
			Data: `C:\Users\u\AppData\Roaming\com.acme.app`, Cache: `C:\Users\u\AppData\Local\com.acme.app\Cache`,
			Config: `C:\Users\u\AppData\Roaming\com.acme.app\Config`, Log: `C:\Users\u\AppData\Local\com.acme.app\Logs`,
			Temp: `C:\Temp\com.acme.app`}},
	}
	for _, tc := range tests {
		got, err := computeDirs(tc.in)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s:\n got %+v\nwant %+v", tc.name, got, tc.want)
		}
	}
	if _, err := computeDirs(dirInput{GOOS: "linux", Getenv: env(nil), AppID: "a"}); err == nil {
		t.Error("linux without HOME succeeded")
	}
	if _, err := computeDirs(dirInput{GOOS: "windows", Getenv: env(nil), AppID: "a"}); err == nil {
		t.Error("windows without APPDATA succeeded")
	}
	if _, err := computeDirs(dirInput{GOOS: "darwin", Getenv: env(nil), AppID: "a"}); err == nil {
		t.Error("macos without HOME succeeded")
	}
}

// useApp configures an app whose directories live under a private HOME.
func useApp(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for _, k := range []string{"XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR"} {
		t.Setenv(k, "")
	}
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", filepath.Join(home, "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "Local"))
	t.Setenv("USERPROFILE", home)
	prev := app
	t.Cleanup(func() { appMu.Lock(); app = prev; appMu.Unlock() })
	if err := Configure(App{ID: "com.acme.test", Name: "Test", Version: "1.2.3"}); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestDirsCreatedOnDemand(t *testing.T) {
	home := useApp(t)
	for name, f := range map[string]func() (string, error){
		"data": AppDataDir, "cache": CacheDir, "config": ConfigDir, "log": LogDir, "temp": TempDir,
	} {
		p, err := f()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		st, err := os.Stat(p)
		if err != nil || !st.IsDir() {
			t.Errorf("%s: %s not created: %v", name, p, err)
		}
		if name != "temp" && !strings.HasPrefix(p, home) {
			t.Errorf("%s: %s is outside HOME %s", name, p, home)
		}
		if !strings.Contains(p, "com.acme.test") {
			t.Errorf("%s: %s does not name the app", name, p)
		}
	}
	all, err := AllDirs()
	if err != nil || len(all) != 5 {
		t.Fatalf("AllDirs: %v %v", all, err)
	}
}

func TestConfigureRejectsUnsafeIDs(t *testing.T) {
	for _, id := range []string{"", "..", ".", "a/b", `a\b`, "-x", "a b"} {
		if err := Configure(App{ID: id}); err == nil {
			t.Errorf("appId %q accepted", id)
		}
	}
}

func TestCurrentFromEnv(t *testing.T) {
	prev := app
	appMu.Lock()
	app = nil
	appMu.Unlock()
	t.Cleanup(func() { appMu.Lock(); app = prev; appMu.Unlock() })
	t.Setenv("KRATE_APP_ID", "")
	if _, err := Current(); err == nil {
		t.Fatal("unconfigured app has an identity")
	}
	t.Setenv("KRATE_APP_ID", "com.env.app")
	t.Setenv("KRATE_APP_VERSION", "9")
	a, err := Current()
	if err != nil || a.ID != "com.env.app" || a.Version != "9" {
		t.Fatalf("%+v %v", a, err)
	}
	t.Setenv("KRATE_APP_ID", "../evil")
	if _, err := Current(); err == nil {
		t.Fatal("unsafe KRATE_APP_ID accepted")
	}
}

func TestLocaleFromEnv(t *testing.T) {
	tests := []struct {
		env  map[string]string
		want string
	}{
		{nil, ""},
		{map[string]string{"LANG": "en_US.UTF-8"}, "en-US"},
		{map[string]string{"LANG": "C"}, ""},
		{map[string]string{"LANG": "POSIX", "LC_ALL": "de_DE@euro"}, "de-DE"},
		{map[string]string{"LC_ALL": "fr_CA.UTF-8", "LANG": "en_US"}, "fr-CA"},
		{map[string]string{"LC_ALL": "C", "LANG": "pt_BR"}, "pt-BR"},
	}
	for _, tc := range tests {
		if got := localeFromEnv(env(tc.env)); got != tc.want {
			t.Errorf("%v: got %q, want %q", tc.env, got, tc.want)
		}
	}
}

func TestInfo(t *testing.T) {
	useApp(t)
	i := GetInfo()
	if i.OS == "" || i.Arch == "" || i.Locale == "" || i.AppID != "com.acme.test" || i.AppVersion != "1.2.3" {
		t.Fatalf("%+v", i)
	}
}

func TestCheckURL(t *testing.T) {
	ok := []string{"https://example.com/a?b=c", "http://localhost:8080", "mailto:a@b.c", "HTTPS://EXAMPLE.COM"}
	bad := []string{"", "file:///etc/passwd", "javascript:alert(1)", "ftp://x", "https://", "-flag", "https://a b", "https://a\nb",
		"calc.exe", "ms-msdt:/id", "krate://app", "tel:123"}
	for _, u := range ok {
		if err := checkURL(u); err != nil {
			t.Errorf("%q: %v", u, err)
		}
	}
	for _, u := range bad {
		if err := checkURL(u); err == nil {
			t.Errorf("%q accepted", u)
		}
	}
	AllowScheme("tel")
	defer func() { schemeMu.Lock(); delete(schemes, "tel"); schemeMu.Unlock() }()
	if err := checkURL("tel:123"); err != nil {
		t.Errorf("allowed scheme: %v", err)
	}
}

func TestOpenExternalCallsHandler(t *testing.T) {
	var got []string
	openFunc = func(u string) error { got = append(got, u); return nil }
	defer func() { openFunc = startHandler }()
	if err := OpenExternal(context.Background(), "https://example.com"); err != nil {
		t.Fatal(err)
	}
	if err := OpenExternal(context.Background(), "file:///etc/passwd"); err == nil {
		t.Fatal("file URL opened")
	}
	if len(got) != 1 || got[0] != "https://example.com" {
		t.Fatalf("handler saw %v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := OpenExternal(ctx, "https://example.com"); err == nil {
		t.Fatal("canceled context opened a URL")
	}
}

func str(s string) func(*wire.Encoder) { return func(e *wire.Encoder) { e.String(s) } }

func registry(t *testing.T, perms ...string) (*sdktest.Caller, permission.Set) {
	t.Helper()
	set, err := permission.Parse(perms)
	if err != nil {
		t.Fatal(err)
	}
	reg := scheme.NewRegistry()
	Register(reg, set)
	return sdktest.New(t, reg), set
}

func TestRegisterPermissions(t *testing.T) {
	useApp(t)
	var opened []string
	openFunc = func(u string) error { opened = append(opened, u); return nil }
	defer func() { openFunc = startHandler }()

	// Defaults: os:info yes; openExternal and secrets denied.
	c, _ := registry(t)
	if r := c.Call("krate/os.appDataDir", nil); r.Failed || !strings.Contains(r.OK, "com.acme.test") {
		t.Fatalf("appDataDir: %+v", r)
	}
	if r := c.Call("krate/os.info", nil); r.Failed || !strings.Contains(r.OK, `"appId":"com.acme.test"`) || !strings.Contains(r.OK, `"appVersion":"1.2.3"`) {
		t.Fatalf("info: %+v", r)
	}
	for id, perm := range map[string]string{
		"krate/os.openExternal": "shell:openExternal", "krate/os.secretSet": "os:secrets",
		"krate/os.secretGet": "os:secrets.read", "krate/os.secretDelete": "os:secrets",
	} {
		r := c.Call(id, str("x"))
		if !r.Failed || r.ErrCode != "permission_denied" || r.ErrData != `{"permission":"`+perm+`"}` {
			t.Errorf("%s: %+v", id, r)
		}
	}
	if len(opened) != 0 {
		t.Fatal("denied openExternal opened a URL")
	}

	// With the permission.
	c, _ = registry(t, "shell:openExternal")
	if r := c.Call("krate/os.openExternal", str("https://example.com")); r.Failed || r.OK != "null" {
		t.Fatalf("openExternal: %+v", r)
	}
	if r := c.Call("krate/os.openExternal", str("file:///etc/passwd")); r.ErrCode != "bad_args" {
		t.Fatalf("file URL: %+v", r)
	}
	if r := c.Call("krate/os.openExternal", nil); r.ErrCode != "bad_args" {
		t.Fatalf("missing arg: %+v", r)
	}
	if r := c.Call("krate/os.appDataDir", nil); r.ErrCode != "permission_denied" {
		t.Fatalf("explicit list drops the defaults: %+v", r)
	}
	if len(opened) != 1 {
		t.Fatalf("opened %v", opened)
	}
}

func TestSecretsUnsupportedMapsToCode(t *testing.T) {
	useApp(t)
	saved := backend
	backend = nil
	defer func() { backend = saved }()
	c, _ := registry(t, "os:secrets", "os:secrets.read")
	r := c.Call("krate/os.secretSet", func(e *wire.Encoder) { e.String("k"); e.String("v") })
	if !r.Failed || r.ErrCode != "unsupported" {
		t.Fatalf("%+v", r)
	}
	if _, err := SecretGet(context.Background(), "k"); !errors.Is(err, krate.ErrUnsupported) {
		t.Fatalf("%v", err)
	}
	if err := SecretSet(context.Background(), "", "v"); err == nil {
		t.Fatal("empty key accepted")
	}
}

// memBackend is an in-memory store for the registration tests.
type memBackend map[string]string

func (m memBackend) set(_ context.Context, s, k, v string) error { m[s+"/"+k] = v; return nil }
func (m memBackend) get(_ context.Context, s, k string) (string, error) {
	v, ok := m[s+"/"+k]
	if !ok {
		return "", ErrSecretNotFound
	}
	return v, nil
}
func (m memBackend) remove(_ context.Context, s, k string) error { delete(m, s+"/"+k); return nil }

func TestSecretsRoundTripThroughUI(t *testing.T) {
	useApp(t)
	saved := backend
	backend = memBackend{}
	defer func() { backend = saved }()
	c, _ := registry(t, "os:secrets", "os:secrets.read")
	if r := c.Call("krate/os.secretGet", str("tok")); r.Failed || r.OK != "null" {
		t.Fatalf("missing secret: %+v", r)
	}
	if r := c.Call("krate/os.secretSet", func(e *wire.Encoder) { e.String("tok"); e.String("s3cret") }); r.Failed {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/os.secretGet", str("tok")); r.OK != `"s3cret"` {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/os.secretDelete", str("tok")); r.Failed {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/os.secretGet", str("tok")); r.OK != "null" {
		t.Fatalf("%+v", r)
	}
	// Write-only apps cannot read back.
	c, _ = registry(t, "os:secrets")
	if r := c.Call("krate/os.secretGet", str("tok")); r.ErrCode != "permission_denied" {
		t.Fatalf("%+v", r)
	}
}
