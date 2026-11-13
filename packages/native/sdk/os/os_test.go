package sdkos

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/internal/sdktest"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/wire"
)

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
