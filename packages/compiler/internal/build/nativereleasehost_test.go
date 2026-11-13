package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/native/hostts"
	"github.com/kratejs/krate/packages/native/wire"
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

// A release build compiles the host children into host/ next to the
// executable, and the embedded manifest starts them from there. The child
// reaches the SDK (kv) through the shell's parent socket.
func TestNativeReleaseBunHostChild(t *testing.T) {
	requireRelease(t)
	if _, err := exec.LookPath("bun"); err != nil {
		requireE2E(t, "bun is not installed")
	}
	ex := e2eRequireDeps(t)
	cfg, err := config.Load(ex)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Native == nil {
		t.Fatal("the example has no native config")
	}
	cfg.Native.Host.Runtime = "bun"
	out := filepath.Join(t.TempDir(), "dist-native")
	res, err := New(ex, cfg).BuildNativeRelease(NativeReleaseOptions{OutDir: out})
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	bin := res.Binaries[0].Path
	// bun build --compile writes krate-host-bun.exe on Windows; the manifest
	// keeps the bare name and the shell adds the suffix when it starts it.
	childName := "krate-host-bun"
	if runtime.GOOS == "windows" {
		childName += ".exe"
	}
	child := filepath.Join(filepath.Dir(bin), "host", childName)
	if st, err := os.Stat(child); err != nil || st.Size() < 1<<20 {
		t.Fatalf("the compiled bun host child is missing or tiny: %v", err)
	}
	if len(res.Report.Targets) != 1 || len(res.Report.Targets[0].Host) != 1 || res.Report.Targets[0].Host[0] != "linux-"+runtime.GOARCH+"/host/krate-host-bun" && runtime.GOOS == "linux" {
		t.Errorf("report targets = %+v", res.Report.Targets)
	}
	if len(res.Manifest.HostChildren) != 1 || res.Manifest.HostChildren[0].Command != nil || res.Manifest.HostChildren[0].Entry != "host/krate-host-bun" {
		t.Fatalf("host children = %+v", res.Manifest.HostChildren)
	}

	app := runReleaseBinary(t, bin)
	if got := app.call(t, "src/lib/notes.host.addNote", func(e *wire.Encoder) { e.String("from bun") }); !strings.Contains(got, `"ok"`) {
		t.Fatalf("addNote: %s", got)
	}
	if got := app.call(t, "src/lib/notes.host.lastNote", nil); !strings.Contains(got, "from bun") {
		t.Fatalf("lastNote (kv through the parent socket): %s", got)
	}
}
