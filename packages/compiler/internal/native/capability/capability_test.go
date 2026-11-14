package capability

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/native"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", ".."))
}

// TestIDsJSONMatchesSDK keeps the compiler's copy of the id table in step with
// packages/native/sdk/ids.json.
func TestIDsJSONMatchesSDK(t *testing.T) {
	want, err := os.ReadFile(filepath.Join(repoRoot(t), "packages", "native", "sdk", "ids.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, idsJSON) {
		t.Fatal("ids.json is stale; run: cp packages/native/sdk/ids.json packages/compiler/internal/native/capability/ids.json")
	}
	if len(Rows()) < 70 {
		t.Fatalf("only %d rows", len(Rows()))
	}
}

func project(t *testing.T, files map[string]string) (string, *config.Config, *native.Placement) {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.Resolve(root)
	p, err := native.Analyze(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return root, cfg, p
}

func analyze(t *testing.T, files map[string]string) *Result {
	t.Helper()
	root, cfg, p := project(t, files)
	res, err := Analyze(root, cfg, p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func sitesOf(r *Result, perm string) []Site { return r.Needs[perm] }

func TestNamespaceEscapesCountAsWholeModule(t *testing.T) {
	res := analyze(t, map[string]string{
		"src/pages/index.tsx": `import * as clip from "@krate/app/clipboard";
use(clip);
export default function Page() { return <div/>; }
`,
	})
	if !reflect.DeepEqual(res.Permissions, []string{"clipboard", "clipboard:read"}) {
		t.Fatalf("permissions = %v", res.Permissions)
	}
}

func TestHostModuleImportsAndLiteralIDs(t *testing.T) {
	res := analyze(t, map[string]string{
		"src/pages/index.tsx": `export default function Page() { return <div/>; }`,
		"src/lib/notes.host.ts": `import { secretGet } from "@krate/app/os";
export async function token() { return secretGet("t"); }
`,
		"src/lib/raw.ts": `import { call } from "@krate/app";
export const x = () => call("krate/tray.Set", []);
export const y = (op: string) => call("krate/shortcut." + op, []);
`,
	})
	want := []string{"os:secrets.read", "shortcut", "tray"}
	if !reflect.DeepEqual(res.Permissions, want) {
		t.Fatalf("permissions = %v, want %v", res.Permissions, want)
	}
	if s := sitesOf(res, "os:secrets.read"); len(s) != 1 || s[0].Kind != "host" || s[0].File != "src/lib/notes.host.ts" {
		t.Fatalf("host site = %+v", s)
	}
	if s := sitesOf(res, "tray"); len(s) != 1 || s[0].Line != 2 || s[0].Symbol != "krate/tray.Set" {
		t.Fatalf("literal site = %+v", s)
	}
}

func TestRequireLinksAnAreaTheCodeNeverReaches(t *testing.T) {
	r := &Result{UIAreas: []string{"kv", "os"}}
	if strings.Contains(strings.Join(r.Tags(false), ","), "krate_no_app") == false {
		t.Fatal("app should be shaken without Require")
	}
	r.Require("app")
	r.Require("app")
	if !reflect.DeepEqual(r.UIAreas, []string{"app", "kv", "os"}) {
		t.Fatalf("areas = %v", r.UIAreas)
	}
	if strings.Contains(strings.Join(r.Tags(false), ","), "krate_no_app") {
		t.Error("the app area is still shaken out")
	}
	if len(r.Permissions) != 0 {
		t.Errorf("Require must not grant permissions: %v", r.Permissions)
	}
}

var testPlugins = []PluginInfo{
	{Name: "sys", Ops: []PluginOp{
		{Name: "info", Permission: "plugin:sys.read"},
		{Name: "memory", Permission: "plugin:sys.read"},
		{Name: "watch", Permission: "plugin:sys.monitor"},
		{Name: "free"},
	}},
	{Name: "other", Ops: []PluginOp{{Name: "run", Permission: "plugin:other.run"}}},
}

func analyzePlugins(t *testing.T, files map[string]string) *Result {
	t.Helper()
	root, cfg, p := project(t, files)
	res, err := Analyze(root, cfg, p, Options{Plugins: testPlugins})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestPluginNamedImports(t *testing.T) {
	res := analyzePlugins(t, map[string]string{"src/pages/index.tsx": `import { info, watch as w } from "krate/plugin/sys";
export default function P() { return <b onClick={() => { info(); w(); }}>x</b>; }
`})
	if got := strings.Join(res.Permissions, ","); got != "plugin:sys.monitor,plugin:sys.read" {
		t.Errorf("permissions = %s", got)
	}
	if got := strings.Join(res.PluginOps["sys"], ","); got != "info,watch" {
		t.Errorf("ops = %v", res.PluginOps)
	}
	if _, ok := res.PluginOps["other"]; ok {
		t.Error("an unused plugin is reported as used")
	}
	site := res.Needs["plugin:sys.read"][0]
	if site.File != "src/pages/index.tsx" || !strings.Contains(site.Symbol, "info from krate/plugin/sys") {
		t.Errorf("site = %+v", site)
	}
	// Plugin ops are not SDK areas.
	for _, a := range res.UIAreas {
		if strings.HasPrefix(a, "plugin:") {
			t.Errorf("area %q", a)
		}
	}
}
