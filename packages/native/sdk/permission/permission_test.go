package permission

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/internal/sdktest"
	"github.com/kratejs/krate/packages/native/wire"
)

func TestDefaults(t *testing.T) {
	for _, list := range [][]string{nil, {}} {
		s, err := Parse(list)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := s.List(), []string{"app", "clipboard", "db", "dialog", "kv", "menu", "notify", "os:info", "window"}; !reflect.DeepEqual(got, want) {
			t.Errorf("Parse(%v) = %v, want %v", list, got, want)
		}
	}
}

func TestParse(t *testing.T) {
	s, err := Parse([]string{"fs", "os:secrets"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := s.List(), []string{"fs", "fs:read", "os:secrets"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if s.Allows("os:secrets.read") {
		t.Error("os:secrets must not imply os:secrets.read")
	}
	if s.Allows("kv") {
		t.Error("an explicit list must not keep the defaults")
	}
	if !s.Allows("") {
		t.Error("the empty permission is always allowed")
	}
	if (Set{}).Allows("kv") {
		t.Error("the zero Set allows something")
	}
	_, err = Parse([]string{"fs", "FS"})
	if err == nil || !strings.Contains(err.Error(), `"FS"`) || !strings.Contains(err.Error(), "known:") {
		t.Errorf("unknown name: %v", err)
	}
	fr, _ := Parse([]string{"fs:read"})
	if fr.Allows("fs") {
		t.Error("fs:read implies fs")
	}
	all := AllowAll()
	for _, p := range All() {
		if !all.Allows(p.Name) {
			t.Errorf("AllowAll lacks %s", p.Name)
		}
	}
}

func TestDeniedShape(t *testing.T) {
	err := Denied("os:secrets")
	var ke *krate.Error
	if !errors.As(err, &ke) || ke.Code != "permission_denied" {
		t.Fatalf("%v", err)
	}
	b, _ := json.Marshal(ke.Data)
	if string(b) != `{"permission":"os:secrets"}` {
		t.Fatalf("%s", b)
	}
}

func TestQueryID(t *testing.T) {
	set, _ := Parse([]string{"kv", "fs:read"})
	reg := scheme.NewRegistry()
	RegisterIn(reg, set)
	c := sdktest.New(t, reg)
	for name, want := range map[string]string{"kv": "true", "fs:read": "true", "fs": "false", "nope": "false"} {
		r := c.Call("krate/permission.query", func(e *wire.Encoder) { e.String(name) })
		if r.OK != want {
			t.Errorf("query(%s) = %+v, want %s", name, r, want)
		}
	}
	if r := c.Call("krate/permission.query", nil); r.ErrCode != "bad_args" {
		t.Errorf("%+v", r)
	}
}

func TestRegisterRequiresTableRow(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("registering an id missing from the table did not panic")
		}
	}()
	Register(scheme.NewRegistry(), AllowAll(), "krate/nope.nothing", nil)
}

func TestDefineTwicePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate Define did not panic")
		}
	}()
	Define(Info{Name: "kv"})
}

// The package documentation carries the permission table; keep it complete.
func TestDocListsEveryPermission(t *testing.T) {
	src, err := os.ReadFile("permission.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range All() {
		if !strings.Contains(string(src), "//\t"+p.Name+" ") {
			t.Errorf("the package comment does not list %q", p.Name)
		}
	}
}

var update = flag.Bool("update", false, "rewrite names.json")

// names.json is the copy of the permission names the compiler validates
// against (packages/compiler/internal/config). It must match the table.
func TestNamesJSONMatchesTable(t *testing.T) {
	var out struct {
		Names    []string `json:"names"`
		Defaults []string `json:"defaults"`
	}
	for _, i := range All() {
		out.Names = append(out.Names, i.Name)
	}
	out.Defaults = Defaults().List()
	want, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	if *update {
		if err := os.WriteFile("names.json", want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile("names.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("names.json is stale; run: go test ./sdk/permission -run NamesJSON -update\nhave:\n%s\nwant:\n%s", got, want)
	}
}

func TestUIDefaultsAndDenials(t *testing.T) {
	d := Defaults()
	for _, n := range []string{"window", "app", "menu", "dialog", "notify", "clipboard"} {
		if !d.Allows(n) {
			t.Errorf("default set lacks %s", n)
		}
	}
	for _, n := range []string{"clipboard:read", "tray", "shortcut", "fs", "fs:read", "os:secrets", "os:secrets.read", "shell:openExternal", "net"} {
		if d.Allows(n) {
			t.Errorf("default set allows %s", n)
		}
	}
}
