package build

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/native/hostts"
)

func TestPlanReleaseHostChildren(t *testing.T) {
	app := t.TempDir()
	for _, f := range []string{"host/node/host.mjs", "host/host.js", "host/host.js.map"} {
		p := filepath.Join(app, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ar := &NativeAppResult{AppDir: app}
	ar.Manifest.HostJS = &NativeHostJSMeta{Bundle: "host/host.js"}
	ar.Manifest.HostChildren = []NativeHostChildMeta{{
		Runtime: "node", Entry: "host/node/host.mjs", Map: "host/node/host.mjs.map",
		Command: []string{"node", "{entry}"}, Funcs: []string{"a.f"},
	}}
	ar.HostGroups = []hostts.Group{
		{Runtime: "node", Bundle: "/b/node.mjs"},
		{Runtime: "quickjs", Registered: []hostts.Registration{{ID: "s.g"}}},
		{Runtime: "scriptc", Bundle: "/b/scriptc.mjs", Funcs: []string{"s.g"}, Registered: []hostts.Registration{{ID: "s.g"}}},
	}
	plan, err := planReleaseHostChildren(ar)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range plan {
		names = append(names, string(p.rt)+":"+p.name+":"+p.bundle)
	}
	want := []string{"node:krate-host-node:/b/node.mjs", "scriptc:krate-host-scriptc:/b/scriptc.mjs"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("plan = %v, want %v", names, want)
	}
	cs := ar.Manifest.HostChildren
	if len(cs) != 2 || cs[0].Entry != "host/krate-host-node" || cs[0].Command != nil || cs[0].Map != "" || cs[0].Funcs[0] != "a.f" ||
		cs[1].Runtime != "scriptc" || cs[1].Entry != "host/krate-host-scriptc" || cs[1].Funcs[0] != "s.g" {
		t.Errorf("children = %+v", cs)
	}
	// The script copy is gone, and the QuickJS stand-in of the scriptc ids too.
	if _, err := os.Stat(filepath.Join(app, "host", "node")); err == nil {
		t.Error("the node script directory is still in the stage")
	}
	if ar.Manifest.HostJS != nil {
		t.Error("hostjs still set although the scriptc child serves its ids")
	}
	if _, err := os.Stat(filepath.Join(app, "host", "host.js")); err == nil {
		t.Error("host.js is still in the stage")
	}
}
