package all

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk"
	"github.com/kratejs/krate/packages/native/sdk/db"
	"github.com/kratejs/krate/packages/native/sdk/internal/fakeplat"
	"github.com/kratejs/krate/packages/native/sdk/internal/sdktest"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/wire"
)

func isolate(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	// Windows cannot remove an open database file; this runs before the
	// TempDir cleanup.
	t.Cleanup(func() { _ = db.CloseDefault() })
	for _, k := range []string{"XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR"} {
		t.Setenv(k, "")
	}
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", filepath.Join(home, "r"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "l"))
}

// A call to any id in the table never reports not_found, whatever the
// permissions: it either runs or fails with permission_denied naming the
// permission the table lists.
func TestEveryIDIsRegisteredAndGated(t *testing.T) {
	isolate(t)
	if _, err := Register(scheme.NewRegistry(), Options{AppID: "com.acme.all"}); err != nil {
		t.Fatal(err)
	}
	// "net" grants nothing that gates an id, so every gated id is denied.
	reg := scheme.NewRegistry()
	set, err := permission.Parse([]string{"net"})
	if err != nil {
		t.Fatal(err)
	}
	RegisterAreas(reg, set)
	// The UI half, served by a Host on a fake platform, under the same set.
	host := sdk.NewHost()
	host.Attach(fakeplat.New())
	t.Cleanup(host.Close)
	RegisterUI(reg, host, set, nil)
	c := sdktest.New(t, reg)
	for _, id := range sdk.IDs {
		r := c.Call(id.Name, func(e *wire.Encoder) { e.String("x") })
		if strings.HasSuffix(id.Name, ".events") || strings.Contains(id.Args, "(stream)") {
			// A stream opens first and fails when read; sdk_test checks that.
			continue
		}
		if r.ErrCode == "not_found" {
			t.Errorf("%s is in the id table but not registered", id.Name)
			continue
		}
		if id.Permission == "" {
			if r.ErrCode == "permission_denied" {
				t.Errorf("%s has no permission but was denied", id.Name)
			}
			continue
		}
		want := `{"permission":"` + id.Permission + `"}`
		if r.ErrCode != "permission_denied" || r.ErrData != want {
			t.Errorf("%s: want permission_denied %s, got %+v", id.Name, want, r)
		}
	}
}

// Host code is trusted: an in-process call through Invoke reaches an id whose
// permission the app does not hold, while the UI's call to it is denied.
func TestInProcessInvokeBypassesPermissionGate(t *testing.T) {
	isolate(t)
	reg := scheme.NewRegistry()
	if _, err := Register(reg, Options{AppID: "com.acme.trusted", Permissions: []string{"net"}}); err != nil {
		t.Fatal(err)
	}
	c := sdktest.New(t, reg)
	set := func(e *wire.Encoder) { e.String("k"); e.String("v") }
	if r := c.Call("krate/kv.set", set); r.ErrCode != "permission_denied" {
		t.Fatalf("the UI call must be denied, got %+v", r)
	}
	frame := wire.NewEncoder()
	frame.Raw([]byte(`["k","v"]`))
	if _, err := reg.Invoke(krate.Background(), "krate/kv.set", frame.Frame()); err != nil {
		t.Fatalf("host code call: %v", err)
	}
	get := wire.NewEncoder()
	get.Raw([]byte(`["k"]`))
	fr, err := reg.Invoke(krate.Background(), "krate/kv.get", get.Frame())
	if err != nil {
		t.Fatal(err)
	}
	d, _ := wire.NewDecoder(fr)
	if s, err := d.String(); err != nil || s != "v" {
		t.Fatalf("%q %v", s, err)
	}
}
