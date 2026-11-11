package all

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk"
	"github.com/kratejs/krate/packages/native/sdk/internal/fakeplat"
	"github.com/kratejs/krate/packages/native/sdk/internal/sdktest"
	sdkos "github.com/kratejs/krate/packages/native/sdk/os"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/wire"
)

func isolate(t *testing.T) {
	t.Helper()
	home := t.TempDir()
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
		if strings.HasSuffix(id.Name, ".events") {
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

func TestDefaultsAndDenyDefaults(t *testing.T) {
	isolate(t)
	reg := scheme.NewRegistry()
	set, err := Register(reg, Options{AppID: "com.acme.defaults", Version: "3"})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"os:info", "kv", "db"} {
		if !set.Allows(p) {
			t.Errorf("default set lacks %s", p)
		}
	}
	for _, p := range []string{"fs", "fs:read", "os:secrets", "os:secrets.read", "net", "shell:openExternal"} {
		if set.Allows(p) {
			t.Errorf("default set allows %s", p)
		}
	}
	c := sdktest.New(t, reg)
	if r := c.Call("krate/os.info", nil); r.Failed || !strings.Contains(r.OK, `"appVersion":"3"`) {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/permission.query", func(e *wire.Encoder) { e.String("fs") }); r.OK != "false" {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/permission.query", func(e *wire.Encoder) { e.String("kv") }); r.OK != "true" {
		t.Fatalf("%+v", r)
	}
	// Truly unknown ids stay not_found.
	if r := c.Call("krate/os.nope", nil); r.ErrCode != "not_found" {
		t.Fatalf("%+v", r)
	}
	if _, err := Register(scheme.NewRegistry(), Options{AppID: "x", Permissions: []string{"bogus"}}); err == nil {
		t.Fatal("unknown permission accepted")
	}
	if _, err := Register(scheme.NewRegistry(), Options{AppID: "../x"}); err == nil {
		t.Fatal("unsafe appId accepted")
	}
}

func TestCapabilities(t *testing.T) {
	for _, c := range []string{"os.info", "os.secrets", "os.openExternal", "fs", "db", "kv", "net"} {
		found := false
		for _, n := range krate.Capabilities() {
			found = found || n == c
		}
		if !found {
			t.Errorf("capability %q is not registered", c)
		}
	}
	for _, c := range []string{"os.info", "fs", "db", "kv", "net"} {
		if !krate.Supports(c) {
			t.Errorf("Supports(%q) = false", c)
		}
	}
	if krate.Supports("no.such.capability") {
		t.Error("an unknown capability is supported")
	}
	_ = sdkos.ErrSecretNotFound
}

// calledByName reports whether a wrapper builds the id from a template such
// as `krate/window.${name}` and passes the operation name as a string, which
// is how the window methods share one helper.
func calledByName(src, id string) bool {
	area, op, _ := strings.Cut(strings.TrimPrefix(id, "krate/"), ".")
	return strings.Contains(src, "`krate/"+area+".${") && strings.Contains(src, `"`+op+`"`)
}

var idRe = regexp.MustCompile(`krate/[a-zA-Z]+\.[a-zA-Z]+`)

// The TypeScript wrappers and the docs must mention exactly the ids in the
// table.
func TestTSWrappersAndDocsMatchTable(t *testing.T) {
	root := filepath.Join("..", "..", "..", "app", "src")
	if _, err := os.Stat(root); err != nil {
		t.Skip("packages/app is not checked out")
	}
	inTable := map[string]bool{}
	byFile := map[string][]string{}
	for _, id := range sdk.IDs {
		inTable[id.Name] = true
		byFile[id.TS] = append(byFile[id.TS], id.Name)
	}
	for file, ids := range byFile {
		b, err := os.ReadFile(filepath.Join(root, file+".ts"))
		if err != nil {
			t.Errorf("wrapper %s.ts: %v", file, err)
			continue
		}
		src := string(b)
		for _, id := range ids {
			if !strings.Contains(src, `"`+id+`"`) && !calledByName(src, id) {
				t.Errorf("%s.ts does not call %s", file, id)
			}
		}
		for _, id := range idRe.FindAllString(src, -1) {
			if !inTable[id] {
				t.Errorf("%s.ts mentions %s, which is not in sdk/ids.go", file, id)
			}
		}
	}
	// Every app/src file that mentions a krate/ id other than the SDK's own
	// files is a wrapper without a table row.
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".ts")
		if _, ok := byFile[name]; ok || !strings.HasSuffix(e.Name(), ".ts") {
			continue
		}
		b, _ := os.ReadFile(filepath.Join(root, e.Name()))
		for _, id := range idRe.FindAllString(string(b), -1) {
			if !inTable[id] && !strings.HasPrefix(id, "krate/runtime") {
				t.Errorf("%s mentions %s, which is not in sdk/ids.go", e.Name(), id)
			}
		}
	}

	docDir := filepath.Join("..", "..", "..", "web", "src", "content", "docs", "native")
	b, err := os.ReadFile(filepath.Join(docDir, "sdk.md"))
	if err != nil {
		t.Skip("docs page missing: ", err)
	}
	if ui, err := os.ReadFile(filepath.Join(docDir, "sdk-ui.md")); err == nil {
		b = append(b, ui...)
	}
	var missing []string
	for _, id := range sdk.IDs {
		if !strings.Contains(string(b), "`"+id.Name+"`") {
			missing = append(missing, id.Name)
		}
	}
	for _, p := range permission.All() {
		if !strings.Contains(string(b), "`"+p.Name+"`") {
			missing = append(missing, "permission "+p.Name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("sdk.md and sdk-ui.md do not mention: %v", missing)
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
